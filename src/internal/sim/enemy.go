package sim

import (
	"maps"
	"slices"
	"time"
)

type EnemyState int

const (
	EnemyPatrol EnemyState = iota
	EnemyChase
	EnemyReturn
)

func (s EnemyState) String() string {
	switch s {
	case EnemyPatrol:
		return "patrol"
	case EnemyChase:
		return "chase"
	case EnemyReturn:
		return "return"
	}
	return "unknown"
}

// arriveDistance is how close an enemy must get to a point to count as there.
const arriveDistance = 2

func (w *World) stepEnemies(dt time.Duration) []DamageEvent {
	var events []DamageEvent
	// Sorted so damage events come out in the same order every run.
	for _, id := range slices.Sorted(maps.Keys(w.enemies)) {
		e := w.enemies[id]
		if e.Health <= 0 {
			e.Vel = Vec2{}
			continue
		}
		if ev, ok := w.think(e, dt); ok {
			events = append(events, ev)
		}
		w.slide(&e.Pos, &e.Vel, w.cfg.EnemyHalfSize, dt.Seconds())
		e.cooldown = max(e.cooldown-dt, 0)
	}
	return events
}

func (w *World) think(e *Enemy, dt time.Duration) (DamageEvent, bool) {
	secs := dt.Seconds()
	switch e.State {
	case EnemyPatrol:
		if p := w.spot(e); p != nil {
			e.State, e.Target = EnemyChase, p.ID
			return w.chase(e, p, secs)
		}
		offset := w.cfg.PatrolRadius
		if e.patrolLeft {
			offset = -offset
		}
		waypoint := e.home.Add(Vec2{X: offset})
		if e.Pos.DistanceTo(waypoint) < arriveDistance {
			e.patrolLeft = !e.patrolLeft
		}
		e.Vel = velocityToward(e.Pos, waypoint, w.cfg.PatrolSpeed, secs)

	case EnemyChase:
		p, ok := w.players[e.Target]
		if !ok || !w.stillChasing(e, p) {
			e.State, e.Target = EnemyReturn, 0
			e.Vel = velocityToward(e.Pos, e.home, w.cfg.EnemySpeed, secs)
			return DamageEvent{}, false
		}
		return w.chase(e, p, secs)

	case EnemyReturn:
		// No new targets until home, so a player can break off a chase.
		if e.Pos.DistanceTo(e.home) < arriveDistance {
			e.State = EnemyPatrol
		}
		e.Vel = velocityToward(e.Pos, e.home, w.cfg.EnemySpeed, secs)
	}
	return DamageEvent{}, false
}

func (w *World) chase(e *Enemy, p *Player, secs float64) (DamageEvent, bool) {
	dist := e.Pos.DistanceTo(p.Pos)
	touching := w.cfg.EnemyHalfSize + w.cfg.PlayerHalfSize
	if dist > touching {
		stop := p.Pos.Add(e.Pos.Sub(p.Pos).Scale(touching / dist))
		e.Vel = velocityToward(e.Pos, stop, w.cfg.EnemySpeed, secs)
	} else {
		e.Vel = Vec2{}
	}

	if dist > w.cfg.EnemyAttackRange || e.cooldown > 0 {
		return DamageEvent{}, false
	}
	p.Health = max(p.Health-w.cfg.EnemyDamage, 0)
	e.cooldown = w.cfg.EnemyAttackCooldown
	return DamageEvent{
		Source:       e.ID,
		Target:       p.ID,
		Amount:       w.cfg.EnemyDamage,
		TargetHealth: p.Health,
		Killed:       p.Health == 0,
	}, true
}

// spot returns the nearest live player the enemy can see, or nil.
func (w *World) spot(e *Enemy) *Player {
	var best *Player
	bestDist := w.cfg.DetectRadius
	for _, p := range w.players {
		if p.Health <= 0 || !w.canSee(e.Pos, p.Pos) {
			continue
		}
		d := e.Pos.DistanceTo(p.Pos)
		if d < bestDist || (d == bestDist && best != nil && p.ID < best.ID) {
			best, bestDist = p, d
		}
	}
	return best
}

func (w *World) stillChasing(e *Enemy, p *Player) bool {
	return p.Health > 0 &&
		e.Pos.DistanceTo(e.home) <= w.cfg.LeashRadius &&
		e.Pos.DistanceTo(p.Pos) <= w.cfg.LoseRadius &&
		w.canSee(e.Pos, p.Pos)
}

func (w *World) canSee(from, to Vec2) bool {
	for _, wall := range w.walls {
		if wall.Intersects(from, to) {
			return false
		}
	}
	return true
}

// velocityToward heads for target at speed, slowing on the last tick so it
// lands on target instead of overshooting.
func velocityToward(from, target Vec2, speed, secs float64) Vec2 {
	d := target.Sub(from)
	l := d.Len()
	if l == 0 || secs == 0 {
		return Vec2{}
	}
	return d.Scale(min(speed, l/secs) / l)
}
