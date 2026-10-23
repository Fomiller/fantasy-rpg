package sim

import (
	"errors"
	"time"

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
	old, had := p.Equipment[inst.Slot]
	p.Equipment[inst.Slot] = inst
	w.applyGear(p)
	return old, had, nil
}

// applyGear recomputes stats from equipment. A change in max health moves
// current health by the same amount, so swapping gear never heals.
func (w *World) applyGear(p *Player) {
	p.stats = map[items.Stat]int{}
	for _, inst := range p.Equipment {
		for s, v := range inst.Stats() {
			p.stats[s] += v
		}
	}
	maxHealth := w.cfg.PlayerHealth + p.stats[items.MaxHealth]
	p.Health = max(min(p.Health+maxHealth-p.MaxHealth, maxHealth), 1)
	p.MaxHealth = maxHealth
}

func (w *World) attackDamage(p *Player) int {
	if _, armed := p.Equipment[items.Weapon]; armed {
		return max(p.stats[items.Damage], 1)
	}
	return w.cfg.AttackDamage + p.stats[items.Damage]
}

func (w *World) attackCooldown(p *Player) time.Duration {
	return percentFaster(w.cfg.AttackCooldown, p.stats[items.AttackSpeed])
}

func (w *World) extractTime(p *Player) time.Duration {
	return percentFaster(w.cfg.ExtractTime, p.stats[items.ExtractSpeed])
}

// MaxSpeed is sent to the client so its prediction moves at the same speed.
func (w *World) MaxSpeed(p Player) float64 {
	return w.maxSpeed(&p)
}

func (w *World) maxSpeed(p *Player) float64 {
	return w.cfg.MaxSpeed * float64(100+min(p.stats[items.MoveSpeed], w.cfg.MoveSpeedCap)) / 100
}

// damageTaken shrinks a hit by armor. 50 armor halves it. A hit always does at least 1.
func (w *World) damageTaken(p *Player, dmg int) int {
	return max(dmg*w.cfg.ArmorScale/(w.cfg.ArmorScale+p.stats[items.Armor]), 1)
}

func (w *World) detectRadius(a Archetype, p *Player) float64 {
	return a.DetectRadius * float64(100+p.stats[items.DetectRadius]) / 100
}

// percentFaster turns "+N% speed" into a shorter duration: +100% halves it.
func percentFaster(d time.Duration, pct int) time.Duration {
	return d * 100 / time.Duration(100+pct)
}
