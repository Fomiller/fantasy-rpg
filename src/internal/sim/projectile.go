package sim

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"time"
)

// Projectile is an enemy's shot. It belongs to the world, so it keeps flying
// after its shooter dies.
type Projectile struct {
	ID       EntityID
	Owner    EntityID
	Pos, Vel Vec2
	Radius   float64
	damage   int
	left     float64
}

func (w *World) Projectiles() []Projectile {
	out := make([]Projectile, 0, len(w.shots))
	for _, s := range w.shots {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b Projectile) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// fire shoots along the enemy's aim. There is no leading, so a target moving
// sideways can dodge.
func (w *World) fire(e *Enemy, a Archetype) {
	w.nextID++
	w.shots[w.nextID] = &Projectile{
		ID:     w.nextID,
		Owner:  e.ID,
		Pos:    e.Pos,
		Vel:    e.Aim.Scale(a.Projectile.Speed),
		Radius: a.Projectile.Radius,
		damage: a.Damage,
		left:   a.Projectile.Range,
	}
}

// stepProjectiles moves each shot and stops it at the first wall or live
// player in its path. Shots never hit enemies.
func (w *World) stepProjectiles(dt time.Duration) []DamageEvent {
	var events []DamageEvent
	players := slices.Sorted(maps.Keys(w.players))
	for _, id := range slices.Sorted(maps.Keys(w.shots)) {
		s := w.shots[id]
		step := s.Vel.Scale(dt.Seconds())
		if l := step.Len(); l > s.left {
			step = step.Scale(s.left / l)
		}
		to := s.Pos.Add(step)

		wallAt := math.Inf(1)
		for _, wall := range w.walls {
			if t, ok := wall.entry(s.Pos, to); ok {
				wallAt = min(wallAt, t)
			}
		}
		var hit *Player
		hitAt := math.Inf(1)
		for _, pid := range players {
			p := w.players[pid]
			if p.Health <= 0 {
				continue
			}
			if t, ok := box(p.Pos, w.cfg.PlayerHalfSize+s.Radius).entry(s.Pos, to); ok && t < hitAt {
				hit, hitAt = p, t
			}
		}

		switch {
		case hit != nil && hitAt <= wallAt:
			events = append(events, w.hurt(s.Owner, hit, s.damage))
			delete(w.shots, id)
		case !math.IsInf(wallAt, 1):
			delete(w.shots, id)
		default:
			s.Pos = to
			s.left -= step.Len()
			if s.left <= 0 {
				delete(w.shots, id)
			}
		}
	}
	return events
}
