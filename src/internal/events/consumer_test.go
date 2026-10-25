package events

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kfake"
)

func cluster(t *testing.T, topics ...string) *kfake.Cluster {
	t.Helper()
	c, err := kfake.NewCluster(kfake.SeedTopics(1, topics...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func send(t *testing.T, c *kfake.Cluster, topic string, keys ...string) {
	t.Helper()
	k, err := NewKafka(c.ListenAddrs(), prometheus.NewRegistry(), quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	for _, key := range keys {
		if err := k.Send(t.Context(), topic, key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
}

// seen keeps the keys a handler took, in order.
type seen struct {
	mu   sync.Mutex
	keys []string
}

func (s *seen) add(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
}

func (s *seen) wait(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		got := slices.Clone(s.keys)
		s.mu.Unlock()
		if slices.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("handled %v, want %v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func consume(t *testing.T, cfg ConsumerConfig, handle Handler) (stop func()) {
	t.Helper()
	c, err := NewConsumer(cfg, quiet())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, handle); close(done) }()
	stop = func() { cancel(); <-done; c.Close() }
	t.Cleanup(stop)
	return stop
}

// A handler that fails is retried on the same record before the next one.
// A dropped record is skipped. Both count as done, so a restart of the
// group starts after them.
func TestGroupConsumerRetriesThenCommits(t *testing.T) {
	c := cluster(t, TopicRunEnded)
	send(t, c, TopicRunEnded, "run-1", "run-2", "run-3")
	lag := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "lag"}, []string{"topic", "partition"})
	cfg := ConsumerConfig{Brokers: c.ListenAddrs(), Topics: []string{TopicRunEnded}, Group: "inventory", Lag: lag}

	var got seen
	fails := 2
	stop := consume(t, cfg, func(_ context.Context, key string, _ []byte) error {
		got.add(key)
		switch {
		case key == "run-1" && fails > 0:
			fails--
			return errors.New("database is down")
		case key == "run-2":
			return Drop(errors.New("lease lost"))
		}
		return nil
	})
	got.wait(t, "run-1", "run-1", "run-1", "run-2", "run-3")
	if n := testutil.ToFloat64(lag.WithLabelValues(TopicRunEnded, "0")); n != 0 {
		t.Fatalf("lag = %v, want 0", n)
	}
	stop()

	send(t, c, TopicRunEnded, "run-4")
	var again seen
	consume(t, cfg, func(_ context.Context, key string, _ []byte) error {
		again.add(key)
		return nil
	})
	again.wait(t, "run-4")
}

// With no group, a consumer only sees what is produced after it starts.
func TestTailConsumerStartsAtTheEnd(t *testing.T) {
	c := cluster(t, TopicInventoryApplied)
	send(t, c, TopicInventoryApplied, "old")
	var got seen
	consume(t, ConsumerConfig{Brokers: c.ListenAddrs(), Topics: []string{TopicInventoryApplied}}, func(_ context.Context, key string, _ []byte) error {
		got.add(key)
		return nil
	})
	// The consumer finds the end some time after it starts, so keep sending
	// until something arrives.
	deadline := time.Now().Add(5 * time.Second)
	for {
		send(t, c, TopicInventoryApplied, "new")
		got.mu.Lock()
		keys := slices.Clone(got.keys)
		got.mu.Unlock()
		if len(keys) > 0 {
			if slices.Contains(keys, "old") {
				t.Fatalf("handled %v, want nothing from before the start", keys)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("handled nothing")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
