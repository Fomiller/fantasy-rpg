package sim

import (
	"testing"
	"time"
)

func enemyOf(t *testing.T, w *World, id EntityID) Enemy {
	t.Helper()
	e, ok := w.Enemy(id)
	if !ok {
		t.Fatalf("enemy %d missing", id)
	}
	return e
}

func TestEnemyPatrolsAroundHome(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	home := Vec2{100, 50}
	id := w.AddEnemy(home, 30)

	minX, maxX := home.X, home.X
	for range 200 {
		w.Step(dt)
		e := enemyOf(t, w, id)
		minX, maxX = min(minX, e.Pos.X), max(maxX, e.Pos.X)
		if e.Pos.Y != home.Y || e.State != EnemyPatrol {
			t.Fatalf("enemy = %+v, want patrol along y=%v", e, home.Y)
		}
	}
	r := DefaultConfig().PatrolRadius
	if minX != home.X-r || maxX != home.X+r {
		t.Fatalf("patrolled x %v..%v, want %v..%v", minX, maxX, home.X-r, home.X+r)
	}
}

func TestEnemyDetection(t *testing.T) {
	tests := []struct {
		name   string
		player Vec2
		walls  []Rect
		want   EnemyState
	}{
		{name: "in radius", player: Vec2{150, 0}, want: EnemyChase},
		{name: "out of radius", player: Vec2{250, 0}, want: EnemyPatrol},
		{name: "behind wall", player: Vec2{150, 0}, walls: []Rect{{Vec2{60, -50}, Vec2{80, 50}}}, want: EnemyPatrol},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorld(DefaultConfig(), tt.walls)
			player := w.AddPlayer(tt.player)
			id := w.AddEnemy(Vec2{}, 30)
			w.Step(dt)
			e := enemyOf(t, w, id)
			if e.State != tt.want {
				t.Fatalf("state = %v, want %v", e.State, tt.want)
			}
			if tt.want == EnemyChase && (e.Target != player || e.Vel.X <= 0) {
				t.Fatalf("enemy = %+v, want moving toward player %d", e, player)
			}
		})
	}
}

func TestEnemyChasesAndHitsOnCooldown(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, 30)

	var hits []DamageEvent
	for range 60 {
		for _, ev := range w.Step(dt).Damage {
			if ev.Source == id {
				hits = append(hits, ev)
			}
		}
	}
	// 3s total: about 0.5s to close in, then one hit per second.
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3: %+v", len(hits), hits)
	}
	if p, _ := w.Player(player); p.Health != 70 {
		t.Fatalf("player health = %d, want 70", p.Health)
	}
	e := enemyOf(t, w, id)
	touching := DefaultConfig().EnemyHalfSize + DefaultConfig().PlayerHalfSize
	if d := e.Pos.DistanceTo(Vec2{100, 0}); d < touching-0.01 || d > touching+0.01 {
		t.Fatalf("enemy stopped %v away, want %v", d, touching)
	}
}

func TestEnemyKillsPlayerThenGoesHome(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnemyDamage = cfg.PlayerHealth
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, 30)

	var killed bool
	for range 40 {
		for _, ev := range w.Step(dt).Damage {
			killed = killed || ev.Killed
		}
	}
	if !killed {
		t.Fatal("enemy never killed the player")
	}

	w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{1, 0}})
	steps(w, 40)
	if p, _ := w.Player(player); p.Pos != (Vec2{100, 0}) {
		t.Fatalf("dead player moved to %v", p.Pos)
	}
	if e := enemyOf(t, w, id); e.State != EnemyPatrol || e.Pos.DistanceTo(Vec2{}) > cfg.PatrolRadius {
		t.Fatalf("enemy = %+v, want back on patrol near home", e)
	}
}

func TestPlayerOutrunsEnemy(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{150, 0})
	id := w.AddEnemy(Vec2{}, 30)
	w.Step(dt)
	if enemyOf(t, w, id).State != EnemyChase {
		t.Fatal("enemy did not start chasing")
	}

	w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{1, 0}})
	var gaveUp bool
	for range 200 {
		w.Step(dt)
		if enemyOf(t, w, id).State == EnemyReturn {
			gaveUp = true
			break
		}
	}
	if !gaveUp {
		t.Fatal("enemy never gave up the chase")
	}

	w.Submit(MoveCommand{Player: player, Seq: 2, Direction: Vec2{}})
	for range 200 {
		w.Step(dt)
	}
	if e := enemyOf(t, w, id); e.State != EnemyPatrol {
		t.Fatalf("enemy = %+v, want patrol", e)
	}
}

func TestDeadEnemyDoesNothing(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{20, 0})
	id := w.AddEnemy(Vec2{}, 30)
	w.enemies[id].Health = 0

	if events := steps(w, int(2*time.Second/dt)); len(events) != 0 {
		t.Fatalf("dead enemy made events: %+v", events)
	}
	if p, _ := w.Player(player); p.Health != DefaultConfig().PlayerHealth {
		t.Fatalf("player health = %d", p.Health)
	}
	if e := enemyOf(t, w, id); e.Pos != (Vec2{}) {
		t.Fatalf("dead enemy moved to %v", e.Pos)
	}
}

func TestRectIntersects(t *testing.T) {
	r := Rect{Vec2{-10, -10}, Vec2{10, 10}}
	tests := []struct {
		name string
		a, b Vec2
		want bool
	}{
		{"crosses", Vec2{-50, 0}, Vec2{50, 0}, true},
		{"diagonal through", Vec2{-50, -50}, Vec2{50, 50}, true},
		{"stops short", Vec2{-50, 0}, Vec2{-20, 0}, false},
		{"passes beside", Vec2{-50, 20}, Vec2{50, 20}, false},
		{"starts inside", Vec2{0, 0}, Vec2{50, 50}, true},
	}
	for _, tt := range tests {
		if got := r.Intersects(tt.a, tt.b); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
