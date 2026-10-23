// Package sim is the authoritative game simulation. It has no networking and
// no Godot types, so it can be stepped and tested on its own.
package sim

import (
	"cmp"
	"math"
	"slices"
	"time"
)

type EntityID uint64

type Config struct {
	MaxSpeed       float64
	Acceleration   float64
	Friction       float64
	PlayerHalfSize float64
	PlayerHealth   int
	AttackRange    float64
	AttackDamage   int
	AttackCooldown time.Duration
}

// DefaultConfig matches the movement values on the Godot Player node.
func DefaultConfig() Config {
	return Config{
		MaxSpeed:       220,
		Acceleration:   1600,
		Friction:       2000,
		PlayerHalfSize: 16,
		PlayerHealth:   100,
		AttackRange:    64,
		AttackDamage:   10,
		AttackCooldown: 500 * time.Millisecond,
	}
}

type Player struct {
	ID           EntityID
	Pos, Vel     Vec2
	Health       int
	LastInputSeq uint32
	input        Vec2
	cooldown     time.Duration
}

type Enemy struct {
	ID     EntityID
	Pos    Vec2
	Health int
}

type MoveCommand struct {
	Player    EntityID
	Seq       uint32
	Direction Vec2
}

type AttackCommand struct {
	Player  EntityID
	Seq     uint32
	Target  EntityID
	Ability string
}

type DamageEvent struct {
	Source, Target EntityID
	Amount         int
	TargetHealth   int
	Killed         bool
}

type World struct {
	Tick    uint64
	cfg     Config
	walls   []Rect
	players map[EntityID]*Player
	enemies map[EntityID]*Enemy
	nextID  EntityID
	pending []any
}

func NewWorld(cfg Config, walls []Rect) *World {
	return &World{
		cfg:     cfg,
		walls:   walls,
		players: map[EntityID]*Player{},
		enemies: map[EntityID]*Enemy{},
	}
}

func (w *World) AddPlayer(pos Vec2) EntityID {
	w.nextID++
	w.players[w.nextID] = &Player{ID: w.nextID, Pos: pos, Health: w.cfg.PlayerHealth}
	return w.nextID
}

func (w *World) AddEnemy(pos Vec2, health int) EntityID {
	w.nextID++
	w.enemies[w.nextID] = &Enemy{ID: w.nextID, Pos: pos, Health: health}
	return w.nextID
}

func (w *World) RemovePlayer(id EntityID) {
	delete(w.players, id)
}

// Players returns copies sorted by ID, so snapshots are stable tick to tick.
func (w *World) Players() []Player {
	out := make([]Player, 0, len(w.players))
	for _, p := range w.players {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b Player) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (w *World) Enemies() []Enemy {
	out := make([]Enemy, 0, len(w.enemies))
	for _, e := range w.enemies {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b Enemy) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (w *World) Player(id EntityID) (Player, bool) {
	p, ok := w.players[id]
	if !ok {
		return Player{}, false
	}
	return *p, true
}

func (w *World) Enemy(id EntityID) (Enemy, bool) {
	e, ok := w.enemies[id]
	if !ok {
		return Enemy{}, false
	}
	return *e, true
}

// Submit queues a client command. Commands only take effect on the next Step,
// so every client's input is applied at a tick boundary.
func (w *World) Submit(cmd any) {
	w.pending = append(w.pending, cmd)
}

func (w *World) Step(dt time.Duration) []DamageEvent {
	var events []DamageEvent
	for _, cmd := range w.pending {
		switch c := cmd.(type) {
		case MoveCommand:
			w.applyMove(c)
		case AttackCommand:
			if ev, ok := w.applyAttack(c); ok {
				events = append(events, ev)
			}
		}
	}
	w.pending = w.pending[:0]

	for _, p := range w.players {
		w.movePlayer(p, dt)
		p.cooldown = max(p.cooldown-dt, 0)
	}
	w.Tick++
	return events
}

func (w *World) applyMove(c MoveCommand) {
	p, ok := w.players[c.Player]
	if !ok || c.Seq <= p.LastInputSeq {
		return
	}
	dir := c.Direction
	// Direction comes from the client. NaN or Inf would poison the position forever.
	if !finite(dir.X) || !finite(dir.Y) {
		return
	}
	if l := dir.Len(); l > 1 {
		dir = dir.Scale(1 / l)
	}
	p.input = dir
	p.LastInputSeq = c.Seq
}

func (w *World) applyAttack(c AttackCommand) (DamageEvent, bool) {
	p, ok := w.players[c.Player]
	if !ok || p.Health <= 0 || p.cooldown > 0 {
		return DamageEvent{}, false
	}
	e, ok := w.enemies[c.Target]
	if !ok || e.Health <= 0 || p.Pos.DistanceTo(e.Pos) > w.cfg.AttackRange {
		return DamageEvent{}, false
	}
	e.Health = max(e.Health-w.cfg.AttackDamage, 0)
	p.cooldown = w.cfg.AttackCooldown
	return DamageEvent{
		Source:       p.ID,
		Target:       e.ID,
		Amount:       w.cfg.AttackDamage,
		TargetHealth: e.Health,
		Killed:       e.Health == 0,
	}, true
}

func (w *World) movePlayer(p *Player, dt time.Duration) {
	secs := dt.Seconds()
	if p.input != (Vec2{}) {
		p.Vel = p.Vel.MoveToward(p.input.Scale(w.cfg.MaxSpeed), w.cfg.Acceleration*secs)
	} else {
		p.Vel = p.Vel.MoveToward(Vec2{}, w.cfg.Friction*secs)
	}

	// One axis at a time, so a player sliding along a wall keeps the other axis.
	half := w.cfg.PlayerHalfSize
	p.Pos.X += p.Vel.X * secs
	for _, wall := range w.walls {
		if box(p.Pos, half).Overlaps(wall) {
			if p.Vel.X > 0 {
				p.Pos.X = wall.Min.X - half
			} else {
				p.Pos.X = wall.Max.X + half
			}
			p.Vel.X = 0
		}
	}
	p.Pos.Y += p.Vel.Y * secs
	for _, wall := range w.walls {
		if box(p.Pos, half).Overlaps(wall) {
			if p.Vel.Y > 0 {
				p.Pos.Y = wall.Min.Y - half
			} else {
				p.Pos.Y = wall.Max.Y + half
			}
			p.Vel.Y = 0
		}
	}
}

func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
