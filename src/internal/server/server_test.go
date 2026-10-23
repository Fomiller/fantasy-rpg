package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

type harness struct {
	url   string
	enemy sim.EntityID
}

// testCave spawns players at the origin, next to one enemy, with the exit
// far away unless a test moves it.
func testCave() caves.Cave {
	return caves.Cave{
		Name:         "test",
		Bounds:       sim.Rect{Min: sim.Vec2{X: -600, Y: -320}, Max: sim.Vec2{X: 600, Y: 320}},
		PlayerSpawns: []sim.Vec2{{}},
		Extraction:   sim.Rect{Min: sim.Vec2{X: 500, Y: 200}, Max: sim.Vec2{X: 600, Y: 320}},
	}
}

func start(t *testing.T) harness {
	t.Helper()
	return startCave(t, testCave(), sim.DefaultConfig())
}

func startCave(t *testing.T, cave caves.Cave, cfg sim.Config) harness {
	t.Helper()
	// Enemy stays put, so the only damage in these tests comes from players.
	cfg.DetectRadius, cfg.PatrolSpeed = 0, 0
	world := cave.NewWorld(cfg)
	enemy := world.AddEnemy(sim.Vec2{X: 40, Y: 0}, 30)
	srv := New(world, cave, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	loop := &tick.Loop{
		Interval: 10 * time.Millisecond,
		Metrics:  tick.NewMetrics(prometheus.NewRegistry(), "test"),
		Step:     srv.Step,
	}
	go loop.Run(ctx)

	hs := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	t.Cleanup(func() {
		hs.Close()
		cancel()
		srv.Close()
	})
	return harness{url: "ws" + strings.TrimPrefix(hs.URL, "http"), enemy: enemy}
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func send(t *testing.T, conn *websocket.Conn, msg *gamev1.ClientMessage) {
	t.Helper()
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		t.Fatal(err)
	}
}

func join(t *testing.T, conn *websocket.Conn) uint64 {
	t.Helper()
	return joinAccepted(t, conn).GetPlayerId()
}

func joinAccepted(t *testing.T, conn *websocket.Conn) *gamev1.JoinAccepted {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: "tester"},
	}})
	msg := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetJoined() != nil })
	return msg.GetJoined()
}

// waitFor reads until match returns true, or fails after two seconds.
func waitFor(t *testing.T, conn *websocket.Conn, match func(*gamev1.ServerMessage) bool) *gamev1.ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no matching message: %v", err)
		}
		var msg gamev1.ServerMessage
		if err := proto.Unmarshal(b, &msg); err != nil {
			t.Fatal(err)
		}
		if match(&msg) {
			return &msg
		}
	}
}

func entity(snap *gamev1.WorldSnapshot, id uint64) *gamev1.EntityState {
	for _, e := range snap.GetEntities() {
		if e.GetId() == id {
			return e
		}
	}
	return nil
}

func TestJoinThenMoveShowsInSnapshot(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	id := join(t, conn)

	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Move{
		Move: &gamev1.MoveInput{Seq: 1, Direction: &gamev1.Vec2{X: 0, Y: 1}},
	}})
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		e := entity(m.GetSnapshot(), id)
		return e != nil && e.GetPosition().GetY() > 20
	})
}

func TestAttackBroadcastsDamage(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	id := join(t, conn)

	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, TargetId: uint64(h.enemy), AbilityId: "basic"},
	}})
	msg := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetDamage() != nil })
	if d := msg.GetDamage(); d.GetSourceId() != id || d.GetTargetHealth() != 20 {
		t.Fatalf("damage = %v, want from player %d leaving 20 health", d, id)
	}
}

func TestBothClientsSeeEachOther(t *testing.T) {
	h := start(t)
	a := dial(t, h.url)
	b := dial(t, h.url)
	idA := join(t, a)
	idB := join(t, b)

	waitFor(t, a, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), idB) != nil })
	waitFor(t, b, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), idA) != nil })
}

func TestDisconnectRemovesPlayer(t *testing.T) {
	h := start(t)
	a := dial(t, h.url)
	b := dial(t, h.url)
	idA := join(t, a)
	join(t, b)

	a.Close(websocket.StatusNormalClosure, "")
	waitFor(t, b, func(m *gamev1.ServerMessage) bool {
		return m.GetSnapshot() != nil && entity(m.GetSnapshot(), idA) == nil
	})
}

func TestFirstMessageMustBeJoin(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Move{
		Move: &gamev1.MoveInput{Seq: 1},
	}})
	_, _, err := conn.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("got %v, want policy violation close", err)
	}
}

func TestWrongProtocolVersionIsClosed(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version + 1, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: "old client"},
	}})
	_, _, err := conn.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("got %v, want policy violation close", err)
	}
}

func attack(t *testing.T, conn *websocket.Conn, seq uint32, target sim.EntityID) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: seq, TargetId: uint64(target), AbilityId: "basic"},
	}})
}

func TestTwoClientsKillSameEnemy(t *testing.T) {
	h := start(t)
	a := dial(t, h.url)
	b := dial(t, h.url)
	idA := join(t, a)
	idB := join(t, b)

	// Both clients must see every hit the same way.
	hit := func(conn *websocket.Conn, seq uint32, from uint64, health int32) {
		t.Helper()
		attack(t, conn, seq, h.enemy)
		for _, c := range []*websocket.Conn{a, b} {
			d := waitFor(t, c, func(m *gamev1.ServerMessage) bool { return m.GetDamage() != nil }).GetDamage()
			if d.GetSourceId() != from || d.GetTargetHealth() != health || d.GetKilled() != (health == 0) {
				t.Fatalf("damage = %v, want from %d leaving %d", d, from, health)
			}
		}
	}

	// 30 health, 10 per hit. A hits again once its cooldown ends.
	hit(a, 1, idA, 20)
	hit(b, 1, idB, 10)
	time.Sleep(sim.DefaultConfig().AttackCooldown)
	hit(a, 2, idA, 0)

	for _, conn := range []*websocket.Conn{a, b} {
		waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
			e := entity(m.GetSnapshot(), uint64(h.enemy))
			return e != nil && e.GetHealth() == 0
		})
	}

}

func TestJoinSendsCaveLayout(t *testing.T) {
	cave := testCave()
	cave.Walls = []sim.Rect{{Min: sim.Vec2{X: 100, Y: 100}, Max: sim.Vec2{X: 164, Y: 164}}}
	cave.PlayerSpawns = []sim.Vec2{{X: 1, Y: 2}, {X: 3, Y: 4}}
	h := startCave(t, cave, sim.DefaultConfig())

	first := joinAccepted(t, dial(t, h.url))
	second := joinAccepted(t, dial(t, h.url))
	layout := first.GetCave()
	if layout.GetName() != "test" || len(layout.GetWalls()) != 1 || layout.GetExtraction().GetMin().GetX() != 500 {
		t.Fatalf("layout = %v", layout)
	}
	if first.GetSpawn().GetX() != 1 || second.GetSpawn().GetX() != 3 {
		t.Fatalf("spawns = %v and %v, want each spawn point in turn", first.GetSpawn(), second.GetSpawn())
	}
}

func TestExtractionClosesConnection(t *testing.T) {
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	h := startCave(t, cave, cfg)

	leaver := dial(t, h.url)
	other := dial(t, h.url)
	id := join(t, leaver)
	join(t, other)

	waitFor(t, other, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	waitFor(t, leaver, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	_, _, err := leaver.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("got %v, want normal close after extraction", err)
	}
}
