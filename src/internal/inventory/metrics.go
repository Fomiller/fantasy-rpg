package inventory

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Metered times every call to s and counts the ones that fail.
func Metered(s Store, reg prometheus.Registerer) Store {
	m := &metered{
		store: s,
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "store_duration_seconds",
			Help:    "Time spent in one store call, including failed ones.",
			Buckets: []float64{.001, .002, .005, .01, .02, .05, .1, .25, .5, 1, 2},
		}, []string{"call"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "store_errors_total",
			Help: "Store calls that returned an error.",
		}, []string{"call"}),
	}
	reg.MustRegister(m.duration, m.errors)
	return m
}

type metered struct {
	store    Store
	duration *prometheus.HistogramVec
	errors   *prometheus.CounterVec
}

// observe takes err by pointer, since a deferred call reads its arguments early.
func (m *metered) observe(call string, start time.Time, err *error) {
	m.duration.WithLabelValues(call).Observe(time.Since(start).Seconds())
	if *err != nil {
		m.errors.WithLabelValues(call).Inc()
	}
}

func (m *metered) Load(ctx context.Context, name, class string) (c Character, ok bool, err error) {
	defer m.observe("load", time.Now(), &err)
	return m.store.Load(ctx, name, class)
}

func (m *metered) Create(ctx context.Context, c Character) (err error) {
	defer m.observe("create", time.Now(), &err)
	return m.store.Create(ctx, c)
}

func (m *metered) SaveLobby(ctx context.Context, name, class string, l Loadout, stash []items.Instance) (err error) {
	defer m.observe("save_lobby", time.Now(), &err)
	return m.store.SaveLobby(ctx, name, class, l, stash)
}

func (m *metered) EndRun(ctx context.Context, runID string, c Character) (err error) {
	defer m.observe("end_run", time.Now(), &err)
	return m.store.EndRun(ctx, runID, c)
}
