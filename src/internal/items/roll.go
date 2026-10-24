package items

import (
	"crypto/rand"
	"fmt"
	mrand "math/rand/v2"
	"slices"
)

// rarityWeights are out of 1000. See docs/src/design/loot.md.
var rarityWeights = [...]int{Common: 700, Uncommon: 220, Rare: 70, Epic: 10}

const (
	maxPrefixes = 2
	maxSuffixes = 2
)

type Instance struct {
	ID    string
	DefID string
	Name  string
	Slot  Slot
	// WeaponType is empty for anything but a weapon.
	WeaponType WeaponType
	Rarity     Rarity
	Level      int
	Base       map[Stat]int
	Affixes    []Affix
	Quantity   int
	// Stack is the most of this item one inventory slot holds.
	Stack int
}

type Affix struct {
	ID    string
	Name  string
	Stat  Stat
	Tier  int
	Value int
}

// Stats sums base stats and affixes.
func (i Instance) Stats() map[Stat]int {
	out := map[Stat]int{}
	for s, v := range i.Base {
		out[s] += v
	}
	for _, a := range i.Affixes {
		out[a.Stat] += a.Value
	}
	return out
}

// Roller makes items for one cave run. Every roll draws from one generator,
// so the same seed and the same calls give the same items. It is not safe
// for concurrent use; the tick loop is its only caller.
type Roller struct {
	data    *Data
	rng     *mrand.Rand
	uniques map[string]bool
}

func NewRoller(data *Data, seed uint64) *Roller {
	return &Roller{data: data, rng: mrand.New(mrand.NewPCG(seed, seed)), uniques: map[string]bool{}}
}

// Drop rolls a drop table. boost multiplies every rarity weight above Common,
// so elites and bosses give better items.
func (r *Roller) Drop(table string, level, boost int) []Instance {
	t, ok := r.data.Tables[table]
	if !ok {
		return nil
	}
	var out []Instance
	for range r.between(t.Rolls) {
		out = append(out, r.entry(t, level, boost)...)
	}
	return out
}

func (r *Roller) entry(t Table, level, boost int) []Instance {
	var open []Entry
	for _, e := range t.Entries {
		if def, ok := r.data.Defs[e.Item]; ok && (def.MinLevel > level || r.uniques[def.ID]) {
			continue
		}
		open = append(open, e)
	}
	if len(open) == 0 {
		return nil
	}
	e := pick(r.rng, open, func(e Entry) int { return e.Weight })
	switch {
	case e.Table != "":
		return r.Drop(e.Table, level, boost)
	case e.Item != "":
		inst := r.roll(r.data.Defs[e.Item], level, r.rarity(r.data.Defs[e.Item], boost))
		if e.Quantity != (Range{}) {
			inst.Quantity = r.between(e.Quantity)
		}
		return []Instance{inst}
	}
	return nil
}

// StarterKit is the Common gear every player gets on join, until the stash
// can carry gear between runs.
var StarterKit = []string{"rusty_sword", "leather_vest"}

// Make rolls one item at a set rarity, for starter gear and tests.
func (r *Roller) Make(defID string, level int, rarity Rarity) (Instance, error) {
	def, ok := r.data.Defs[defID]
	if !ok {
		return Instance{}, fmt.Errorf("unknown item %q", defID)
	}
	if def.Unique != (rarity == Unique) {
		return Instance{}, fmt.Errorf("item %q cannot be %v", defID, rarity)
	}
	return r.roll(def, level, rarity), nil
}

func (r *Roller) rarity(def Def, boost int) Rarity {
	if def.Unique {
		return Unique
	}
	if !def.Slot.IsGear() {
		return Common
	}
	weights := rarityWeights
	for i := Uncommon; i <= Epic; i++ {
		weights[i] *= boost
	}
	return pick(r.rng, []Rarity{Common, Uncommon, Rare, Epic}, func(x Rarity) int { return weights[x] })
}

func (r *Roller) roll(def Def, level int, rarity Rarity) Instance {
	inst := Instance{ID: NewID(), DefID: def.ID, Name: def.Name, Slot: def.Slot, WeaponType: def.WeaponType, Rarity: rarity, Level: level, Base: map[Stat]int{}, Quantity: 1, Stack: def.Stack}
	// Sorted so the same seed rolls stats in the same order.
	for _, s := range sortedStats(def.Base) {
		inst.Base[s] = r.between(def.Base[s])
	}

	if rarity == Unique {
		r.uniques[def.ID] = true
		for _, f := range def.Fixed {
			inst.Affixes = append(inst.Affixes, Affix{Name: f.Name, Stat: f.Stat, Value: r.between(f.Range)})
		}
		return inst
	}

	count := map[Rarity]int{Uncommon: 1, Rare: 2 + r.rng.IntN(2), Epic: 4}[rarity]
	groups := map[string]bool{}
	types := map[string]int{}
	for range count {
		var open []AffixDef
		for _, a := range r.data.Affixes {
			full := (a.Type == "prefix" && types[a.Type] >= maxPrefixes) || (a.Type == "suffix" && types[a.Type] >= maxSuffixes)
			if !full && !groups[a.Group] && slices.Contains(a.Slots, def.Slot) && a.Tiers[0].Level <= level {
				open = append(open, a)
			}
		}
		if len(open) == 0 {
			break
		}
		a := pick(r.rng, open, func(a AffixDef) int { return a.Weight })
		groups[a.Group] = true
		types[a.Type]++
		tier := 0
		for i, t := range a.Tiers {
			if t.Level <= level {
				tier = i
			}
		}
		inst.Affixes = append(inst.Affixes, Affix{ID: a.ID, Name: a.Name, Stat: a.Stat, Tier: tier + 1, Value: r.between(a.Tiers[tier].Range)})
	}
	return inst
}

func (r *Roller) between(x Range) int {
	return x[0] + r.rng.IntN(x[1]-x[0]+1)
}

func pick[T any](rng *mrand.Rand, options []T, weight func(T) int) T {
	total := 0
	for _, o := range options {
		total += weight(o)
	}
	n := rng.IntN(total)
	for _, o := range options {
		if n -= weight(o); n < 0 {
			return o
		}
	}
	return options[len(options)-1]
}

func sortedStats(m map[Stat]Range) []Stat {
	out := make([]Stat, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// NewID is a random UUIDv4. It comes from crypto/rand, not the run seed,
// because ids must stay unique across runs once items are saved.
func NewID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
