// Package sim is the authoritative game simulation. It has no networking and
// no Godot types, so it can be stepped and tested on its own.
package sim

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

type EntityID uint64

type Config struct {
	MaxSpeed       float64
	Acceleration   float64
	Friction       float64
	PlayerHalfSize float64
	// AttackDamage is weapon damage with no weapon equipped.
	AttackDamage int
	Classes      *classes.Data

	EnemyHalfSize float64
	Archetypes    map[ArchetypeID]Archetype

	ExtractTime time.Duration

	ArmorScale   int
	MoveSpeedCap int

	InventorySize int
	PickupRange   float64
	LootLevel     int
	ChestLoot     string
}

// DefaultConfig matches the movement values on the Godot Player node.
func DefaultConfig() Config {
	return Config{
		MaxSpeed:       220,
		Acceleration:   1600,
		Friction:       2000,
		PlayerHalfSize: 16,
		AttackDamage:   10,
		Classes:        classes.Default(),

		EnemyHalfSize: 16,
		Archetypes:    defaultArchetypes(),

		ExtractTime: 3 * time.Second,

		ArmorScale:   50,
		MoveSpeedCap: 20,

		InventorySize: 20,
		PickupRange:   48,
		LootLevel:     1,
		ChestLoot:     "cave1_chest",
	}
}

type Player struct {
	ID EntityID
	// Class is the class id. It is set on join and never changes.
	Class string
	// Level comes from XP, the banked total. RunXP is earned this run and
	// is not banked until the run ends.
	Level        int
	XP           int
	RunXP        int
	Resource     float64
	MaxResource  int
	Pos, Vel     Vec2
	Health       int
	MaxHealth    int
	Equipment    map[items.Slot]items.Instance
	Inventory    inventory.Bag
	LastInputSeq uint32
	input        Vec2
	inputs       []MoveCommand
	queuedSeq    uint32
	class        classes.Class
	cast         *cast
	cooldowns    map[string]time.Duration
	statuses     map[classes.StatusKind]status
	// sinceHit is how long since the player last dealt or took a hit.
	sinceHit time.Duration
	// ExtractProgress runs from 0 to 1 while the player stands in the extraction zone.
	ExtractProgress float64
	extracting      time.Duration
	stats           map[items.Stat]int
	// looting is the pile whose loot window is open, and lootSeen the pile
	// version that window last got.
	looting  EntityID
	lootSeen uint64
}

type Enemy struct {
	ID        EntityID
	Archetype ArchetypeID
	Pos, Vel  Vec2
	Health    int
	MaxHealth int
	State     EnemyState
	Target    EntityID
	// Aim is a unit vector toward the target, locked for the length of a charge.
	Aim             Vec2
	home            Vec2
	patrolLeft      bool
	timer           time.Duration
	swung           bool
	cooldown        time.Duration
	specialCooldown time.Duration
	lastSeen        Vec2
	searching       bool
	looks           []Vec2
	// trail holds points that each have line of sight to the one before,
	// so Return can walk home around corners.
	trail   []Vec2
	hitBy   EntityID
	charged map[EntityID]bool
	blocked bool
}

type MoveCommand struct {
	Player    EntityID
	Seq       uint32
	Direction Vec2
}

// AttackCommand asks for an ability. Aim is a direction. With no aim the
// ability aims at Target.
type AttackCommand struct {
	Player  EntityID
	Seq     uint32
	Target  EntityID
	Ability string
	Aim     Vec2
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
	Abilities []AbilityEvent
	Rejected  []Rejection
	Extracted []EntityID
	// Inventory lists players whose bag or equipment changed.
	Inventory      []EntityID
	PickupRejected []PickupRejection
	Loot           []LootEvent
}

type World struct {
	Tick    uint64
	cfg     Config
	walls   []Rect
	exit    *Rect
	players map[EntityID]*Player
	enemies map[EntityID]*Enemy
	piles   map[EntityID]*Pile
	shots   map[EntityID]*Projectile
	loot    *items.Roller
	ai      *rand.Rand
	nextID  EntityID
	pending []any
}

