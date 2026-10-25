package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// refuseProduce makes the broker fail every produce while down is set.
func refuseProduce(cluster *kfake.Cluster, down *atomic.Bool) {
	cluster.ControlKey(int16(kmsg.Produce), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		if !down.Load() {
			return nil, nil, false
		}
		req := kreq.(*kmsg.ProduceRequest)
		resp := req.ResponseKind().(*kmsg.ProduceResponse)
		for _, rt := range req.Topics {
			t := kmsg.NewProduceResponseTopic()
			t.Topic, t.TopicID = rt.Topic, rt.TopicID
			for _, rp := range rt.Partitions {
				p := kmsg.NewProduceResponseTopicPartition()
				p.Partition = rp.Partition
				p.ErrorCode = kerr.InvalidRecord.Code
				t.Partitions = append(t.Partitions, p)
			}
			resp.Topics = append(resp.Topics, t)
		}
		return resp, nil, true
	})
}

func readRunEnded(t *testing.T, cluster *kfake.Cluster) []*eventsv1.RunEnded {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumeTopics(events.TopicRunEnded), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fetches := cl.PollFetches(ctx)
	var out []*eventsv1.RunEnded
	for _, r := range fetches.Records() {
		var msg eventsv1.RunEnded
		if err := proto.Unmarshal(r.Value, &msg); err != nil {
			t.Fatal(err)
		}
		if string(r.Key) != msg.GetRunId() {
			t.Fatalf("key %q, run id %q", r.Key, msg.GetRunId())
		}
		out = append(out, &msg)
	}
	return out
}

// A run.ended the broker refuses is retried until it is acked. The seat
// stays on the runs list the whole time, so the server cannot shut down on
// a result that is not durable. The instance never writes the store itself.
func TestRunEndedRetriesUntilAcked(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, events.TopicRunEnded))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	var down atomic.Bool
	down.Store(true)
	refuseProduce(cluster, &down)
	// A record in flight outlives the store timeout, so this bounds each try.
	bus, err := events.NewKafka(cluster.ListenAddrs(), prometheus.NewRegistry(), quiet(), kgo.RecordDeliveryTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bus.Close)

	spy := newSpy()
	h, cfg := extractAtSpawn(t, spy, func(s *Server) { s.Results = EventResults{Sender: bus} })
	conn, _ := joinAs(t, h, "ann")
	run := leaseOf(t, spy.Memory, "ann").RunID
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil })
	wantClose(t, conn, websocket.StatusNormalClosure, "run over")
	deadline := time.Now().Add(3 * storeTimeout)
	for testutil.ToFloat64(h.srv.PendingSaves) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("run end never went pending")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runs := listedRuns(t, h); !slices.Equal(runs, []string{run}) {
		t.Fatalf("runs with run.ended unacked = %v, want %s kept", runs, run)
	}

	down.Store(false)
	deadline = time.Now().Add(3 * storeTimeout)
	for len(listedRuns(t, h)) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("runs = %v, want the seat freed once acked", listedRuns(t, h))
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitPending(t, h, 0)
	got := readRunEnded(t, cluster)
	if len(got) != 1 {
		t.Fatalf("run.ended records = %v, want one", got)
	}
	msg := got[0]
	if msg.GetRunId() != run || msg.GetAccount() != "ann" || msg.GetClass() != classes.Starter || msg.GetOutcome() != eventsv1.RunOutcome_RUN_OUTCOME_EXTRACTED ||
		msg.GetInstance() != h.srv.Instance() || msg.GetXp() != int64(cfg.Classes.Progression.ExtractBonus) || len(msg.GetEquipment()) != len(items.StarterKit) {
		t.Fatalf("run.ended = %v", msg)
	}
	if n := spy.count("end run"); n != 0 {
		t.Fatalf("store end runs = %d, want none from the instance", n)
	}
	if !leaseOf(t, spy.Memory, "ann").Held() {
		t.Fatal("lease cleared, want it held until inventory applies the run")
	}
}

// inventory.applied opens a saving lobby at once. The poll is set too slow
// to be what does it.
func TestWorldClearsSavingOnInventoryApplied(t *testing.T) {
	h := serveWorld(t, func(l *Lobby) { l.leaseRetry = time.Hour })
	ctx := context.Background()
	if err := h.store.Create(ctx, inventory.Character{Name: "ann", Class: classes.Starter}); err != nil {
		t.Fatal(err)
	}
	run := items.NewID()
	c, err := h.store.StartRun(ctx, "ann", classes.Starter, run, "instance-0@3f9a1c")
	if err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.url)
	if lobby := lobbyAs(t, conn, "ann"); !lobby.GetSaving() {
		t.Fatalf("lobby = %v, want saving", lobby)
	}

	c.Stash = []items.Instance{helm("loot")}
	if err := h.store.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	payload, err := proto.Marshal(&eventsv1.InventoryApplied{Version: events.Version, Account: "ann", RunId: run, StashVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.lobby.HandleApplied(ctx, "ann", payload); err != nil {
		t.Fatal(err)
	}
	waitFor(t, conn, saving(false))
	if _, _, st := lobbyItems(t, conn); len(st.GetItems()) != 1 || st.GetItems()[0].GetId() != "loot" {
		t.Fatalf("stash = %v, want the run's loot", st.GetItems())
	}
}

// With no Redpanda, the instance writes the run end through the inventory
// service's HTTP API, as local dev does.
func TestRunEndOverHTTPWithoutEvents(t *testing.T) {
	mux := http.NewServeMux()
	httpstore.Register(mux, inventory.NewMemory(), quiet())
	inv := httptest.NewServer(mux)
	t.Cleanup(inv.Close)

	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	h := serveWith(t, cave, calm(cfg), httpstore.NewClient(inv.URL, inv.Client()))
	conn, _ := joinAs(t, h, "ann")
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil })
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return c.XP == cfg.Classes.Progression.ExtractBonus && !c.Run.Held() })
}
