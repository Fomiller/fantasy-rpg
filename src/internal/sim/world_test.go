package sim

import (
	"math"
	"slices"
	"testing"
	"time"
)

const dt = 50 * time.Millisecond

// passive keeps enemies still, blind and harmless, for tests about players.
// A hit still turns them to chase, but they cannot move or reach anyone.
func passive() Config {
	c := DefaultConfig()
	for id, a := range c.Archetypes {
		a.DetectRadius, a.PatrolSpeed, a.Speed, a.AttackRange, a.Special = 0, 0, 0, 0, SpecialNone
		c.Archetypes[id] = a
	}
	return c
}

// steps returns the damage from n steps.
func steps(w *World, n int) []DamageEvent {
	var events []DamageEvent
	for range n {
		events = append(events, w.Step(dt).Damage...)
	}
	return events
}

// testRoomWalls is a closed room with a pillar at x=168..232, matching the
// room the Godot player_test builds.
func testRoomWalls() []Rect {
	return []Rect{
		{Vec2{-616, -352}, Vec2{616, -320}},
		{Vec2{-616, 320}, Vec2{616, 352}},
		{Vec2{-632, -320}, Vec2{-600, 320}},
		{Vec2{600, -320}, Vec2{632, 320}},
		{Vec2{168, -32}, Vec2{232, 32}},
	}
}

func TestMoveAcceleratesThenHitsMaxSpeed(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 0}})

	steps(w, 1)
	p, _ := w.Player(id)
	if p.Vel.X <= 0 || p.Vel.X >= 220 {
		t.Fatalf("after one tick vel.x = %v, want between 0 and max speed", p.Vel.X)
	}
	steps(w, 10)
	p, _ = w.Player(id)
	if p.Vel.X != 220 {
		t.Fatalf("vel.x = %v, want 220", p.Vel.X)
	}
}

func TestDiagonalInputIsNormalized(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 1}})
	steps(w, 20)
	p, _ := w.Player(id)
	if speed := p.Vel.Len(); math.Abs(speed-220) > 1e-6 {
		t.Fatalf("diagonal speed = %v, want 220", speed)
	}
}

func TestStaleMoveIsIgnored(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 5, Direction: Vec2{1, 0}})
	w.Submit(MoveCommand{Player: id, Seq: 4, Direction: Vec2{-1, 0}})
	steps(w, 5)
	p, _ := w.Player(id)
	if p.Vel.X <= 0 || p.LastInputSeq != 5 {
		t.Fatalf("vel.x = %v seq = %d, want older input ignored", p.Vel.X, p.LastInputSeq)
	}
}

func TestFrictionStopsPlayer(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 0}})
	steps(w, 10)
	w.Submit(MoveCommand{Player: id, Seq: 2})
	steps(w, 10)
	p, _ := w.Player(id)
	if p.Vel != (Vec2{}) {
		t.Fatalf("vel = %v, want zero", p.Vel)
	}
}

// Same setup as client/godot/tests/player_test.gd, so both sides stop at the same spot.
func TestPillarStopsPlayerLikeClient(t *testing.T) {
	w := NewWorld(DefaultConfig(), testRoomWalls())
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 0}})
	steps(w, 40)
	p, _ := w.Player(id)
	if p.Pos.X != 152 || p.Vel.X != 0 {
		t.Fatalf("pos.x = %v vel.x = %v, want stopped at 152", p.Pos.X, p.Vel.X)
	}
}

func TestWallSlideKeepsOtherAxis(t *testing.T) {
	w := NewWorld(DefaultConfig(), testRoomWalls())
	id := w.AddPlayer(Vec2{0, 300})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 1}})
	steps(w, 20)
	p, _ := w.Player(id)
	if p.Pos.Y != 304 || p.Pos.X <= 0 {
		t.Fatalf("pos = %v, want pinned to bottom wall at y=304 and still moving right", p.Pos)
	}
}

func TestAttackDamagesAndKills(t *testing.T) {
	w := NewWorld(passive(), nil)
	player := w.AddPlayer(Vec2{})
	enemy := w.AddEnemy(Vec2{40, 0}, Melee)

	var events []DamageEvent
	for seq := uint32(1); seq <= 3; seq++ {
		w.Submit(AttackCommand{Player: player, Seq: seq, Target: enemy, Ability: "basic"})
		events = append(events, steps(w, 10)...)
	}
	if len(events) != 3 {
		t.Fatalf("got %d damage events, want 3", len(events))
	}
	last := events[2]
	if !last.Killed || last.TargetHealth != 0 {
		t.Fatalf("last event = %+v, want kill", last)
	}
	if e, _ := w.Enemy(enemy); e.Health != 0 {
		t.Fatalf("enemy health = %d, want 0", e.Health)
	}
}

