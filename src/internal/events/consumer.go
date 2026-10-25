package events

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	retryFirst    = 100 * time.Millisecond
	retryMax      = 5 * time.Second
	commitTimeout = 5 * time.Second
)

// Handler takes one record's key and value. An error is retried until it
// returns nil, unless it is wrapped in Drop.
type Handler func(ctx context.Context, key string, value []byte) error

type dropped struct{ err error }

func (d dropped) Error() string { return d.err.Error() }
func (d dropped) Unwrap() error { return d.err }

// Drop marks an error a retry cannot fix. The consumer logs it and moves on.
func Drop(err error) error { return dropped{err} }

// Dropped reports if err came from Drop.
func Dropped(err error) bool {
	var d dropped
	return errors.As(err, &d)
}

// ConsumerConfig says what a Consumer reads.
type ConsumerConfig struct {
	Brokers []string
	Topics  []string
	// Group commits each record once its handler is done with it. With no
	// group the consumer starts at the end of every partition and commits
	// nothing, so each process sees every record from its start on.
	Group string
	// Lag, when set, gets the records behind the high watermark by topic and
	// partition.
	Lag *prometheus.GaugeVec
	// Opts go after the defaults, so tests can override them.
	Opts []kgo.Opt
}

// Consumer hands records to a Handler one at a time, in partition order.
type Consumer struct {
	client *kgo.Client
	group  bool
	lag    *prometheus.GaugeVec
	log    *slog.Logger
}

func NewConsumer(cfg ConsumerConfig, log *slog.Logger) (*Consumer, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(cfg.Brokers...), kgo.ConsumeTopics(cfg.Topics...)}
	if cfg.Group != "" {
		opts = append(opts, kgo.ConsumerGroup(cfg.Group), kgo.DisableAutoCommit(), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	} else {
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	}
	client, err := kgo.NewClient(append(opts, cfg.Opts...)...)
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, group: cfg.Group != "", lag: cfg.Lag, log: log}, nil
}

// Run polls until ctx is done. A record whose handler has not finished when
// ctx ends is not committed, so the next start reads it again.
func (c *Consumer) Run(ctx context.Context, handle Handler) {
	for {
		fetches := c.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			c.log.Warn("consume", "topic", topic, "partition", partition, "err", err)
		})
		var done []*kgo.Record
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			for _, r := range p.Records {
				if !c.handle(ctx, handle, r) {
					return
				}
				done = append(done, r)
				c.setLag(r, p.HighWatermark)
			}
		})
		c.commit(done)
	}
}

// handle reports false only when ctx ended before the record was done.
func (c *Consumer) handle(ctx context.Context, handle Handler, r *kgo.Record) bool {
	wait := retryFirst
	for {
		err := handle(ctx, string(r.Key), r.Value)
		if err == nil {
			return true
		}
		if Dropped(err) {
			c.log.Error("dropped a record", "topic", r.Topic, "key", string(r.Key), "partition", r.Partition, "offset", r.Offset, "err", err)
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		c.log.Warn("handle record", "topic", r.Topic, "key", string(r.Key), "offset", r.Offset, "retry_in", wait, "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return false
		}
		wait = min(wait*2, retryMax)
	}
}

// commit outlives Run's ctx, so what was handled before a stop is kept.
func (c *Consumer) commit(done []*kgo.Record) {
	if !c.group || len(done) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()
	if err := c.client.CommitRecords(ctx, done...); err != nil {
		c.log.Warn("commit offsets", "records", len(done), "err", err)
	}
}

func (c *Consumer) setLag(r *kgo.Record, highWatermark int64) {
	if c.lag != nil {
		c.lag.WithLabelValues(r.Topic, strconv.Itoa(int(r.Partition))).Set(float64(max(highWatermark-r.Offset-1, 0)))
	}
}

func (c *Consumer) Close() { c.client.Close() }
