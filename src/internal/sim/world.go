// Package sim is the authoritative game simulation. It has no networking and
// no Godot types, so it can be stepped and tested on its own.
package sim

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
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

	EnemyHalfSize       float64
	EnemySpeed          float64
	PatrolSpeed         float64
	PatrolRadius        float64
	DetectRadius        float64
	LoseRadius          float64
	LeashRadius         float64
	EnemyAttackRange    float64
	EnemyDamage         int
	EnemyAttackCooldown time.Duration

	ExtractTime time.Duration

	ArmorScale   int
	MoveSpeedCap int
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

		// Slower than players, so a player can always run away.
		EnemyHalfSize:       16,
		EnemySpeed:          140,
		PatrolSpeed:         60,
		PatrolRadius:        80,
		DetectRadius:        200,
		LoseRadius:          320,
		LeashRadius:         450,
		EnemyAttackRange:    40,
		EnemyDamage:         10,
		EnemyAttackCooldown: time.Second,

		ExtractTime: 3 * time.Second,

		ArmorScale:   50,
		MoveSpeedCap: 20,
	}
}

type Player struct {
	ID           EntityID
	Pos, Vel     Vec2
	Health       int
	MaxHealth    int
	Equipment    map[items.Slot]items.Instance
	LastInputSeq uint32
	input        Vec2
	inputs       []MoveCommand
	queuedSeq    uint32
	cooldown     time.Duration
	// ExtractProgress runs from 0 to 1 while the player stands in the extraction zone.
	ExtractProgress float64
	extracting      time.Duration
	stats           map[items.Stat]int
}

type Enemy struct {
	ID         EntityID
	Pos, Vel   Vec2
	Health     int
	State      EnemyState
	Target     EntityID
	home       Vec2
	patrolLeft bool
	cooldown   time.Duration
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

// Events is what happened during one Step.
type Events struct {
	Damage    []DamageEvent
	Extracted []EntityID
}

type World struct {
	Tick    uint64
	cfg     Config
	walls   []Rect
	exit    *Rect
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

// SetExtraction marks the zone players stand in to leave the cave.
func (w *World) SetExtraction(r Rect) {
	w.exit = &r
}

func (w *World) AddPlayer(pos Vec2) EntityID {
	w.nextID++
	w.players[w.nextID] = &Player{
		ID:        w.nextID,
		Pos:       pos,
		Health:    w.cfg.PlayerHealth,
		MaxHealth: w.cfg.PlayerHealth,
		Equipment: map[items.Slot]items.Instance{},
		stats:     map[items.Stat]int{},
	}
	return w.nextID
}

func (w *World) AddEnemy(pos Vec2, health int) EntityID {
	w.nextID++
	w.enemies[w.nextID] = &Enemy{ID: w.nextID, Pos: pos, Health: health, home: pos}
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

func (w *World) Step(dt time.Duration) Events {
	var ev Events
	for _, cmd := range w.pending {
		switch c := cmd.(type) {
		case MoveCommand:
			w.applyMove(c)
		case AttackCommand:
			if d, ok := w.applyAttack(c); ok {
				ev.Damage = append(ev.Damage, d)
			}
		}
	}
	w.pending = w.pending[:0]

	for _, p := range w.players {
		w.movePlayer(p, dt)
		p.cooldown = max(p.cooldown-dt, 0)
	}
	ev.Damage = append(ev.Damage, w.stepEnemies(dt)...)
	ev.Extracted = w.stepExtraction(dt)
	w.Tick++
	return ev
}

// stepExtraction removes players who stood in the zone long enough. Stepping
// out resets the timer.
func (w *World) stepExtraction(dt time.Duration) []EntityID {
	if w.exit == nil {
		return nil
	}
	var out []EntityID
	for _, id := range slices.Sorted(maps.Keys(w.players)) {
		p := w.players[id]
		if p.Health <= 0 || !w.exit.Contains(p.Pos) {
			p.extracting, p.ExtractProgress = 0, 0
			continue
		}
		p.extracting += dt
		need := w.extractTime(p)
		p.ExtractProgress = min(p.extracting.Seconds()/need.Seconds(), 1)
		if p.extracting >= need {
			delete(w.players, id)
			out = append(out, id)
		}
	}
	return out
}

// maxQueuedInputs bounds how far behind a player's inputs can fall. The
// client sends one input per tick, so a longer queue only adds lag.
const maxQueuedInputs = 8

// applyMove queues an input. Each tick uses one, so a client that sends
// faster than the tick rate cannot move faster.
func (w *World) applyMove(c MoveCommand) {
	p, ok := w.players[c.Player]
	if !ok || c.Seq <= p.queuedSeq {
		return
	}
	// Direction comes from the client. NaN or Inf would poison the position forever.
	if !finite(c.Direction.X) || !finite(c.Direction.Y) {
		return
	}
	if l := c.Direction.Len(); l > 1 {
		c.Direction = c.Direction.Scale(1 / l)
	}
	if len(p.inputs) == maxQueuedInputs {
		p.inputs = p.inputs[1:]
	}
	p.inputs = append(p.inputs, c)
	p.queuedSeq = c.Seq
}

// nextInput takes the oldest queued input. With none queued the player keeps
// the last one, so a late packet does not stop them.
func (p *Player) nextInput() {
	if len(p.inputs) == 0 {
		return
	}
	p.input = p.inputs[0].Direction
	p.LastInputSeq = p.inputs[0].Seq
	p.inputs = p.inputs[1:]
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
	dmg := w.attackDamage(p)
	e.Health = max(e.Health-dmg, 0)
	p.cooldown = w.attackCooldown(p)
	return DamageEvent{
		Source:       p.ID,
		Target:       e.ID,
		Amount:       dmg,
		TargetHealth: e.Health,
		Killed:       e.Health == 0,
	}, true
}

func (w *World) movePlayer(p *Player, dt time.Duration) {
	p.nextInput()
	if p.Health <= 0 {
		p.Vel = Vec2{}
		return
	}
	secs := dt.Seconds()
	if p.input != (Vec2{}) {
		p.Vel = p.Vel.MoveToward(p.input.Scale(w.maxSpeed(p)), w.cfg.Acceleration*secs)
	} else {
		p.Vel = p.Vel.MoveToward(Vec2{}, w.cfg.Friction*secs)
	}

	w.slide(&p.Pos, &p.Vel, w.cfg.PlayerHalfSize, secs)
}

// slide moves one axis at a time, so a body sliding along a wall keeps the other axis.
func (w *World) slide(pos, vel *Vec2, half, secs float64) {
	pos.X += vel.X * secs
	for _, wall := range w.walls {
		if box(*pos, half).Overlaps(wall) {
			if vel.X > 0 {
				pos.X = wall.Min.X - half
			} else {
				pos.X = wall.Max.X + half
			}
			vel.X = 0
		}
	}
	pos.Y += vel.Y * secs
	for _, wall := range w.walls {
		if box(*pos, half).Overlaps(wall) {
			if vel.Y > 0 {
				pos.Y = wall.Min.Y - half
			} else {
				pos.Y = wall.Max.Y + half
			}
			vel.Y = 0
		}
	}
}

func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
