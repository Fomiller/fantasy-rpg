// Package classes loads class and ability data. See
// docs/src/design/classes.md for the rules.
package classes

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Starter is the class a player gets when they do not pick one.
const Starter = "warrior"

type Class struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Base      map[items.Stat]int `json:"base"`
	PerLevel  map[items.Stat]int `json:"per_level"`
	Resource  Resource           `json:"resource"`
	Weapons   []items.WeaponType `json:"weapons"`
	Basic     string             `json:"basic"`
	Abilities []string           `json:"abilities"`
}

// CanWield reports whether the class may equip a weapon of type t.
func (c Class) CanWield(t items.WeaponType) bool {
	return slices.Contains(c.Weapons, t)
}

// Resource is the one shape rage, energy and mana all fit. Rates are per
// second and gains are per hit.
type Resource struct {
	Kind         string `json:"kind"`
	Max          int    `json:"max"`
	Start        int    `json:"start"`
	Regen        int    `json:"regen"`
	OnHitDealt   int    `json:"on_hit_dealt"`
	OnHitTaken   int    `json:"on_hit_taken"`
	Decay        int    `json:"decay"`
	DecayDelayMS int    `json:"decay_delay_ms"`
}

func (r Resource) DecayDelay() time.Duration { return ms(r.DecayDelayMS) }

type Ability struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Cost        int      `json:"cost"`
	CooldownMS  int      `json:"cooldown_ms"`
	WindupMS    int      `json:"windup_ms"`
	Root        bool     `json:"root"`
	UnlockLevel int      `json:"unlock_level"`
	Target      Target   `json:"target"`
	Effects     []Effect `json:"effects"`
}

func (a Ability) Cooldown() time.Duration { return ms(a.CooldownMS) }
func (a Ability) Windup() time.Duration   { return ms(a.WindupMS) }

type Shape string

const (
	Arc        Shape = "arc"
	Self       Shape = "self"
	Projectile Shape = "projectile"
	Area       Shape = "area"
)

type Target struct {
	Shape  Shape   `json:"shape"`
	Range  float64 `json:"range"`
	ArcDeg float64 `json:"arc_deg"`
	Speed  float64 `json:"speed"`
	Radius float64 `json:"radius"`
	Burst  float64 `json:"burst"`
}

type EffectType string

const (
	Damage EffectType = "damage"
	Status EffectType = "status"
	Move   EffectType = "move"
	Blink  EffectType = "blink"
)

type StatusKind string

const (
	Guard StatusKind = "guard"
	Slow  StatusKind = "slow"
	Stun  StatusKind = "stun"
)

type Effect struct {
	Type       EffectType `json:"type"`
	Power      int        `json:"power"`
	Status     StatusKind `json:"status"`
	DurationMS int        `json:"duration_ms"`
	Amount     int        `json:"amount"`
	Distance   float64    `json:"distance"`
}

func (e Effect) Duration() time.Duration { return ms(e.DurationMS) }

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// Progression is the XP curve and the XP rules. XPToNext[i] is the XP to go
// from level i+1 to i+2.
type Progression struct {
	MaxLevel            int   `json:"max_level"`
	XPToNext            []int `json:"xp_to_next"`
	ChestXP             int   `json:"chest_xp"`
	AssistWindowMS      int   `json:"assist_window_ms"`
	ExtractBonus        int   `json:"extract_bonus"`
	ExtractBonusPercent int   `json:"extract_bonus_percent"`
	DeathKeepPercent    int   `json:"death_keep_percent"`
}

func (p Progression) AssistWindow() time.Duration { return ms(p.AssistWindowMS) }

// XPToReach is the total XP a character needs to be at level.
func (p Progression) XPToReach(level int) int {
	total := 0
	for _, n := range p.XPToNext[:min(max(level-1, 0), len(p.XPToNext))] {
		total += n
	}
	return total
}

