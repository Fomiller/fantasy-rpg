package server

import "github.com/prometheus/client_golang/prometheus"

// Metrics are what a world or instance server exports about its players and
// their sockets. Register them with Collectors.
type Metrics struct {
	Players       prometheus.Gauge
	SentBytes     prometheus.Counter
	ReceivedBytes prometheus.Counter
	SlowKicks     prometheus.Counter
}

func newMetrics(players, help string) *Metrics {
	return &Metrics{
		Players: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: players,
			Help: help,
		}),
		SentBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_sent_bytes_total",
			Help: "WebSocket message bytes written to clients.",
		}),
		ReceivedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_received_bytes_total",
			Help: "WebSocket message bytes read from clients.",
		}),
		SlowKicks: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_slow_kicks_total",
			Help: "Clients dropped because their outbox was full.",
		}),
	}
}

func (m *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.Players, m.SentBytes, m.ReceivedBytes, m.SlowKicks}
}
