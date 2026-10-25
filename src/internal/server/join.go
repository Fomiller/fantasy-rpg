package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const (
	outboxSize  = 64
	joinTimeout = 5 * time.Second
	// leaseRetry is how often a lobby held by a run checks if the run ended.
	leaseRetry = 2 * time.Second
	// closeGrace is how long a server waits for the client to close after a
	// ticket or a run's end before closing itself. Closing at once can drop
	// that last message: Godot empties its read buffer when the close lands.
	closeGrace = 2 * time.Second
)

// Lobby is the world server: join, character load and create, the town,
// stash moves, and handing players to an instance. Its connection and store
// write handling is shared with the instance server's Server.
type Lobby struct {
	// Store keeps characters and stashes by player name. Set it before
	// serving. Only connection and writer goroutines call it.
	Store inventory.Store
	// TickRate is sent to clients on join. It must match the tick loop.
	TickRate int
	// TownMaxPlayers caps the players walking the town. Players in a run do
	// not count.
	TownMaxPlayers int
	// rules answers class, gear and stat questions. On the world server it is
	// the town, which Step runs. On an instance server it is the cave.
	rules *sim.World
	// town is empty on an instance server.
	town caves.Cave
	// inTown has every player with a body in the town.
	inTown     map[sim.EntityID]*client
	townSpawns int
	loot       *items.Roller
	log        *slog.Logger
	inbox      chan func()
	done       chan struct{}
	stop       sync.Once
	// conns has every connected player by name, until their last write lands.
	conns map[string]*client
	// Instances is where the town's entrance sends players. With none, every
	// instance is full.
	Instances Pool
	// JoinKey signs the tokens instance servers check.
	JoinKey []byte
	// Events gets player.session from the world and instance.join from an
	// instance. Set it before serving.
	Events events.Producer
	// WorldID names this world server in player.session.
	WorldID string

	// released runs on the loop once a player's last write lands.
	released func(*client)

	// PendingSaves counts run ends that are past their patience and still
	// retrying. Register it to export it.
	PendingSaves prometheus.Gauge
	// Tests shorten these.
	endRunPatience, pendingRetry, leaseRetry, closeGrace time.Duration
}

type client struct {
	name   string
	class  string
	out    chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	writes *writer
	// kick closes the connection once the outbox is written.
	kick chan closeMsg
	// gone is set once the connection has closed.
	gone bool
	// id is the player's entity in the town or the cave, or 0 with none.
	id sim.EntityID
	// lobby is the player's items while they are in the town. It is the
	// source of truth there, and the town's player only mirrors it.
	lobby *lobby
	// atEntrance is set while the player stands in the town's entrance, so
	// only the step in sends them to a run.
	atEntrance bool
	// entering is set while the player is on their way into a run: behind
	// their lobby saves on the world server, taking the lease on an instance.
	entering bool
	// inRun is set while a run elsewhere holds the account.
	inRun bool
	// applied wakes waitRun when inventory has applied a run for the account.
	applied chan struct{}
	// runID and stash are set from the join token and the lease, so the run
	// end needs no read.
	runID string
	stash []items.Instance
	// pending is the run end not in the store yet.
	pending *inventory.Character
	// saved is the lobby as the store has it once queued writes land. A failed
	// lobby save goes back to it. The seqs order lobby saves.
	saved              inventory.Character
	lobbySeq, savedSeq int
}

// NewLobby makes a world server with town as its town, and no instances to
// send anyone to.
func NewLobby(town caves.Cave, cfg sim.Config, loot *items.Roller, log *slog.Logger) *Lobby {
	s := newShared(town.NewWorld(cfg, nil), loot, log)
	s.town = town
	return s
}

func newShared(rules *sim.World, loot *items.Roller, log *slog.Logger) *Lobby {
	return &Lobby{
		Store:          inventory.NewMemory(),
		TickRate:       20,
		TownMaxPlayers: 32,
		rules:          rules,
		inTown:         map[sim.EntityID]*client{},
		loot:           loot,
		log:            log,
		inbox:          make(chan func(), 1024),
		done:           make(chan struct{}),
		conns:          map[string]*client{},
		Events:         events.Nop{},

		PendingSaves: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "store_pending_saves",
			Help: "Run end saves that failed for longer than their patience and are still retrying.",
		}),
		endRunPatience: endRunPatience,
		pendingRetry:   pendingRetry,
		leaseRetry:     leaseRetry,
		closeGrace:     closeGrace,
	}
}

