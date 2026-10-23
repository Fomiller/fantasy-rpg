package sim

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func enemyOf(t *testing.T, w *World, id EntityID) Enemy {
	t.Helper()
	e, ok := w.Enemy(id)
	if !ok {
		t.Fatalf("enemy %d missing", id)
	}
	return e
}

func playerOf(t *testing.T, w *World, id EntityID) Player {
	t.Helper()
	p, ok := w.Player(id)
	if !ok {
		t.Fatalf("player %d missing", id)
	}
	return p
}

func ticks(d time.Duration) int {
	return int(d / dt)
}

// tune changes one archetype's row in a copy of the default config.
func tune(id ArchetypeID, f func(*Archetype)) Config {
	cfg := DefaultConfig()
	a := cfg.Archetypes[id]
	f(&a)
	cfg.Archetypes[id] = a
	return cfg
}

// stepUntil steps until done holds, and fails after n steps.
func stepUntil(t *testing.T, w *World, n int, done func(Events) bool) {
	t.Helper()
	for range n {
		if done(w.Step(dt)) {
			return
		}
	}
	t.Fatalf("not done after %d steps", n)
}

func hitsFrom(events []DamageEvent, source EntityID) []DamageEvent {
	var out []DamageEvent
	for _, ev := range events {
		if ev.Source == source {
			out = append(out, ev)
		}
	}
	return out
}

func TestEnemyPatrolsAroundHome(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	home := Vec2{100, 50}
	id := w.AddEnemy(home, Melee)

	minX, maxX := home.X, home.X
	idled := false
	for range 300 {
		w.Step(dt)
		e := enemyOf(t, w, id)
		minX, maxX = min(minX, e.Pos.X), max(maxX, e.Pos.X)
		idled = idled || e.State == EnemyIdle
		if e.Pos.Y != home.Y || (e.State != EnemyPatrol && e.State != EnemyIdle) {
			t.Fatalf("enemy = %+v, want patrol along y=%v", e, home.Y)
		}
	}
	r := DefaultConfig().Archetypes[Melee].PatrolRadius
	if minX > home.X-r+arriveDistance || maxX < home.X+r-arriveDistance || minX < home.X-r || maxX > home.X+r || !idled {
		t.Fatalf("patrolled x %v..%v, idled %v, want %v..%v with a pause at the ends", minX, maxX, idled, home.X-r, home.X+r)
	}
}

func TestEnemyDetection(t *testing.T) {
	tests := []struct {
		name   string
		player Vec2
		walls  []Rect
		want   EnemyState
	}{
		{name: "in radius", player: Vec2{150, 0}, want: EnemyDetect},
		{name: "out of radius", player: Vec2{250, 0}, want: EnemyPatrol},
		{name: "behind wall", player: Vec2{150, 0}, walls: []Rect{{Vec2{60, -50}, Vec2{80, 50}}}, want: EnemyPatrol},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorld(DefaultConfig(), tt.walls)
			player := w.AddPlayer(tt.player)
			id := w.AddEnemy(Vec2{}, Melee)
			w.Step(dt)
			e := enemyOf(t, w, id)
			if e.State != tt.want {
				t.Fatalf("state = %v, want %v", e.State, tt.want)
			}
			if tt.want == EnemyDetect && (e.Target != player || e.Vel != (Vec2{}) || e.Aim != (Vec2{1, 0})) {
				t.Fatalf("enemy = %+v, want standing and facing player %d", e, player)
			}
		})
	}
}