func TestAttackRejected(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(w *World, player, enemy EntityID)
		target func(enemy EntityID) EntityID
	}{
		{
			name:   "out of range",
			setup:  func(w *World, player, _ EntityID) { w.players[player].Pos = Vec2{-200, 0} },
			target: func(e EntityID) EntityID { return e },
		},
		{
			name:   "on cooldown",
			setup:  func(w *World, player, _ EntityID) { w.players[player].cooldown = time.Second },
			target: func(e EntityID) EntityID { return e },
		},
		{
			name:   "target already dead",
			setup:  func(w *World, _, enemy EntityID) { w.enemies[enemy].Health = 0 },
			target: func(e EntityID) EntityID { return e },
		},
		{
			name:   "unknown target",
			setup:  func(*World, EntityID, EntityID) {},
			target: func(EntityID) EntityID { return 999 },
		},
		{
			name:   "dead attacker",
			setup:  func(w *World, player, _ EntityID) { w.players[player].Health = 0 },
			target: func(e EntityID) EntityID { return e },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorld(passive(), nil)
			player := w.AddPlayer(Vec2{})
			enemy := w.AddEnemy(Vec2{40, 0}, Melee)
			tt.setup(w, player, enemy)
			before, _ := w.Enemy(enemy)

			w.Submit(AttackCommand{Player: player, Seq: 1, Target: tt.target(enemy), Ability: "basic"})
			if events := w.Step(dt).Damage; len(events) != 0 {
				t.Fatalf("got %+v, want attack rejected", events)
			}
			if after, _ := w.Enemy(enemy); after.Health != before.Health {
				t.Fatalf("enemy health changed from %d to %d", before.Health, after.Health)
			}
		})
	}
}

func TestStepAdvancesTick(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	steps(w, 3)
	if w.Tick != 3 {
		t.Fatalf("tick = %d, want 3", w.Tick)
	}
}

func TestNonFiniteMoveIsIgnored(t *testing.T) {
	for _, dir := range []Vec2{{math.NaN(), 0}, {0, math.Inf(1)}} {
		w := NewWorld(DefaultConfig(), nil)
		id := w.AddPlayer(Vec2{})
		w.Submit(MoveCommand{Player: id, Seq: 1, Direction: dir})
		steps(w, 5)
		p, _ := w.Player(id)
		if p.Pos != (Vec2{}) || p.LastInputSeq != 0 {
			t.Fatalf("direction %v moved the player to %v", dir, p.Pos)
		}
	}
}

func TestRemovePlayerAndSortedLists(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	a := w.AddPlayer(Vec2{})
	b := w.AddPlayer(Vec2{})
	w.AddEnemy(Vec2{}, Melee)
	w.AddEnemy(Vec2{}, Ranged)
	w.RemovePlayer(a)

	players := w.Players()
	if len(players) != 1 || players[0].ID != b {
		t.Fatalf("players = %+v, want only %d", players, b)
	}
	enemies := w.Enemies()
	if len(enemies) != 2 || enemies[0].ID > enemies[1].ID {
		t.Fatalf("enemies = %+v, want two sorted by ID", enemies)
	}
}

func TestStandingInExtractionZoneExtracts(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	w.SetExtraction(Rect{Vec2{-50, -50}, Vec2{50, 50}})
	inside := w.AddPlayer(Vec2{})
	outside := w.AddPlayer(Vec2{200, 0})

	need := int(DefaultConfig().ExtractTime / dt)
	for i := 1; i < need; i++ {
		if ev := w.Step(dt); len(ev.Extracted) != 0 {
			t.Fatalf("step %d: extracted %v too early", i, ev.Extracted)
		}
	}
	if p, _ := w.Player(inside); p.ExtractProgress < 0.9 || p.ExtractProgress >= 1 {
		t.Fatalf("progress = %v, want just under 1", p.ExtractProgress)
	}
	if ev := w.Step(dt); !slices.Equal(ev.Extracted, []EntityID{inside}) {
		t.Fatalf("extracted = %v, want [%d]", ev.Extracted, inside)
	}
	if _, ok := w.Player(inside); ok {
		t.Fatal("extracted player is still in the world")
	}
	if p, _ := w.Player(outside); p.ExtractProgress != 0 {
		t.Fatalf("outside player progress = %v", p.ExtractProgress)
	}
}

func TestLeavingExtractionZoneResetsTimer(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	w.SetExtraction(Rect{Vec2{-50, -50}, Vec2{50, 50}})
	id := w.AddPlayer(Vec2{})
	steps(w, 20)

	w.players[id].Pos = Vec2{200, 0}
	w.Step(dt)
	if p, _ := w.Player(id); p.ExtractProgress != 0 {
		t.Fatalf("progress = %v after leaving, want 0", p.ExtractProgress)
	}
}

func TestDeadPlayerCannotExtract(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	w.SetExtraction(Rect{Vec2{-50, -50}, Vec2{50, 50}})
	id := w.AddPlayer(Vec2{})
	w.players[id].Health = 0
	for range 2 * int(DefaultConfig().ExtractTime/dt) {
		if ev := w.Step(dt); len(ev.Extracted) != 0 {
			t.Fatalf("dead player extracted")
		}
	}
}
