// Package server connects WebSocket clients to one sim.World. The world is
// only touched from the tick loop goroutine: connections hand work to it
// through an inbox, and it hands encoded messages back through each client's
// outbox.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
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
	// Store keeps loadouts by player name. Set it before serving.
	Store   inventory.Store
	world   *sim.World
	cave    caves.Cave
	spawns  int
	loot    *items.Roller
	log     *slog.Logger
	inbox   chan func()
	done    chan struct{}
	stop    sync.Once
	clients map[sim.EntityID]*client
}

type client struct {
	name   string
	out    chan []byte
	cancel context.CancelFunc
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
	for _, ev := range events.Damage {
		s.broadcast(protocol.Damage(ev))
	}
	for _, id := range events.Extracted {
		s.broadcast(protocol.Extracted(id))
		if c, ok := s.clients[id]; ok {
			c.finish()
			delete(s.clients, id)
		}
		s.log.Info("player extracted", "player", id)
	}
	for _, id := range events.Inventory {
		s.sendItems(id)
	}
}

// sendItems tells the player what they carry and saves it. Saving on every
// change means a disconnect or extraction never loses anything.
func (s *Server) sendItems(id sim.EntityID) {
	c, ok := s.clients[id]
	p, alive := s.world.Player(id)
	if !ok || !alive {
		return
	}
	for _, msg := range []*gamev1.ServerMessage{protocol.Equipment(p), protocol.Inventory(p)} {
		if b, err := protocol.EncodeServer(msg); err == nil {
			c.send(b)
		}
	}
	ctx := context.Background()
	var err error
	// A player who lost everything starts over with the starter kit.
	if l := p.Loadout(); l.Empty() {
		err = s.Store.Delete(ctx, c.name)
	} else {
		err = s.Store.Save(ctx, c.name, l)
	}
	if err != nil {
		s.log.Error("save loadout", "player", id, "name", c.name, "err", err)
	}
}

func (s *Server) nameInUse(name string) bool {
	for _, c := range s.clients {
		if c.name == name {
			return true
		}
	}
	return false
}

func (s *Server) broadcast(msg *gamev1.ServerMessage) {
	if len(s.clients) == 0 {
		return
	}
	b, err := protocol.EncodeServer(msg)
	if err != nil {
		s.log.Error("encode server message", "err", err)
		return
	}
	for _, c := range s.clients {
		c.send(b)
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

// giveLoadout restores what the player had last time, or the starter kit.
func (s *Server) giveLoadout(id sim.EntityID, name string) {
	l, ok, err := s.Store.Load(context.Background(), name)
	if err != nil {
		s.log.Error("load loadout", "name", name, "err", err)
	}
	if !ok {
		l = s.starterKit()
	}
	if err := s.world.Restore(id, l); err != nil {
		s.log.Error("restore loadout", "player", id, "err", err)
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

// finish closes the connection once everything already queued is written.
func (c *client) finish() {
	select {
	case c.out <- nil:
	default:
		c.cancel()
	}
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

	name, err := readJoin(ctx, conn)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	c := &client{name: name, out: make(chan []byte, outboxSize), cancel: cancel}
	joined := make(chan sim.EntityID, 1)
	ok := s.enqueue(func() {
		// One connection per name, or two copies of a player could load and
		// save the same items.
		if s.nameInUse(name) {
			joined <- 0
			return
		}
		spawn := s.cave.PlayerSpawns[s.spawns%len(s.cave.PlayerSpawns)]
		s.spawns++
		id := s.world.AddPlayer(spawn)
		s.clients[id] = c
		if b, err := protocol.EncodeServer(protocol.Joined(id, s.cave, spawn, s.TickRate)); err == nil {
			c.send(b)
		}
		s.giveLoadout(id, name)
		s.sendItems(id)
		joined <- id
	})
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}

	var id sim.EntityID
	select {
	case id = <-joined:
	case <-ctx.Done():
		return
	}
	if id == 0 {
		conn.Close(websocket.StatusPolicyViolation, "name already playing")
		return
	}
	log := s.log.With("player", id, "name", name)
	log.Info("player joined")
	defer func() {
		s.enqueue(func() {
			s.world.RemovePlayer(id)
			delete(s.clients, id)
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
		if cmd, ok := protocol.Command(msg, id); ok {
			if !s.enqueue(func() { s.world.Submit(cmd) }) {
				return
			}
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
			if b == nil {
				conn.Close(websocket.StatusNormalClosure, "extracted")
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, b); err != nil {
				return
			}
		}
	}
}

func readJoin(ctx context.Context, conn *websocket.Conn) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, joinTimeout)
	defer cancel()
	_, b, err := conn.Read(ctx)
	if err != nil {
		return "", err
	}
	msg, err := protocol.DecodeClient(b)
	if err != nil {
		return "", err
	}
	join := msg.GetJoin()
	if join == nil {
		return "", errors.New("first message must be join")
	}
	if join.GetName() == "" {
		return "", errors.New("join needs a name")
	}
	return join.GetName(), nil
}
