// Command bot plays the game headless over the real protocol, for load
// tests. Each bot joins the town, walks into the entrance, plays a run in the
// cave and comes back, over and over.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("bot stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg config
	flag.StringVar(&cfg.url, "url", "ws://127.0.0.1:8080/ws", "world server WebSocket URL")
	flag.StringVar(&cfg.via, "via", "", "dial this scheme and host instead, for every URL, keeping the Host header")
	n := flag.Int("n", 1, "bots in this process")
	ramp := flag.Float64("ramp", 5, "bots started per second")
	prefix := flag.String("name-prefix", cmp.Or(os.Getenv("JOB_COMPLETION_INDEX"), "0"), "bots are named bot-<prefix>-<n>")
	duration := flag.Duration("duration", 0, "stop after this long, 0 to run until a signal")
	metricsAddr := flag.String("metrics-addr", ":9090", "where /metrics is served, empty for nowhere")
	flag.DurationVar(&cfg.townTime, "town-time", 5*time.Second, "time to walk the town before the entrance")
	flag.BoolVar(&cfg.fight, "fight", true, "attack enemies in reach on the way to the exit")
	flag.DurationVar(&cfg.runTimeout, "run-timeout", 3*time.Minute, "give up on a run that has not ended by then")
	flag.Parse()
	if *n <= 0 || *ramp <= 0 {
		return errors.New("-n and -ramp must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := newMetrics(reg)
	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		srv := &http.Server{Addr: *metricsAddr, Handler: mux}
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics", "err", err)
			}
		}()
		defer srv.Close()
	}

	slog.Info("bots starting", "n", *n, "url", cfg.url, "via", cfg.via, "prefix", *prefix)
	start := time.NewTicker(time.Duration(float64(time.Second) / *ramp))
	defer start.Stop()
	var wg sync.WaitGroup
	for i := range *n {
		if i > 0 {
			select {
			case <-ctx.Done():
			case <-start.C:
			}
		}
		if ctx.Err() != nil {
			break
		}
		b := newBot(fmt.Sprintf("bot-%s-%d", *prefix, i), cfg, m)
		wg.Go(func() {
			m.active.Inc()
			defer m.active.Dec()
			b.run(ctx)
		})
	}
	wg.Wait()
	slog.Info("bots stopped")
	return nil
}

func newBot(name string, cfg config, m *metrics) *bot {
	return &bot{name: name, cfg: cfg, m: m, log: slog.Default(), rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
}
