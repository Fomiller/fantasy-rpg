package main

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/logging"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
	"github.com/Fomiller/fantasy-rpg/src/internal/towns"
)

func main() {
	logging.New("world-server")
	if err := run(); err != nil {
		slog.Error("world-server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rate, err := positive("TICK_RATE", 20)
	if err != nil {
		return err
	}
	townMax, err := positive("TOWN_MAX_PLAYERS", 32)
	if err != nil {
		return err
	}
	softPercent, err := positive("TOWN_SOFT_CAP_PERCENT", 75)
	if err != nil {
		return err
	}
	key, err := jointoken.Key(os.Getenv("JOIN_TOKEN_KEY"))
	if err != nil {
		return err
	}
	managerURL := os.Getenv("INSTANCE_MANAGER_URL")
	list, err := server.ParseInstances(os.Getenv("INSTANCES"))
	if err != nil {
		return err
	}
	if managerURL == "" && len(list) == 0 {
		slog.Warn("INSTANCES is empty, so the town's entrance is always full")
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	store, storeName, closeStore, err := httpstore.Open(ctx, os.Getenv("INVENTORY_URL"), os.Getenv("DATABASE_URL"), os.Getenv("STORE"), reg)
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
	// The pod name in the cluster. Two world servers on one host need their
	// own WORLD_ID, since it is the shard in their session claims.
	worldID := os.Getenv("WORLD_ID")
	if worldID == "" {
		worldID, _ = os.Hostname()
	}

	lobby := server.NewLobby(caves.Town(), sim.DefaultConfig(), items.NewRoller(items.Default(), rand.Uint64()), slog.Default())
	lobby.TickRate = rate
	lobby.TownMaxPlayers = townMax
	lobby.Store = store
	lobby.JoinKey = key
	lobby.Instances = server.NewPool(managerURL, list)
	lobby.Events = bus
	lobby.WorldID = worldID
	reg.MustRegister(lobby.PendingSaves)
	reg.MustRegister(lobby.Metrics.Collectors()...)
	defer lobby.Close()

	townDir, dirName := towns.Open(os.Getenv("VALKEY_ADDR"), towns.NewCaps(townMax, softPercent), reg)
	defer townDir.Close()
	var shard *towns.Shard
	if dirName != "off" {
		shard = &towns.Shard{
			Dir:      townDir,
			ID:       worldID,
			URL:      townURL(),
			Revision: os.Getenv("TOWN_REVISION"),
			Started:  time.Now(),
			Players:  lobby.TownPlayers,
			Log:      slog.Default(),
		}
		lobby.Towns = shard
	}
	slog.Info("town directory", "directory", dirName, "town", worldID)

	loop := &tick.Loop{
		Interval: time.Second / time.Duration(rate),
		Metrics:  tick.NewMetrics(reg, "world-server"),
		Step:     lobby.Step,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /ws", lobby.HandleWS)
	// For the bot director. The public route is /ws only.
	mux.HandleFunc("GET /v1/players", lobby.HandlePlayers)
	srv := &http.Server{
		Addr:    ":" + port(),
		Handler: mux,
		// Shutdown does not reach hijacked WebSocket connections. Tying request
		// contexts to the signal closes them when the server stops.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() { errc <- loop.Run(ctx) }()
	go lobby.RunHeartbeat(ctx)
	if shard != nil {
		go shard.Run(ctx)
	}
	if busName == "kafka" {
		applied, err := events.NewConsumer(events.ConsumerConfig{
			Brokers: strings.Split(os.Getenv("KAFKA_BROKERS"), ","),
			Topics:  []string{events.TopicInventoryApplied},
		}, slog.Default())
		if err != nil {
			return err
		}
		defer applied.Close()
		go applied.Run(ctx, lobby.HandleApplied)
	}
	if dir, ok := lobby.Instances.(*server.Directory); ok {
		go dir.Run(ctx, pollEvery)
	}
	slog.Info("world-server listening", "addr", srv.Addr, "instances", len(list), "instance_manager", managerURL, "tick_rate", rate, "town_max_players", townMax)

	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shard != nil {
		shard.Drain(shutdownCtx)
	}
	return srv.Shutdown(shutdownCtx)
}

// pollEvery is how often each instance's /status is read.
const pollEvery = 2 * time.Second

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

// townURL is where a front door sends a player to reach this shard.
func townURL() string {
	if u := os.Getenv("TOWN_URL"); u != "" {
		return u
	}
	return "ws://127.0.0.1:" + port() + "/ws"
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
