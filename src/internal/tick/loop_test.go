package tick

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestLoopRunsAtFixedRate(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry(), "test")
	var steps int
	var gotDT time.Duration
	l := &Loop{Interval: 10 * time.Millisecond, Metrics: m, Step: func(dt time.Duration) {
		steps++
		gotDT = dt
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 205*time.Millisecond)
	defer cancel()
	if err := l.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want deadline exceeded", err)
	}

	// About 20 ticks in 205ms. Leave room for a slow CI runner.
	if steps < 15 || steps > 22 {
		t.Fatalf("ran %d steps, want about 20", steps)
	}
	if gotDT != 10*time.Millisecond {
		t.Fatalf("dt = %v, want the fixed interval", gotDT)
	}
	if got := testutil.ToFloat64(m.Ticks); int(got) != steps {
		t.Fatalf("ticks metric = %v, want %d", got, steps)
	}
}

func TestSlowStepCountsMissedTicks(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry(), "test")
	var steps int
	l := &Loop{Interval: 10 * time.Millisecond, Metrics: m, Step: func(time.Duration) {
		steps++
		if steps == 1 {
			time.Sleep(35 * time.Millisecond)
		}
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = l.Run(ctx)

	if missed := testutil.ToFloat64(m.Missed); missed < 2 || missed > 4 {
		t.Fatalf("missed = %v, want about 3 for a 35ms step at 10ms ticks", missed)
	}
	if testutil.CollectAndCount(m.Duration) != 1 {
		t.Fatal("tick duration histogram not collected")
	}
}
