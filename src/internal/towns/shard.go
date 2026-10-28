package towns

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// callTimeout bounds each directory call, so a hung Valkey never holds up a
// beat or a join.
const callTimeout = time.Second

// Shard is one world server's place in the directory.
type Shard struct {
	Dir      Directory
	ID       string
	URL      string
	Revision string
	Started  time.Time
	// Players is the head count in the town. It must be safe to call from
	// any goroutine.
	Players  func() int
	Log      *slog.Logger
	draining atomic.Bool
	failing  bool
}

// Self is this shard as it publishes itself.
func (s *Shard) Self() Town {
	return Town{
		ID:       s.ID,
		URL:      s.URL,
		Players:  s.Players(),
		Revision: s.Revision,
		Draining: s.draining.Load(),
		Started:  s.Started,
	}
}

// Run publishes the shard every Beat until ctx ends.
func (s *Shard) Run(ctx context.Context) {
	tick := time.NewTicker(Beat)
	defer tick.Stop()
	for {
		s.publish(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Shard) publish(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := s.Dir.Publish(pctx, s.Self())
	if ctx.Err() != nil {
		return
	}
	// Once per outage, not once per beat.
	switch {
	case err != nil && !s.failing:
		s.Log.Warn("town directory publish", "err", err)
	case err == nil && s.failing:
		s.Log.Info("town directory publish recovered")
	}
	s.failing = err != nil
}

// Drain marks the shard draining, so no front door sends anyone here, and
// publishes it at once. Run keeps publishing it as draining.
func (s *Shard) Drain(ctx context.Context) {
	s.draining.Store(true)
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := s.Dir.Publish(ctx, s.Self()); err != nil {
		s.Log.Warn("town directory drain", "err", err)
	}
}

// Joined remembers this shard as name's last.
func (s *Shard) Joined(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := s.Dir.Remember(ctx, name, s.ID); err != nil {
		s.Log.Debug("town directory remember", "name", name, "err", err)
	}
}

// Pick is the shard a front door sends name to.
func (s *Shard) Pick(name string) (Town, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return s.Dir.Pick(ctx, name, s.Self())
}