func NewWorld(cfg Config, walls []Rect) *World {
	return &World{
		cfg:     cfg,
		walls:   walls,
		players: map[EntityID]*Player{},
		enemies: map[EntityID]*Enemy{},
		piles:   map[EntityID]*Pile{},
		shots:   map[EntityID]*Projectile{},
		ai:      newAI(0),
	}
}

// aiStream keeps AI rolls on their own stream, so they never change which
// loot the same seed drops.
const aiStream = 0xa1

func newAI(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, aiStream))
}

// SeedAI seeds enemy AI from the run seed, so a run can be replayed.
func (w *World) SeedAI(seed uint64) {
	w.ai = newAI(seed)
}

// SetExtraction marks the zone players stand in to leave the cave.
func (w *World) SetExtraction(r Rect) {
	w.exit = &r
}

// AddPlayer adds a level 1 player of the starter class.
func (w *World) AddPlayer(pos Vec2) EntityID {
	return w.addPlayer(pos, w.cfg.Classes.Classes[classes.Starter], 0)
}

// AddPlayerAs adds a player of a class with banked XP, which sets the
// level. An empty class is the starter.
func (w *World) AddPlayerAs(pos Vec2, class string, xp int) (EntityID, error) {
	if class == "" {
		class = classes.Starter
	}
	c, ok := w.cfg.Classes.Classes[class]
	if !ok {
		return 0, fmt.Errorf("unknown class %q", class)
	}
	if xp < 0 {
		return 0, fmt.Errorf("xp is %d", xp)
	}
	return w.addPlayer(pos, c, xp), nil
}

func (w *World) addPlayer(pos Vec2, c classes.Class, xp int) EntityID {
	w.nextID++
	p := &Player{
		ID:          w.nextID,
		Class:       c.ID,
		Level:       w.cfg.Classes.Progression.LevelFor(xp),
		XP:          xp,
		Resource:    float64(c.Resource.Start),
		MaxResource: c.Resource.Max,
		Pos:         pos,
		Equipment:   map[items.Slot]items.Instance{},
		Inventory:   inventory.Bag{Size: w.cfg.InventorySize},
		class:       c,
		cooldowns:   map[string]time.Duration{},
		statuses:    map[classes.StatusKind]status{},
	}
	w.applyGear(p)
	w.players[p.ID] = p
	return p.ID
}

func (w *World) AddEnemy(pos Vec2, archetype ArchetypeID) EntityID {
	w.nextID++
	health := w.cfg.Archetypes[archetype].Health
	w.enemies[w.nextID] = &Enemy{ID: w.nextID, Archetype: archetype, Pos: pos, Health: health, MaxHealth: health, home: pos}
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
	w.stepAbilities(dt, &ev)
	for _, cmd := range w.pending {
		switch c := cmd.(type) {
		case MoveCommand:
			w.applyMove(c)
		case AttackCommand:
			w.applyAttack(c, &ev)
		case ItemCommand:
			if w.applyItem(c, &ev) {
				ev.changed(c.player())
			}
		case OpenLootCommand, CloseLootCommand:
			w.applyLoot(c)
		}
	}
	w.pending = w.pending[:0]

	for _, p := range w.players {
		w.movePlayer(p, dt)
	}
	ev.Damage = append(ev.Damage, w.stepEnemies(dt)...)
	ev.Damage = append(ev.Damage, w.stepProjectiles(dt)...)
	w.dropOnKill(&ev)
	ev.Extracted = w.stepExtraction(dt)
	w.stepLoot(&ev)
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

func (w *World) movePlayer(p *Player, dt time.Duration) {
	p.nextInput()
	if p.Health <= 0 {
		p.Vel = Vec2{}
		return
	}
	secs := dt.Seconds()
	// A root acts like letting go of the keys, so the client can predict it.
	if input := p.input; input != (Vec2{}) && !p.rooted() {
		p.Vel = p.Vel.MoveToward(input.Scale(w.maxSpeed(p)), w.cfg.Acceleration*secs)
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
