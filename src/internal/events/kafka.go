package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

const (
	// deliveryTimeout bounds retries on the best effort topics, so a broker
	// that is down does not fill the buffer.
	deliveryTimeout = 30 * time.Second
	closeTimeout    = 5 * time.Second
)

// Kafka produces to Redpanda with franz-go.
type Kafka struct {
	client *kgo.Client
	log    *slog.Logger
	// Errors counts records that failed to produce, by topic.
	Errors *prometheus.CounterVec
}

// NewKafka makes an idempotent producer with acks=all. opts go after the
// defaults, so tests can override them.
func NewKafka(brokers []string, reg prometheus.Registerer, log *slog.Logger, opts ...kgo.Opt) (*Kafka, error) {
	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordDeliveryTimeout(deliveryTimeout),
	}, opts...)...)
	if err != nil {
		return nil, err
	}
	k := &Kafka{
		client: client,
		log:    log,
		Errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "events_produce_errors_total",
			Help: "Event records that failed to produce, by topic.",
		}, []string{"topic"}),
	}
	if err := reg.Register(k.Errors); err != nil {
		client.Close()
		return nil, err
	}
	return k, nil
}

// Produce uses TryProduce, which fails at once with a full buffer instead of
// blocking the tick loop that calls it.
func (k *Kafka) Produce(topic, key string, msg proto.Message) {
	b, err := proto.Marshal(msg)
	if err != nil {
		k.failed(topic, key, err)
		return
	}
	k.client.TryProduce(context.Background(), &kgo.Record{Topic: topic, Key: []byte(key), Value: b}, func(r *kgo.Record, err error) {
		if err != nil {
			k.failed(r.Topic, string(r.Key), err)
		}
	})
}

func (k *Kafka) failed(topic, key string, err error) {
	k.Errors.WithLabelValues(topic).Inc()
	k.log.Warn("produce event", "topic", topic, "key", key, "err", err)
}

func (k *Kafka) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := k.client.Flush(ctx); err != nil {
		k.log.Warn("flush events", "err", err)
	}
	k.client.Close()
}
