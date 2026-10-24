// Package server connects WebSocket clients to one sim.World. The world is
// only touched from the tick loop goroutine: connections hand work to it
// through an inbox, and it hands encoded messages back through each client's
// outbox.
//
// A player joins into the lobby, where they move items between stash, bag
// and gear, and enters the cave from there. When the run ends they go back
// to the lobby on the same connection.
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
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const (
	outboxSize  = 64
	joinTimeout = 5 * time.Second
)

type Server struct {
	// TickRate is sent to clients on join. It must match the tick loop.
	TickRate int
	// Store keeps characters and stashes by player name. Set it before
	// serving. Only connection and writer goroutines call it, never the tick.
	Store  inventory.Store
	world  *sim.World
	cave   caves.Cave
	spawns int
	loot   *items.Roller
	log    *slog.Logger
	inbox  chan func()
	done   chan struct{}
	stop   sync.Once
	// conns has every connected player by name, until their last write lands.
	// clients has only those in the cave.
	conns   map[string]*client
	clients map[sim.EntityID]*client

	// PendingSaves counts run ends that are past their patience and still
	// retrying. Register it to export it.
	PendingSaves prometheus.Gauge
	// Tests shorten these.
	endRunPatience, pendingRetry time.Duration
}

type client struct {
	name   string
	class  string
	out    chan []byte
	cancel context.CancelFunc
	writes *writer
	// id is the player's entity in the cave. In the lobby it is 0 and lobby is set.
	id    sim.EntityID
	lobby *lobby
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

func New(world *sim.World, cave caves.Cave, loot *items.Roller, log *slog.Logger) *Server {
	return &Server{
		TickRate: 20,
		Store:    inventory.NewMemory(),
		world:    world,
		cave:     cave,
		loot:     loot,
		log:      log,
		inbox:    make(chan func(), 1024),
		done:     make(chan struct{}),
		conns:    map[string]*client{},
		clients:  map[sim.EntityID]*client{},

		PendingSaves: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "store_pending_saves",
			Help: "Run end saves that failed for longer than their patience and are still retrying.",
		}),
		endRunPatience: endRunPatience,
		pendingRetry:   pendingRetry,
	}
}

// Close stops accepting work. Call it once the tick loop has stopped, so
// connections waiting on the inbox give up instead of blocking forever.
func (s *Server) Close() {
	s.stop.Do(func() { close(s.done) })
}

// Step runs on the tick loop goroutine.
func (s *Server) Step(dt time.Duration) {
	for drained := false; !drained; {
		select {
		case f := <-s.inbox:
			f()
		default:
			drained = true
		}
	}

	events := s.world.Step(dt)

	s.broadcast(protocol.Snapshot(s.world))
	for _, ev := range events.Abilities {
		s.broadcast(protocol.AbilityUsed(ev))
	}
	for _, r := range events.Rejected {
		s.sendTo(r.Player, protocol.AbilityRejected(r))
	}
	for _, ev := range events.Damage {
		s.broadcast(protocol.Damage(ev))
	}
	for _, ev := range events.XP {
		s.sendTo(ev.Player, protocol.XPGained(ev))
	}
	// An extracting player's summary rides on their Extracted, which has to
	// come after the XP above and before the lobby.
	summaries := map[sim.EntityID]*gamev1.RunSummary{}
	after := map[sim.EntityID]inventory.Character{}
	for _, end := range events.RunEnded {
		summaries[end.Player], after[end.Player] = s.endRun(end)
	}
	for _, id := range events.Extracted {
		s.sendTo(id, protocol.Extracted(id, summaries[id]))
		s.broadcastExcept(id, protocol.Extracted(id, nil))
		s.toLobby(id, after[id])
		s.log.Info("player extracted", "player", id)
	}
	for _, id := range events.Inventory {
		s.sendItems(id)
	}
	for _, r := range events.PickupRejected {
		s.sendTo(r.Player, protocol.PickupRejected(r))
	}
	for _, ev := range events.Loot {
		s.sendTo(ev.Player, protocol.Loot(ev))
	}
}

// sendItems tells the player what they carry. Nothing is saved until the run ends.
func (s *Server) sendItems(id sim.EntityID) {
	c, ok := s.clients[id]
	p, alive := s.world.Player(id)
	if !ok || !alive {
		return
	}
	s.sendToClient(c, protocol.Equipment(s.world, p))
	s.sendToClient(c, protocol.Inventory(p))
}

