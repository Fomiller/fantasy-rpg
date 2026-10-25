package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

// testKey signs every token in these tests.
var testKey = []byte("test-join-key")

// harness is a world server and one instance server on one store, as the
// game runs them.
type harness struct {
	// url is the world server's.
	url   string
	enemy sim.EntityID
	srv   *Server
	world *Lobby
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

// testTown has the entrance a step above the spawn and the stash chest a
// step below it, so tests reach either at once. The return spawn is out of
// the chest's range.
func testTown() caves.Cave {
	c, err := caves.Parse("test town", `
#######
###D###
#..S..#
#R.T..#
#######
`)
	if err != nil {
		panic(err)
	}
	return c
}

// runTown steps the world server's town every 10 ms until ctx ends.
func runTown(ctx context.Context, l *Lobby) {
	loop := &tick.Loop{
		Interval: 10 * time.Millisecond,
		Metrics:  tick.NewMetrics(prometheus.NewRegistry(), "test"),
		Step:     l.Step,
	}
	go loop.Run(ctx)
}

func start(t *testing.T) harness {
	t.Helper()
	return startCave(t, testCave(), sim.DefaultConfig())
}

func startCave(t *testing.T, cave caves.Cave, cfg sim.Config, configure ...func(*Server)) harness {
	t.Helper()
	return serveWith(t, cave, calm(cfg), inventory.NewMemory(), configure...)
}

// calm stops enemies moving or reaching anyone, so the only damage in these
// tests comes from players.
func calm(cfg sim.Config) sim.Config {
	for id, a := range cfg.Archetypes {
		a.DetectRadius, a.PatrolSpeed, a.Speed, a.AttackRange, a.Special = 0, 0, 0, 0, sim.SpecialNone
		cfg.Archetypes[id] = a
	}
	return cfg
}

// serve leaves the enemy as cfg has it.
func serve(t *testing.T, cave caves.Cave, cfg sim.Config) harness {
	t.Helper()
	return serveWith(t, cave, cfg, inventory.NewMemory())
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// configure runs before the servers start, so it can set what they read.
func serveWith(t *testing.T, cave caves.Cave, cfg sim.Config, store inventory.Store, configure ...func(*Server)) harness {
	t.Helper()
	loot := items.NewRoller(items.Default(), 1)
	world := cave.NewWorld(cfg, loot)
	enemy := world.AddEnemy(sim.Vec2{X: 40, Y: 0}, sim.Melee)
	srv := New(world, cave, loot, quiet())
	srv.Store = store
	srv.Tokens = jointoken.NewVerifier(testKey)
	srv.closeGrace = 10 * time.Millisecond
	for _, f := range configure {
		f(srv)
	}

	ctx, cancel := context.WithCancel(context.Background())
	loop := &tick.Loop{
		Interval: 10 * time.Millisecond,
		Metrics:  tick.NewMetrics(prometheus.NewRegistry(), "test"),
		Step:     srv.Step,
	}
	go loop.Run(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", srv.HandleStatus)
	mux.HandleFunc("GET /instances/"+srv.ID+"/ws", srv.HandleWS)
	is := httptest.NewServer(mux)

	lobby := NewLobby(testTown(), cfg, items.NewRoller(items.Default(), 2), quiet())
	lobby.Store = store
	lobby.JoinKey = testKey
	lobby.leaseRetry = 20 * time.Millisecond
	lobby.closeGrace = 10 * time.Millisecond
	lobby.Instances = NewDirectory([]Instance{{ID: srv.ID, Internal: is.URL, Public: wsURL(is.URL) + "/instances/" + srv.ID + "/ws"}})
	lobby.Instances.Poll(ctx)
	runTown(ctx, lobby)
	go lobby.Instances.Run(ctx, 10*time.Millisecond)
	ws := httptest.NewServer(http.HandlerFunc(lobby.HandleWS))

	t.Cleanup(func() {
		ws.Close()
		is.Close()
		cancel()
		lobby.Close()
		srv.Close()
	})
	return harness{url: wsURL(ws.URL), enemy: enemy, srv: srv, world: lobby}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

// instanceURL is where the harness's instance takes joins.
func (h harness) instanceURL() string {
	return h.world.Instances.list[0].Public
}

// token makes a join token for this harness's instance, as the world would.
func (h harness) token(t *testing.T, name, run string, at time.Time) string {
	t.Helper()
	tok, err := jointoken.Sign(testKey, jointoken.Claims{Name: name, Class: classes.Starter, Inst: h.srv.Instance(), Run: run}, at)
	if err != nil {
		t.Fatal(err)
	}
	return tok
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

var testers atomic.Int64

// join puts a new player in the cave and returns their instance connection.
func join(t *testing.T, h harness) (*websocket.Conn, uint64) {
	t.Helper()
	conn, joined := joinAccepted(t, h)
	return conn, joined.GetPlayerId()
}

func joinAccepted(t *testing.T, h harness) (*websocket.Conn, *gamev1.JoinAccepted) {
	t.Helper()
	return joinAs(t, h, fmt.Sprintf("tester%d", testers.Add(1)))
}

// joinAs joins the world and walks straight from the town into the cave.
func joinAs(t *testing.T, h harness, name string) (*websocket.Conn, *gamev1.JoinAccepted) {
	t.Helper()
	world := dial(t, h.url)
	lobbyAs(t, world, name)
	return enterInstance(t, world)
}

func lobbyAs(t *testing.T, conn *websocket.Conn, name string) *gamev1.Lobby {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: name},
	}})
	return townLobby(t, conn)
}

// townIDs has each world connection's player in the town, from JoinAccepted.
var townIDs sync.Map

// townLobby reads the join into the town up to its Lobby.
func townLobby(t *testing.T, conn *websocket.Conn) *gamev1.Lobby {
	t.Helper()
	joined := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetJoined() != nil }).GetJoined()
	if !joined.GetTown() {
		t.Fatalf("joined = %v, want the town", joined)
	}
	townIDs.Store(conn, joined.GetPlayerId())
	return waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil }).GetLobby()
}

