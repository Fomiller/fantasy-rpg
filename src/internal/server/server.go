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
	// Store keeps loadouts, stashes and banked XP by player name. Set it before serving.
	Store  inventory.Store
	world  *sim.World
	cave   caves.Cave
	spawns int
	loot   *items.Roller
	log    *slog.Logger
	inbox  chan func()
	done   chan struct{}
	stop   sync.Once
	// conns has every connected player by name. clients has only those in the cave.
	conns   map[string]*client
	clients map[sim.EntityID]*client
}

type client struct {
	name   string
	class  string
	out    chan []byte
	cancel context.CancelFunc
	// id is the player's entity in the cave. In the lobby it is 0 and lobby is set.
	id    sim.EntityID
	lobby *lobby
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
	for _, end := range events.RunEnded {
		s.saveXP(end)
		summaries[end.Player] = s.endRun(end)
	}
	for _, id := range events.Extracted {
		s.sendTo(id, protocol.Extracted(id, summaries[id]))
		s.broadcastExcept(id, protocol.Extracted(id, nil))
		s.toLobby(id)
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

// sendItems tells the player what they carry and saves it. Saving on every
// change means a disconnect never loses anything.
func (s *Server) sendItems(id sim.EntityID) {
	c, ok := s.clients[id]
	p, alive := s.world.Player(id)
	if !ok || !alive {
		return
	}
	s.sendToClient(c, protocol.Equipment(s.world, p))
	s.sendToClient(c, protocol.Inventory(p))
	if err := s.Store.Save(context.Background(), c.name, p.Loadout()); err != nil {
		s.log.Error("save loadout", "player", id, "name", c.name, "err", err)
	}
}

func (s *Server) saveXP(end sim.RunEnd) {
	c, ok := s.clients[end.Player]
	if !ok {
		return
	}
	if err := s.Store.SaveXP(context.Background(), c.name, end.XP); err != nil {
		s.log.Error("save xp", "player", end.Player, "name", c.name, "err", err)
	}
}

// endRun tells the player about any new level and moves an extracted bag to
// the stash. A death's summary goes out here and the player goes back to the
// lobby. An extraction's summary goes out with Extracted instead.
func (s *Server) endRun(end sim.RunEnd) *gamev1.RunSummary {
	c, ok := s.clients[end.Player]
	if !ok {
		return nil
	}
	if end.Levels > 0 {
		s.sendTo(end.Player, protocol.LevelUp(end.Level, protocol.ClassInfo(s.world.Classes(), end.Class)))
	}
	summary := protocol.RunSummary(s.world.Classes().Progression, c.name, end)
	if end.Extracted {
		summary.Overflow = protocol.Items(s.stashKept(c.name, end.Kept, end.Gear))
		return summary
	}
	s.sendTo(end.Player, protocol.RunEnded(summary))
	// The gear and bag already lie on the ground, so the next lobby starts
	// from the starter kit. The stash is untouched.
	if err := s.Store.Delete(context.Background(), c.name); err != nil {
		s.log.Error("delete loadout", "player", end.Player, "name", c.name, "err", err)
	}
	s.world.RemovePlayer(end.Player)
	s.toLobby(end.Player)
	return summary
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
	c := &client{name: name, class: class, out: make(chan []byte, outboxSize), cancel: cancel}
	joined := make(chan error, 1)
	ok := s.enqueue(func() {
		// One connection per name, or two copies of a player could load and
		// save the same items.
		if _, ok := s.conns[name]; ok {
			joined <- errors.New("name already playing")
			return
		}
		if _, ok := s.world.Classes().Classes[class]; !ok {
			joined <- fmt.Errorf("unknown class %q", class)
			return
		}
		if err := s.openLobby(c); err != nil {
			joined <- err
			return
		}
		s.conns[name] = c
		joined <- nil
	})
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}

	select {
	case err = <-joined:
	case <-ctx.Done():
		return
	}
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	log := s.log.With("name", name)
	log.Info("player joined")
	defer func() {
		s.enqueue(func() {
			if c.id != 0 {
				if end, ok := s.world.RemovePlayer(c.id); ok {
					s.saveXP(end)
				}
				delete(s.clients, c.id)
			}
			delete(s.conns, name)
		})
		log.Info("player left")
	}()

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

// handle runs on the tick loop, which is the only place c.id and c.lobby change.
func (s *Server) handle(c *client, msg *gamev1.ClientMessage) {
	switch {
	case msg.GetEnterCave() != nil:
		s.enterCave(c)
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
