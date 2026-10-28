package chat

import "github.com/prometheus/client_golang/prometheus"

const (
	DropRateLimited = "rate_limited"
	DropUnavailable = "unavailable"
	DropNotOnline   = "not_online"
	DropNotJoined   = "not_joined"
	DropBadCommand  = "bad_command"
	DropNoReply     = "no_reply"
	DropEmpty       = "empty"
)

type Metrics struct {
	Messages *prometheus.CounterVec
	Drops    *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	return &Metrics{
		Messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "chat_messages_total",
			Help: "Chat lines sent by players on this server, by channel.",
		}, []string{"channel"}),
		Drops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "chat_drops_total",
			Help: "Chat lines not delivered, by reason.",
		}, []string{"reason"}),
	}
}

func (m *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.Messages, m.Drops}
}
