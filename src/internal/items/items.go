// Package items loads item data and rolls item instances. See
// docs/src/design/loot.md for the rules.
package items

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type Stat string

const (
	Damage       Stat = "damage"
	AttackSpeed  Stat = "attack_speed"
	Armor        Stat = "armor"
	MaxHealth    Stat = "max_health"
	MoveSpeed    Stat = "move_speed"
	ExtractSpeed Stat = "extract_speed"
	Heal         Stat = "heal"
	DetectRadius Stat = "detect_radius"
)

var stats = []Stat{Damage, AttackSpeed, Armor, MaxHealth, MoveSpeed, ExtractSpeed, Heal, DetectRadius}

type Slot string

const (
	Weapon     Slot = "weapon"
	Head       Slot = "head"
	Chest      Slot = "chest"
	Hands      Slot = "hands"
	Feet       Slot = "feet"
	Trinket    Slot = "trinket"
	Consumable Slot = "consumable"
	Material   Slot = "material"
)

// GearSlots are the slots a player can equip.
var GearSlots = []Slot{Weapon, Head, Chest, Hands, Feet, Trinket}

func (s Slot) IsGear() bool { return slices.Contains(GearSlots, s) }

type Rarity int

const (
	Common Rarity = iota
	Uncommon
	Rare
	Epic
	Unique
)

func (r Rarity) String() string {
	return [...]string{"common", "uncommon", "rare", "epic", "unique"}[r]
}

// Range is an inclusive [min, max] pair, written as a two element JSON array.
type Range [2]int

type Def struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Slot     Slot           `json:"slot"`
	Base     map[Stat]Range `json:"base"`
	MinLevel int            `json:"min_level"`
	Stack    int            `json:"stack"`
	Value    int            `json:"value"`
	Unique   bool           `json:"unique"`
	Fixed    []Fixed        `json:"fixed"`
}

// Fixed is an affix line a unique always has.
type Fixed struct {
	Name  string `json:"name"`
	Stat  Stat   `json:"stat"`
	Range Range  `json:"range"`
}

type AffixDef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Group  string `json:"group"`
	Stat   Stat   `json:"stat"`
	Slots  []Slot `json:"slots"`
	Weight int    `json:"weight"`
	Tiers  []Tier `json:"tiers"`
}

type Tier struct {
	Level int   `json:"level"`
	Range Range `json:"range"`
}

type Table struct {
	ID      string  `json:"id"`
	Rolls   Range   `json:"rolls"`
	Entries []Entry `json:"entries"`
}

// Entry is one of: an item, another table, or nothing.
type Entry struct {
	Weight   int    `json:"weight"`
	Nothing  bool   `json:"nothing"`
	Item     string `json:"item"`
	Table    string `json:"table"`
	Quantity Range  `json:"quantity"`
}

type Data struct {
	Defs    map[string]Def
	Affixes []AffixDef
	Tables  map[string]Table
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

func Load(fsys embed.FS) (*Data, error) {
	var defs []Def
	var tables []Table
	d := &Data{Defs: map[string]Def{}, Tables: map[string]Table{}}
	for name, v := range map[string]any{"data/items.json": &defs, "data/affixes.json": &d.Affixes, "data/tables.json": &tables} {
		b, err := fsys.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, def := range defs {
		if _, dup := d.Defs[def.ID]; dup {
			return nil, fmt.Errorf("item %q defined twice", def.ID)
		}
		d.Defs[def.ID] = def
	}
	for _, t := range tables {
		if _, dup := d.Tables[t.ID]; dup {
			return nil, fmt.Errorf("table %q defined twice", t.ID)
		}
		d.Tables[t.ID] = t
	}
	return d, d.validate()
}

func (d *Data) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	for id, def := range d.Defs {
		if !slices.Contains(append(GearSlots, Consumable, Material), def.Slot) {
			bad("item %q: unknown slot %q", id, def.Slot)
		}
		if def.Stack < 1 || (def.Slot.IsGear() && def.Stack != 1) {
			bad("item %q: stack %d", id, def.Stack)
		}
		for stat, r := range def.Base {
			checkStat(bad, "item "+id, stat, r)
		}
		for _, f := range def.Fixed {
			checkStat(bad, "item "+id, f.Stat, f.Range)
		}
		if def.Unique != (len(def.Fixed) > 0) {
			bad("item %q: uniques need fixed affixes and only uniques have them", id)
		}
	}

	seen := map[string]bool{}
	for _, a := range d.Affixes {
		if seen[a.ID] {
			bad("affix %q defined twice", a.ID)
		}
		seen[a.ID] = true
		if a.Type != "prefix" && a.Type != "suffix" {
			bad("affix %q: type %q", a.ID, a.Type)
		}
		if a.Weight <= 0 || len(a.Tiers) == 0 || a.Group == "" {
			bad("affix %q: needs a weight, a group and tiers", a.ID)
		}
		for _, s := range a.Slots {
			if !s.IsGear() {
				bad("affix %q: slot %q is not gear", a.ID, s)
			}
		}
		for i, t := range a.Tiers {
			checkStat(bad, "affix "+a.ID, a.Stat, t.Range)
			if i > 0 && t.Level <= a.Tiers[i-1].Level {
				bad("affix %q: tier levels must go up", a.ID)
			}
		}
	}

	for id, t := range d.Tables {
		if t.Rolls[0] < 1 || t.Rolls[0] > t.Rolls[1] {
			bad("table %q: rolls %v", id, t.Rolls)
		}
		for _, e := range t.Entries {
			kinds := 0
			for _, set := range []bool{e.Nothing, e.Item != "", e.Table != ""} {
				if set {
					kinds++
				}
			}
			if kinds != 1 || e.Weight <= 0 {
				bad("table %q: each entry needs a weight and exactly one of nothing, item, table", id)
			}
			if def, ok := d.Defs[e.Item]; e.Item != "" && !ok {
				bad("table %q: unknown item %q", id, e.Item)
			} else if ok && e.Quantity != (Range{}) && (def.Stack == 1 || e.Quantity[0] < 1 || e.Quantity[0] > e.Quantity[1] || e.Quantity[1] > def.Stack) {
				bad("table %q: bad quantity %v for %q", id, e.Quantity, e.Item)
			}
			if _, ok := d.Tables[e.Table]; e.Table != "" && !ok {
				bad("table %q: unknown table %q", id, e.Table)
			}
		}
		if d.loops(id, nil) {
			bad("table %q: loops back on itself", id)
		}
	}
	return errors.Join(errs...)
}

func (d *Data) loops(id string, path []string) bool {
	if slices.Contains(path, id) {
		return true
	}
	path = append(path, id)
	for _, e := range d.Tables[id].Entries {
		if e.Table != "" && d.loops(e.Table, path) {
			return true
		}
	}
	return false
}

func checkStat(bad func(string, ...any), who string, stat Stat, r Range) {
	if !slices.Contains(stats, stat) {
		bad("%s: unknown stat %q", who, stat)
	}
	if r[0] > r[1] || r[0] < 0 {
		bad("%s: %s range %v", who, stat, r)
	}
}
