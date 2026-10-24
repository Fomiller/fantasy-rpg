package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

type harness struct {
	url   string
	enemy sim.EntityID
	srv   *Server
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
	// Enemy stays put and cannot reach anyone, so the only damage in these
	// tests comes from players.
	for id, a := range cfg.Archetypes {
		a.DetectRadius, a.PatrolSpeed, a.Speed, a.AttackRange, a.Special = 0, 0, 0, 0, sim.SpecialNone
		cfg.Archetypes[id] = a
	}
	loot := items.NewRoller(items.Default(), 1)
	world := cave.NewWorld(cfg, loot)
	enemy := world.AddEnemy(sim.Vec2{X: 40, Y: 0}, sim.Melee)
	srv := New(world, cave, loot, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
	return harness{url: "ws" + strings.TrimPrefix(hs.URL, "http"), enemy: enemy, srv: srv}
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

var testers atomic.Int64

func joinAccepted(t *testing.T, conn *websocket.Conn) *gamev1.JoinAccepted {
	t.Helper()
	return joinAs(t, conn, fmt.Sprintf("tester%d", testers.Add(1)))
}

func joinAs(t *testing.T, conn *websocket.Conn, name string) *gamev1.JoinAccepted {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: name},
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
	if d := msg.GetDamage(); d.GetSourceId() != id || d.GetAmount() <= 0 || d.GetTargetHealth() != 30-d.GetAmount() {
		t.Fatalf("damage = %v, want a hit from player %d", d, id)
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

func TestJoinEquipsStarterKit(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	id := join(t, conn)

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
	conn := dial(t, h.url)
	join(t, conn)

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
	conn := dial(t, h.url)
	join(t, conn)

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

func TestReconnectGetsItemsBack(t *testing.T) {
	h := start(t)
	first := dial(t, h.url)
	joinAs(t, first, "ann")
	sendItem(t, first, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	sword := waitFor(t, first, inventoryWith("rusty_sword")).GetInventory().GetItems()[0]
	first.Close(websocket.StatusNormalClosure, "")

	// The server frees the name once it handles the disconnect, so retry
	// until it does.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn := dial(t, h.url)
		send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _, err := conn.Read(ctx)
		cancel()
		if err == nil {
			inv := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetInventory() != nil }).GetInventory()
			if len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != sword.GetId() {
				t.Fatalf("inventory after reconnect = %v, want %v", inv.GetItems(), sword)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not rejoin as ann: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNameInUseIsRejected(t *testing.T) {
	h := start(t)
	joinAs(t, dial(t, h.url), "ann")
	second := dial(t, h.url)
	send(t, second, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	_, _, err := second.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("got %v, want policy violation close", err)
	}
}

func TestAbilitiesOverTheWire(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	joined := joinAccepted(t, conn)
	if c := joined.GetClassInfo(); c.GetId() != "warrior" || c.GetBasic() != "slash" || len(c.GetAbilities()) != 3 {
		t.Fatalf("class = %v", c)
	}

	// A run starts with no rage.
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, AbilityId: "cleave", Aim: &gamev1.Vec2{X: 1}},
	}})
	r := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetAbilityRejected() != nil }).GetAbilityRejected()
	if r.GetSeq() != 1 || r.GetAbilityId() != "cleave" || r.GetReason() != sim.RejectResource {
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
		return p != nil && p.GetClassId() == "warrior" && p.GetResource() == 8 && p.GetMaxResource() == 100
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
	ann, bob := dial(t, h.url), dial(t, h.url)
	join(t, ann)
	join(t, bob)

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
	conn := dial(t, h.url)
	join(t, conn)

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

	conn := dial(t, h.url)
	id := joinAs(t, conn, "ann").GetPlayerId()
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted().GetPlayerId() == id })
	// An empty run still banks the flat extraction bonus.
	if xp, err := h.srv.Store.LoadXP(context.Background(), "ann"); err != nil || xp != cfg.Classes.Progression.ExtractBonus {
		t.Fatalf("saved xp = %d, %v", xp, err)
	}
}

func TestJoinLoadsBankedXP(t *testing.T) {
	h := start(t)
	prog := sim.DefaultConfig().Classes.Progression
	h.srv.Store.SaveXP(context.Background(), "ann", prog.XPToReach(prog.MaxLevel))
	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	if eq := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment(); stat(eq, "max_health") != 165 {
		t.Fatalf("max health = %d, want the level 10 bonus", stat(eq, "max_health"))
	}
}