// Step runs the town on the tick loop goroutine, the one goroutine that
// touches lobby state.
func (s *Lobby) Step(dt time.Duration) {
	s.drain()
	s.rules.Step(dt)
	s.checkEntrance()
	if len(s.inTown) == 0 {
		return
	}
	b, err := protocol.EncodeServer(protocol.Snapshot(s.rules))
	if err != nil {
		s.log.Error("encode town snapshot", "err", err)
		return
	}
	for _, c := range s.inTown {
		c.send(b)
	}
}

// Close stops accepting work. Call it once the loop has stopped, so
// connections waiting on the inbox give up instead of blocking forever.
func (s *Lobby) Close() {
	s.stop.Do(func() { close(s.done) })
}

func (s *Lobby) drain() {
	for {
		select {
		case f := <-s.inbox:
			f()
		default:
			return
		}
	}
}

func (s *Lobby) enqueue(f func()) bool {
	select {
	case s.inbox <- f:
		return true
	case <-s.done:
		return false
	}
}

func (s *Lobby) sendToClient(c *client, msg *gamev1.ServerMessage) {
	if b, err := protocol.EncodeServer(msg); err == nil {
		c.send(b)
	}
}

type closeMsg struct {
	code   websocket.StatusCode
	reason string
}

func newClient(name, class string, ctx context.Context, cancel context.CancelFunc) *client {
	return &client{name: name, class: class, out: make(chan []byte, outboxSize), kick: make(chan closeMsg, 1), applied: make(chan struct{}, 1), ctx: ctx, cancel: cancel, writes: newWriter()}
}

// close sends what is queued, then closes with reason. Only the first call counts.
func (c *client) close(code websocket.StatusCode, reason string) {
	select {
	case c.kick <- closeMsg{code, reason}:
	default:
	}
}

// send never blocks the loop. A client too slow to drain its outbox is
// dropped rather than holding up everyone else.
func (c *client) send(b []byte) {
	select {
	case c.out <- b:
	default:
		c.cancel()
	}
}

func (s *Lobby) starterKit() inventory.Loadout {
	var l inventory.Loadout
	for _, def := range items.StarterKit {
		inst, err := s.loot.Make(def, 1, items.Common)
		if err != nil {
			s.log.Error("starter kit", "item", def, "err", err)
			continue
		}
		l.Equipment = append(l.Equipment, inst)
	}
	return l
}

func (s *Lobby) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.log.Warn("websocket accept", "err", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	join, err := readJoin(ctx, conn)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	name := join.GetName()
	class := join.GetClassId()
	returning := join.GetReturning()
	if class == "" {
		class = classes.Starter
	}
	c := newClient(name, class, ctx, cancel)
	claimed := make(chan error, 1)
	ok := s.enqueue(func() {
		// One connection per name, or two copies of a player could load and
		// save the same items.
		if _, ok := s.conns[name]; ok {
			claimed <- errors.New("name already playing")
			return
		}
		if _, ok := s.rules.Classes().Classes[class]; !ok {
			claimed <- fmt.Errorf("unknown class %q", class)
			return
		}
		s.conns[name] = c
		claimed <- nil
	})
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	// Wait even if the client goes, or a late claim would hold the name forever.
	select {
	case err = <-claimed:
	case <-s.done:
		return
	}
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	log := s.log.With("name", name)
	go s.saveLoop(name, c.writes)
	defer func() {
		s.enqueue(func() { s.leave(c) })
		log.Info("player left")
	}()

	ch, err := s.loadCharacter(ctx, name, class)
	if err != nil {
		log.Error("load character", "err", err)
		// Never fall back to a starter kit, which would later save over real gear.
		conn.Close(websocket.StatusTryAgainLater, "characters unavailable")
		return
	}
	if s.deadLease(ctx, ch.Run) {
		log.Warn("clearing a dead run lease", "run", ch.Run.RunID, "instance", ch.Run.Instance)
		s.clearRun(c, ch.Run.RunID)
		ch.Run = inventory.Lease{}
	}
	if !s.enqueue(func() { s.enterTown(c, ch, returning) }) {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	log.Info("player joined")

	go s.writeLoop(ctx, conn, c, cancel)
	s.readLoop(ctx, conn, log, func(msg *gamev1.ClientMessage) { s.handle(c, msg) })
}

// readLoop hands each message to handle on the loop until the connection ends.
func (s *Lobby) readLoop(ctx context.Context, conn *websocket.Conn, log *slog.Logger, handle func(*gamev1.ClientMessage)) {
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure && status != websocket.StatusGoingAway && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				log.Info("read stopped", "err", err)
			}
			return
		}
		msg, err := protocol.DecodeClient(b)
		if err != nil {
			conn.Close(websocket.StatusPolicyViolation, err.Error())
			return
		}
		if !s.enqueue(func() { handle(msg) }) {
			return
		}
	}
}

