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
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const (
	outboxSize  = 64
	joinTimeout = 5 * time.Second
)

type Server struct {
	world   *sim.World
	spawn   sim.Vec2
	log     *slog.Logger
	inbox   chan func()
	done    chan struct{}
	stop    sync.Once
	clients map[sim.EntityID]*client
}

type client struct {
	out    chan []byte
	cancel context.CancelFunc
}

func New(world *sim.World, spawn sim.Vec2, log *slog.Logger) *Server {
	return &Server{
		world:   world,
		spawn:   spawn,
		log:     log,
		inbox:   make(chan func(), 1024),
		done:    make(chan struct{}),
		clients: map[sim.EntityID]*client{},
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
	for _, ev := range events {
		s.broadcast(protocol.Damage(ev))
	}
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

	c := &client{out: make(chan []byte, outboxSize), cancel: cancel}
	joined := make(chan sim.EntityID, 1)
	ok := s.enqueue(func() {
		id := s.world.AddPlayer(s.spawn)
		s.clients[id] = c
		if b, err := protocol.EncodeServer(protocol.Joined(id)); err == nil {
			c.send(b)
		}
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
	return join.GetName(), nil
}