func townID(t *testing.T, conn *websocket.Conn) uint64 {
	t.Helper()
	id, ok := townIDs.Load(conn)
	if !ok {
		t.Fatal("not in the town")
	}
	return id.(uint64)
}

var moveSeq atomic.Uint32

// walk sends one move. The server keeps going that way until the next one.
func walk(t *testing.T, conn *websocket.Conn, x, y float32) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Move{
		Move: &gamev1.MoveInput{Seq: moveSeq.Add(1), Direction: &gamev1.Vec2{X: x, Y: y}},
	}})
}

// townPos waits for a snapshot where the player in the town matches.
func townPos(t *testing.T, conn *websocket.Conn, match func(*gamev1.EntityState) bool) *gamev1.EntityState {
	t.Helper()
	id := townID(t, conn)
	return entity(waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		e := entity(m.GetSnapshot(), id)
		return e != nil && match(e)
	}).GetSnapshot(), id)
}

// enterInstance walks into the town's entrance and follows the ticket, as
// the client does.
func enterInstance(t *testing.T, world *websocket.Conn) (*websocket.Conn, *gamev1.JoinAccepted) {
	t.Helper()
	ticket := enterTicket(t, world)
	conn := dial(t, ticket.GetUrl())
	sendToken(t, conn, ticket.GetToken())
	return conn, waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetJoined() != nil }).GetJoined()
}

func enterTicket(t *testing.T, world *websocket.Conn) *gamev1.InstanceTicket {
	t.Helper()
	walk(t, world, 0, -1)
	ticket := waitFor(t, world, func(m *gamev1.ServerMessage) bool { return m.GetTicket() != nil }).GetTicket()
	wantClose(t, world, websocket.StatusNormalClosure, "entering instance")
	return ticket
}

func sendToken(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_InstanceJoin{InstanceJoin: &gamev1.InstanceJoinRequest{Token: token}}})
}

// wantClose reads past any messages until the server closes with code and reason.
func wantClose(t *testing.T, conn *websocket.Conn, code websocket.StatusCode, reason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		var closed websocket.CloseError
		if !errors.As(err, &closed) || closed.Code != code || closed.Reason != reason {
			t.Fatalf("got %v, want close %d %q", err, code, reason)
		}
		return
	}
}

// backInWorld rejoins the world after a run and waits for the run end to land.
func backInWorld(t *testing.T, h harness, name string) *websocket.Conn {
	t.Helper()
	conn, lobby := rejoinLobby(t, h.url, name)
	if lobby.GetSaving() {
		waitFor(t, conn, saving(false))
	}
	return conn
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
	conn, id := join(t, h)

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
	conn, id := join(t, h)

	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, TargetId: uint64(h.enemy), AbilityId: "basic"},
	}})
	msg := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetDamage() != nil })
	if d := msg.GetDamage(); d.GetSourceId() != id || d.GetAmount() <= 0 || d.GetTargetHealth() != 30-d.GetAmount() {
		t.Fatalf("damage = %v, want a hit from player %d", d, id)
	}
}

