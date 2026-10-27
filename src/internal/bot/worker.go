package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Assignment is the bot director's answer to a worker: the slots this pod
// runs. Slot n is the bot named bot-<prefix>-<n>.
type Assignment struct {
	Slots []int `json:"slots"`
}

// Worker runs one bot per slot the bot director gives its pod, and grows or
// shrinks to match each answer.
type Worker struct {
	// Director is the bot director's base URL.
	Director string
	Pod      string
	Prefix   string
	Bot      Config
	Metrics  *Metrics
	Log      *slog.Logger
	// Every is how often the worker asks for its share.
	Every time.Duration
	// Grace is how long the worker keeps its last share with no answer.
	// After that it stops every bot.
	Grace time.Duration
	// Ramp caps the bots started per poll, so a big share does not join at
	// once.
	Ramp int

	client *http.Client
	mu     sync.Mutex
	bots   map[int]*running
}

type running struct {
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

// Run polls until ctx is done, then stops every bot and waits for each to
// close its socket.
func (w *Worker) Run(ctx context.Context) {
	w.client = &http.Client{Timeout: w.Every}
	if w.Log == nil {
		w.Log = slog.Default()
	}
	w.mu.Lock()
	w.bots = map[int]*running{}
	w.mu.Unlock()
	bots, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	lastOK := time.Now()
	tick := time.NewTicker(w.Every)
	defer tick.Stop()
	for {
		a, err := w.poll(ctx)
		switch {
		case err == nil:
			lastOK = time.Now()
			w.set(bots, a.Slots)
		case ctx.Err() == nil:
			w.Metrics.assignmentErrors.Inc()
			w.Log.Warn("bot director", "err", err)
			if time.Since(lastOK) > w.Grace {
				w.set(bots, nil)
			}
		}
		select {
		case <-ctx.Done():
			w.set(bots, nil)
			w.wait()
			return
		case <-tick.C:
		}
	}
}

func (w *Worker) poll(ctx context.Context) (Assignment, error) {
	have := w.have()
	parts := make([]string, len(have))
	for i, s := range have {
		parts[i] = strconv.Itoa(s)
	}
	q := url.Values{"pod": {w.Pod}, "have": {strings.Join(parts, ",")}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.Director+"/v1/assignment?"+q.Encode(), nil)
	if err != nil {
		return Assignment{}, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Assignment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Assignment{}, fmt.Errorf("assignment: %s", resp.Status)
	}
	var a Assignment
	return a, json.NewDecoder(resp.Body).Decode(&a)
}

// set stops the bots not in slots and starts up to Ramp of the missing ones.
// A slot still stopping is not started again until its bot has closed, so
// one name never has two sockets.
func (w *Worker) set(ctx context.Context, slots []int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Metrics.assigned.Set(float64(len(slots)))
	for slot, r := range w.bots {
		select {
		case <-r.done:
			delete(w.bots, slot)
			continue
		default:
		}
		if !r.stopping && !slices.Contains(slots, slot) {
			r.stopping = true
			r.cancel()
		}
	}
	started := 0
	for _, slot := range slots {
		if _, ok := w.bots[slot]; ok || started >= w.Ramp {
			continue
		}
		started++
		bctx, cancel := context.WithCancel(ctx)
		r := &running{cancel: cancel, done: make(chan struct{})}
		w.bots[slot] = r
		b := New(fmt.Sprintf("bot-%s-%d", w.Prefix, slot), w.Bot, w.Metrics)
		b.log = w.Log
		go func() {
			defer close(r.done)
			b.Run(bctx)
		}()
	}
}

// have is every slot with a bot that has not closed yet, stopping ones too,
// so the director hands a slot on only once it is free.
func (w *Worker) have() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []int
	for slot, r := range w.bots {
		select {
		case <-r.done:
		default:
			out = append(out, slot)
		}
	}
	slices.Sort(out)
	return out
}

// Running is the bots playing that are not on their way out.
func (w *Worker) Running() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, r := range w.bots {
		if !r.stopping {
			n++
		}
	}
	return n
}

func (w *Worker) wait() {
	w.mu.Lock()
	bots := make([]*running, 0, len(w.bots))
	for _, r := range w.bots {
		bots = append(bots, r)
	}
	w.mu.Unlock()
	for _, r := range bots {
		<-r.done
	}
}
