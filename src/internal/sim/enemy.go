package sim

import (
	"maps"
	"math"
	"slices"
	"time"
)

type EnemyState int

// EnemyPatrol is the zero value, so a new enemy starts patrolling.
const (
	EnemyPatrol EnemyState = iota
	EnemyIdle
	EnemyDetect
	EnemyChase
	EnemyAttack
	EnemySearch
	EnemyRetreat
	EnemyReturn
	EnemyCharge
	EnemyDead
)

func (s EnemyState) String() string {
	switch s {
	case EnemyPatrol:
		return "patrol"
	case EnemyIdle:
		return "idle"
	case EnemyDetect:
		return "detect"
	case EnemyChase:
		return "chase"
	case EnemyAttack:
		return "attack"
	case EnemySearch:
		return "search"
	case EnemyRetreat:
		return "retreat"
	case EnemyReturn:
		return "return"
	case EnemyCharge:
		return "charge"
	case EnemyDead:
		return "dead"
	}
	return "unknown"
}

// arriveDistance is how close an enemy must get to a point to count as there.
const arriveDistance = 2

// lookRadius bounds the extra points an enemy checks around where it lost its target.
const lookRadius = 64

func (w *World) stepEnemies(dt time.Duration) []DamageEvent {
	var events []DamageEvent
	secs := dt.Seconds()
	// Sorted so damage events and AI rolls come in the same order every run.
	for _, id := range slices.Sorted(maps.Keys(w.enemies)) {
		e := w.enemies[id]
		a := w.cfg.Archetypes[e.Archetype]
		e.cooldown = max(e.cooldown-dt, 0)
		e.specialCooldown = max(e.specialCooldown-dt, 0)
		e.timer = max(e.timer-dt, 0)

		// One transition per tick, so every state shows in at least one snapshot.
		if next, target, ok := w.next(e, a); ok {
			w.enter(e, a, next, target)
		}
		e.hitBy = 0
		if ev, ok := w.act(e, a, secs); ok {
			events = append(events, ev)
		}

		from, want := e.Pos, e.Vel
		w.slide(&e.Pos, &e.Vel, w.cfg.EnemyHalfSize, secs)
		e.blocked = want != (Vec2{}) && e.Pos.DistanceTo(from) < want.Len()*secs/2
		if e.State == EnemyCharge && want != (Vec2{}) {
			events = append(events, w.chargeHits(e, a)...)
		}
		w.leaveTrail(e, from)
	}
	return events
}

// next runs the transition table for the enemy's state. Within a state the
// checks are in priority order, and the first match wins.
func (w *World) next(e *Enemy, a Archetype) (EnemyState, EntityID, bool) {
	if e.State == EnemyDead {
		return 0, 0, false
	}
	if e.Health <= 0 {
		return EnemyDead, 0, true
	}
	target := w.players[e.Target]
	lost := w.lost(e, a, target)
	leashed := e.Pos.DistanceTo(e.home) > a.LeashRadius
	low := a.RetreatHealth > 0 && float64(e.Health) <= a.RetreatHealth*float64(e.MaxHealth)

	switch e.State {
	case EnemyIdle, EnemyPatrol:
		if p := w.spot(e, a); p != nil {
			return EnemyDetect, p.ID, true
		}
		if w.hitByPlayer(e) {
			return EnemyChase, e.hitBy, true
		}
		if e.State == EnemyIdle && e.timer <= 0 {
			return EnemyPatrol, 0, true
		}
		if e.State == EnemyPatrol && w.reached(e, e.waypoint(a)) {
			return EnemyIdle, 0, true
		}

	case EnemyDetect:
		if e.timer <= 0 && !lost {
			return EnemyChase, e.Target, true
		}
		if lost {
			return EnemySearch, 0, true
		}

	case EnemyChase:
		if leashed {
			return EnemyReturn, 0, true
		}
		if lost {
			return EnemySearch, 0, true
		}
		dist := e.Pos.DistanceTo(target.Pos)
		switch {
		case low, dist < a.KeepAway:
			return EnemyRetreat, e.Target, true
		case a.Special == SpecialCharge && e.specialCooldown <= 0 && dist >= a.ChargeMin && dist <= a.ChargeMax:
			return EnemyCharge, e.Target, true
		case dist <= a.AttackRange && e.cooldown <= 0:
			return EnemyAttack, e.Target, true
		}

	case EnemyAttack:
		switch {
		case leashed:
			return EnemyReturn, 0, true
		case lost:
			return EnemySearch, 0, true
		case e.swung:
			return EnemyChase, e.Target, true
		}

	case EnemyRetreat:
		switch {
		case leashed, low && lost:
			return EnemyReturn, 0, true
		case lost:
			return EnemySearch, 0, true
		case !low && e.Pos.DistanceTo(target.Pos) > a.KeepAway:
			return EnemyChase, e.Target, true
		}

	case EnemySearch:
		if w.hitByPlayer(e) {
			return EnemyChase, e.hitBy, true
		}
		if leashed {
			return EnemyReturn, 0, true
		}
		// Already alert, so it skips Detect.
		if p := w.spot(e, a); p != nil {
			return EnemyChase, p.ID, true
		}
		if e.searching && e.timer <= 0 {
			return EnemyReturn, 0, true
		}

	case EnemyReturn:
		if len(e.trail) == 0 && e.Pos.DistanceTo(e.home) < arriveDistance {
			return EnemyPatrol, 0, true
		}

	case EnemyCharge:
		// A dash that hits a wall stops there.
		if e.timer <= 0 || e.blocked {
			return EnemyChase, e.Target, true
		}
	}
	return 0, 0, false
}

