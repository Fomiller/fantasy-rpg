package db

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kfake"
	"google.golang.org/protobuf/proto"

	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// run.ended delivered twice, as a producer retry after a lost ack does,
// lands in Postgres once with one inventory.applied.
func TestRunEndedTwiceAppliesOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := newStore(t, newDBURL(t, true))
	const run = "6f1c2b3a-0000-4000-8000-000000000001"
	if err := s.Create(ctx, inventory.Character{Name: "ann", Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "ann", "warrior", run, "instance-0@3f9a1c"); err != nil {
		t.Fatal(err)
	}

	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, events.TopicRunEnded))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus, err := events.NewKafka(cluster.ListenAddrs(), prometheus.NewRegistry(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	end := inventory.RunEnd{RunID: run, Outcome: inventory.Extracted, Character: inventory.Character{Name: "ann", Class: "warrior", XP: 40, Stash: []items.Instance{sword()}}}
	payload, err := proto.Marshal(end.Event())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := bus.Send(ctx, events.TopicRunEnded, run, payload); err != nil {
			t.Fatal(err)
		}
	}

	consumer, err := events.NewConsumer(events.ConsumerConfig{Brokers: cluster.ListenAddrs(), Topics: []string{events.TopicRunEnded}, Group: "inventory"}, log)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	handled := make(chan error, 2)
	apply := inventory.RunEndedHandler(s, log)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go consumer.Run(runCtx, func(ctx context.Context, key string, value []byte) error {
		err := apply(ctx, key, value)
		handled <- err
		return err
	})
	for range 2 {
		select {
		case err := <-handled:
			if err != nil {
				t.Fatalf("handle = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("run.ended was not consumed twice")
		}
	}

	c, ok, err := s.Load(ctx, "ann", "warrior")
	if err != nil || !ok {
		t.Fatalf("load = %v, %v", ok, err)
	}
	if c.XP != 40 || len(c.Stash) != 1 || c.Run.Held() {
		t.Fatalf("ann = %+v, want the run's XP and stash and no lease", c)
	}
	var version int64
	if err := s.pool.QueryRow(ctx, `SELECT stash_version FROM accounts WHERE name = 'ann'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if n := outboxRows(t, s); n != 1 || version != 1 {
		t.Fatalf("outbox rows = %d, stash version = %d, want one write", n, version)
	}
}
