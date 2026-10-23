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
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

func main() {
	if err := run(); err != nil {
		slog.Error("cave-server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rate, err := tickRate()
	if err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	cave := caves.Loop()
	world := cave.NewWorld(sim.DefaultConfig())
	seed, err := lootSeed()
	if err != nil {
		return err
	}
	game := server.New(world, cave, items.NewRoller(items.Default(), seed), slog.Default())
	defer game.Close()

	loop := &tick.Loop{
		Interval: time.Second / time.Duration(rate),
		Metrics:  tick.NewMetrics(reg, "cave-server"),
		Step:     game.Step,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /ws", game.HandleWS)
	srv := &http.Server{
		Addr:    ":8080",
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

	slog.Info("cave-server listening", "addr", srv.Addr, "tick_rate", rate, "cave", cave.Name, "loot_seed", seed)
	go func() { errc <- loop.Run(ctx) }()

	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func tickRate() (int, error) {
	v := os.Getenv("TICK_RATE")
	if v == "" {
		return 20, nil
	}
	rate, err := strconv.Atoi(v)
	if err != nil || rate <= 0 {
		return 0, errors.New("TICK_RATE must be a positive integer")
	}
	return rate, nil
}

// lootSeed comes from LOOT_SEED to replay a run, or is random. It is logged
// on start either way.
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