func TestBothClientsSeeEachOther(t *testing.T) {
	h := start(t)
	a, idA := join(t, h)
	b, idB := join(t, h)

	waitFor(t, a, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), idB) != nil })
	waitFor(t, b, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), idA) != nil })
}

func TestDisconnectRemovesPlayer(t *testing.T) {
	h := start(t)
	a, idA := join(t, h)
	b, _ := join(t, h)

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
	a, idA := join(t, h)
	b, idB := join(t, h)

	// Both clients must see every hit the same way. Starter weapon damage
	// rolls, so track health from each hit instead of assuming a number.
	health := int32(30)
	hit := func(conn *websocket.Conn, seq uint32, from uint64) {
		t.Helper()
		attack(t, conn, seq, h.enemy)
		var seen *gamev1.DamageEvent
		for _, c := range []*websocket.Conn{a, b} {
			d := waitFor(t, c, func(m *gamev1.ServerMessage) bool { return m.GetDamage() != nil }).GetDamage()
			if seen != nil && !proto.Equal(d, seen) {
				t.Fatalf("clients disagree: %v vs %v", seen, d)
			}
			seen = d
		}
		health = max(health-seen.GetAmount(), 0)
		if seen.GetSourceId() != from || seen.GetTargetHealth() != health || seen.GetKilled() != (health == 0) {
			t.Fatalf("damage = %v, want from %d leaving %d", seen, from, health)
		}
	}

	for seq := uint32(1); health > 0; seq++ {
		hit(a, seq, idA)
		if health > 0 {
			hit(b, seq, idB)
		}
		time.Sleep(sim.DefaultConfig().Classes.Abilities["slash"].Cooldown())
	}

	for _, conn := range []*websocket.Conn{a, b} {
		waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
			e := entity(m.GetSnapshot(), uint64(h.enemy))
			return e != nil && e.GetHealth() == 0
		})
	}

}

func TestJoinSendsInstanceLayout(t *testing.T) {
	cave := testCave()
	cave.Walls = []sim.Rect{{Min: sim.Vec2{X: 100, Y: 100}, Max: sim.Vec2{X: 164, Y: 164}}}
	cave.PlayerSpawns = []sim.Vec2{{X: 1, Y: 2}, {X: 3, Y: 4}}
	h := startCave(t, cave, sim.DefaultConfig())

	_, first := joinAccepted(t, h)
	_, second := joinAccepted(t, h)
	layout := first.GetLayout()
	if layout.GetName() != "test" || len(layout.GetWalls()) != 1 || layout.GetExtraction().GetMin().GetX() != 500 {
		t.Fatalf("layout = %v", layout)
	}
	if first.GetSpawn().GetX() != 1 || second.GetSpawn().GetX() != 3 {
		t.Fatalf("spawns = %v and %v, want each spawn point in turn", first.GetSpawn(), second.GetSpawn())
	}
}

// An extraction ends the run: the instance closes, the world opens the lobby
// on what the run banked, and the next walk into the entrance is a new run.
func TestExtractionEndsTheRun(t *testing.T) {
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	// Long enough for the other player to walk into the cave before ann leaves.
	cfg.ExtractTime = 600 * time.Millisecond
	h := startCave(t, cave, cfg)

	leaver, joined := joinAs(t, h, "ann")
	id := joined.GetPlayerId()
	other, _ := join(t, h)

	waitFor(t, other, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	waitFor(t, leaver, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	wantClose(t, leaver, websocket.StatusNormalClosure, "run over")
	waitFor(t, other, func(m *gamev1.ServerMessage) bool {
		return m.GetSnapshot() != nil && entity(m.GetSnapshot(), id) == nil
	})

	world := backInWorld(t, h, "ann")
	if _, again := enterInstance(t, world); again.GetPlayerId() == id || again.GetPlayerId() == 0 {
		t.Fatalf("entered again as %d, want a new player after %d", again.GetPlayerId(), id)
	}
}

func TestJoinEquipsStarterKit(t *testing.T) {
	h := start(t)
	conn, id := join(t, h)

	eq := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment()
	if len(eq.GetItems()) != len(items.StarterKit) || eq.GetItems()[0].GetItemDefId() != "rusty_sword" {
		t.Fatalf("equipment = %v", eq)
	}
	snap := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), id) != nil }).GetSnapshot()
	if p := entity(snap, id); p.GetMaxHealth() != 120 || p.GetHealth() != 120 {
		t.Fatalf("player = %v", p)
	}
}

