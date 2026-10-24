package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

const (
	storeTimeout = 2 * time.Second
	// endRunPatience is how long a run end keeps retrying. Keeping it as a
	// pending save after that is FOM-223.
	endRunPatience = 10 * time.Second
	retryBackoff   = 100 * time.Millisecond
)

var errShuttingDown = errors.New("server shutting down")

// storeWrite is one store call for one player. The tick loop builds it from
// copies, so the call never touches sim or lobby state.
type storeWrite struct {
	op       string
	lobby    bool
	patience time.Duration
	call     func(context.Context) error
}

// writer is a player's queue of store writes. push never blocks, so the tick
// loop can always hand off a write.
type writer struct {
	mu     sync.Mutex
	queue  []storeWrite
	closed bool
	wake   chan struct{}
}

func newWriter() *writer {
	return &writer{wake: make(chan struct{}, 1)}
}

func (w *writer) push(sw storeWrite) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	// A lobby save writes whole values, so a newer one makes a queued one moot.
	if n := len(w.queue); sw.lobby && n > 0 && w.queue[n-1].lobby {
		w.queue[n-1] = sw
	} else {
		w.queue = append(w.queue, sw)
	}
	w.mu.Unlock()
	w.signal()
}

func (w *writer) close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.signal()
}

func (w *writer) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// next waits for the next write. It returns false once the writer is closed
// and empty, or the server stops with nothing left queued.
func (w *writer) next(stop <-chan struct{}) (storeWrite, bool) {
	for {
		w.mu.Lock()
		if len(w.queue) > 0 {
			sw := w.queue[0]
			w.queue = w.queue[1:]
			w.mu.Unlock()
			return sw, true
		}
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return storeWrite{}, false
		}
		select {
		case <-w.wake:
		case <-stop:
			return storeWrite{}, false
		}
	}
}

// saveLoop runs one player's writes in order. The name stays claimed until
// the last write lands, so a rejoin never loads a character about to change.
func (s *Server) saveLoop(c *client) {
	for {
		sw, ok := c.writes.next(s.done)
		if !ok {
			break
		}
		err := s.write(sw)
		s.enqueue(func() {
			if err != nil {
				s.log.Error("store write", "op", sw.op, "name", c.name, "err", err)
			}
		})
	}
	s.enqueue(func() {
		if s.conns[c.name] == c {
			delete(s.conns, c.name)
		}
	})
}

func (s *Server) write(sw storeWrite) error {
	deadline := time.Now().Add(sw.patience)
	for backoff := retryBackoff; ; backoff = min(backoff*2, storeTimeout) {
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		err := sw.call(ctx)
		cancel()
		if err == nil || time.Now().Add(backoff).After(deadline) {
			return err
		}
		select {
		case <-time.After(backoff):
		case <-s.done:
			return err
		}
	}
}

// loadCharacter runs on the connection goroutine. A name and class that never
// joined get the starter kit, which is made on the tick loop because the
// roller is not safe to share.
func (s *Server) loadCharacter(ctx context.Context, name, class string) (inventory.Character, error) {
	load := func() (inventory.Character, bool, error) {
		ctx, cancel := context.WithTimeout(ctx, storeTimeout)
		defer cancel()
		return s.Store.Load(ctx, name, class)
	}
	ch, ok, err := load()
	if err != nil || ok {
		return ch, err
	}
	kit := make(chan inventory.Loadout, 1)
	if !s.enqueue(func() { kit <- s.starterKit() }) {
		return ch, errShuttingDown
	}
	select {
	case ch.Loadout = <-kit:
	case <-ctx.Done():
		return ch, ctx.Err()
	}
	create, cancel := context.WithTimeout(ctx, storeTimeout)
	err = s.Store.Create(create, ch)
	cancel()
	if err != nil {
		return ch, err
	}
	// Load again, in case another join created it first.
	if ch, ok, err = load(); err == nil && !ok {
		err = inventory.ErrNoCharacter
	}
	return ch, err
}

// saveLobby queues the lobby as it is now.
func (s *Server) saveLobby(c *client) {
	name, class, l, stash := c.name, c.class, c.lobby.loadout(), slices.Clone(c.lobby.stash.Items)
	c.writes.push(storeWrite{op: "save lobby", lobby: true, call: func(ctx context.Context) error {
		return s.Store.SaveLobby(ctx, name, class, l, stash)
	}})
}

// saveRun queues the one write a run end makes.
func (s *Server) saveRun(c *client, ch inventory.Character) {
	runID := c.runID
	ch.Loadout = inventory.Loadout{Equipment: slices.Clone(ch.Loadout.Equipment), Items: slices.Clone(ch.Loadout.Items)}
	ch.Stash = slices.Clone(ch.Stash)
	c.writes.push(storeWrite{op: "end run", patience: endRunPatience, call: func(ctx context.Context) error {
		return s.Store.EndRun(ctx, runID, ch)
	}})
}
