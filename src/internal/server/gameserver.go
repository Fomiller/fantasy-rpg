package server

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Fomiller/fantasy-rpg/src/internal/gameserver"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
)

const (
	// healthEvery is how often the tick loop pings Agones. A stuck tick stops
	// the pings, so Agones marks the server Unhealthy.
	healthEvery = 2 * time.Second
	drainPoll   = 50 * time.Millisecond
)

// sdkQueue runs SDK calls in order on one goroutine, so the tick loop never
// waits on the sidecar.
type sdkQueue struct {
	mu   sync.Mutex
	jobs []func()
	wake chan struct{}
}

func (q *sdkQueue) push(f func()) {
	q.mu.Lock()
	q.jobs = append(q.jobs, f)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *sdkQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

func (q *sdkQueue) run(stop <-chan struct{}) {
	for {
		q.mu.Lock()
		jobs := q.jobs
		q.jobs = nil
		q.mu.Unlock()
		for _, f := range jobs {
			f()
		}
		select {
		case <-q.wake:
		case <-stop:
			return
		}
	}
}

// pingHealth runs on the loop. It skips a beat rather than pile up pings
// behind a slow sidecar.
func (s *Server) pingHealth(now time.Time) {
	if now.Sub(s.lastHealth) < healthEvery || s.sdk.len() > 0 {
		return
	}
	s.lastHealth = now
	held := map[string]bool{}
	for _, c := range s.conns {
		held[c.runID] = true
	}
	s.sdk.push(func() {
		if err := s.GameServer.Health(); err != nil {
			s.log.Warn("health ping", "err", err)
		}
		s.dropUnjoined(held, now)
	})
}

// dropUnjoined takes a run off the list once its token has expired with no
// join, so a seat the allocation took for nobody is freed.
func (s *Server) dropUnjoined(held map[string]bool, now time.Time) {
	runs, err := s.GameServer.Runs()
	if err != nil {
		s.log.Warn("read runs", "err", err)
		return
	}
	seen := map[string]time.Time{}
	dropped := false
	for _, run := range runs {
		if held[run] {
			continue
		}
		since, ok := s.unjoined[run]
		if !ok {
			since = now
		}
		if now.Sub(since) < jointoken.Lifetime {
			seen[run] = since
			continue
		}
		if err := s.GameServer.RemoveRun(run); err != nil {
			s.log.Warn("remove run", "run", run, "err", err)
			continue
		}
		s.log.Info("dropped a run that never joined", "run", run)
		dropped = true
	}
	s.unjoined = seen
	if dropped {
		// An allocation with nobody in it still counts as used, so the server
		// can shut down.
		s.enqueue(func() { s.used = true })
	}
}

// addRun runs on the loop when a join claims a seat.
func (s *Server) addRun(runID string) {
	s.sdk.push(func() {
		if err := s.GameServer.AddRun(runID); err != nil {
			s.log.Warn("add run", "run", runID, "err", err)
		}
	})
}

// release runs on the loop once a player's last write has landed, so the
// seat stays taken until the run end is in the store.
func (s *Server) release(c *client) {
	if c.runID == "" {
		return
	}
	s.sdk.push(func() {
		if err := s.GameServer.RemoveRun(c.runID); err != nil {
			s.log.Warn("remove run", "run", c.runID, "err", err)
		}
	})
}

// stopIfEmpty shuts the server down once the last player is gone and their
// writes have landed. Locally that resets the world.
func (s *Server) stopIfEmpty(now time.Time) {
	if !s.used || s.draining || s.stopping || len(s.conns) > 0 || now.Before(s.nextStop) {
		return
	}
	s.stopping = true
	capacity := int64(s.MaxPlayers)
	s.sdk.push(func() {
		stopped, err := gameserver.ShutdownIfEmpty(s.GameServer, capacity)
		if err != nil {
			s.log.Error("shut down on empty", "err", err)
		}
		if stopped {
			s.log.Info("instance empty, shutting down")
			return
		}
		s.enqueue(func() {
			s.stopping = false
			s.nextStop = time.Now().Add(healthEvery)
		})
	})
}

// resetWorld is the local Shutdown. The boot id stays, since it is the same
// process.
func (s *Server) resetWorld() {
	s.stopping = false
	if len(s.conns) > 0 {
		return
	}
	s.used, s.spawns = false, 0
	if s.Reset == nil {
		return
	}
	s.world, s.loot = s.Reset()
	s.rules = s.world
	s.log.Info("instance reset")
}

// Drain stops new joins and lets the runs in progress play on for up to
// timeout. Then it ends the rest as a disconnect does, and waits until
// their run ends land or ctx is done.
func (s *Server) Drain(ctx context.Context, timeout time.Duration) {
	s.enqueue(func() {
		s.draining = true
		s.sdk.push(func() {
			if err := s.GameServer.SetRunCapacity(0); err != nil {
				s.log.Warn("close runs list", "err", err)
			}
		})
	})
	end, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if s.waitEmpty(end) {
		return
	}
	s.enqueue(func() {
		n := 0
		for _, c := range s.conns {
			if c.id != 0 || c.entering {
				n++
			}
			c.close(websocket.StatusGoingAway, reasonDraining)
		}
		s.log.Warn("drain timed out, ending runs", "runs", n)
	})
	s.waitEmpty(ctx)
}

// waitEmpty reports if every player left and their writes landed before
// ctx was done.
func (s *Server) waitEmpty(ctx context.Context) bool {
	tick := time.NewTicker(drainPoll)
	defer tick.Stop()
	for {
		empty := make(chan bool, 1)
		if !s.enqueue(func() { empty <- len(s.conns) == 0 }) {
			return false
		}
		select {
		case ok := <-empty:
			if ok {
				return true
			}
		case <-ctx.Done():
			return false
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return false
		}
	}
}
