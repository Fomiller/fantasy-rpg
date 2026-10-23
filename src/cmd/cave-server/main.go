package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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

	world := sim.NewWorld(sim.DefaultConfig(), sim.TestRoomWalls())
	world.AddEnemy(sim.Vec2{X: 0, Y: -150}, 30)

	loop := &tick.Loop{
		Interval: time.Second / time.Duration(rate),
		Metrics:  tick.NewMetrics(reg, "cave-server"),
		Step:     func(dt time.Duration) { world.Step(dt) },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: ":8080", Handler: mux}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	slog.Info("cave-server listening", "addr", srv.Addr, "tick_rate", rate)
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