func stat(eq *gamev1.EquipmentUpdated, name string) int32 {
	for _, s := range eq.GetStats() {
		if s.GetStat() == name {
			return s.GetValue()
		}
	}
	return -1
}

func TestUnequipSendsNewStats(t *testing.T) {
	h := start(t)
	conn, _ := join(t, h)

	armed := waitFor(t, conn, equipmentWith("rusty_sword")).GetEquipment()
	if stat(armed, "max_health") != 120 || stat(armed, "armor") < 5 {
		t.Fatalf("stats = %v", armed.GetStats())
	}
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	unarmed := waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		return m.GetEquipment() != nil && !equipmentWith("rusty_sword")(m)
	}).GetEquipment()
	if got, want := stat(unarmed, "damage"), int32(sim.DefaultConfig().AttackDamage); got != want {
		t.Fatalf("unarmed damage = %d, want %d", got, want)
	}
}

func sendItem(t *testing.T, conn *websocket.Conn, msg *gamev1.ClientMessage) {
	t.Helper()
	msg.ProtocolVersion = protocol.Version
	send(t, conn, msg)
}

func inventoryWith(defID string) func(*gamev1.ServerMessage) bool {
	return func(m *gamev1.ServerMessage) bool {
		for _, it := range m.GetInventory().GetItems() {
			if it.GetItemDefId() == defID {
				return true
			}
		}
		return false
	}
}

func equipmentWith(defID string) func(*gamev1.ServerMessage) bool {
	return func(m *gamev1.ServerMessage) bool {
		for _, it := range m.GetEquipment().GetItems() {
			if it.GetItemDefId() == defID {
				return true
			}
		}
		return false
	}
}

func TestLootThenEquip(t *testing.T) {
	h := start(t)
	conn, _ := join(t, h)

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	inv := waitFor(t, conn, inventoryWith("rusty_sword")).GetInventory()
	if inv.GetCapacity() != uint32(sim.DefaultConfig().InventorySize) {
		t.Fatalf("capacity = %d", inv.GetCapacity())
	}
	swordID := inv.GetItems()[0].GetId()

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Drop{Drop: &gamev1.DropRequest{ItemId: swordID}}})
	var lootID uint64
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		for _, e := range m.GetSnapshot().GetEntities() {
			if e.GetKind() == gamev1.EntityKind_ENTITY_KIND_LOOT && e.GetItemCount() == 1 {
				lootID = e.GetId()
				return true
			}
		}
		return false
	})

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Pickup{Pickup: &gamev1.PickupRequest{TargetId: lootID}}})
	waitFor(t, conn, inventoryWith("rusty_sword"))
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Equip{Equip: &gamev1.EquipRequest{ItemId: swordID}}})
	eq := waitFor(t, conn, equipmentWith("rusty_sword")).GetEquipment()
	if eq.GetItems()[0].GetId() != swordID {
		t.Fatalf("equipped %v, want the sword that was looted", eq.GetItems()[0])
	}
}

// A dropped connection ends the run with what the player carried, and the
// world lobby opens on it.
func TestReconnectGetsItemsBack(t *testing.T) {
	h := start(t)
	first, _ := joinAs(t, h, "ann")
	sendItem(t, first, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	sword := waitFor(t, first, inventoryWith("rusty_sword")).GetInventory().GetItems()[0]
	first.Close(websocket.StatusNormalClosure, "")

	conn := backInWorld(t, h, "ann")
	if inv := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetInventory() != nil }).GetInventory(); len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != sword.GetId() {
		t.Fatalf("inventory after reconnect = %v, want %v", inv.GetItems(), sword)
	}
}

// A second token for a name already in this instance is turned away, even
// for a new run.
func TestNameInUseIsRejected(t *testing.T) {
	h := start(t)
	joinAs(t, h, "ann")
	second := dial(t, h.instanceURL())
	sendToken(t, second, h.token(t, "ann", items.NewID(), time.Now()))
	wantClose(t, second, websocket.StatusPolicyViolation, "already in a run")
}

