package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/gameserver"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

func main() {
	cmd := run
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		cmd = runMigrate
	}
	if err := cmd(); err != nil {
		slog.Error("instance-server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Serving outlives the signal, so runs play on while the server drains.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rate, err := positive("TICK_RATE", 20)
	if err != nil {
		return err
	}
	key, err := jointoken.Key(os.Getenv("JOIN_TOKEN_KEY"))
	if err != nil {
		return err
	}
	maxPlayers, err := positive("MAX_PLAYERS", 8)
	if err != nil {
		return err
	}
	drainTimeout, err := duration("DRAIN_TIMEOUT", 600*time.Second)
	if err != nil {
		return err
	}
	cave, ok := caves.ByName(cmp.Or(os.Getenv("CAVE"), "loop"))
	if !ok {
		return fmt.Errorf("CAVE %q is not a cave", os.Getenv("CAVE"))
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	inventoryURL := os.Getenv("INVENTORY_URL")
	store, storeName, closeStore, err := httpstore.Open(ctx, inventoryURL, os.Getenv("DATABASE_URL"), reg)
	if err != nil {
		return err
	}
	defer closeStore()
	slog.Info("character store", "store", storeName)

	bus, busName, err := events.Open(os.Getenv("KAFKA_BROKERS"), reg, slog.Default())
	if err != nil {
		return err
	}
	defer bus.Close()
	slog.Info("events", "producer", busName)

	seed, err := lootSeed()
	if err != nil {
		return err
	}
	world, loot := newWorld(cave, seed)
	game := server.New(world, cave, loot, slog.Default())
	game.TickRate = rate
	game.Store = store
	game.MaxPlayers = maxPlayers
	game.Tokens = jointoken.NewVerifier(key)
	game.Events = bus
	// run.ended is consumed by the inventory service. Without it, or without
	// Redpanda, the result goes straight to the store as in local dev.
	results := "store"
	if busName == "kafka" && inventoryURL != "" {
		game.Results, results = server.EventResults{Sender: bus}, events.TopicRunEnded
	}
	slog.Info("run results", "to", results)
	if id := os.Getenv("INSTANCE_ID"); id != "" {
		game.ID = id
	}
	next := seed
	game.Reset = func() (*sim.World, *items.Roller) {
		next = nextSeed(next)
		slog.Info("new world", "loot_seed", next)
		return newWorld(cave, next)
	}
	reg.MustRegister(game.PendingSaves)
	reg.MustRegister(game.Metrics.Collectors()...)
	defer game.Close()

	if os.Getenv("AGONES_SDK_GRPC_PORT") != "" {
		if game.GameServer, err = gameserver.NewAgones(); err != nil {
			return err
		}
	}
	gs := game.GameServer
	if err := gs.SetAnnotation(gameserver.BootKey, game.Boot()); err != nil {
		return err
	}
	if err := gs.SetRunCapacity(int64(maxPlayers)); err != nil {
		return err
	}

	loop := &tick.Loop{
		Interval: time.Second / time.Duration(rate),
		Metrics:  tick.NewMetrics(reg, "instance-server"),
		Step:     game.Step,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /status", game.HandleStatus)
	// The public route is a path prefix per instance, so the socket lives
	// under it and Traefik needs no rewrite.
	mux.HandleFunc("GET /instances/"+game.ID+"/ws", game.HandleWS)
	srv := &http.Server{
		Addr:    ":" + cmp.Or(os.Getenv("PORT"), "8080"),
		Handler: mux,
		// Shutdown does not reach hijacked WebSocket connections. Tying request
		// contexts to the signal closes them when the server stops.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 2)
	go func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() { errc <- loop.Run(ctx) }()
	if err := gs.Ready(); err != nil {
		return err
	}
	slog.Info("instance-server listening", "addr", srv.Addr, "tick_rate", rate, "cave", cave.Name, "loot_seed", seed, "instance", game.Instance())

	select {
	case <-sig.Done():
	case err := <-errc:
		return err
	}
	// A second signal stops at once.
	stop()
	slog.Info("draining", "timeout", drainTimeout)
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), drainTimeout+runEndGrace)
	defer cancelDrain()
	game.Drain(drainCtx, drainTimeout)
	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdownCtx)
}

// runEndGrace is how long the last run ends get to land after the drain
// times out. With the drain, it fits in the pod's termination grace period.
const runEndGrace = 20 * time.Second

func runMigrate() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return migrate(ctx)
}

func positive(name string, fallback int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, errors.New(name + " must be a positive integer")
	}
	return n, nil
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, errors.New(name + " must be a positive duration, like 600s")
	}
	return d, nil
}

func newWorld(cave caves.Cave, seed uint64) (*sim.World, *items.Roller) {
	loot := items.NewRoller(items.Default(), seed)
	world := cave.NewWorld(sim.DefaultConfig(), loot)
	world.SeedAI(seed)
	return world, loot
}

// nextSeed steps a seed for the next world, so a replay with LOOT_SEED gets
// the same worlds in order.
func nextSeed(seed uint64) uint64 {
	return seed*6364136223846793005 + 1442695040888963407
}

// lootSeed comes from LOOT_SEED to replay a run, or is random. It seeds both
// loot and enemy AI, and is logged on start either way.
func lootSeed() (uint64, error) {
	if v := os.Getenv("LOOT_SEED"); v != "" {
		seed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, errors.New("LOOT_SEED must be a whole number")
		}
		return seed, nil
	}
	return rand.Uint64(), nil
}
