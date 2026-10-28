package bot

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

// serveGame runs a world server and one yard instance on one store, as
// `just net-test` does, and returns the world's URL.
func serveGame(t *testing.T) string {
	t.Helper()
	key := []byte("bot-test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := inventory.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	loop := func(step func(time.Duration)) {
		l := &tick.Loop{Interval: 50 * time.Millisecond, Metrics: tick.NewMetrics(prometheus.NewRegistry(), "test"), Step: step}
		go l.Run(ctx)
	}

	yard := caves.Yard()
	loot := items.NewRoller(items.Default(), 1)
	game := server.New(yard.NewWorld(sim.DefaultConfig(), loot), yard, loot, log)
	game.Store = store
	game.Tokens = jointoken.NewVerifier(key)
	loop(game.Step)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", game.HandleStatus)
	mux.HandleFunc("GET /instances/"+game.ID+"/ws", game.HandleWS)
	is := httptest.NewServer(mux)

	town := server.NewLobby(caves.Town(), sim.DefaultConfig(), items.NewRoller(items.Default(), 2), log)
	town.Store = store
	town.JoinKey = key
	dir := server.NewDirectory([]server.Instance{{ID: game.ID, Internal: is.URL, Public: wsURL(is.URL) + "/instances/" + game.ID + "/ws"}})
	dir.Poll(ctx)
	go dir.Run(ctx, 50*time.Millisecond)
	town.Instances = dir
	loop(town.Step)
	ws := httptest.NewServer(http.HandlerFunc(town.HandleWS))

	t.Cleanup(func() {
		ws.Close()
		is.Close()
		cancel()
		town.Close()
		game.Close()
	})
	return wsURL(ws.URL)
}

func wsURL(u string) string { return "ws" + strings.TrimPrefix(u, "http") }

// One bot goes round the whole loop: town, ticket, a looted chest, the
// exit, and back to a town whose stash has the loot.
func TestBotPlaysTheLoop(t *testing.T) {
	url := serveGame(t)
	m := NewMetrics(prometheus.NewRegistry())
	b := New("bot-test-0", Config{URL: url, TownTime: 500 * time.Millisecond, Fight: true, RunTimeout: 20 * time.Second}, m)
	b.log = slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(30 * time.Second)
	for b.runs.Load() < 1 || b.stash.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("after 30s: runs %d, stash %d, errors %v", b.runs.Load(), b.stash.Load(), errorCounts(m))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := testutil.ToFloat64(m.runs.WithLabelValues("extracted")); got != 1 {
		t.Fatalf("extracted runs = %v, want 1", got)
	}
	for name, c := range map[string]prometheus.Collector{"join": m.join, "ticket": m.ticket, "instance join": m.instanceJoin, "saving": m.saving} {
		if testutil.CollectAndCount(c) == 0 {
			t.Errorf("%s has no samples", name)
		}
	}
	if n := testutil.CollectAndCount(m.errors); n != 0 {
		t.Errorf("errors = %v, want none", errorCounts(m))
	}
}

func errorCounts(m *Metrics) string {
	var b strings.Builder
	for _, stage := range []string{"world_dial", "world_join", "town", "instance_dial", "instance_join", "run", "enter_full", "enter_saving", "enter_in_run"} {
		if v := testutil.ToFloat64(m.errors.WithLabelValues(stage)); v > 0 {
			b.WriteString(stage + " ")
		}
	}
	return b.String()
}

func TestRewriteKeepsTheHost(t *testing.T) {
	u, host, err := rewrite("wss://fantasy-rpg.fomiller.com/instances/i-0/ws", "http://traefik.kube-system:80")
	if err != nil || u != "ws://traefik.kube-system:80/instances/i-0/ws" || host != "fantasy-rpg.fomiller.com" {
		t.Fatalf("rewrite = %q %q %v", u, host, err)
	}
	if u, host, _ := rewrite("ws://127.0.0.1:8080/ws", ""); u != "ws://127.0.0.1:8080/ws" || host != "" {
		t.Fatalf("no via = %q %q", u, host)
	}
}

func TestPathTurnsOnlyAtCorners(t *testing.T) {
	c := caves.Town()
	layout := protocol.Joined(1, c, c.PlayerSpawns[0], 20, nil, nil).GetJoined().GetLayout()
	g := newGrid(layout)
	route := g.path(point{c.PlayerSpawns[0].X, c.PlayerSpawns[0].Y}, g.floors(layout.GetEntrance()))
	if len(route) < 2 || len(route) > 4 {
		t.Fatalf("route = %v, want a few turns", route)
	}
	last := route[len(route)-1]
	if !inRect(layout.GetEntrance(), last) {
		t.Fatalf("route ends at %v, outside the entrance", last)
	}
}
