// Command bot-director decides how many bots run and hands each bot worker
// its share. `bot-director status` and `bot-director set <mode> [count]`
// talk to a director on localhost, for `kubectl exec`.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/Fomiller/fantasy-rpg/src/internal/director"
	"github.com/Fomiller/fantasy-rpg/src/internal/logging"
)

func main() {
	logger := logging.New("bot-director")
	// client-go logs through klog, which would otherwise print its own plain lines.
	klog.SetSlogLogger(logger)
	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "status":
		err = call(http.MethodGet, "/v1/status", nil)
	case len(os.Args) > 1 && os.Args[1] == "set":
		err = set(os.Args[2:])
	case len(os.Args) > 1:
		err = errors.New("usage: bot-director [status | set <off|fill|fixed> [count]]")
	default:
		err = run()
	}
	if err != nil {
		slog.Error("bot-director stopped", "err", err)
		os.Exit(1)
	}
}

func port() string { return cmp.Or(os.Getenv("PORT"), "8080") }

func set(args []string) error {
	if len(args) == 0 || len(args) > 2 {
		return errors.New("usage: bot-director set <off|fill|fixed> [count]")
	}
	t := director.Target{Mode: director.Mode(args[0])}
	if len(args) == 2 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("count %q is not a number", args[1])
		}
		t.Count = n
	}
	if err := t.Validate(); err != nil {
		return err
	}
	body, _ := json.Marshal(t)
	return call(http.MethodPut, "/v1/target", body)
}

func call(method, path string, body []byte) error {
	req, err := http.NewRequest(method, "http://localhost:"+port()+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	os.Stdout.Write(b)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return nil
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config()
	if err != nil {
		return err
	}
	urls := strings.FieldsFunc(os.Getenv("PLAYER_URLS"), func(r rune) bool { return r == ',' })
	if len(urls) == 0 {
		return errors.New("PLAYER_URLS must list the world server and instance manager, like http://world:8080,http://instance-manager:8080")
	}

	players := director.NewEndpoints(urls)
	players.Optional = strings.FieldsFunc(os.Getenv("OPTIONAL_PLAYER_URLS"), func(r rune) bool { return r == ',' })

	namespace := cmp.Or(os.Getenv("NAMESPACE"), "fantasy-rpg")
	var store director.Store = &director.Memory{}
	var scaler director.Scaler = director.NoScaler{}
	workers, configMap := os.Getenv("WORKER_DEPLOYMENT"), os.Getenv("TARGET_CONFIGMAP")
	if workers != "" || configMap != "" {
		// In the cluster both are empty, which means the pod's service account.
		rest, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
		if err != nil {
			return err
		}
		kube, err := kubernetes.NewForConfig(rest)
		if err != nil {
			return err
		}
		if workers != "" {
			scaler = director.Deployment{Client: kube, Namespace: namespace, Name: workers}
		}
		if configMap != "" {
			store = director.ConfigMap{Client: kube, Namespace: namespace, Name: configMap}
		}
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	d := director.New(cfg, store, scaler, players, director.NewMetrics(reg), slog.Default())
	if err := d.Load(ctx); err != nil {
		return fmt.Errorf("load target: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	d.Register(mux)
	srv := &http.Server{Addr: ":" + port(), Handler: mux}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go d.Run(ctx)
	t := d.Status().Target
	slog.Info("bot-director listening", "addr", srv.Addr, "mode", t.Mode, "count", t.Count, "per_pod", cfg.PerPod, "max_workers", cfg.MaxWorkers, "workers", workers, "players", urls)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func config() (director.Config, error) {
	cfg := director.Config{Default: director.Target{Mode: director.Mode(cmp.Or(os.Getenv("DEFAULT_MODE"), "off"))}}
	var errs []error
	num := func(name string, fallback int) int {
		v := os.Getenv(name)
		if v == "" {
			return fallback
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			errs = append(errs, errors.New(name+" must be a whole number"))
		}
		return n
	}
	dur := func(name string, fallback time.Duration) time.Duration {
		v := os.Getenv(name)
		if v == "" {
			return fallback
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			errs = append(errs, errors.New(name+" must be a duration, like 30s"))
		}
		return d
	}
	cfg.Default.Count = num("DEFAULT_COUNT", 0)
	cfg.PerPod = max(1, num("BOTS_PER_POD", 25))
	cfg.MaxWorkers = num("MAX_WORKERS", 4)
	cfg.Fill = director.FillRule{
		Max:       num("FILL_MAX_BOTS", 16),
		Slack:     num("FILL_SLACK", 2),
		Step:      num("FILL_STEP", 2),
		UpDelay:   dur("FILL_UP_DELAY", 10*time.Second),
		DownDelay: dur("FILL_DOWN_DELAY", time.Minute),
	}
	cfg.Every = max(time.Second, dur("INTERVAL", 5*time.Second))
	cfg.PodTTL = dur("WORKER_TTL", 15*time.Second)
	if err := cfg.Default.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("DEFAULT_MODE and DEFAULT_COUNT: %w", err))
	}
	return cfg, errors.Join(errs...)
}
