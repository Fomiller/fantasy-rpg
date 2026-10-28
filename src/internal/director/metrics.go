package director

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	target   prometheus.Gauge
	mode     *prometheus.GaugeVec
	desired  prometheus.Gauge
	humans   prometheus.Gauge
	bots     prometheus.Gauge
	workers  prometheus.Gauge
	replicas prometheus.Gauge
	errors   *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		target:   f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_target", Help: "The target's count: total players in fill mode, bots in fixed mode."}),
		mode:     f.NewGaugeVec(prometheus.GaugeOpts{Name: "bot_director_mode", Help: "1 for the target's mode."}, []string{"mode"}),
		desired:  f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_desired", Help: "Bots the director wants running."}),
		humans:   f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_humans", Help: "Humans in the town and the caves, as last counted."}),
		bots:     f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_bots", Help: "Bots in the town and the caves, as last counted."}),
		workers:  f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_workers", Help: "Worker pods polling for a share."}),
		replicas: f.NewGauge(prometheus.GaugeOpts{Name: "bot_director_worker_replicas", Help: "Replicas the director set on the worker Deployment."}),
		errors: f.NewCounterVec(prometheus.CounterOpts{
			Name: "bot_director_errors_total", Help: "Failed calls, by what: players, scale or store.",
		}, []string{"what"}),
	}
}