func TestDetectWaitsReactTimeThenChases(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	w.AddPlayer(Vec2{150, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	react := ticks(DefaultConfig().Archetypes[Melee].ReactTime)
	for i := range react {
		w.Step(dt)
		if e := enemyOf(t, w, id); e.State != EnemyDetect {
			t.Fatalf("step %d: state = %v, want detect for the whole react time", i, e.State)
		}
	}
	w.Step(dt)
	if e := enemyOf(t, w, id); e.State != EnemyChase || e.Vel.X <= 0 {
		t.Fatalf("enemy = %+v, want chasing", e)
	}
}

func TestMeleeHitsAfterWindup(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	melee := DefaultConfig().Archetypes[Melee]

	var states []EnemyState
	var hits []int
	for i := range 60 {
		if len(hitsFrom(w.Step(dt).Damage, id)) > 0 {
			hits = append(hits, i)
		}
		states = append(states, enemyOf(t, w, id).State)
	}
	if len(hits) != 2 {
		t.Fatalf("hits at steps %v, want 2 in 3s", hits)
	}
	windup := ticks(melee.Windup)
	for _, at := range hits {
		for i := at - windup; i <= at; i++ {
			if states[i] != EnemyAttack {
				t.Fatalf("step %d is %v, want the %d steps before the hit at %d in attack", i, states[i], windup, at)
			}
		}
	}
	if gap := hits[1] - hits[0]; gap < ticks(melee.Cooldown+melee.Windup) {
		t.Fatalf("hits %d steps apart, want at least cooldown plus windup", gap)
	}
	if p := playerOf(t, w, player); p.Health != p.MaxHealth-2*w.damageTaken(w.players[player], melee.Damage) {
		t.Fatalf("player health = %d", p.Health)
	}
	touching := DefaultConfig().EnemyHalfSize + DefaultConfig().PlayerHalfSize
	if d := enemyOf(t, w, id).Pos.DistanceTo(Vec2{100, 0}); d < touching-0.01 || d > touching+0.01 {
		t.Fatalf("enemy stopped %v away, want %v", d, touching)
	}
}

func TestSteppingOutOfWindupDodges(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	stepUntil(t, w, 40, func(Events) bool { return enemyOf(t, w, id).State == EnemyAttack })

	w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{1, 0}})
	if hits := hitsFrom(steps(w, ticks(DefaultConfig().Archetypes[Melee].Windup)+1), id); len(hits) != 0 {
		t.Fatalf("hit a player who stepped away: %+v", hits)
	}
	if e := enemyOf(t, w, id); e.State != EnemyChase {
		t.Fatalf("state = %v, want back to chase after the swing", e.State)
	}
}

func TestHitWakesEnemyThatCannotSee(t *testing.T) {
	cfg := tune(Melee, func(a *Archetype) { a.DetectRadius = 0 })
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{})
	id := w.AddEnemy(Vec2{50, 0}, Melee)
	swing(w, AttackCommand{Player: player, Seq: 1, Target: id})
	if e := enemyOf(t, w, id); e.State != EnemyChase || e.Target != player {
		t.Fatalf("enemy = %+v, want chasing its attacker", e)
	}
}

func TestLowHealthMeleeFleesThenHeals(t *testing.T) {
	cfg := DefaultConfig()
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	stepUntil(t, w, 40, func(Events) bool { return enemyOf(t, w, id).State == EnemyChase })
	w.enemies[id].Health = 5

	w.Step(dt)
	if e := enemyOf(t, w, id); e.State != EnemyRetreat {
		t.Fatalf("state = %v, want retreat at low health", e.State)
	}
	w.Step(dt)
	if e := enemyOf(t, w, id); e.Vel.X >= 0 {
		t.Fatalf("vel = %v, want moving away from the player", e.Vel)
	}
	// Hide the player. A fleeing enemy goes home instead of searching.
	w.players[player].Pos = Vec2{5000, 0}
	w.Step(dt)
	if e := enemyOf(t, w, id); e.State != EnemyReturn {
		t.Fatalf("state = %v, want return", e.State)
	}
	stepUntil(t, w, 200, func(Events) bool { return enemyOf(t, w, id).State == EnemyPatrol })
	if e := enemyOf(t, w, id); e.Health != e.MaxHealth {
		t.Fatalf("health = %d, want healed to %d at home", e.Health, e.MaxHealth)
	}
}

func TestRangedKeepsDistanceAndShoots(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, Ranged)
	ranged := DefaultConfig().Archetypes[Ranged]

	var events []DamageEvent
	retreated, shot := false, false
	for range 80 {
		events = append(events, w.Step(dt).Damage...)
		e := enemyOf(t, w, id)
		retreated = retreated || e.State == EnemyRetreat
		shot = shot || len(w.Projectiles()) > 0
		if d := e.Pos.DistanceTo(Vec2{100, 0}); d < 100-0.01 {
			t.Fatalf("ranged enemy closed to %v", d)
		}
	}
	e := enemyOf(t, w, id)
	if d := e.Pos.DistanceTo(Vec2{100, 0}); !retreated || d <= ranged.KeepAway || d > ranged.AttackRange {
		t.Fatalf("retreated %v, ended %v away, want backed off to between %v and %v", retreated, d, ranged.KeepAway, ranged.AttackRange)
	}
	hits := hitsFrom(events, id)
	want := w.damageTaken(w.players[player], ranged.Damage)
	if !shot || len(hits) == 0 || hits[0].Amount != want {
		t.Fatalf("shot %v, hits %+v, want projectiles that hit for %d", shot, hits, want)
	}
	if p := playerOf(t, w, player); p.Health != p.MaxHealth-len(hits)*want {
		t.Fatalf("player health = %d after %d hits", p.Health, len(hits))
	}
}

