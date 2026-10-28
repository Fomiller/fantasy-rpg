package bot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// fakeDirector answers every poll with slots, or 503 while down.
type fakeDirector struct {
	mu    sync.Mutex
	slots []int
	down  bool
	have  string
}

func (f *fakeDirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.have = r.URL.Query().Get("have")
	if f.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	json.NewEncoder(w).Encode(Assignment{Slots: f.slots})
}

func (f *fakeDirector) set(slots []int, down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.slots, f.down = slots, down
}

func waitRunning(t *testing.T, w *Worker, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for w.Running() != want {
		if time.Now().After(deadline) {
			t.Fatalf("running = %d, want %d", w.Running(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The worker grows by Ramp per poll, shrinks at once, keeps its share a
// while when the director is down, then stops every bot.
func TestWorkerFollowsItsShare(t *testing.T) {
	fd := &fakeDirector{slots: []int{0, 1, 2, 3, 4}}
	srv := httptest.NewServer(fd)
	defer srv.Close()
	w := &Worker{
		Director: srv.URL, Pod: "w-a", Prefix: "t",
		// Nothing listens there, so each bot just retries its dial.
		Bot:     Config{URL: "ws://127.0.0.1:1/ws", RunTimeout: time.Second},
		Metrics: NewMetrics(prometheus.NewRegistry()),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Every:   20 * time.Millisecond, Grace: 300 * time.Millisecond, Ramp: 2,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	time.Sleep(5 * time.Millisecond)
	if n := w.Running(); n > 2 {
		t.Fatalf("first poll started %d, want at most the ramp of 2", n)
	}
	waitRunning(t, w, 5)

	fd.set([]int{0, 1}, false)
	waitRunning(t, w, 2)
	deadline := time.Now().Add(5 * time.Second)
	for {
		fd.mu.Lock()
		have := fd.have
		fd.mu.Unlock()
		if have == "0,1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("have = %q, want the stopped bots gone", have)
		}
		time.Sleep(10 * time.Millisecond)
	}

	fd.set(nil, true)
	time.Sleep(100 * time.Millisecond)
	if n := w.Running(); n != 2 {
		t.Fatalf("director down briefly: running = %d, want 2", n)
	}
	waitRunning(t, w, 0)

	fd.set([]int{7}, false)
	waitRunning(t, w, 1)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if got := w.have(); len(got) != 0 {
		t.Fatalf("after stop, have = %v", got)
	}
}
