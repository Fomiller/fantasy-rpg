package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
)

func main() {
	if err := run(); err != nil {
		slog.Error("inventory stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	url := os.Getenv("DATABASE_URL")
	store, storeName, closeStore, err := db.OpenStore(ctx, url, reg)
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

	// Rows the relay has not sent when it stops wait for the next start.
	if url != "" {
		relay, err := db.NewRelay(ctx, url, bus, reg, slog.Default())
		if err != nil {
			return err
		}
		defer relay.Close()
		relayCtx, stopRelay := context.WithCancel(ctx)
		relayDone := make(chan struct{})
		go func() { relay.Run(relayCtx); close(relayDone) }()
		defer func() { stopRelay(); <-relayDone }()
	}

	if busName == "kafka" {
		lag := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "inventory_consumer_lag",
			Help: "Records behind the high watermark, by topic and partition.",
		}, []string{"topic", "partition"})
		reg.MustRegister(lag)
		runs, err := events.NewConsumer(events.ConsumerConfig{
			Brokers: strings.Split(os.Getenv("KAFKA_BROKERS"), ","),
			Topics:  []string{events.TopicRunEnded},
			Group:   "inventory",
			Lag:     lag,
		}, slog.Default())
		if err != nil {
			return err
		}
		defer runs.Close()
		consumeCtx, stopConsume := context.WithCancel(ctx)
		consumeDone := make(chan struct{})
		go func() { runs.Run(consumeCtx, inventory.RunEndedHandler(store, slog.Default())); close(consumeDone) }()
		defer func() { stopConsume(); <-consumeDone }()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	httpstore.Register(mux, store, slog.Default())
	srv := &http.Server{Addr: ":" + cmp.Or(os.Getenv("PORT"), "8080"), Handler: mux}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	slog.Info("inventory listening", "addr", srv.Addr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