// shoot fires one projectile from an enemy at the origin along +x.
func shoot(w *World) EntityID {
	id := w.AddEnemy(Vec2{}, Ranged)
	e := w.enemies[id]
	e.Health, e.Aim = 0, Vec2{1, 0}
	w.fire(e, w.cfg.Archetypes[Ranged])
	return id
}

func TestProjectiles(t *testing.T) {
	tests := []struct {
		name   string
		walls  []Rect
		player Vec2
		hit    bool
	}{
		{name: "hits an exposed player", player: Vec2{200, 0}, hit: true},
		{name: "wall blocks it", walls: []Rect{{Vec2{60, -50}, Vec2{80, 50}}}, player: Vec2{200, 0}},
		{name: "grazes past", player: Vec2{200, 23}},
		{name: "runs out of range", player: Vec2{400, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorld(DefaultConfig(), tt.walls)
			player := w.AddPlayer(tt.player)
			// An enemy in the line of fire is not hit.
			bystander := w.AddEnemy(Vec2{40, 0}, Melee)
			shooter := shoot(w)

			events := steps(w, 40)
			if len(w.Projectiles()) != 0 {
				t.Fatalf("projectile still flying: %+v", w.Projectiles())
			}
			hits := hitsFrom(events, shooter)
			if tt.hit != (len(hits) == 1) || (tt.hit && hits[0].Target != player) {
				t.Fatalf("hits = %+v, want hit %v", hits, tt.hit)
			}
			if e := enemyOf(t, w, bystander); e.Health != e.MaxHealth {
				t.Fatalf("projectile hit an enemy: %+v", e)
			}
		})
	}
}

func TestProjectileArmorAndDodge(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{200, 0})
	// With the Warrior's 5 base armor that makes 50.
	equip(t, w, player, gear(items.Chest, map[items.Stat]int{items.Armor: 45}))
	shooter := shoot(w)
	hits := hitsFrom(steps(w, 20), shooter)
	if len(hits) != 1 || hits[0].Amount != DefaultConfig().Archetypes[Ranged].Damage/2 {
		t.Fatalf("hits = %+v, want armor to halve the damage", hits)
	}

	w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{0, 1}})
	shoot(w)
	if hits := steps(w, 20); len(hits) != 0 {
		t.Fatalf("hit a player moving sideways: %+v", hits)
	}
}