// endRun tells the player about any new level and saves the run's result in
// one write. A death's summary goes out here and the player goes back to the
// lobby. An extraction's summary goes out with Extracted instead, and the
// character returned is the lobby to open after it.
func (s *Server) endRun(end sim.RunEnd) (*gamev1.RunSummary, inventory.Character) {
	c, ok := s.clients[end.Player]
	if !ok {
		return nil, inventory.Character{}
	}
	if end.Levels > 0 {
		s.sendTo(end.Player, protocol.LevelUp(end.Level, protocol.ClassInfo(s.world.Classes(), end.Class)))
	}
	summary := protocol.RunSummary(s.world.Classes().Progression, c.name, end)
	ch := inventory.Character{Name: c.name, Class: c.class, XP: end.XP}
	if end.Extracted {
		var overflow []items.Instance
		ch.Stash, overflow = stashKept(c.stash, end.Kept)
		ch.Loadout = inventory.Loadout{Equipment: end.Gear, Items: overflow}
		summary.Overflow = protocol.Items(overflow)
		s.saveRun(c, ch)
		return summary, ch
	}
	s.sendTo(end.Player, protocol.RunEnded(summary))
	// The gear and bag already lie on the ground, so the next lobby starts
	// from the starter kit. The stash is untouched.
	ch.Loadout, ch.Stash = s.starterKit(), c.stash
	s.saveRun(c, ch)
	s.world.RemovePlayer(end.Player)
	s.toLobby(end.Player, ch)
	return summary, ch
}

func (s *Server) sendTo(id sim.EntityID, msg *gamev1.ServerMessage) {
	if c, ok := s.clients[id]; ok {
		s.sendToClient(c, msg)
	}
}

func (s *Server) sendToClient(c *client, msg *gamev1.ServerMessage) {
	if b, err := protocol.EncodeServer(msg); err == nil {
		c.send(b)
	}
}

func (s *Server) broadcast(msg *gamev1.ServerMessage) {
	s.broadcastExcept(0, msg)
}

func (s *Server) broadcastExcept(skip sim.EntityID, msg *gamev1.ServerMessage) {
	if len(s.clients) == 0 {
		return
	}
	b, err := protocol.EncodeServer(msg)
	if err != nil {
		s.log.Error("encode server message", "err", err)
		return
	}
	for id, c := range s.clients {
		if id != skip {
			c.send(b)
		}
	}
}

// send never blocks the tick loop. A client too slow to drain its outbox is
// dropped rather than holding up everyone else.
func (c *client) send(b []byte) {
	select {
	case c.out <- b:
	default:
		c.cancel()
	}
}

func (s *Server) starterKit() inventory.Loadout {
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

func (s *Server) enqueue(f func()) bool {
	select {
	case s.inbox <- f:
		return true
	case <-s.done:
		return false
	}
}

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
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
	c := &client{name: name, class: class, out: make(chan []byte, outboxSize), cancel: cancel, writes: newWriter()}
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
		if _, ok := s.world.Classes().Classes[class]; !ok {
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

// leave runs on the tick loop once the connection is gone. A run in progress
// ends as a death would for XP, but keeps what the player carried until the
// 30 s rule in FOM-217.
func (s *Server) leave(c *client) {
	if c.id != 0 {
		p, _ := s.world.Player(c.id)
		if end, ok := s.world.RemovePlayer(c.id); ok {
			s.saveRun(c, inventory.Character{Name: c.name, Class: c.class, XP: end.XP, Loadout: p.Loadout(), Stash: c.stash})
		}
		delete(s.clients, c.id)
	}
	c.writes.close()
}

// handle runs on the tick loop, which is the only place c.id and c.lobby change.
func (s *Server) handle(c *client, msg *gamev1.ClientMessage) {
	switch {
	case msg.GetEnterInstance() != nil:
		s.enterInstance(c)
	case msg.GetStashMove() != nil:
		s.moveItem(c, msg.GetStashMove())
	case c.id != 0:
		if cmd, ok := protocol.Command(msg, c.id); ok {
			s.world.Submit(cmd)
		}
	}
}

func (s *Server) writeLoop(ctx context.Context, conn *websocket.Conn, c *client, cancel context.CancelFunc) {
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