func (w *World) enter(e *Enemy, a Archetype, s EnemyState, target EntityID) {
	switch e.State {
	case EnemyCharge:
		e.specialCooldown = a.ChargeCooldown
	case EnemyReturn:
		// Healing on the way home would let a player hit it at the leash edge for free.
		e.Health = e.MaxHealth
	}
	e.State, e.Target, e.timer = s, target, 0
	switch s {
	case EnemyIdle:
		e.patrolLeft = !e.patrolLeft
		e.timer = w.jitter(a.IdleTime)
	case EnemyPatrol:
		e.trail = nil
	case EnemyDetect:
		e.face(w.players[target])
		e.timer = a.ReactTime
	case EnemyAttack:
		e.timer, e.swung = a.Windup, false
	case EnemySearch:
		e.searching, e.looks = false, nil
	case EnemyCharge:
		e.face(w.players[target])
		e.timer = a.ChargeWindup + a.ChargeTime
		e.charged = map[EntityID]bool{}
	}
}

// act sets the enemy's velocity for this tick and makes any hit it lands.
func (w *World) act(e *Enemy, a Archetype, secs float64) (DamageEvent, bool) {
	e.Vel = Vec2{}
	target := w.players[e.Target]
	switch e.State {
	case EnemyPatrol:
		e.Vel = velocityToward(e.Pos, e.waypoint(a), a.PatrolSpeed, secs)

	case EnemyDetect:
		e.face(target)

	case EnemyChase:
		w.chase(e, a, target, secs)

	case EnemyAttack:
		// Aimed until the windup ends, then the hit lands only if the target is still there.
		e.face(target)
		if e.timer <= 0 && !e.swung {
			e.swung = true
			return w.strike(e, a, target)
		}

	case EnemySearch:
		w.search(e, a, secs)

	case EnemyRetreat:
		e.face(target)
		e.Vel = e.Aim.Scale(-a.Speed)

	case EnemyReturn:
		e.Vel = velocityToward(e.Pos, e.homeward(), a.Speed, secs)

	case EnemyCharge:
		if e.timer <= a.ChargeTime {
			e.Vel = e.Aim.Scale(a.ChargeSpeed)
		}
	}
	return DamageEvent{}, false
}

func (w *World) chase(e *Enemy, a Archetype, p *Player, secs float64) {
	// A charge can end with the target gone. Search takes over next tick.
	if p == nil {
		return
	}
	e.face(p)
	dist := e.Pos.DistanceTo(p.Pos)
	// A shooter holds where it can hit from instead of walking into melee.
	if a.Projectile != nil && dist <= a.AttackRange {
		return
	}
	touching := w.cfg.EnemyHalfSize + w.cfg.PlayerHalfSize
	if dist > touching {
		stop := p.Pos.Add(e.Pos.Sub(p.Pos).Scale(touching / dist))
		e.Vel = velocityToward(e.Pos, stop, a.Speed, secs)
	}
}

func (w *World) strike(e *Enemy, a Archetype, p *Player) (DamageEvent, bool) {
	e.cooldown = a.Cooldown
	if a.Projectile != nil {
		w.fire(e, a)
		return DamageEvent{}, false
	}
	if p == nil || p.Health <= 0 || e.Pos.DistanceTo(p.Pos) > a.AttackRange {
		return DamageEvent{}, false
	}
	return w.hurt(e.ID, p, a.Damage), true
}

