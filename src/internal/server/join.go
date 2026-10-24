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
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
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
)

// Lobby is the lobby half of a game server: join, character load and
// create, the lobby, stash moves, and each player's store writes. The world
// server is a Lobby on its own. An instance server's Server adds a run.
type Lobby struct {
	// Store keeps characters and stashes by player name. Set it before
	// serving. Only connection and writer goroutines call it.
	Store inventory.Store
	// rules answers class, gear and stat questions. The lobby never steps it.
	rules *sim.World
	loot  *items.Roller
	log   *slog.Logger
	inbox chan func()
	done  chan struct{}
	stop  sync.Once
	// conns has every connected player by name, until their last write lands.
	conns map[string]*client
	// run is nil on the world server, which has no way into a run yet.
	run run

	// PendingSaves counts run ends that are past their patience and still
	// retrying. Register it to export it.
	PendingSaves prometheus.Gauge
	// Tests shorten these.
	endRunPatience, pendingRetry, leaseRetry time.Duration
}

// run is what an instance server adds to the lobby. Every call is on the
// goroutine that owns the lobby.
type run interface {
	enter(c *client)
	command(c *client, msg *gamev1.ClientMessage)
	leave(c *client)
	// staleLease reports a lease this server knows is dead.
	staleLease(l inventory.Lease) bool
}

type client struct {
	name   string
	class  string
	out    chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	writes *writer
	// gone is set once the connection has closed.
	gone bool
	// id is the player's entity in the cave. In the lobby it is 0 and lobby is set.
	id    sim.EntityID
	lobby *lobby
	// entering is set while the run lease is being taken.
	entering bool
	// inRun is set while a run elsewhere holds the account.
	inRun bool
	// runID and stash are set from cave entry, so the run end needs no read.
	runID string
	stash []items.Instance
	// pending is the run end not in the store yet.
	pending *inventory.Character
	// saved is the lobby as the store has it once queued writes land. A failed
	// lobby save goes back to it. The seqs order lobby saves.
	saved              inventory.Character
	lobbySeq, savedSeq int
}

// NewLobby makes a lobby with no run, as the world server runs it.
func NewLobby(rules *sim.World, loot *items.Roller, log *slog.Logger) *Lobby {
	return &Lobby{
		Store: inventory.NewMemory(),
		rules: rules,
		loot:  loot,
		log:   log,
		inbox: make(chan func(), 1024),
		done:  make(chan struct{}),
		conns: map[string]*client{},

		PendingSaves: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "store_pending_saves",
			Help: "Run end saves that failed for longer than their patience and are still retrying.",
		}),
		endRunPatience: endRunPatience,
		pendingRetry:   pendingRetry,
		leaseRetry:     leaseRetry,
	}
}

// Run does the lobby's work until ctx ends. The world server has no tick
// loop, so this is the one goroutine that touches lobby state.
func (s *Lobby) Run(ctx context.Context) {
	for {
		select {
		case f := <-s.inbox:
			f()
		case <-ctx.Done():
			return
		}
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
	if class == "" {
		class = classes.Starter
	}
	c := &client{name: name, class: class, out: make(chan []byte, outboxSize), ctx: ctx, cancel: cancel, writes: newWriter()}
	claimed := make(chan error, 1)
	// resume is set when the join takes over a run end that is still saving.
	var resume *inventory.Character
	ok := s.enqueue(func() {
		// One connection per name, or two copies of a player could load and
		// save the same items.
		if old, ok := s.conns[name]; ok {
			if old.pending == nil || old.class != class || !old.writes.reopen() {
				claimed <- errors.New("name already playing")
				return
			}
			c.writes, c.pending = old.writes, old.pending
			s.conns[name] = c
			ch := cloneCharacter(*c.pending)
			resume = &ch
			claimed <- nil
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
	if resume == nil {
		go s.saveLoop(name, c.writes)
	}
	defer func() {
		s.enqueue(func() { s.leave(c) })
		log.Info("player left")
	}()

	// A pending save is newer than the store, so it is what the lobby opens from.
	var ch inventory.Character
	if resume != nil {
		ch = *resume
	} else {
		ch, err = s.loadCharacter(ctx, name, class)
	}
	if err != nil {
		log.Error("load character", "err", err)
		// Never fall back to a starter kit, which would later save over real gear.
		conn.Close(websocket.StatusTryAgainLater, "characters unavailable")
		return
	}
	if ch.Run.Held() && s.run != nil && s.run.staleLease(ch.Run) {
		log.Warn("clearing a dead run lease", "run", ch.Run.RunID, "instance", ch.Run.Instance)
		s.clearRun(c, ch.Run.RunID)
		ch.Run = inventory.Lease{}
	}
	if !s.enqueue(func() { s.openLobby(c, ch) }) {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	log.Info("player joined")

	go s.writeLoop(ctx, conn, c, cancel)

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
		if !s.enqueue(func() { s.handle(c, msg) }) {
			return
		}
	}
}

// leave runs on the loop once the connection is gone.
func (s *Lobby) leave(c *client) {
	c.gone = true
	if s.run != nil {
		s.run.leave(c)
	}
	c.writes.close()
}

// handle runs on the loop, which is the only place c.id and c.lobby change.
func (s *Lobby) handle(c *client, msg *gamev1.ClientMessage) {
	switch {
	case msg.GetEnterInstance() != nil:
		s.enterInstance(c)
	case msg.GetStashMove() != nil:
		s.moveItem(c, msg.GetStashMove())
	case s.run != nil:
		s.run.command(c, msg)
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

// waitRun reloads the character once the lease is gone, so the lobby opens
// on what the run banked.
func (s *Lobby) waitRun(c *client) {
	tick := time.NewTicker(s.leaseRetry)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
		}
		ctx, cancel := context.WithTimeout(c.ctx, storeTimeout)
		ch, ok, err := s.Store.Load(ctx, c.name, c.class)
		cancel()
		if err != nil || !ok || ch.Run.Held() {
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
