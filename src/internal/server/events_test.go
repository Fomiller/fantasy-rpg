package server

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// waitRecords polls, since events go out on the tick after what made them.
func waitRecords(t *testing.T, m *events.Memory, topic string, n int) []events.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		recs := m.Records(topic)
		if len(recs) >= n {
			return recs
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %v, want %d records", topic, recs, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorldProducesPlayerSession(t *testing.T) {
	bus := events.NewMemory()
	h := serveWorld(t, func(l *Lobby) {
		l.Events = bus
		l.WorldID = "world-a"
	})
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	recs := waitRecords(t, bus, events.TopicPlayerSession, 1)
	conn.Close(websocket.StatusNormalClosure, "")
	recs = waitRecords(t, bus, events.TopicPlayerSession, 2)

	for i, kind := range []eventsv1.PlayerSessionKind{eventsv1.PlayerSessionKind_PLAYER_SESSION_KIND_ENTER, eventsv1.PlayerSessionKind_PLAYER_SESSION_KIND_LEAVE} {
		msg := recs[i].Msg.(*eventsv1.PlayerSession)
		if recs[i].Key != "ann" || msg.GetAccount() != "ann" || msg.GetKind() != kind || msg.GetWorld() != "world-a" || msg.GetVersion() != events.Version || msg.GetTime() == nil {
			t.Fatalf("player.session %d = key %q %v, want %v for ann", i, recs[i].Key, msg, kind)
		}
	}
	if n := len(bus.Records(events.TopicInstanceJoin)); n != 0 {
		t.Fatalf("instance.join from the world = %d", n)
	}
}

func TestInstanceProducesJoin(t *testing.T) {
	bus := events.NewMemory()
	h := startCave(t, testCave(), sim.DefaultConfig(), func(s *Server) {
		s.Events = bus
		s.MaxPlayers = 1
	})
	joinAs(t, h, "ann")
	lease := leaseOf(t, h.srv.Store, "ann")

	seed(t, h, inventory.Character{Name: "bob", Loadout: h.srv.starterKit()})
	bobRun := items.NewID()
	conn := dial(t, h.instanceURL())
	sendToken(t, conn, h.token(t, "bob", bobRun, time.Now()))
	wantClose(t, conn, websocket.StatusPolicyViolation, "instance full")

	recs := waitRecords(t, bus, events.TopicInstanceJoin, 2)
	for i, want := range []struct {
		run, account, reason string
		result               eventsv1.InstanceJoinResult
	}{
		{lease.RunID, "ann", "", eventsv1.InstanceJoinResult_INSTANCE_JOIN_RESULT_ACCEPTED},
		{bobRun, "bob", "instance full", eventsv1.InstanceJoinResult_INSTANCE_JOIN_RESULT_REJECTED},
	} {
		msg := recs[i].Msg.(*eventsv1.InstanceJoin)
		if recs[i].Key != want.run || msg.GetRunId() != want.run || msg.GetAccount() != want.account || msg.GetResult() != want.result || msg.GetReason() != want.reason || msg.GetInstance() != h.srv.Instance() || msg.GetVersion() != events.Version {
			t.Fatalf("instance.join %d = key %q %v, want %+v", i, recs[i].Key, msg, want)
		}
	}
}

// A token whose run already ended is rejected after the lease check.
func TestInstanceJoinRejectsUsedRun(t *testing.T) {
	bus := events.NewMemory()
	h := startCave(t, testCave(), sim.DefaultConfig(), func(s *Server) { s.Events = bus })
	seed(t, h, inventory.Character{Name: "ann", Loadout: h.srv.starterKit()})
	ctx := context.Background()
	run := items.NewID()
	c, err := h.srv.Store.StartRun(ctx, "ann", classes.Starter, run, h.srv.Instance())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Store.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.instanceURL())
	sendToken(t, conn, h.token(t, "ann", run, time.Now()))
	wantClose(t, conn, websocket.StatusPolicyViolation, "token used")
	recs := waitRecords(t, bus, events.TopicInstanceJoin, 1)
	if msg := recs[0].Msg.(*eventsv1.InstanceJoin); recs[0].Key != run || msg.GetResult() != eventsv1.InstanceJoinResult_INSTANCE_JOIN_RESULT_REJECTED || msg.GetReason() != "token used" {
		t.Fatalf("instance.join = key %q %v, want rejected token used", recs[0].Key, msg)
	}
}
