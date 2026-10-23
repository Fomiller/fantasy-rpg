package protocol

import (
	"testing"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
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
	w.Step(0)

	snap := Snapshot(w).GetSnapshot()
	if snap.GetTick() != 1 || len(snap.GetEntities()) != 2 {
		t.Fatalf("snapshot = %v", snap)
	}
	p, e := snap.GetEntities()[0], snap.GetEntities()[1]
	if p.GetId() != uint64(player) || p.GetKind() != gamev1.EntityKind_ENTITY_KIND_PLAYER || p.GetHealth() != 100 || p.GetPosition().GetX() != 1 {
		t.Fatalf("player entity = %v", p)
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
	if id := Joined(5).GetJoined().GetPlayerId(); id != 5 {
		t.Fatalf("joined player = %d, want 5", id)
	}
}