// LevelFor is the level a total XP gives. XP past the cap still counts but
// gives no more levels.
func (p Progression) LevelFor(xp int) int {
	level := 1
	for _, n := range p.XPToNext {
		if xp < n {
			break
		}
		xp -= n
		level++
	}
	return level
}

// ToNext is the XP from level to the next one. It is 0 at the cap.
func (p Progression) ToNext(level int) int {
	if level < 1 || level >= p.MaxLevel {
		return 0
	}
	return p.XPToNext[level-1]
}

type Data struct {
	Classes     map[string]Class
	Abilities   map[string]Ability
	Progression Progression
}

//go:embed data/*.json
var files embed.FS

// Default is the data shipped in the binary. Bad data fails tests, so a
// panic here means a build skipped them.
func Default() *Data {
	d, err := Load(files)
	if err != nil {
		panic(err)
	}
	return d
}

// Load reads data/classes.json, data/abilities.json and
// data/progression.json from fsys.
func Load(fsys fs.FS) (*Data, error) {
	var classes []Class
	var abilities []Ability
	var progression Progression
	for name, v := range map[string]any{"data/classes.json": &classes, "data/abilities.json": &abilities, "data/progression.json": &progression} {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		// Unknown fields are errors, so a typo like "cooldwn_ms" cannot
		// quietly become a zero.
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	d := &Data{Classes: map[string]Class{}, Abilities: map[string]Ability{}, Progression: progression}
	for _, c := range classes {
		if _, dup := d.Classes[c.ID]; dup {
			return nil, fmt.Errorf("class %q defined twice", c.ID)
		}
		d.Classes[c.ID] = c
	}
	for _, a := range abilities {
		if _, dup := d.Abilities[a.ID]; dup {
			return nil, fmt.Errorf("ability %q defined twice", a.ID)
		}
		d.Abilities[a.ID] = a
	}
	return d, d.validate()
}

func (d *Data) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if _, ok := d.Classes[Starter]; !ok {
		bad("no %q class for players who do not pick one", Starter)
	}
	for id, c := range d.Classes {
		if id == "" || c.Name == "" {
			bad("class %q: needs an id and a name", id)
		}
		for stat, v := range c.Base {
			if !stat.Valid() {
				bad("class %q: unknown stat %q", id, stat)
			}
			if v < 0 {
				bad("class %q: %s is %d", id, stat, v)
			}
		}
		for stat, v := range c.PerLevel {
			if !stat.Valid() {
				bad("class %q: unknown per_level stat %q", id, stat)
			}
			if v < 0 {
				bad("class %q: per_level %s is %d", id, stat, v)
			}
		}
		if c.Base[items.MaxHealth] <= 0 {
			bad("class %q: needs max_health", id)
		}
		r := c.Resource
		if r.Kind == "" || r.Max <= 0 || r.Start < 0 || r.Start > r.Max {
			bad("class %q: resource needs a kind, a max, and a start from 0 to max", id)
		}
		if min(r.Regen, r.OnHitDealt, r.OnHitTaken, r.Decay, r.DecayDelayMS) < 0 {
			bad("class %q: resource rates cannot be negative", id)
		}
		for _, t := range c.Weapons {
			if !t.Valid() {
				bad("class %q: unknown weapon type %q", id, t)
			}
		}
		if !slices.Contains(c.Abilities, c.Basic) {
			bad("class %q: basic %q is not in its abilities", id, c.Basic)
		}
		for i, aid := range c.Abilities {
			a, ok := d.Abilities[aid]
			switch {
			case !ok:
				bad("class %q: unknown ability %q", id, aid)
			case slices.Index(c.Abilities, aid) != i:
				bad("class %q: lists %q twice", id, aid)
			case aid == c.Basic && a.Cost != 0:
				bad("class %q: basic %q must cost nothing", id, aid)
			case aid == c.Basic && a.UnlockLevel != 1:
				bad("class %q: basic %q must unlock at level 1", id, aid)
			case a.Cost > r.Max:
				bad("class %q: %q costs more than the resource holds", id, aid)
			}
		}
	}

	checkProgression(bad, d.Progression)

	for id, a := range d.Abilities {
		if id == "" || a.Name == "" {
			bad("ability %q: needs an id and a name", id)
		}
		if a.Cost < 0 || a.CooldownMS < 0 || a.WindupMS < 0 {
			bad("ability %q: cost, cooldown and windup cannot be negative", id)
		}
		if a.UnlockLevel < 1 || a.UnlockLevel > d.Progression.MaxLevel {
			bad("ability %q: unlock_level must be from 1 to max_level", id)
		}
		if a.Root && a.WindupMS == 0 {
			bad("ability %q: root needs a windup", id)
		}
		checkTarget(bad, id, a.Target)
		if len(a.Effects) == 0 {
			bad("ability %q: needs at least one effect", id)
		}
		for _, e := range a.Effects {
			checkEffect(bad, id, a.Target.Shape, e)
		}
	}
	return errors.Join(errs...)
}

