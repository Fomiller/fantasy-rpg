// Command bot plays the game headless over the real protocol, for load
// tests. With -n it runs that many bots. With -director it is a bot worker:
// it runs the share of bots the bot director gives its pod.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Fomiller/fantasy-rpg/src/internal/bot"
	"github.com/Fomiller/fantasy-rpg/src/internal/logging"
)

func main() {
	logging.New("bot")
	if err := run(); err != nil {
		slog.Error("bot stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg bot.Config
	flag.StringVar(&cfg.URL, "url", "ws://127.0.0.1:8080/ws", "world server WebSocket URL")
	flag.StringVar(&cfg.Via, "via", "", "dial this scheme and host instead, for every URL, keeping the Host header")
	n := flag.Int("n", 1, "bots in this process")
	ramp := flag.Float64("ramp", 5, "bots started per second")
	prefix := flag.String("name-prefix", cmp.Or(os.Getenv("JOB_COMPLETION_INDEX"), "0"), "bots are named bot-<prefix>-<n>")
	duration := flag.Duration("duration", 0, "stop after this long, 0 to run until a signal")
	metricsAddr := flag.String("metrics-addr", ":9090", "where /metrics is served, empty for nowhere")
	flag.DurationVar(&cfg.TownTime, "town-time", 5*time.Second, "time to walk the town before the entrance")
	flag.BoolVar(&cfg.Fight, "fight", true, "attack enemies in reach on the way to the exit")
	flag.DurationVar(&cfg.RunTimeout, "run-timeout", 3*time.Minute, "give up on a run that has not ended by then")
	director := flag.String("director", "", "bot director URL. Set, the bot count comes from there and -n is ignored")
	pod := flag.String("pod", os.Getenv("POD_NAME"), "this worker's name to the bot director, the hostname when empty")
	every := flag.Duration("poll", 2*time.Second, "how often a worker asks the bot director for its share")
	grace := flag.Duration("grace", time.Minute, "how long a worker keeps its share when the bot director does not answer")
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
	m := bot.NewMetrics(reg)
	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := &http.Server{Addr: *metricsAddr, Handler: mux}
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics", "err", err)
			}
		}()
		defer srv.Close()
	}

	if *director != "" {
		if *pod == "" {
			*pod, _ = os.Hostname()
		}
		w := &bot.Worker{
			Director: *director, Pod: *pod, Prefix: *prefix, Bot: cfg, Metrics: m,
			Every: *every, Grace: *grace, Ramp: max(1, int(*ramp*every.Seconds())),
		}
		slog.Info("bot worker starting", "director", *director, "pod", *pod, "url", cfg.URL, "via", cfg.Via, "prefix", *prefix)
		w.Run(ctx)
		slog.Info("bot worker stopped")
		return nil
	}

	slog.Info("bots starting", "n", *n, "url", cfg.URL, "via", cfg.Via, "prefix", *prefix)
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
		b := bot.New(fmt.Sprintf("bot-%s-%d", *prefix, i), cfg, m)
		wg.Go(func() { b.Run(ctx) })
	}
	wg.Wait()
	slog.Info("bots stopped")
	return nil
}
