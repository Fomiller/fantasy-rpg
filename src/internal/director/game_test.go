package director

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/bot"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

type game struct {
	ws string
	// players are the world's and the instance's internal URLs.
	players []string
}

// serveGame runs a world server and one yard instance on one store, as the
// bot package's test does, with /v1/players on both.
func serveGame(t *testing.T) game {
	t.Helper()
	key := []byte("director-test")
	log := quiet()
	store := inventory.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	loop := func(step func(time.Duration)) {
		l := &tick.Loop{Interval: 50 * time.Millisecond, Metrics: tick.NewMetrics(prometheus.NewRegistry(), "test"), Step: step}
		go l.Run(ctx)
	}

	yard := caves.Yard()
	loot := items.NewRoller(items.Default(), 1)
	cave := server.New(yard.NewWorld(sim.DefaultConfig(), loot), yard, loot, log)
	cave.Store = store
	cave.Tokens = jointoken.NewVerifier(key)
	loop(cave.Step)
	imux := http.NewServeMux()
	imux.HandleFunc("GET /status", cave.HandleStatus)
	imux.HandleFunc("GET /v1/players", cave.HandlePlayers)
	imux.HandleFunc("GET /instances/"+cave.ID+"/ws", cave.HandleWS)
	is := httptest.NewServer(imux)

	town := server.NewLobby(caves.Town(), sim.DefaultConfig(), items.NewRoller(items.Default(), 2), log)
	town.Store = store
	town.JoinKey = key
	dir := server.NewDirectory([]server.Instance{{ID: cave.ID, Internal: is.URL, Public: wsURL(is.URL) + "/instances/" + cave.ID + "/ws"}})
	dir.Poll(ctx)
	go dir.Run(ctx, 50*time.Millisecond)
	town.Instances = dir
	loop(town.Step)
	wmux := http.NewServeMux()
	wmux.HandleFunc("GET /ws", town.HandleWS)
	wmux.HandleFunc("GET /v1/players", town.HandlePlayers)
	ws := httptest.NewServer(wmux)

	t.Cleanup(func() {
		ws.Close()
		is.Close()
		cancel()
		town.Close()
		cave.Close()
	})
	return game{ws: wsURL(ws.URL) + "/ws", players: []string{ws.URL, is.URL}}
}

func wsURL(u string) string { return "ws" + strings.TrimPrefix(u, "http") }

func waitStatus(t *testing.T, what string, ok func() bool, show func() any) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: after 30s, %+v", what, show())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The director and one worker against a real world: fill mode tops the
// game up to its target with bots, takes one away when a human joins, and
// puts it back when they leave.
func TestFillFollowsAHuman(t *testing.T) {
	g := serveGame(t)
	cfg := Config{
		Default:    Target{Mode: Fill, Count: 3},
		PerPod:     25,
		MaxWorkers: 1,
		Fill:       FillRule{Max: 10, Slack: 1, Step: 10, DownDelay: 300 * time.Millisecond},
		Every:      100 * time.Millisecond,
		PodTTL:     5 * time.Second,
	}
	sc := &fakeScaler{}
	d, _ := newDirector(cfg, &Memory{}, NewEndpoints(g.players), sc)
	mux := http.NewServeMux()
	d.Register(mux)
	ds := httptest.NewServer(mux)
	defer ds.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	bots := bot.Config{URL: g.ws, TownTime: 300 * time.Millisecond, Fight: true, RunTimeout: 20 * time.Second}
	w := &bot.Worker{
		Director: ds.URL, Pod: "worker-0", Prefix: "fill", Bot: bots,
		Metrics: bot.NewMetrics(prometheus.NewRegistry()), Log: quiet(),
		Every: 100 * time.Millisecond, Grace: 5 * time.Second, Ramp: 5,
	}
	workerDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(workerDone)
	}()
	show := func() any {
		return struct {
			Status
			Running int
		}{d.Status(), w.Running()}
	}

	waitStatus(t, "three bots", func() bool { return d.Status().Desired == 3 && w.Running() == 3 && d.Status().Bots > 0 }, show)
	if sc.get() != 1 {
		t.Fatalf("replicas = %d, want 1", sc.get())
	}

	human := bot.New("ann", bot.Config{URL: g.ws, TownTime: time.Minute, RunTimeout: 20 * time.Second, Human: true}, bot.NewMetrics(prometheus.NewRegistry()))
	hctx, leave := context.WithCancel(ctx)
	humanDone := make(chan struct{})
	go func() {
		human.Run(hctx)
		close(humanDone)
	}()
	waitStatus(t, "a human joins", func() bool { s := d.Status(); return s.Humans == 1 && s.Desired == 2 && w.Running() == 2 }, show)

	leave()
	<-humanDone
	waitStatus(t, "the human leaves", func() bool { s := d.Status(); return s.Humans == 0 && s.Desired == 3 && w.Running() == 3 }, show)

	if err := d.SetTarget(ctx, Target{Mode: Off}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, "off", func() bool { return w.Running() == 0 && sc.get() == 0 }, show)
	cancel()
	<-workerDone
}
