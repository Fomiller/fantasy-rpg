package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func newRelay(t *testing.T, url string, bus events.Sender) *Relay {
	t.Helper()
	r, err := NewRelay(t.Context(), url, bus, prometheus.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func outboxRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// endRuns plays one run per id for ann and returns her store.
func endRuns(t *testing.T, url string, runs ...string) *Store {
	t.Helper()
	ctx := t.Context()
	s := newStore(t, url)
	c := inventory.Character{Name: "ann", Class: "warrior", Stash: []items.Instance{sword()}}
	if err := s.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if _, err := s.StartRun(ctx, "ann", "warrior", run, "instance-0@3f9a1c"); err != nil {
			t.Fatal(err)
		}
		c.XP += 10
		if err := s.EndRun(ctx, run, c); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestEndRunWritesInventoryApplied(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url := newDBURL(t, true)
	const run = "6f1c2b3a-0000-4000-8000-000000000001"
	s := endRuns(t, url, run)
	// A retry and a lobby move write nothing to the outbox.
	if err := s.EndRun(ctx, run, inventory.Character{Name: "ann", Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, nil); err != nil {
		t.Fatal(err)
	}
	if n := outboxRows(t, s); n != 1 {
		t.Fatalf("outbox has %d rows, want the one run end", n)
	}

	bus := events.NewMemory()
	r := newRelay(t, url, bus)
	if n, err := r.flush(ctx); n != 1 || err != nil {
		t.Fatalf("flush = %d, %v", n, err)
	}
	r.count(ctx)
	if n := outboxRows(t, s); n != 0 {
		t.Fatalf("outbox has %d rows after the relay", n)
	}
	if v := testutil.ToFloat64(r.pending); v != 0 {
		t.Fatalf("outbox_pending = %v", v)
	}
	recs := bus.Records(events.TopicInventoryApplied)
	if len(recs) != 1 || recs[0].Key != "ann" {
		t.Fatalf("records = %+v", recs)
	}
	var got eventsv1.InventoryApplied
	if err := proto.Unmarshal(recs[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	// Create starts the account at 0, so the run end makes it 1.
	if got.GetVersion() != events.Version || got.GetAccount() != "ann" || got.GetRunId() != run || got.GetStashVersion() != 1 || got.GetTime() == nil {
		t.Fatalf("inventory.applied = %v", &got)
	}
}

// failAfter acks n sends and then fails every one.
type failAfter struct {
	*events.Memory
	n int
}

func (f *failAfter) Send(ctx context.Context, topic, key string, payload []byte) error {
	if f.n == 0 {
		return errors.New("broker down")
	}
	f.n--
	return f.Memory.Send(ctx, topic, key, payload)
}

func TestRelayKeepsWhatWasNotAcked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url := newDBURL(t, true)
	runs := []string{"6f1c2b3a-0000-4000-8000-000000000001", "6f1c2b3a-0000-4000-8000-000000000002", "6f1c2b3a-0000-4000-8000-000000000003"}
	s := endRuns(t, url, runs...)

	bus := &failAfter{Memory: events.NewMemory(), n: 1}
	r := newRelay(t, url, bus)
	if n, err := r.flush(ctx); n != 1 || err == nil {
		t.Fatalf("flush = %d, %v, want one sent and the error", n, err)
	}
	r.count(ctx)
	if v := testutil.ToFloat64(r.pending); v != 2 {
		t.Fatalf("outbox_pending = %v, want 2", v)
	}

	bus.n = 10
	if n, err := r.flush(ctx); n != 2 || err != nil {
		t.Fatalf("flush = %d, %v", n, err)
	}
	if n := outboxRows(t, s); n != 0 {
		t.Fatalf("outbox has %d rows", n)
	}
	var order []string
	for _, rec := range bus.Records(events.TopicInventoryApplied) {
		var msg eventsv1.InventoryApplied
		if err := proto.Unmarshal(rec.Payload, &msg); err != nil {
			t.Fatal(err)
		}
		order = append(order, msg.GetRunId())
	}
	if len(order) != 3 || order[0] != runs[0] || order[1] != runs[1] || order[2] != runs[2] {
		t.Fatalf("sent %v, want the runs in order", order)
	}
}

func TestRelayRunStopsWithTheContext(t *testing.T) {
	t.Parallel()
	url := newDBURL(t, true)
	s := endRuns(t, url, "6f1c2b3a-0000-4000-8000-000000000001")
	bus := events.NewMemory()
	r := newRelay(t, url, bus)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	for outboxRows(t, s) > 0 {
		select {
		case <-done:
			t.Fatal("run returned early")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if n := len(bus.Records(events.TopicInventoryApplied)); n != 1 {
		t.Fatalf("relayed %d records, want 1", n)
	}
}

// gate holds every send until open is closed. started closes on the first.
type gate struct {
	*events.Memory
	started chan struct{}
	open    chan struct{}
	once    sync.Once
}

func (g *gate) Send(ctx context.Context, topic, key string, payload []byte) error {
	g.once.Do(func() { close(g.started) })
	select {
	case <-g.open:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.Memory.Send(ctx, topic, key, payload)
}

func runIDs(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("6f1c2b3a-0000-4000-8000-%012d", i))
	}
	return out
}

func sentRuns(t *testing.T, bus *events.Memory) []string {
	t.Helper()
	var out []string
	for _, rec := range bus.Records(events.TopicInventoryApplied) {
		var msg eventsv1.InventoryApplied
		if err := proto.Unmarshal(rec.Payload, &msg); err != nil {
			t.Fatal(err)
		}
		out = append(out, msg.GetRunId())
	}
	return out
}

// Two inventory replicas each run a relay on one outbox. Each row goes out
// once, in id order.
func TestTwoRelaysSendOnceInOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url := newDBURL(t, true)
	runs := runIDs(1, 20)
	s := endRuns(t, url, runs...)
	bus := &gate{Memory: events.NewMemory(), started: make(chan struct{}), open: make(chan struct{})}
	a, b := newRelay(t, url, bus), newRelay(t, url, bus)

	type result struct {
		n   int
		err error
	}
	first := make(chan result, 1)
	go func() {
		n, err := a.flush(ctx)
		first <- result{n, err}
	}()
	<-bus.started
	// Without the lock, the second relay would wait on FOR UPDATE.
	skipCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if n, err := b.flush(skipCtx); n != 0 || err != nil {
		t.Fatalf("second relay flush = %d, %v, want a skip while the first holds the lock", n, err)
	}
	close(bus.open)
	if r := <-first; r.n != len(runs) || r.err != nil {
		t.Fatalf("first relay flush = %d, %v", r.n, r.err)
	}

	a.every, b.every = time.Millisecond, time.Millisecond
	runCtx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, r := range []*Relay{a, b} {
		wg.Go(func() { r.Run(runCtx) })
	}
	more := runIDs(21, 60)
	c := inventory.Character{Name: "ann", Class: "warrior", Stash: []items.Instance{sword()}}
	for _, run := range more {
		if _, err := s.StartRun(ctx, "ann", "warrior", run, "instance-0@3f9a1c"); err != nil {
			t.Fatal(err)
		}
		c.XP += 10
		if err := s.EndRun(ctx, run, c); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(10 * time.Second)
	for outboxRows(t, s) > 0 {
		select {
		case <-deadline:
			t.Fatal("the relays did not empty the outbox")
		case <-time.After(5 * time.Millisecond):
		}
	}
	stop()
	wg.Wait()

	want := append(runs, more...)
	got := sentRuns(t, bus.Memory)
	if len(got) != len(want) {
		t.Fatalf("sent %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d is run %s, want %s", i, got[i], want[i])
		}
	}
}
