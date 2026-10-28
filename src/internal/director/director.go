package director

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	// Default is the target until someone sets one.
	Default Target
	// PerPod is the bots one worker pod runs at most. Replicas are
	// ceil(desired / PerPod).
	PerPod int
	// MaxWorkers caps the replicas, so no mode runs more than
	// MaxWorkers * PerPod bots.
	MaxWorkers int
	Fill       FillRule
	// Every is how often the director counts players and rescales.
	Every time.Duration
	// PodTTL frees a worker's slots once it has not polled for this long.
	PodTTL time.Duration
}

// Status is what GET /v1/status says.
type Status struct {
	Target   Target `json:"target"`
	Desired  int    `json:"desired"`
	Replicas int    `json:"replicas"`
	Workers  int    `json:"workers"`
	Humans   int    `json:"humans"`
	Bots     int    `json:"bots"`
	// Counted is when the players were last counted. Zero means never.
	Counted time.Time `json:"counted"`
}

type Director struct {
	cfg     Config
	store   Store
	scaler  Scaler
	source  Source
	metrics *Metrics
	log     *slog.Logger
	kick    chan struct{}

	mu      sync.Mutex
	target  Target
	fill    FillRule
	assign  *Assigner
	desired int
	status  Status
	// started is set once the first count has seeded fill mode.
	started bool
}

func New(cfg Config, store Store, scaler Scaler, source Source, m *Metrics, log *slog.Logger) *Director {
	return &Director{
		cfg: cfg, store: store, scaler: scaler, source: source, metrics: m, log: log,
		kick:   make(chan struct{}, 1),
		target: cfg.Default,
		fill:   cfg.Fill,
		assign: NewAssigner(cfg.PerPod, cfg.PodTTL),
	}
}

// Load reads the saved target, if there is one.
func (d *Director) Load(ctx context.Context) error {
	t, ok, err := d.store.Load(ctx)
	if err != nil || !ok {
		return err
	}
	d.mu.Lock()
	d.target = t
	d.mu.Unlock()
	return nil
}

// Run counts and rescales every Every, and at once after a PUT.
func (d *Director) Run(ctx context.Context) {
	tick := time.NewTicker(d.cfg.Every)
	defer tick.Stop()
	for {
		d.Step(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.kick:
		}
	}
}

func (d *Director) maxBots() int {
	return d.cfg.MaxWorkers * d.cfg.PerPod
}

// Step is one round: count, decide, split, scale.
func (d *Director) Step(ctx context.Context, now time.Time) {
	p, err := d.source.Players(ctx)
	if err != nil {
		d.metrics.errors.WithLabelValues("players").Inc()
		d.log.Warn("count players", "err", err)
	}

	d.mu.Lock()
	if err == nil {
		d.status.Humans, d.status.Bots, d.status.Counted = p.Humans, p.Bots, now
		if !d.started {
			// After a restart, start from the bots already playing.
			d.started = true
			d.desired = p.Bots
			d.fill.Reset(p.Bots)
		}
	}
	switch d.target.Mode {
	case Fixed:
		d.desired = min(d.target.Count, d.maxBots())
	case Fill:
		// Without a count, hold what runs now.
		if err == nil {
			d.fill.Max = min(d.cfg.Fill.Max, d.maxBots())
			d.desired = d.fill.Next(d.target.Count, p.Humans, now)
		}
	default:
		d.desired = 0
	}
	d.assign.Balance(d.desired, now)
	replicas := (d.desired + d.cfg.PerPod - 1) / d.cfg.PerPod
	d.status.Target, d.status.Desired, d.status.Workers = d.target, d.desired, d.assign.Pods()
	d.metrics.observe(d.status)
	d.mu.Unlock()

	if err := d.scaler.Scale(ctx, replicas); err != nil {
		d.metrics.errors.WithLabelValues("scale").Inc()
		d.log.Error("scale workers", "replicas", replicas, "err", err)
		return
	}
	d.mu.Lock()
	d.status.Replicas = replicas
	d.mu.Unlock()
	d.metrics.replicas.Set(float64(replicas))
}

func (m *Metrics) observe(s Status) {
	m.target.Set(float64(s.Target.Count))
	if s.Target.Mode == Off {
		m.target.Set(0)
	}
	for _, mode := range []Mode{Off, Fill, Fixed} {
		v := 0.0
		if mode == s.Target.Mode {
			v = 1
		}
		m.mode.WithLabelValues(string(mode)).Set(v)
	}
	m.desired.Set(float64(s.Desired))
	m.humans.Set(float64(s.Humans))
	m.bots.Set(float64(s.Bots))
	m.workers.Set(float64(s.Workers))
}

// SetTarget saves t, then applies it. A target that does not save is not
// applied, so a restart never goes back on what the caller was told.
func (d *Director) SetTarget(ctx context.Context, t Target) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := d.store.Save(ctx, t); err != nil {
		d.metrics.errors.WithLabelValues("store").Inc()
		return err
	}
	d.mu.Lock()
	if t.Mode == Fill && d.target.Mode != Fill {
		d.fill.Reset(d.desired)
	}
	d.target = t
	d.mu.Unlock()
	select {
	case d.kick <- struct{}{}:
	default:
	}
	return nil
}

func (d *Director) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.status
	s.Target = d.target
	return s
}

// Assignment is pod's share. have is the slots it still runs bots for.
func (d *Director) Assignment(pod string, have []int, now time.Time) []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.assign.Poll(pod, have, now)
	d.assign.Balance(d.desired, now)
	return d.assign.Slots(pod)
}

// Register adds the director's API to mux. It is for the cluster only: the
// Service is ClusterIP and no route points at it.
func (d *Director) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/target", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, d.Status().Target)
	})
	mux.HandleFunc("PUT /v1/target", d.handleSet)
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, d.Status())
	})
	mux.HandleFunc("GET /v1/assignment", d.handleAssignment)
}

type errorBody struct {
	Error string `json:"error"`
}

func (d *Director) handleSet(w http.ResponseWriter, r *http.Request) {
	var t Target
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&t); err != nil {
		reply(w, http.StatusBadRequest, errorBody{"want {mode, count}"})
		return
	}
	if err := t.Validate(); err != nil {
		reply(w, http.StatusBadRequest, errorBody{err.Error()})
		return
	}
	if err := d.SetTarget(r.Context(), t); err != nil {
		d.log.Error("set target", "target", t, "err", err)
		reply(w, http.StatusServiceUnavailable, errorBody{"not saved"})
		return
	}
	d.log.Info("target set", "mode", t.Mode, "count", t.Count)
	reply(w, http.StatusOK, t)
}

func (d *Director) handleAssignment(w http.ResponseWriter, r *http.Request) {
	pod := r.URL.Query().Get("pod")
	if pod == "" {
		reply(w, http.StatusBadRequest, errorBody{"want pod"})
		return
	}
	var have []int
	for _, s := range strings.Split(r.URL.Query().Get("have"), ",") {
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			reply(w, http.StatusBadRequest, errorBody{"have is slot numbers"})
			return
		}
		have = append(have, n)
	}
	reply(w, http.StatusOK, map[string][]int{"slots": d.Assignment(pod, have, time.Now())})
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
