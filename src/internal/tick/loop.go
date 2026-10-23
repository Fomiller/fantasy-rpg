// Package tick runs a step function at a fixed rate and records how long each
// tick takes and how many ticks were missed.
package tick

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Duration prometheus.Histogram
	Missed   prometheus.Counter
	Ticks    prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer, server string) *Metrics {
	labels := prometheus.Labels{"server": server}
	m := &Metrics{
		Duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "game_tick_duration_seconds",
			Help:        "Time spent in one simulation step.",
			ConstLabels: labels,
			Buckets:     []float64{.0005, .001, .002, .005, .01, .02, .05, .1},
		}),
		Missed: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "game_ticks_missed_total",
			Help:        "Ticks skipped because a step overran its interval.",
			ConstLabels: labels,
		}),
		Ticks: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "game_ticks_total",
			Help:        "Simulation steps run.",
			ConstLabels: labels,
		}),
	}
	reg.MustRegister(m.Duration, m.Missed, m.Ticks)
	return m
}

type Loop struct {
	Interval time.Duration
	Step     func(dt time.Duration)
	Metrics  *Metrics
}

// Run steps until ctx is done. Every step gets the same dt so the simulation
// stays deterministic. When a step overruns, the ticks it covered are skipped
// and counted as missed rather than run back to back to catch up.
func (l *Loop) Run(ctx context.Context) error {
	next := time.Now()
	for {
		next = next.Add(l.Interval)

		start := time.Now()
		l.Step(l.Interval)
		l.Metrics.Duration.Observe(time.Since(start).Seconds())
		l.Metrics.Ticks.Inc()

		if behind := time.Since(next); behind > 0 {
			missed := behind / l.Interval
			l.Metrics.Missed.Add(float64(missed))
			next = next.Add(missed * l.Interval)
		}

		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
