package sim

import (
	"errors"
	"fmt"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

var errNoPlayer = errors.New("no such player")

// Equip puts an item in its slot. It returns whatever was there before.
func (w *World) Equip(id EntityID, inst items.Instance) (items.Instance, bool, error) {
	p, ok := w.players[id]
	if !ok {
		return items.Instance{}, false, errNoPlayer
	}
	if p.Health <= 0 {
		return items.Instance{}, false, errors.New("dead players cannot equip")
	}
	if !inst.Slot.IsGear() {
		return items.Instance{}, false, errors.New(inst.Name + " cannot be equipped")
	}
	if inst.Slot == items.Weapon && !p.class.CanWield(inst.WeaponType) {
		return items.Instance{}, false, fmt.Errorf("a %s cannot use %s", p.class.Name, inst.Name)
	}
	old, had := p.Equipment[inst.Slot]
	p.Equipment[inst.Slot] = inst
	w.applyGear(p)
	return old, had, nil
}

// applyGear recomputes stats from the class base, level and equipment. A change in
// max health moves current health by the same amount, so swapping gear
// never heals.
func (w *World) applyGear(p *Player) {
	p.stats = baseStats(p.class)
	for s, v := range p.class.PerLevel {
		p.stats[s] += v * (p.Level - 1)
	}
	for _, inst := range p.Equipment {
		for s, v := range inst.Stats() {
			p.stats[s] += v
		}
	}
	maxHealth := p.stats[items.MaxHealth]
	p.Health = max(min(p.Health+maxHealth-p.MaxHealth, maxHealth), 1)
	p.MaxHealth = maxHealth
}

// Stats are the player's stats as the sim uses them. Heal is left out
// because it only means something on a consumable.
func (w *World) Stats(p Player) map[items.Stat]int {
	out := map[items.Stat]int{}
	for _, s := range items.AllStats {
		out[s] = p.stats[s]
	}
	delete(out, items.Heal)
	out[items.Damage] = w.attackDamage(&p)
	out[items.MoveSpeed] = min(out[items.MoveSpeed], w.cfg.MoveSpeedCap)
	return out
}

func baseStats(c classes.Class) map[items.Stat]int {
	out := map[items.Stat]int{}
	for s, v := range c.Base {
		out[s] = v
	}
	return out
}

func (w *World) attackDamage(p *Player) int {
	if _, armed := p.Equipment[items.Weapon]; armed {
		return max(p.stats[items.Damage], 1)
	}
	return w.cfg.AttackDamage + p.stats[items.Damage]
}

func (w *World) extractTime(p *Player) time.Duration {
	return percentFaster(w.cfg.ExtractTime, p.stats[items.ExtractSpeed])
}

// MaxSpeed is sent to the client so its prediction moves at the same speed.
func (w *World) MaxSpeed(p Player) float64 {
	return w.maxSpeed(&p)
}

func (w *World) maxSpeed(p *Player) float64 {
	speed := w.cfg.MaxSpeed * float64(100+min(p.stats[items.MoveSpeed], w.cfg.MoveSpeedCap)) / 100
	if s, ok := p.statuses[classes.Slow]; ok {
		speed = speed * float64(100-s.amount) / 100
	}
	return speed
}

// damageTaken shrinks a hit by armor, then by guard. 50 armor halves it. A
// hit always does at least 1.
func (w *World) damageTaken(p *Player, dmg int) int {
	dmg = max(dmg*w.cfg.ArmorScale/(w.cfg.ArmorScale+p.stats[items.Armor]), 1)
	if g, ok := p.statuses[classes.Guard]; ok {
		dmg = max(dmg*(100-g.amount)/100, 1)
	}
	return dmg
}

func (w *World) detectRadius(a Archetype, p *Player) float64 {
	return a.DetectRadius * float64(100+p.stats[items.DetectRadius]) / 100
}

// percentFaster turns "+N% speed" into a shorter duration: +100% halves it.
func percentFaster(d time.Duration, pct int) time.Duration {
	return d * 100 / time.Duration(100+pct)
}
