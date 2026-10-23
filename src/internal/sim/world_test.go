package sim

import (
	"math"
	"testing"
	"time"
)

const dt = 50 * time.Millisecond

// passive keeps enemies still and blind, for tests about players.
func passive() Config {
	c := DefaultConfig()
	c.DetectRadius = 0
	c.PatrolSpeed = 0
	return c
}

func steps(w *World, n int) []DamageEvent {
	var events []DamageEvent
	for range n {
		events = append(events, w.Step(dt)...)
	}
	return events
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
	w := NewWorld(DefaultConfig(), TestRoomWalls())
	id := w.AddPlayer(Vec2{})
	w.Submit(MoveCommand{Player: id, Seq: 1, Direction: Vec2{1, 0}})
	steps(w, 40)
	p, _ := w.Player(id)
	if p.Pos.X != 152 || p.Vel.X != 0 {
		t.Fatalf("pos.x = %v vel.x = %v, want stopped at 152", p.Pos.X, p.Vel.X)
	}
}

func TestWallSlideKeepsOtherAxis(t *testing.T) {
	w := NewWorld(DefaultConfig(), TestRoomWalls())
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
	enemy := w.AddEnemy(Vec2{40, 0}, 30)

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
			enemy := w.AddEnemy(Vec2{40, 0}, 30)
			tt.setup(w, player, enemy)
			before, _ := w.Enemy(enemy)

			w.Submit(AttackCommand{Player: player, Seq: 1, Target: tt.target(enemy), Ability: "basic"})
			if events := w.Step(dt); len(events) != 0 {
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
	w.AddEnemy(Vec2{}, 30)
	w.AddEnemy(Vec2{}, 30)
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
