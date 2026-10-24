package sim

import (
	"maps"
	"math"
	"slices"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// BasicAbility stands for the class's basic attack, whatever its id.
const BasicAbility = "basic"

// Reasons the caster is told an ability did not start.
const (
	RejectUnknown  = "unknown"
	RejectLocked   = "locked"
	RejectBusy     = "busy"
	RejectCooldown = "cooldown"
	RejectResource = "resource"
	RejectAim      = "aim"
)

// AbilityEvent is an ability that started. Everyone is told, so they can
// draw the windup.
type AbilityEvent struct {
	Source   EntityID
	Ability  string
	Aim      Vec2
	Windup   time.Duration
	Cooldown time.Duration
}

type Rejection struct {
	Player  EntityID
	Seq     uint32
	Ability string
	Reason  string
}

type cast struct {
	ability classes.Ability
	aim     Vec2
	left    time.Duration
}

type status struct {
	amount int
	left   time.Duration
}

// Classes is the class data the world was built with.
func (w *World) Classes() *classes.Data {
	return w.cfg.Classes
}

// applyAttack starts an ability. Cost and cooldown are paid now, and the
// effects land when the windup ends.
func (w *World) applyAttack(c AttackCommand, ev *Events) {
	p, ok := w.players[c.Player]
	if !ok || p.Health <= 0 {
		return
	}
	id := c.Ability
	if id == "" || id == BasicAbility {
		id = p.class.Basic
	}
	reject := func(reason string) {
		ev.Rejected = append(ev.Rejected, Rejection{Player: p.ID, Seq: c.Seq, Ability: id, Reason: reason})
	}
	a, ok := w.cfg.Classes.Abilities[id]
	switch {
	case !ok || !slices.Contains(p.class.Abilities, id):
		reject(RejectUnknown)
		return
	case p.Level < a.UnlockLevel:
		reject(RejectLocked)
		return
	case p.cast != nil:
		reject(RejectBusy)
		return
	case p.cooldowns[id] > 0:
		reject(RejectCooldown)
		return
	case p.Resource < float64(a.Cost):
		reject(RejectResource)
		return
	}
	aim, ok := w.aim(p, c)
	if !ok && a.Target.Shape != classes.Self {
		reject(RejectAim)
		return
	}

	cooldown := a.Cooldown()
	if id == p.class.Basic {
		cooldown = percentFaster(cooldown, p.stats[items.AttackSpeed])
	}
	p.Resource -= float64(a.Cost)
	p.cooldowns[id] = cooldown
	p.cast = &cast{ability: a, aim: aim, left: a.Windup()}
	ev.Abilities = append(ev.Abilities, AbilityEvent{Source: p.ID, Ability: id, Aim: aim, Windup: a.Windup(), Cooldown: cooldown})
	if p.cast.left <= 0 {
		w.resolve(p, ev)
	}
}

// aim is the client's aim, or toward target_id when the client sent none.
func (w *World) aim(p *Player, c AttackCommand) (Vec2, bool) {
	if l := c.Aim.Len(); l > 0 && finite(l) {
		return c.Aim.Scale(1 / l), true
	}
	if e, ok := w.enemies[c.Target]; ok && e.Health > 0 {
		if d := e.Pos.Sub(p.Pos); d.Len() > 0 {
			return d.Scale(1 / d.Len()), true
		}
	}
	return Vec2{}, false
}

// stepAbilities runs every player's timers, and lands any windup that ends
// this tick against where enemies are now.
func (w *World) stepAbilities(dt time.Duration, ev *Events) {
	for _, id := range slices.Sorted(maps.Keys(w.players)) {
		p := w.players[id]
		if p.Health <= 0 {
			p.cast = nil
			continue
		}
		for a, left := range p.cooldowns {
			p.cooldowns[a] = max(left-dt, 0)
		}
		for s, st := range p.statuses {
			if st.left -= dt; st.left <= 0 {
				delete(p.statuses, s)
			} else {
				p.statuses[s] = st
			}
		}
		w.stepResource(p, dt)
		if p.cast != nil {
			if p.cast.left -= dt; p.cast.left <= 0 {
				w.resolve(p, ev)
			}
		}
	}
}

func (w *World) stepResource(p *Player, dt time.Duration) {
	r := p.class.Resource
	p.sinceHit += dt
	rate := float64(r.Regen)
	if p.sinceHit > r.DecayDelay() {
		rate -= float64(r.Decay)
	}
	p.Resource = min(max(p.Resource+rate*dt.Seconds(), 0), float64(r.Max))
}

// gain adds resource for a hit dealt or taken. Any hit holds off decay.
func (w *World) gain(p *Player, n int) {
	p.Resource = min(p.Resource+float64(n), float64(p.class.Resource.Max))
	p.sinceHit = 0
}

func (w *World) resolve(p *Player, ev *Events) {
	c := p.cast
	p.cast = nil
	var targets []*Enemy
	if c.ability.Target.Shape == classes.Arc {
		targets = w.inArc(p.Pos, c.aim, c.ability.Target)
	}
	dealt := false
	for _, e := range c.ability.Effects {
		switch e.Type {
		case classes.Damage:
			dmg := max(w.attackDamage(p)*e.Power/100, 1)
			for _, target := range targets {
				if target.Health > 0 {
					ev.Damage = append(ev.Damage, w.hit(p, target, dmg))
					dealt = true
				}
			}
		case classes.Status:
			// Statuses do not stack. A new one replaces the old.
			p.statuses[e.Status] = status{amount: e.Amount, left: e.Duration()}
		}
	}
	// Once per swing, however many it hit.
	if dealt {
		w.gain(p, p.class.Resource.OnHitDealt)
	}
}

// inArc returns live enemies whose center is within range and within half
// the arc of aim, sorted by ID.
func (w *World) inArc(from, aim Vec2, t classes.Target) []*Enemy {
	// A little slack so an enemy right on the edge of a 180° arc counts.
	edge := math.Cos(t.ArcDeg/2*math.Pi/180) - 1e-9
	var out []*Enemy
	for _, id := range slices.Sorted(maps.Keys(w.enemies)) {
		e := w.enemies[id]
		d := e.Pos.Sub(from)
		l := d.Len()
		if e.Health <= 0 || l > t.Range {
			continue
		}
		if l > 0 && (d.X*aim.X+d.Y*aim.Y)/l < edge {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (w *World) hit(p *Player, e *Enemy, dmg int) DamageEvent {
	e.Health = max(e.Health-dmg, 0)
	e.hitBy = p.ID
	if e.hits == nil {
		e.hits = map[EntityID]time.Duration{}
	}
	e.hits[p.ID] = w.now
	return DamageEvent{
		Source:       p.ID,
		Target:       e.ID,
		Amount:       dmg,
		TargetHealth: e.Health,
		Killed:       e.Health == 0,
	}
}

func (p *Player) rooted() bool {
	return p.cast != nil && p.cast.ability.Root
}
