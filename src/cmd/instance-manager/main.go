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

	"agones.dev/agones/pkg/client/clientset/versioned"
	"agones.dev/agones/pkg/client/informers/externalversions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/Fomiller/fantasy-rpg/src/internal/logging"
	"github.com/Fomiller/fantasy-rpg/src/internal/manager"
)

const drainDelay = 5 * time.Second

func main() {
	logger := logging.New("instance-manager")
	// client-go logs through klog, which would otherwise print its own plain lines.
	klog.SetSlogLogger(logger)
	if err := run(); err != nil {
		slog.Error("instance-manager stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := manager.Config{
		Namespace:  cmp.Or(os.Getenv("NAMESPACE"), "fantasy-rpg"),
		Fleet:      cmp.Or(os.Getenv("FLEET"), "fantasy-rpg-instance"),
		PublicURL:  strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/"),
		Host:       os.Getenv("ROUTE_HOST"),
		EntryPoint: cmp.Or(os.Getenv("ROUTE_ENTRY_POINT"), "web"),
	}
	if cfg.PublicURL == "" {
		return errors.New("PUBLIC_URL must be set, like wss://fantasy-rpg.fomiller.com")
	}

	// In the cluster both are empty, which means the pod's service account.
	rest, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		return err
	}
	agones, err := versioned.NewForConfig(rest)
	if err != nil {
		return err
	}
	kube, err := kubernetes.NewForConfig(rest)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(rest)
	if err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := manager.New(cfg, agones, kube, dyn, manager.NewMetrics(reg), slog.Default())

	informers := externalversions.NewSharedInformerFactoryWithOptions(agones, 10*time.Minute, externalversions.WithNamespace(cfg.Namespace))
	if err := m.Watch(informers.Agones().V1().GameServers().Informer()); err != nil {
		return err
	}
	informers.Start(ctx.Done())
	defer informers.Shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	m.Register(mux)
	srv := &http.Server{Addr: ":" + cmp.Or(os.Getenv("PORT"), "8080"), Handler: mux}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	slog.Info("instance-manager listening", "addr", srv.Addr, "namespace", cfg.Namespace, "fleet", cfg.Fleet)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}

	// The Service drops a terminating pod a moment after SIGTERM. Keep
	// answering until then, and close each connection after its reply so the
	// world moves to the other replica.
	srv.SetKeepAlivesEnabled(false)
	time.Sleep(drainDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