// search walks to where the target was last seen, then looks around for SearchTime.
func (w *World) search(e *Enemy, a Archetype, secs float64) {
	if !e.searching {
		if !w.reached(e, e.lastSeen) {
			e.Vel = velocityToward(e.Pos, e.lastSeen, a.Speed, secs)
			return
		}
		e.searching, e.timer = true, a.SearchTime
		for range w.ai.IntN(3) {
			angle, r := w.ai.Float64()*2*math.Pi, w.ai.Float64()*lookRadius
			e.looks = append(e.looks, e.lastSeen.Add(Vec2{X: math.Cos(angle) * r, Y: math.Sin(angle) * r}))
		}
		return
	}
	if len(e.looks) > 0 && w.reached(e, e.looks[0]) {
		e.looks = e.looks[1:]
	}
	if len(e.looks) > 0 {
		e.Vel = velocityToward(e.Pos, e.looks[0], a.PatrolSpeed, secs)
	}
}

// chargeHits hits each player the dash touches, once per charge.
func (w *World) chargeHits(e *Enemy, a Archetype) []DamageEvent {
	var events []DamageEvent
	body := box(e.Pos, w.cfg.EnemyHalfSize)
	for _, id := range slices.Sorted(maps.Keys(w.players)) {
		p := w.players[id]
		if p.Health <= 0 || e.charged[id] || !body.Overlaps(box(p.Pos, w.cfg.PlayerHalfSize)) {
			continue
		}
		e.charged[id] = true
		events = append(events, w.hurt(e.ID, p, a.ChargeDamage))
	}
	return events
}

func (w *World) hurt(source EntityID, p *Player, dmg int) DamageEvent {
	dmg = w.damageTaken(p, dmg)
	p.Health = max(p.Health-dmg, 0)
	w.gain(p, p.class.Resource.OnHitTaken)
	return DamageEvent{
		Source:       source,
		Target:       p.ID,
		Amount:       dmg,
		TargetHealth: p.Health,
		Killed:       p.Health == 0,
	}
}

// leaveTrail drops a point whenever the enemy loses sight of the last one,
// so the way home is a chain of straight lines.
func (w *World) leaveTrail(e *Enemy, from Vec2) {
	switch e.State {
	case EnemyPatrol, EnemyIdle, EnemyReturn, EnemyDead:
		return
	}
	last := e.home
	if n := len(e.trail); n > 0 {
		last = e.trail[n-1]
	}
	if !w.canSee(e.Pos, last) {
		e.trail = append(e.trail, from)
	}
}

// homeward is the next point on the way home. It drops trail points as the
// enemy reaches them.
func (e *Enemy) homeward() Vec2 {
	for n := len(e.trail); n > 0; n-- {
		if e.Pos.DistanceTo(e.trail[n-1]) >= arriveDistance {
			return e.trail[n-1]
		}
		e.trail = e.trail[:n-1]
	}
	return e.home
}

func (e *Enemy) waypoint(a Archetype) Vec2 {
	offset := a.PatrolRadius
	if e.patrolLeft {
		offset = -offset
	}
	return e.home.Add(Vec2{X: offset})
}

// reached counts a wall in the way as arriving, so an enemy never stands
// pushing into one.
func (w *World) reached(e *Enemy, to Vec2) bool {
	return e.Pos.DistanceTo(to) < arriveDistance || e.blocked
}

func (e *Enemy) face(p *Player) {
	if p == nil {
		return
	}
	e.lastSeen = p.Pos
	if d := p.Pos.Sub(e.Pos); d.Len() > 0 {
		e.Aim = d.Scale(1 / d.Len())
	}
}

func (w *World) hitByPlayer(e *Enemy) bool {
	p, ok := w.players[e.hitBy]
	return ok && p.Health > 0
}

// jitter spreads a duration by up to 25% either way, so enemies drift out of step.
func (w *World) jitter(d time.Duration) time.Duration {
	return d * time.Duration(75+w.ai.IntN(51)) / 100
}

// spot returns the nearest live player the enemy can see, or nil.
func (w *World) spot(e *Enemy, a Archetype) *Player {
	var best *Player
	bestDist := math.Inf(1)
	for _, p := range w.players {
		d := e.Pos.DistanceTo(p.Pos)
		if p.Health <= 0 || d > w.detectRadius(a, p) || !w.canSee(e.Pos, p.Pos) {
			continue
		}
		if d < bestDist || (d == bestDist && p.ID < best.ID) {
			best, bestDist = p, d
		}
	}
	return best
}

func (w *World) lost(e *Enemy, a Archetype, p *Player) bool {
	return p == nil || p.Health <= 0 ||
		e.Pos.DistanceTo(p.Pos) > a.LoseRadius ||
		!w.canSee(e.Pos, p.Pos)
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
