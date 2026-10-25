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
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

func main() {
	if err := run(); err != nil {
		slog.Error("world-server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	key, err := jointoken.Key(os.Getenv("JOIN_TOKEN_KEY"))
	if err != nil {
		return err
	}
	list, err := server.ParseInstances(os.Getenv("INSTANCES"))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		slog.Warn("INSTANCES is empty, so every Enter Cave is full")
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	store, storeName, closeStore, err := db.OpenStore(ctx, os.Getenv("DATABASE_URL"), reg)
	if err != nil {
		return err
	}
	defer closeStore()
	slog.Info("character store", "store", storeName)

	// The world is only the game's rules, for lobby stats and gear checks. It
	// is never stepped.
	rules := sim.NewWorld(sim.DefaultConfig(), nil)
	lobby := server.NewLobby(rules, items.NewRoller(items.Default(), rand.Uint64()), slog.Default())
	lobby.Store = store
	lobby.JoinKey = key
	lobby.Instances = server.NewDirectory(list)
	reg.MustRegister(lobby.PendingSaves)
	defer lobby.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /ws", lobby.HandleWS)
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
	go lobby.Run(ctx)
	go lobby.Instances.Run(ctx, pollEvery)
	slog.Info("world-server listening", "addr", srv.Addr, "instances", len(list))

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// pollEvery is how often each instance's /status is read.
const pollEvery = 2 * time.Second

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
