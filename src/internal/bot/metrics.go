package bot

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	active       prometheus.Gauge
	connects     *prometheus.CounterVec
	join         prometheus.Histogram
	ticket       prometheus.Histogram
	instanceJoin prometheus.Histogram
	snapshotGap  *prometheus.HistogramVec
	saving       prometheus.Histogram
	runs         *prometheus.CounterVec
	errors       *prometheus.CounterVec
	messages     *prometheus.CounterVec
	// A worker's share from the bot director.
	assigned         prometheus.Gauge
	assignmentErrors prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	latency := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	return &Metrics{
		active:   f.NewGauge(prometheus.GaugeOpts{Name: "bot_active", Help: "Bots running in this process."}),
		assigned: f.NewGauge(prometheus.GaugeOpts{Name: "bot_assigned", Help: "Bots the bot director gave this worker."}),
		assignmentErrors: f.NewCounter(prometheus.CounterOpts{
			Name: "bot_assignment_errors_total", Help: "Polls of the bot director that failed.",
		}),
		connects: f.NewCounterVec(prometheus.CounterOpts{
			Name: "bot_connects_total", Help: "WebSocket dials that connected, by where.",
		}, []string{"where"}),
		join: f.NewHistogram(prometheus.HistogramOpts{
			Name: "bot_join_seconds", Help: "World dial to Joined.", Buckets: latency,
		}),
		ticket: f.NewHistogram(prometheus.HistogramOpts{
			Name: "bot_ticket_seconds", Help: "Step into the entrance to InstanceTicket.", Buckets: latency,
		}),
		instanceJoin: f.NewHistogram(prometheus.HistogramOpts{
			Name: "bot_instance_join_seconds", Help: "Ticket URL dial to Joined.", Buckets: latency,
		}),
		snapshotGap: f.NewHistogramVec(prometheus.HistogramOpts{
			Name: "bot_snapshot_gap_seconds", Help: "Time between snapshots, by where.",
			Buckets: []float64{.01, .025, .04, .05, .06, .075, .1, .15, .25, .5, 1},
		}, []string{"where"}),
		saving: f.NewHistogram(prometheus.HistogramOpts{
			Name: "bot_saving_seconds", Help: "Back in town to saving cleared.", Buckets: latency,
		}),
		runs: f.NewCounterVec(prometheus.CounterOpts{
			Name: "bot_runs_total", Help: "Runs finished, by result.",
		}, []string{"result"}),
		errors: f.NewCounterVec(prometheus.CounterOpts{
			Name: "bot_errors_total", Help: "Closes, rejections and failures, by stage.",
		}, []string{"stage"}),
		messages: f.NewCounterVec(prometheus.CounterOpts{
			Name: "bot_messages_total", Help: "Protocol messages, by where and direction.",
		}, []string{"where", "direction"}),
	}
}