func TestEliteCharge(t *testing.T) {
	elite := DefaultConfig().Archetypes[Elite]
	charged := func(t *testing.T, at float64) (*World, EntityID, EntityID, bool) {
		w := NewWorld(DefaultConfig(), nil)
		player := w.AddPlayer(Vec2{at, 0})
		id := w.AddEnemy(Vec2{}, Elite)
		for range 20 {
			w.Step(dt)
			if enemyOf(t, w, id).State == EnemyCharge {
				return w, player, id, true
			}
		}
		return w, player, id, false
	}

	t.Run("not when too close", func(t *testing.T) {
		if _, _, _, ok := charged(t, 60); ok {
			t.Fatal("charged a player inside ChargeMin")
		}
	})

	t.Run("in range it winds up then dashes and hits once", func(t *testing.T) {
		w, player, id, ok := charged(t, 180)
		if !ok {
			t.Fatal("never charged")
		}
		aim, start := enemyOf(t, w, id).Aim, enemyOf(t, w, id).Pos
		if aim != (Vec2{1, 0}) {
			t.Fatalf("aim = %v, want locked on the player", aim)
		}
		for range ticks(elite.ChargeWindup) - 1 {
			w.Step(dt)
			if e := enemyOf(t, w, id); e.Pos != start || e.State != EnemyCharge {
				t.Fatalf("enemy = %+v, want standing still for the windup", e)
			}
		}
		var hits []DamageEvent
		for range ticks(elite.ChargeTime) + 1 {
			hits = append(hits, hitsFrom(w.Step(dt).Damage, id)...)
		}
		e := enemyOf(t, w, id)
		want := w.damageTaken(w.players[player], elite.ChargeDamage)
		if len(hits) != 1 || hits[0].Amount != want || hits[0].Target != player {
			t.Fatalf("hits = %+v, want one charge hit for %d", hits, want)
		}
		if e.State != EnemyChase || e.specialCooldown != elite.ChargeCooldown {
			t.Fatalf("enemy = %+v, want chase with the charge on cooldown", e)
		}
		if d := e.Pos.Sub(start).X; d < elite.ChargeSpeed*elite.ChargeTime.Seconds()-1 {
			t.Fatalf("dashed %v, want the full charge", d)
		}

		// Held in the band, it must wait out the cooldown before charging again.
		w.players[player].Health = 1000
		hold := func() {
			w.enemies[id].Pos = Vec2{}
			w.players[player].Pos = Vec2{180, 0}
		}
		for range ticks(elite.ChargeCooldown) - 1 {
			hold()
			w.Step(dt)
			if s := enemyOf(t, w, id).State; s == EnemyCharge {
				t.Fatal("charged again while on cooldown")
			}
		}
		stepUntil(t, w, 3, func(Events) bool { hold(); return enemyOf(t, w, id).State == EnemyCharge })
	})

	t.Run("a step out of the line dodges it", func(t *testing.T) {
		w, player, id, ok := charged(t, 180)
		if !ok {
			t.Fatal("never charged")
		}
		w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{0, 1}})
		if hits := hitsFrom(steps(w, ticks(elite.ChargeWindup+elite.ChargeTime)), id); len(hits) != 0 {
			t.Fatalf("hits = %+v, want the dodge to work", hits)
		}
	})

	t.Run("a wall stops the dash", func(t *testing.T) {
		w := NewWorld(DefaultConfig(), []Rect{{Vec2{80, -200}, Vec2{100, -40}}})
		w.AddPlayer(Vec2{180, 0})
		id := w.AddEnemy(Vec2{}, Elite)
		stepUntil(t, w, 20, func(Events) bool { return enemyOf(t, w, id).State == EnemyCharge })
		w.walls = append(w.walls, Rect{Vec2{80, -40}, Vec2{100, 40}})
		stepUntil(t, w, ticks(elite.ChargeWindup+elite.ChargeTime), func(Events) bool { return enemyOf(t, w, id).State != EnemyCharge })
		if e := enemyOf(t, w, id); e.Pos.X != 80-DefaultConfig().EnemyHalfSize {
			t.Fatalf("enemy at %v, want stopped against the wall", e.Pos)
		}
	})
}

func TestLeashSendsEnemyHomeAndHeals(t *testing.T) {
	cfg := tune(Melee, func(a *Archetype) { a.LoseRadius = 10000 })
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{150, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	w.enemies[id].Health = 20
	w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{1, 0}})

	stepUntil(t, w, 200, func(Events) bool { return enemyOf(t, w, id).State == EnemyReturn })
	// It turns around the tick it passes the leash, so it is one step back inside.
	if d := enemyOf(t, w, id).Pos.DistanceTo(Vec2{}); d < cfg.Archetypes[Melee].LeashRadius-10 {
		t.Fatalf("gave up %v from home, want past the leash", d)
	}

	// Return ignores players, even ones that hit it.
	w.Submit(MoveCommand{Player: player, Seq: 2, Direction: Vec2{}})
	w.players[player].Pos = enemyOf(t, w, id).Pos.Add(Vec2{-20, 0})
	w.players[player].Vel = Vec2{}
	if hits := hitsFrom(swing(w, AttackCommand{Player: player, Seq: 1, Target: id}), player); len(hits) != 1 {
		t.Fatalf("hits = %+v, want the player's hit to land", hits)
	}
	stepUntil(t, w, 200, func(Events) bool {
		e := enemyOf(t, w, id)
		if e.State != EnemyReturn && e.State != EnemyPatrol {
			t.Fatalf("state = %v during return", e.State)
		}
		return e.State == EnemyPatrol
	})
	if e := enemyOf(t, w, id); e.Health != e.MaxHealth || e.Pos.DistanceTo(Vec2{}) > 2*arriveDistance {
		t.Fatalf("enemy = %+v, want home at full health", e)
	}
}

