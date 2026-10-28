package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

const (
	// reasonChangingTown follows a TownTicket.
	reasonChangingTown = "changing town"
	// reasonWrongTown is a shard path for another shard. 1013, like a full
	// town, so the client goes back to /ws.
	reasonWrongTown = "wrong town"
	// A drain moves drainBatch players every drainEvery.
	drainEvery = 250 * time.Millisecond
	drainBatch = 4
)

var errTownFull = errors.New(reasonTownFull)

// HandleWS is the front door at /ws. Every world pod is one.
func (s *Lobby) HandleWS(w http.ResponseWriter, r *http.Request) {
	s.serveTown(w, r, false)
}

// HandleTown is this shard's own path, /towns/{id}/ws.
func (s *Lobby) HandleTown(w http.ResponseWriter, r *http.Request) {
	s.serveTown(w, r, true)
}

// Ready is false from the start of a drain, so the Service stops sending
// this pod new players.
func (s *Lobby) Ready() bool {
	return !s.townDraining.Load()
}

func newTownTickets() *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "town_tickets_total",
		Help: "TownTickets sent, by cause: front_door or drain.",
	}, []string{"cause"})
}

// pick is the URL of the shard the directory picks for name, or "" to seat
// them here. An error is no shard with room.
func (s *Lobby) pick(name string) (string, error) {
	if s.Towns == nil {
		return "", nil
	}
	t, err := s.Towns.Pick(name)
	if err != nil {
		return "", err
	}
	if t.ID == s.Towns.ID || t.URL == "" {
		return "", nil
	}
	return t.URL, nil
}

// frontDoorTicket sends a player on to another shard. Nothing is claimed
// here: the target claims and loads the character.
func (s *Lobby) frontDoorTicket(ctx context.Context, conn *websocket.Conn, url string) {
	b, err := protocol.EncodeServer(protocol.TownTicket(url))
	if err == nil {
		err = conn.Write(ctx, websocket.MessageBinary, b)
	}
	if err != nil {
		return
	}
	s.TownTickets.WithLabelValues("front_door").Inc()
	// Let the client close first, so the ticket is not lost to the close. A
	// read that times out would drop the connection, so the timer is apart.
	read := make(chan struct{})
	go func() {
		defer close(read)
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	select {
	case <-read:
	case <-time.After(s.closeGrace):
	}
	conn.Close(websocket.StatusNormalClosure, reasonChangingTown)
}

// moveTown sends c to the shard at url. Like enterInstance it waits behind
// the player's queued lobby saves, and the claim is released behind them, so
// the target's claim finds the name free. Runs on the loop.
func (s *Lobby) moveTown(c *client, url string) {
	if !movable(c) {
		return
	}
	c.entering = true
	s.releaseThen(c, func(owner *client, err error) {
		c.entering = false
		if owner != c || c.gone || c.lobby == nil || err != nil {
			return
		}
		c.session = noSession
		c.lobby, c.inRun = nil, false
		s.leaveTown(c)
		s.sendToClient(c, protocol.TownTicket(url))
		s.TownTickets.WithLabelValues("drain").Inc()
		time.AfterFunc(s.closeGrace, func() { c.close(websocket.StatusNormalClosure, reasonChangingTown) })
		s.log.Info("moved to another town", "name", c.name, "url", url)
	})
}

// movable is a player in the town with no save or trip under way.
func movable(c *client) bool {
	return !c.gone && c.lobby != nil && c.id != 0 && !c.entering && c.pending == nil
}

// Drain stops seating players and moves the town's players to the shards the
// directory picks, a few at a time. It returns once no one is connected, when
// the directory has nowhere to send them, or when ctx ends.
func (s *Lobby) Drain(ctx context.Context) {
	s.townDraining.Store(true)
	if s.Towns != nil {
		s.Towns.Drain(ctx)
	}
	tick := time.NewTicker(drainEvery)
	defer tick.Stop()
	for {
		var names []string
		left := 0
		if !s.onLoop(func() {
			left = len(s.conns)
			for name, c := range s.conns {
				if len(names) == drainBatch {
					break
				}
				if movable(c) {
					names = append(names, name)
				}
			}
		}) || left == 0 {
			return
		}
		for _, name := range names {
			url, err := s.pick(name)
			if err != nil || url == "" {
				s.log.Warn("drain has no town to move players to", "left", left, "err", err)
				return
			}
			s.enqueue(func() {
				if c, ok := s.conns[name]; ok {
					s.moveTown(c, url)
				}
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// WaitGone returns once every player's last write has landed, or ctx ends.
func (s *Lobby) WaitGone(ctx context.Context) {
	for {
		left := 0
		if !s.onLoop(func() { left = len(s.conns) }) || left == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// onLoop runs f on the loop and waits for it. It is false once the loop
// stops taking work.
func (s *Lobby) onLoop(f func()) bool {
	ran := make(chan struct{})
	if !s.enqueue(func() { f(); close(ran) }) {
		return false
	}
	select {
	case <-ran:
		return true
	case <-s.done:
		return false
	}
}
