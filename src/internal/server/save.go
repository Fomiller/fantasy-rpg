package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

const (
	storeTimeout = 2 * time.Second
	retryBackoff = 100 * time.Millisecond
	// A run end that fails for endRunPatience becomes a pending save, retried
	// every pendingRetry until it lands.
	endRunPatience = 10 * time.Second
	pendingRetry   = 5 * time.Second
)

var errShuttingDown = errors.New("server shutting down")

// storeWrite is one store call for one player. The tick loop builds it from
// copies, so the call never touches sim or lobby state.
type storeWrite struct {
	op       string
	lobby    bool
	patience time.Duration
	// keep retries past patience instead of giving up.
	keep bool
	call func(context.Context) error
	// done runs on the tick loop with the client that now holds the name, or nil.
	done func(owner *client, err error)
}

// writer is a player's queue of store writes. push never blocks, so the tick
// loop can always hand off a write.
type writer struct {
	mu     sync.Mutex
	queue  []storeWrite
	closed bool
	// exited is set once next has returned false, so the loop is gone for good.
	exited bool
	wake   chan struct{}
}

func newWriter() *writer {
	return &writer{wake: make(chan struct{}, 1)}
}

// push still takes a write after close, until the loop has exited. A write's
// done can then queue one more, like the run end for a lease taken just as
// the player left.
func (w *writer) push(sw storeWrite) {
	w.mu.Lock()
	if w.exited {
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

// dropLobby forgets queued lobby saves.
func (w *writer) dropLobby() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue = slices.DeleteFunc(w.queue, func(sw storeWrite) bool { return sw.lobby })
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
		w.exited = closed
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
// Each write's done runs before the next write starts, so done sees the
// queue as it is and can add to it.
func (s *Lobby) saveLoop(name string, w *writer) {
	for {
		sw, ok := w.next(s.done)
		if !ok {
			break
		}
		err := s.write(sw)
		handled := make(chan struct{})
		if !s.enqueue(func() {
			defer close(handled)
			if err != nil {
				s.log.Error("store write", "op", sw.op, "name", name, "err", err)
			}
			if sw.done != nil {
				sw.done(s.owner(name, w), err)
			}
		}) {
			return
		}
		select {
		case <-handled:
		case <-s.done:
			return
		}
	}
	s.enqueue(func() {
		if c := s.owner(name, w); c != nil {
			delete(s.conns, name)
			if s.released != nil {
				s.released(c)
			}
		}
	})
}

// owner is the client using w, which changes when a rejoin takes over a
// pending save.
func (s *Lobby) owner(name string, w *writer) *client {
	if c, ok := s.conns[name]; ok && c.writes == w {
		return c
	}
	return nil
}

func (s *Lobby) write(sw storeWrite) error {
	deadline := time.Now().Add(sw.patience)
	pending := false
	defer func() {
		if pending {
			s.PendingSaves.Dec()
		}
	}()
	backoff := retryBackoff
	for {
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		err := sw.call(ctx)
		cancel()
		if err == nil || final(err) {
			return err
		}
		wait := backoff
		backoff = min(backoff*2, storeTimeout)
		if time.Now().Add(wait).After(deadline) {
			if !sw.keep {
				return err
			}
			if !pending {
				pending = true
				s.PendingSaves.Inc()
				s.log.Warn("save pending", "op", sw.op, "err", err)
			}
			wait = s.pendingRetry
		}
		select {
		case <-time.After(wait):
		case <-s.done:
			return err
		}
	}
}

// loadCharacter runs on the connection goroutine. A name and class that never
// joined get the starter kit, which is made on the tick loop because the
// roller is not safe to share.
func (s *Lobby) loadCharacter(ctx context.Context, name, class string) (inventory.Character, error) {
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

// saveLobby queues the lobby as it is now. If the write fails and no newer
// one is queued, the lobby goes back to what the store has.
func (s *Lobby) saveLobby(c *client, itemID string) {
	name, class, l, stash := c.name, c.class, c.lobby.loadout(), slices.Clone(c.lobby.stash.Items)
	c.lobbySeq++
	seq := c.lobbySeq
	c.writes.push(storeWrite{op: "save lobby", lobby: true, call: func(ctx context.Context) error {
		return s.Store.SaveLobby(ctx, name, class, l, stash)
	}, done: func(owner *client, err error) {
		if owner != c {
			return
		}
		if err == nil {
			if seq > c.savedSeq {
				c.saved, c.savedSeq = inventory.Character{XP: c.saved.XP, Loadout: l, Stash: stash}, seq
			}
			return
		}
		inRun := errors.Is(err, inventory.ErrInRun)
		if inRun {
			// A save queued behind this one is from the same lobby, which a run
			// end is about to replace.
			c.writes.dropLobby()
		} else if seq != c.lobbySeq {
			return
		}
		if c.lobby == nil {
			return
		}
		saved := cloneCharacter(c.saved)
		c.lobby = newLobby(c.lobby.xp, s.rules.BagSize(), saved.Loadout, saved.Stash)
		reason := MoveSaving
		if inRun {
			reason = MoveInRun
			s.setInRun(c, true)
			s.sendLobby(c)
		}
		s.sendToClient(c, protocol.StashMoveRejected(itemID, reason))
		s.sendLobbyItems(c)
	}})
}

// saveRun queues the one write a run end makes. Until it lands the lobby
// shows saving and nothing else can change the character.
func (s *Lobby) saveRun(c *client, ch inventory.Character) {
	runID := c.runID
	ch = cloneCharacter(ch)
	pending := &ch
	c.pending = pending
	c.writes.push(storeWrite{op: "end run", patience: s.endRunPatience, keep: true, call: func(ctx context.Context) error {
		return s.Store.EndRun(ctx, runID, *pending)
	}, done: func(owner *client, err error) {
		if owner == nil || owner.pending != pending {
			return
		}
		if errors.Is(err, inventory.ErrLeaseLost) {
			// The run result is gone. Drop the player so a rejoin loads what
			// the store has, not a lobby that was never saved.
			owner.pending = nil
			owner.cancel()
			return
		}
		if err != nil {
			return
		}
		owner.pending = nil
		if owner.lobby != nil {
			s.sendLobby(owner)
		}
	}})
}

// clearRun drops a dead lease before any lobby save can hit it.
func (s *Lobby) clearRun(c *client, runID string) {
	name := c.name
	c.writes.push(storeWrite{op: "clear run", call: func(ctx context.Context) error {
		return s.Store.ClearRun(ctx, name, runID)
	}})
}

// final errors never get better with a retry.
func final(err error) bool {
	return errors.Is(err, inventory.ErrInRun) || errors.Is(err, inventory.ErrRunUsed) || errors.Is(err, inventory.ErrLeaseLost)
}

func cloneCharacter(ch inventory.Character) inventory.Character {
	ch.Loadout = inventory.Loadout{Equipment: slices.Clone(ch.Loadout.Equipment), Items: slices.Clone(ch.Loadout.Items)}
	ch.Stash = slices.Clone(ch.Stash)
	return ch
}