func TestSearchThenReturn(t *testing.T) {
	cfg := DefaultConfig()
	melee := cfg.Archetypes[Melee]
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{150, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	stepUntil(t, w, 40, func(Events) bool { return enemyOf(t, w, id).State == EnemyChase })

	// Far enough to be lost, and out of sight even from where it was last seen.
	lastSeen := w.players[player].Pos
	w.players[player].Pos = Vec2{400, 400}
	w.Step(dt)
	if e := enemyOf(t, w, id); e.State != EnemySearch {
		t.Fatalf("state = %v, want search once the player is gone", e.State)
	}
	stepUntil(t, w, 40, func(Events) bool { return enemyOf(t, w, id).searching })
	if d := enemyOf(t, w, id).Pos.DistanceTo(lastSeen); d > arriveDistance {
		t.Fatalf("started looking %v from where it lost the player", d)
	}
	for range ticks(melee.SearchTime) - 1 {
		w.Step(dt)
		if e := enemyOf(t, w, id); e.State != EnemySearch || e.Pos.DistanceTo(lastSeen) > lookRadius+1 {
			t.Fatalf("enemy = %+v, want looking around near %v", e, lastSeen)
		}
	}
	stepUntil(t, w, 2, func(Events) bool { return enemyOf(t, w, id).State == EnemyReturn })
}

func TestSearchSpotsWithoutDetect(t *testing.T) {
	w := NewWorld(DefaultConfig(), []Rect{{Vec2{60, 20}, Vec2{100, 200}}})
	player := w.AddPlayer(Vec2{150, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	stepUntil(t, w, 40, func(Events) bool { return enemyOf(t, w, id).State == EnemyChase })
	w.players[player].Pos = Vec2{150, 150}
	stepUntil(t, w, 5, func(Events) bool { return enemyOf(t, w, id).State == EnemySearch })

	w.players[player].Pos = Vec2{150, 0}
	w.Step(dt)
	if e := enemyOf(t, w, id); e.State != EnemyChase {
		t.Fatalf("state = %v, want straight back to chase", e.State)
	}
}

// The way home is out of sight behind a wall. The trail gets it around.
func TestReturnFindsWayAroundWall(t *testing.T) {
	w := NewWorld(DefaultConfig(), []Rect{{Vec2{50, -150}, Vec2{150, 50}}})
	player := w.AddPlayer(Vec2{0, 100})
	w.players[player].Health = 100000
	id := w.AddEnemy(Vec2{}, Melee)

	path := []Vec2{{0, 100}, {220, 100}, {220, -250}}
	leg := 0
	trail := false
	for range 400 {
		p := w.players[player]
		if leg < len(path)-1 {
			to := path[leg+1]
			p.Pos = p.Pos.Add(to.Sub(p.Pos).Scale(min(1, 4/to.Sub(p.Pos).Len())))
			if p.Pos == to {
				leg++
			}
		}
		w.Step(dt)
		trail = trail || len(w.enemies[id].trail) > 0
		if leg == len(path)-1 && enemyOf(t, w, id).Pos.DistanceTo(p.Pos) < 40 {
			break
		}
	}
	if !trail || w.canSee(enemyOf(t, w, id).Pos, Vec2{}) {
		t.Fatalf("enemy = %+v, trail %v, want it out of sight of home with a trail", enemyOf(t, w, id), trail)
	}

	w.players[player].Pos = Vec2{5000, 5000}
	stepUntil(t, w, 600, func(Events) bool { return enemyOf(t, w, id).State == EnemyPatrol })
	if d := enemyOf(t, w, id).Pos.DistanceTo(Vec2{}); d > 2*arriveDistance {
		t.Fatalf("enemy stopped %v from home", d)
	}
}

func TestEnemyKillsPlayerThenGoesHome(t *testing.T) {
	cfg := tune(Melee, func(a *Archetype) { a.Damage = 1000 })
	w := NewWorld(cfg, nil)
	player := w.AddPlayer(Vec2{100, 0})
	id := w.AddEnemy(Vec2{}, Melee)

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
	steps(w, 200)
	if p, _ := w.Player(player); p.Pos != (Vec2{100, 0}) {
		t.Fatalf("dead player moved to %v", p.Pos)
	}
	if e := enemyOf(t, w, id); (e.State != EnemyPatrol && e.State != EnemyIdle) || e.Pos.DistanceTo(Vec2{}) > cfg.Archetypes[Melee].PatrolRadius {
		t.Fatalf("enemy = %+v, want back on patrol near home", e)
	}
}

func TestPlayerOutrunsEveryArchetype(t *testing.T) {
	for _, arch := range []ArchetypeID{Melee, Ranged, Elite} {
		t.Run(string(arch), func(t *testing.T) {
			w := NewWorld(DefaultConfig(), nil)
			player := w.AddPlayer(Vec2{150, 0})
			w.players[player].Health = 100000
			id := w.AddEnemy(Vec2{}, arch)
			stepUntil(t, w, 20, func(Events) bool { return enemyOf(t, w, id).State == EnemyChase })

			w.Submit(MoveCommand{Player: player, Seq: 1, Direction: Vec2{1, 0}})
			stepUntil(t, w, 200, func(Events) bool {
				s := enemyOf(t, w, id).State
				return s == EnemySearch || s == EnemyReturn
			})
			stepUntil(t, w, 600, func(Events) bool { return enemyOf(t, w, id).State == EnemyPatrol })
		})
	}
}

func TestDeadEnemyDoesNothing(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	player := w.AddPlayer(Vec2{20, 0})
	id := w.AddEnemy(Vec2{}, Melee)
	w.enemies[id].Health = 0

	if events := steps(w, ticks(2*time.Second)); len(events) != 0 {
		t.Fatalf("dead enemy made events: %+v", events)
	}
	if p, _ := w.Player(player); p.Health != p.MaxHealth {
		t.Fatalf("player health = %d", p.Health)
	}
	if e := enemyOf(t, w, id); e.Pos != (Vec2{}) || e.State != EnemyDead {
		t.Fatalf("dead enemy = %+v", e)
	}
}

// replay runs a fight with all three archetypes and returns everything that
// happened, as text.
func replay(aiSeed, lootSeed uint64) (trace []string, drops []string) {
	w := NewWorld(DefaultConfig(), []Rect{{Vec2{100, -40}, Vec2{140, 40}}})
	w.SetLoot(items.NewRoller(items.Default(), lootSeed))
	w.SeedAI(aiSeed)
	player := w.AddPlayer(Vec2{-200, 0})
	w.players[player].Health = 100000
	w.AddEnemy(Vec2{0, -120}, Melee)
	w.AddEnemy(Vec2{300, 0}, Ranged)
	w.AddEnemy(Vec2{0, 150}, Elite)

	for i := range 600 {
		// Walk in and out of view so enemies search and idle, and roll.
		dir := Vec2{1, 0}
		if (i/80)%2 == 1 {
			dir = Vec2{-1, 0}
		}
		w.Submit(MoveCommand{Player: player, Seq: uint32(i + 1), Direction: dir})
		ev := w.Step(dt)
		for _, d := range ev.Damage {
			trace = append(trace, fmt.Sprintf("%d: %+v", i, d))
		}
		for _, e := range w.Enemies() {
			trace = append(trace, fmt.Sprintf("%d: %d %v %.3f,%.3f", i, e.ID, e.State, e.Pos.X, e.Pos.Y))
		}
	}
	for _, e := range w.Enemies() {
		w.enemies[e.ID].Health = 1
		w.players[player].Pos = e.Pos
		clear(w.players[player].cooldowns)
		swing(w, AttackCommand{Player: player, Target: e.ID})
	}
	for _, p := range w.Piles() {
		for _, inst := range p.Items {
			drops = append(drops, fmt.Sprintf("%s %v %d %v", inst.DefID, inst.Rarity, inst.Quantity, inst.Affixes))
		}
	}
	return trace, drops
}

func TestSameSeedSameFight(t *testing.T) {
	a, dropsA := replay(1, 7)
	b, dropsB := replay(1, 7)
	if !slices.Equal(a, b) || !slices.Equal(dropsA, dropsB) {
		t.Fatal("same seeds gave a different fight")
	}
	c, dropsC := replay(2, 7)
	if slices.Equal(a, c) {
		t.Fatal("AI seed changed nothing, so the test is not exercising AI rolls")
	}
	if !slices.Equal(dropsA, dropsC) {
		t.Fatalf("AI rolls changed the loot: %v vs %v", dropsA, dropsC)
	}
}

func TestEachArchetypeDropsItsTable(t *testing.T) {
	want := map[ArchetypeID]string{Melee: "cave1_grunt", Ranged: "cave1_grunt", Elite: "cave1_elite"}
	for id, a := range DefaultConfig().Archetypes {
		if a.Loot != want[id] {
			t.Errorf("%s drops %q, want %q", id, a.Loot, want[id])
		}
		if _, ok := items.Default().Tables[a.Loot]; !ok {
			t.Errorf("%s drop table %q is not in the item data", id, a.Loot)
		}
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
	if at, _ := r.entry(Vec2{-50, 0}, Vec2{50, 0}); at != 0.4 {
		t.Errorf("entry = %v, want 0.4", at)
	}
}