func TestAbilitiesOverTheWire(t *testing.T) {
	h := start(t)
	conn, joined := joinAccepted(t, h)
	if c := joined.GetClassInfo(); c.GetId() != "warrior" || c.GetBasic() != "slash" || len(c.GetAbilities()) != 3 {
		t.Fatalf("class = %v", c)
	}

	if cleave := joined.GetClassInfo().GetAbilities()[1]; cleave.GetUnlockLevel() != 2 {
		t.Fatalf("cleave = %v, want it to unlock at level 2", cleave)
	}

	// A new player is level 1.
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, AbilityId: "cleave", Aim: &gamev1.Vec2{X: 1}},
	}})
	r := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetAbilityRejected() != nil }).GetAbilityRejected()
	if r.GetSeq() != 1 || r.GetAbilityId() != "cleave" || r.GetReason() != sim.RejectLocked {
		t.Fatalf("rejected = %v", r)
	}

	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 2, AbilityId: "basic", Aim: &gamev1.Vec2{X: 1}},
	}})
	used := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetAbilityUsed() != nil }).GetAbilityUsed()
	if used.GetSourceId() != joined.GetPlayerId() || used.GetAbilityId() != "slash" || used.GetWindupMs() != 100 || used.GetAim().GetX() != 1 {
		t.Fatalf("used = %v", used)
	}
	d := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetDamage() != nil }).GetDamage()
	if d.GetSourceId() != joined.GetPlayerId() || d.GetTargetId() != uint64(h.enemy) {
		t.Fatalf("damage = %v", d)
	}
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		p := entity(m.GetSnapshot(), joined.GetPlayerId())
		return p != nil && p.GetClassId() == "warrior" && p.GetResource() == 8 && p.GetMaxResource() == 100 && p.GetLevel() == 1
	})
}

func TestUnknownClassIsRejected(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: "ann", ClassId: "necromancer"},
	}})
	_, _, err := conn.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("got %v, want policy violation close", err)
	}
}

// Two players loot one chest. What one takes leaves the other's window, and
// the other cannot take it too.
func TestLootWindowOverTheWire(t *testing.T) {
	cave := testCave()
	cave.Chests = []sim.Vec2{{X: 0, Y: 20}}
	h := startCave(t, cave, sim.DefaultConfig())
	ann, _ := join(t, h)
	bob, _ := join(t, h)

	chest := chestID(t, ann)
	isLoot := func(m *gamev1.ServerMessage) bool { return m.GetLoot().GetTargetId() == chest }
	open := &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_LootOpen{LootOpen: &gamev1.LootOpenRequest{TargetId: chest}}}
	sendItem(t, ann, open)
	sendItem(t, bob, open)
	contents := waitFor(t, ann, isLoot).GetLoot()
	waitFor(t, bob, isLoot)
	if len(contents.GetItems()) < 2 || contents.GetClosed() {
		t.Fatalf("contents = %v, want at least two items", contents)
	}

	want := contents.GetItems()[0]
	take := &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Pickup{Pickup: &gamev1.PickupRequest{TargetId: chest, ItemId: want.GetId()}}}
	sendItem(t, ann, take)
	waitFor(t, ann, inventoryWith(want.GetItemDefId()))
	left := waitFor(t, bob, isLoot).GetLoot()
	if len(left.GetItems()) != len(contents.GetItems())-1 {
		t.Fatalf("bob sees %v after ann took %v", left.GetItems(), want)
	}
	for _, it := range left.GetItems() {
		if it.GetId() == want.GetId() {
			t.Fatalf("bob still sees %v", want)
		}
	}

	sendItem(t, bob, take)
	r := waitFor(t, bob, func(m *gamev1.ServerMessage) bool { return m.GetPickupRejected() != nil }).GetPickupRejected()
	if r.GetTargetId() != chest || r.GetItemId() != want.GetId() || r.GetReason() != sim.PickupGone {
		t.Fatalf("rejected = %v", r)
	}
}

func TestLootOpenOutOfRangeIsClosed(t *testing.T) {
	cave := testCave()
	cave.Chests = []sim.Vec2{{X: -300}}
	h := startCave(t, cave, sim.DefaultConfig())
	conn, _ := join(t, h)

	chest := chestID(t, conn)
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_LootOpen{LootOpen: &gamev1.LootOpenRequest{TargetId: chest}}})
	l := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLoot() != nil }).GetLoot()
	if !l.GetClosed() || len(l.GetItems()) != 0 {
		t.Fatalf("loot = %v, want closed with no items", l)
	}
}