func checkProgression(bad func(string, ...any), p Progression) {
	if p.MaxLevel < 1 {
		bad("progression: max_level must be at least 1")
	}
	if len(p.XPToNext) != p.MaxLevel-1 {
		bad("progression: xp_to_next needs max_level - 1 entries, has %d", len(p.XPToNext))
	}
	for i, n := range p.XPToNext {
		if n <= 0 {
			bad("progression: xp_to_next[%d] is %d, must be above 0", i, n)
		}
	}
	if min(p.ChestXP, p.AssistWindowMS, p.ExtractBonus, p.ExtractBonusPercent) < 0 {
		bad("progression: xp rewards and assist window cannot be negative")
	}
	if p.DeathKeepPercent < 0 || p.DeathKeepPercent > 100 {
		bad("progression: death_keep_percent must be from 0 to 100")
	}
}

// checkTarget allows only the shapes the sim can run. Projectile and area
// are in the design but come with the Rogue and Mage.
func checkTarget(bad func(string, ...any), id string, t Target) {
	switch t.Shape {
	case Arc:
		if t.Range <= 0 || t.ArcDeg <= 0 || t.ArcDeg > 360 {
			bad("ability %q: arc needs a range and arc_deg from 1 to 360", id)
		}
		if t.Speed != 0 || t.Radius != 0 || t.Burst != 0 {
			bad("ability %q: arc takes only range and arc_deg", id)
		}
	case Self:
		if t != (Target{Shape: Self}) {
			bad("ability %q: self takes no fields", id)
		}
	case Projectile, Area:
		bad("ability %q: target %q is not supported yet", id, t.Shape)
	default:
		bad("ability %q: unknown target %q", id, t.Shape)
	}
}

// checkEffect allows damage on what an arc hits and statuses on the caster.
// Enemies do not react to statuses until stun and slow land on them.
func checkEffect(bad func(string, ...any), id string, shape Shape, e Effect) {
	switch e.Type {
	case Damage:
		if e.Power <= 0 || e != (Effect{Type: Damage, Power: e.Power}) {
			bad("ability %q: damage takes only a power above 0", id)
		}
		if shape == Self {
			bad("ability %q: damage cannot target self", id)
		}
	case Status:
		if e.Status != Guard && e.Status != Slow {
			bad("ability %q: status %q is not supported yet", id, e.Status)
		}
		if e.DurationMS <= 0 || e.Amount <= 0 || e.Amount > 100 || e.Power != 0 || e.Distance != 0 {
			bad("ability %q: status takes a duration_ms and an amount from 1 to 100", id)
		}
		if shape != Self {
			bad("ability %q: statuses only work on self for now", id)
		}
	case Move, Blink:
		bad("ability %q: effect %q is not supported yet", id, e.Type)
	default:
		bad("ability %q: unknown effect %q", id, e.Type)
	}
}
