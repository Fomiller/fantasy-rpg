package manager

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	startup       prometheus.Histogram
	allocation    *prometheus.HistogramVec
	routingErrors *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		startup: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "instance_startup_seconds",
			Help:    "Time from GameServer create to Ready.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 9),
		}),
		allocation: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "instance_allocation_seconds",
			Help:    "Time to answer /v1/allocate, by result: reused, ready, full or error.",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		routingErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "instance_routing_errors_total",
			Help: "Allocations and routes that failed, by reason.",
		}, []string{"reason"}),
	}
	reg.MustRegister(m.startup, m.allocation, m.routingErrors)
	return m
}