func chestID(t *testing.T, conn *websocket.Conn) uint64 {
	t.Helper()
	var chest uint64
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		for _, e := range m.GetSnapshot().GetEntities() {
			if e.GetKind() == gamev1.EntityKind_ENTITY_KIND_CHEST {
				chest = e.GetId()
				return true
			}
		}
		return false
	})
	return chest
}

func TestExtractionSavesXP(t *testing.T) {
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	h := startCave(t, cave, cfg)

	conn, joined := joinAs(t, h, "ann")
	id := joined.GetPlayerId()
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	// An empty run still banks the flat extraction bonus.
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return c.XP == cfg.Classes.Progression.ExtractBonus })
}

func TestJoinLoadsBankedXP(t *testing.T) {
	h := start(t)
	prog := sim.DefaultConfig().Classes.Progression
	seed(t, h, inventory.Character{Name: "ann", XP: prog.XPToReach(prog.MaxLevel), Loadout: h.srv.starterKit()})
	conn, joined := joinAs(t, h, "ann")
	if eq := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment(); stat(eq, "max_health") != 165 {
		t.Fatalf("max health = %d, want the level 10 bonus", stat(eq, "max_health"))
	}
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		return entity(m.GetSnapshot(), joined.GetPlayerId()).GetLevel() == 10
	})

	// Unlocked, so only the empty rage bar stops it.
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, AbilityId: "cleave", Aim: &gamev1.Vec2{X: 1}},
	}})
	if r := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetAbilityRejected() != nil }).GetAbilityRejected(); r.GetReason() != sim.RejectResource {
		t.Fatalf("rejected = %v, want resource", r)
	}
}

func TestJoinSendsCharacter(t *testing.T) {
	h := start(t)
	seed(t, h, inventory.Character{Name: "ann", XP: 120, Loadout: h.srv.starterKit()})
	_, joined := joinAs(t, h, "ann")
	c := joined.GetCharacter()
	want := &gamev1.CharacterInfo{Name: "ann", ClassId: "warrior", Level: 2, Xp: 120, LevelXp: 20, XpToNext: 150}
	if !proto.Equal(c, want) {
		t.Fatalf("character = %v, want %v", c, want)
	}
}

// progress reads until stop matches and returns the progression messages seen
// on the way, in order.
func progress(t *testing.T, conn *websocket.Conn, stop func(*gamev1.ServerMessage) bool) []*gamev1.ServerMessage {
	t.Helper()
	var seen []*gamev1.ServerMessage
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		if m.GetXpGained() != nil || m.GetLevelUp() != nil || m.GetRunEnded() != nil || m.GetExtracted() != nil {
			seen = append(seen, m)
		}
		return stop(m)
	})
	return seen
}

func kinds(msgs []*gamev1.ServerMessage) []string {
	var out []string
	for _, m := range msgs {
		switch {
		case m.GetXpGained() != nil:
			out = append(out, "xp")
		case m.GetLevelUp() != nil:
			out = append(out, "level_up")
		case m.GetRunEnded() != nil:
			out = append(out, "run_ended")
		case m.GetExtracted() != nil:
			out = append(out, "extracted")
		}
	}
	return out
}

func lootChest(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	chest := chestID(t, conn)
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_LootOpen{LootOpen: &gamev1.LootOpenRequest{TargetId: chest}}})
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Pickup{Pickup: &gamev1.PickupRequest{TargetId: chest}}})
}