// leave runs on the loop once the connection is gone.
func (s *Lobby) leave(c *client) {
	s.leaveTown(c)
	c.gone = true
	c.writes.close()
}

// handle runs on the loop, which is the only place c.lobby changes. Only
// moves reach the town's sim. Attacks, pickups, loot windows, drops and
// potions are dropped here, so nothing in the town can fight or make loot.
// Equip and unequip are lobby moves.
func (s *Lobby) handle(c *client, msg *gamev1.ClientMessage) {
	switch p := msg.GetPayload().(type) {
	case *gamev1.ClientMessage_Move:
		if cmd, ok := protocol.Command(msg, c.id); ok && c.id != 0 {
			s.rules.Submit(cmd)
		}
	case *gamev1.ClientMessage_StashMove:
		s.moveItem(c, p.StashMove.GetItemId(), p.StashMove.GetTo())
	case *gamev1.ClientMessage_Equip:
		s.moveItem(c, p.Equip.GetItemId(), gamev1.ItemPlace_ITEM_PLACE_GEAR)
	case *gamev1.ClientMessage_Unequip:
		id := ""
		if c.lobby != nil {
			id = c.lobby.gear[items.Slot(p.Unequip.GetSlot())].ID
		}
		s.moveItem(c, id, gamev1.ItemPlace_ITEM_PLACE_BAG)
	}
}

// setInRun marks a lobby held by a run elsewhere, and starts checking for
// the run to end.
func (s *Lobby) setInRun(c *client, held bool) {
	if held && !c.inRun {
		go s.waitRun(c)
	}
	c.inRun = held
}

// runApplied is inventory.applied for account. A lobby waiting on a run
// checks the store at once instead of on its next poll, which stays as the
// fallback for when Redpanda is down.
func (s *Lobby) runApplied(account string) {
	s.enqueue(func() {
		if c, ok := s.conns[account]; ok {
			select {
			case c.applied <- struct{}{}:
			default:
			}
		}
	})
}

// waitRun reloads the character once the lease is gone, so the lobby opens
// on what the run banked. A lease whose run is dead is cleared here too.
func (s *Lobby) waitRun(c *client) {
	tick := time.NewTicker(s.leaseRetry)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
		case <-c.applied:
		}
		ctx, cancel := context.WithTimeout(c.ctx, storeTimeout)
		ch, ok, err := s.Store.Load(ctx, c.name, c.class)
		cancel()
		if err != nil || !ok {
			continue
		}
		if s.deadLease(c.ctx, ch.Run) {
			s.log.Warn("clearing a dead run lease", "name", c.name, "run", ch.Run.RunID, "instance", ch.Run.Instance)
			s.clearRun(c, ch.Run.RunID)
			continue
		}
		if ch.Run.Held() {
			continue
		}
		s.enqueue(func() {
			if !c.gone && c.inRun {
				s.openLobby(c, ch)
			}
		})
		return
	}
}

func (s *Lobby) writeLoop(ctx context.Context, conn *websocket.Conn, c *client, cancel context.CancelFunc) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.out:
			if err := conn.Write(ctx, websocket.MessageBinary, b); err != nil {
				return
			}
		case k := <-c.kick:
			// Everything sent before the close is already in the outbox, and
			// this is its only reader.
			for len(c.out) > 0 {
				if err := conn.Write(ctx, websocket.MessageBinary, <-c.out); err != nil {
					return
				}
			}
			conn.Close(k.code, k.reason)
			return
		}
	}
}

func readJoin(ctx context.Context, conn *websocket.Conn) (*gamev1.JoinRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, joinTimeout)
	defer cancel()
	_, b, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	msg, err := protocol.DecodeClient(b)
	if err != nil {
		return nil, err
	}
	join := msg.GetJoin()
	if join == nil {
		return nil, errors.New("first message must be join")
	}
	if join.GetName() == "" {
		return nil, errors.New("join needs a name")
	}
	return join, nil
}
