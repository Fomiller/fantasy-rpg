package events

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func session(account string) *eventsv1.PlayerSession {
	return &eventsv1.PlayerSession{Version: Version, Account: account, Kind: eventsv1.PlayerSessionKind_PLAYER_SESSION_KIND_ENTER}
}

func TestOpenEmptyIsOff(t *testing.T) {
	p, name, err := Open("", prometheus.NewRegistry(), quiet())
	if err != nil || name != "off" {
		t.Fatalf("Open(\"\") = %v, %q, %v", p, name, err)
	}
	if _, ok := p.(Nop); !ok {
		t.Fatalf("Open(\"\") = %T, want Nop", p)
	}
	p.Produce(TopicPlayerSession, "ann", session("ann"))
	p.Close()
}

// Memory keeps a copy, so a message changed after Produce is not what the test reads.
func TestMemoryKeepsByTopic(t *testing.T) {
	m := NewMemory()
	msg := session("ann")
	m.Produce(TopicPlayerSession, "ann", msg)
	m.Produce(TopicInstanceJoin, "run-1", &eventsv1.InstanceJoin{RunId: "run-1"})
	msg.Account = "bob"

	got := m.Records(TopicPlayerSession)
	if len(got) != 1 || got[0].Key != "ann" || got[0].Msg.(*eventsv1.PlayerSession).GetAccount() != "ann" {
		t.Fatalf("player.session = %+v", got)
	}
	if len(m.Records(TopicInstanceJoin)) != 1 || len(m.Records(TopicRunEnded)) != 0 {
		t.Fatal("want one instance.join and no run.ended")
	}
}

func TestKafkaProducesKeyedProto(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, TopicPlayerSession))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)

	k, err := NewKafka(cluster.ListenAddrs(), prometheus.NewRegistry(), quiet())
	if err != nil {
		t.Fatal(err)
	}
	k.Produce(TopicPlayerSession, "ann", session("ann"))
	k.Close()

	consumer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumeTopics(TopicPlayerSession), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fetches := consumer.PollRecords(ctx, 1)
	if err := fetches.Err(); err != nil {
		t.Fatal(err)
	}
	recs := fetches.Records()
	if len(recs) != 1 || string(recs[0].Key) != "ann" {
		t.Fatalf("records = %v, want one keyed ann", recs)
	}
	var got eventsv1.PlayerSession
	if err := proto.Unmarshal(recs[0].Value, &got); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&got, session("ann")) {
		t.Fatalf("payload = %v", &got)
	}
	if n := testutil.ToFloat64(k.Errors.WithLabelValues(TopicPlayerSession)); n != 0 {
		t.Fatalf("errors = %v, want 0", n)
	}
}

// A topic that does not exist is never auto-created. The record fails once
// its delivery timeout passes and is counted.
func TestKafkaCountsFailures(t *testing.T) {
	cluster, err := kfake.NewCluster()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)

	k, err := NewKafka(cluster.ListenAddrs(), prometheus.NewRegistry(), quiet(), kgo.RecordDeliveryTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	k.Produce("no.such.topic", "ann", session("ann"))
	k.Close()
	if n := testutil.ToFloat64(k.Errors.WithLabelValues("no.such.topic")); n != 1 {
		t.Fatalf("errors = %v, want 1", n)
	}
}