// The chest's XP comes first, then the level it gave, then the summary on
// Extracted, and only then the close.
func TestExtractionSendsSummaryLast(t *testing.T) {
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cave.Chests = []sim.Vec2{{X: 0, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 500 * time.Millisecond
	h := startCave(t, cave, cfg)
	seed(t, h, inventory.Character{Name: "ann", XP: 90, Loadout: h.srv.starterKit()})

	leaver, joined := joinAs(t, h, "ann")
	id := joined.GetPlayerId()
	other, _ := join(t, h)
	lootChest(t, leaver)

	var bag []*gamev1.Item
	msgs := progress(t, leaver, func(m *gamev1.ServerMessage) bool {
		if inv := m.GetInventory(); inv != nil {
			bag = inv.GetItems()
		}
		return m.GetExtracted() != nil
	})
	if got := kinds(msgs); !slices.Equal(got, []string{"xp", "level_up", "extracted"}) {
		t.Fatalf("messages = %v, want xp, level_up, extracted", got)
	}
	if xp := msgs[0].GetXpGained(); xp.GetAmount() != 5 || xp.GetSource() != sim.XPChest || xp.GetRunXp() != 5 {
		t.Fatalf("xp = %v", xp)
	}
	if up := msgs[1].GetLevelUp(); up.GetLevel() != 2 || up.GetClassInfo().GetId() != "warrior" {
		t.Fatalf("level up = %v", up)
	}
	// 5 run XP, plus 25 and 25% of 5 for extracting.
	summary := msgs[2].GetExtracted().GetSummary()
	want := &gamev1.RunSummary{
		Extracted:    true,
		RunXp:        5,
		BankedXp:     31,
		LevelsGained: 1,
		Character:    &gamev1.CharacterInfo{Name: "ann", ClassId: "warrior", Level: 2, Xp: 121, LevelXp: 21, XpToNext: 150},
		Kept:         bag,
	}
	if len(bag) == 0 || !proto.Equal(summary, want) {
		t.Fatalf("summary = %v, want %v", summary, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var closed websocket.CloseError
	if _, b, err := leaver.Read(ctx); !errors.As(err, &closed) || closed.Code != websocket.StatusNormalClosure || closed.Reason != "run over" {
		t.Fatalf("after the summary got %x, %v, want the close", b, err)
	}

	seen := waitFor(t, other, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id }).GetExtracted()
	if seen.GetSummary() != nil {
		t.Fatalf("another player got the summary: %v", seen)
	}
}

func TestDeathSendsRunEnded(t *testing.T) {
	cave := testCave()
	cave.Chests = []sim.Vec2{{X: 0, Y: 20}}
	cfg := sim.DefaultConfig()
	melee := cfg.Archetypes[sim.Melee]
	melee.Damage = 10000
	cfg.Archetypes[sim.Melee] = melee
	h := serve(t, cave, cfg)
	potion := mustMake(t, h.srv, "health_potion")
	kit := h.srv.starterKit()
	seed(t, h, inventory.Character{Name: "ann", XP: 99, Loadout: kit, Stash: []items.Instance{potion}})

	conn, _ := joinAs(t, h, "ann")
	lootChest(t, conn)

	msgs := progress(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetRunEnded() != nil })
	if got := kinds(msgs); !slices.Equal(got, []string{"xp", "level_up", "run_ended"}) {
		t.Fatalf("messages = %v, want xp, level_up, run_ended", got)
	}
	// A quarter of 5, rounded down, is enough for level 2 from 99.
	want := &gamev1.RunSummary{
		RunXp:        5,
		BankedXp:     1,
		LevelsGained: 1,
		Character:    &gamev1.CharacterInfo{Name: "ann", ClassId: "warrior", Level: 2, Xp: 100, XpToNext: 150},
	}
	if summary := msgs[2].GetRunEnded().GetSummary(); !proto.Equal(summary, want) {
		t.Fatalf("summary = %v, want %v", summary, want)
	}

	// Back in the lobby with the starter kit, an empty bag and the stash as it was.
	wantClose(t, conn, websocket.StatusNormalClosure, "run over")
	conn = backInWorld(t, h, "ann")
	if eq := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment(); len(eq.GetItems()) != len(items.StarterKit) {
		t.Fatalf("lobby gear after death = %v, want the starter kit", eq.GetItems())
	}
	if inv := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetInventory() != nil }).GetInventory(); len(inv.GetItems()) != 0 {
		t.Fatalf("lobby bag after death = %v, want empty", inv.GetItems())
	}
	if st := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetStash() != nil }).GetStash(); len(st.GetItems()) != 1 || st.GetItems()[0].GetId() != potion.ID {
		t.Fatalf("stash after death = %v, want it untouched", st.GetItems())
	}
	waitSaved(t, h, "ann", func(c inventory.Character) bool {
		l := c.Loadout
		return c.XP == 100 && len(l.Items) == 0 && len(l.Equipment) == len(items.StarterKit) && l.Equipment[0].ID != "" && l.Equipment[0].ID != kit.Equipment[0].ID && len(c.Stash) == 1
	})
}

func mustMake(t *testing.T, s *Server, def string) items.Instance {
	t.Helper()
	inst, err := s.loot.Make(def, 1, items.Common)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}
