package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
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

func botAs(t *testing.T, conn *websocket.Conn, name string) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: name, Bot: true},
	}})
	townLobby(t, conn)
}

func players(t *testing.T, handle http.HandlerFunc) Players {
	t.Helper()
	rec := httptest.NewRecorder()
	handle(rec, httptest.NewRequest("GET", "/v1/players", nil))
	var p Players
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("players: %v, %q", err, rec.Body)
	}
	return p
}

// The bot flag rides in the join token, so the cave counts a bot as the
// town did.
func TestBotsCountApart(t *testing.T) {
	h := start(t)
	ann := dial(t, h.url)
	lobbyAs(t, ann, "ann")
	bot := dial(t, h.url)
	botAs(t, bot, "bot-0")
	waitMetric(t, h.world.Metrics.Players, is(2))
	waitMetric(t, h.world.Metrics.Bots, is(1))
	if got := players(t, h.world.HandlePlayers); got != (Players{Humans: 1, Bots: 1}) {
		t.Fatalf("town players = %+v", got)
	}

	enterInstance(t, bot)
	waitMetric(t, h.srv.Metrics.Bots, is(1))
	waitMetric(t, h.world.Metrics.Bots, is(0))
	if got := players(t, h.srv.HandlePlayers); got != (Players{Bots: 1}) {
		t.Fatalf("cave players = %+v", got)
	}
	if st := instanceStatus(t, h); st.Players != 1 || st.Bots != 1 {
		t.Fatalf("status = %+v", st)
	}
	if got := players(t, h.world.HandlePlayers); got != (Players{Humans: 1}) {
		t.Fatalf("town players = %+v", got)
	}
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
