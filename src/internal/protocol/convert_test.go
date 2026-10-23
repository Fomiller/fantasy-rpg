package protocol

import (
	"testing"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

func TestCommandUsesConnectionPlayer(t *testing.T) {
	move, ok := Command(&gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Move{
		Move: &gamev1.MoveInput{Seq: 4, Direction: &gamev1.Vec2{X: 1, Y: -1}},
	}}, 7)
	if !ok || move != (sim.MoveCommand{Player: 7, Seq: 4, Direction: sim.Vec2{X: 1, Y: -1}}) {
		t.Fatalf("move = %+v, %v", move, ok)
	}

	attack, ok := Command(&gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 2, TargetId: 9, AbilityId: "basic"},
	}}, 7)
	if !ok || attack != (sim.AttackCommand{Player: 7, Seq: 2, Target: 9, Ability: "basic"}) {
		t.Fatalf("attack = %+v, %v", attack, ok)
	}

	if _, ok := Command(&gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Join{}}, 7); ok {
		t.Fatal("join is not a sim command")
	}
}

func TestSnapshotListsPlayersThenEnemies(t *testing.T) {
	cfg := sim.DefaultConfig()
	cfg.DetectRadius = 0
	w := sim.NewWorld(cfg, nil)
	enemy := w.AddEnemy(sim.Vec2{X: 5, Y: 6}, 30)
	player := w.AddPlayer(sim.Vec2{X: 1, Y: 2})
	w.Submit(sim.MoveCommand{Player: player, Seq: 3})
	w.Step(0)

	snap := Snapshot(w).GetSnapshot()
	if snap.GetTick() != 1 || len(snap.GetEntities()) != 2 {
		t.Fatalf("snapshot = %v", snap)
	}
	p, e := snap.GetEntities()[0], snap.GetEntities()[1]
	if p.GetId() != uint64(player) || p.GetKind() != gamev1.EntityKind_ENTITY_KIND_PLAYER || p.GetHealth() != 100 || p.GetPosition().GetX() != 1 {
		t.Fatalf("player entity = %v", p)
	}
	if p.GetLastInputSeq() != 3 || p.GetMaxSpeed() != 220 {
		t.Fatalf("player prediction fields = %v", p)
	}
	if e.GetId() != uint64(enemy) || e.GetKind() != gamev1.EntityKind_ENTITY_KIND_ENEMY || e.GetHealth() != 30 {
		t.Fatalf("enemy entity = %v", e)
	}
}

func TestDamageAndJoined(t *testing.T) {
	d := Damage(sim.DamageEvent{Source: 2, Target: 1, Amount: 10, TargetHealth: 0, Killed: true}).GetDamage()
	if d.GetSourceId() != 2 || d.GetTargetId() != 1 || d.GetAmount() != 10 || !d.GetKilled() {
		t.Fatalf("damage = %v", d)
	}
	cave := caves.Loop()
	j := Joined(5, cave, cave.PlayerSpawns[0], 30).GetJoined()
	if j.GetPlayerId() != 5 || j.GetTickRate() != 30 || len(j.GetCave().GetWalls()) != len(cave.Walls) || j.GetCave().GetName() != "loop" {
		t.Fatalf("joined = %v", j)
	}
	if j.GetSpawn().GetX() != float32(cave.PlayerSpawns[0].X) || j.GetCave().GetExtraction().GetMax().GetX() != float32(cave.Extraction.Max.X) {
		t.Fatalf("joined spawn or exit wrong: %v", j)
	}
	if id := Extracted(5).GetExtracted().GetPlayerId(); id != 5 {
		t.Fatalf("extracted player = %d, want 5", id)
	}
}

func TestItemAndEquipment(t *testing.T) {
	r := items.NewRoller(items.Default(), 1)
	sword, err := r.Make("iron_sword", 3, items.Rare)
	if err != nil {
		t.Fatal(err)
	}
	vest, _ := r.Make("leather_vest", 1, items.Common)

	w := sim.NewWorld(sim.DefaultConfig(), nil)
	id := w.AddPlayer(sim.Vec2{})
	// Equip the vest first to show the message follows slot order, not equip order.
	for _, inst := range []items.Instance{vest, sword} {
		if _, _, err := w.Equip(id, inst); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := w.Player(id)
	got := Equipment(p).GetEquipment().GetItems()
	if len(got) != 2 || got[0].GetSlot() != "weapon" || got[1].GetSlot() != "chest" {
		t.Fatalf("equipment = %v", got)
	}
	it := got[0]
	if it.GetId() != sword.ID || it.GetName() != "Iron Sword" || it.GetRarity() != gamev1.Rarity_RARITY_RARE || it.GetLevel() != 3 {
		t.Fatalf("item = %v", it)
	}
	if len(it.GetAffixes()) != len(sword.Affixes) || it.GetBase()[0].GetStat() != "damage" || it.GetBase()[0].GetValue() != int32(sword.Base[items.Damage]) {
		t.Fatalf("item stats = %v", it)
	}
}
