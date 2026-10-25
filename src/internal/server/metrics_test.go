package server

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

func waitMetric(t *testing.T, m prometheus.Collector, ok func(float64) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok(testutil.ToFloat64(m)) {
		if time.Now().After(deadline) {
			t.Fatalf("metric = %v", testutil.ToFloat64(m))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func is(want float64) func(float64) bool { return func(v float64) bool { return v == want } }

func TestTownPlayersAndBytes(t *testing.T) {
	h := serveWorld(t)
	m := h.lobby.Metrics
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	waitMetric(t, m.Players, is(1))
	waitMetric(t, m.SentBytes, func(v float64) bool { return v > 0 })

	walk(t, conn, 1, 0)
	waitMetric(t, m.ReceivedBytes, func(v float64) bool { return v > 0 })

	conn.Close(websocket.StatusNormalClosure, "")
	waitMetric(t, m.Players, is(0))
}

func TestInstancePlayers(t *testing.T) {
	h := start(t)
	world := dial(t, h.url)
	lobbyAs(t, world, "ann")
	enterInstance(t, world)
	waitMetric(t, h.srv.Metrics.Players, is(1))
	waitMetric(t, h.world.Metrics.Players, is(0))
}

// A full outbox counts one kick, however many sends hit it after.
func TestSlowClientKickCountsOnce(t *testing.T) {
	l := NewLobby(testTown(), sim.DefaultConfig(), nil, quiet())
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := l.newClient("ann", "", ctx, cancel)
	for range outboxSize + 3 {
		c.send([]byte{1})
	}
	if got := testutil.ToFloat64(l.Metrics.SlowKicks); got != 1 {
		t.Fatalf("slow kicks = %v, want 1", got)
	}
	if ctx.Err() == nil {
		t.Fatal("slow client not cancelled")
	}
}
