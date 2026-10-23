package items

import (
	"math"
	"slices"
	"testing"
)

var data = Default()

// withoutIDs drops the random instance ids so rolls can be compared.
func withoutIDs(items []Instance) []Instance {
	out := slices.Clone(items)
	for i := range out {
		out[i].ID = ""
	}
	return out
}

func TestSameSeedSameLoot(t *testing.T) {
	roll := func(seed uint64) []Instance {
		r := NewRoller(data, seed)
		var out []Instance
		for range 200 {
			out = append(out, r.Drop("cave1_chest", 5, 1)...)
		}
		return withoutIDs(out)
	}
	a, b, c := roll(42), roll(42), roll(43)
	if len(a) == 0 {
		t.Fatal("chest dropped nothing")
	}
	if !equal(a, b) {
		t.Fatal("same seed gave different loot")
	}
	if equal(a, c) {
		t.Fatal("different seeds gave the same loot")
	}
}

func equal(a, b []Instance) bool {
	return slices.EqualFunc(a, b, func(x, y Instance) bool {
		if x.DefID != y.DefID || x.Name != y.Name || x.Slot != y.Slot || x.Rarity != y.Rarity || x.Quantity != y.Quantity || len(x.Base) != len(y.Base) || !slices.Equal(x.Affixes, y.Affixes) {
			return false
		}
		for s, v := range x.Base {
			if y.Base[s] != v {
				return false
			}
		}
		return true
	})
}

func TestRarityFollowsWeights(t *testing.T) {
	for _, boost := range []int{1, 3} {
		r := NewRoller(data, 1)
		const n = 50000
		counts := map[Rarity]int{}
		for range n {
			counts[r.rarity(data.Defs["iron_sword"], boost)]++
		}
		total := rarityWeights[Common]
		for i := Uncommon; i <= Epic; i++ {
			total += rarityWeights[i] * boost
		}
		for i := Common; i <= Epic; i++ {
			w := rarityWeights[i]
			if i != Common {
				w *= boost
			}
			want := float64(w) / float64(total)
			got := float64(counts[i]) / n
			if math.Abs(got-want) > 0.01 {
				t.Errorf("boost %d, %v: got %.3f, want %.3f", boost, i, got, want)
			}
		}
	}
}

func TestNonGearIsAlwaysCommon(t *testing.T) {
	r := NewRoller(data, 1)
	for range 1000 {
		if got := r.rarity(data.Defs["health_potion"], 10); got != Common {
			t.Fatalf("potion rolled %v", got)
		}
	}
}

func TestAffixRules(t *testing.T) {
	d := data
	byID := map[string]AffixDef{}
	for _, a := range d.Affixes {
		byID[a.ID] = a
	}
	r := NewRoller(d, 7)
	for _, level := range []int{1, 5, 10} {
		for id, def := range d.Defs {
			if !def.Slot.IsGear() || def.Unique {
				continue
			}
			for _, rarity := range []Rarity{Common, Uncommon, Rare, Epic} {
				for range 50 {
					inst, err := r.Make(id, level, rarity)
					if err != nil {
						t.Fatal(err)
					}
					checkAffixes(t, inst, def, byID)
				}
			}
		}
	}
}

func checkAffixes(t *testing.T, inst Instance, def Def, byID map[string]AffixDef) {
	t.Helper()
	most := map[Rarity]int{Common: 0, Uncommon: 1, Rare: 3, Epic: 4}[inst.Rarity]
	if len(inst.Affixes) > most {
		t.Fatalf("%v %s has %d affixes", inst.Rarity, def.ID, len(inst.Affixes))
	}
	groups := map[string]bool{}
	types := map[string]int{}
	for _, a := range inst.Affixes {
		ad := byID[a.ID]
		if groups[ad.Group] {
			t.Fatalf("%s rolled group %q twice", def.ID, ad.Group)
		}
		groups[ad.Group] = true
		types[ad.Type]++
		if !slices.Contains(ad.Slots, def.Slot) {
			t.Fatalf("%s rolled %s, which is not allowed on %s", def.ID, a.ID, def.Slot)
		}
		tier := ad.Tiers[a.Tier-1]
		if tier.Level > inst.Level || (a.Tier < len(ad.Tiers) && ad.Tiers[a.Tier].Level <= inst.Level) {
			t.Fatalf("%s level %d rolled %s tier %d, want the highest tier allowed", def.ID, inst.Level, a.ID, a.Tier)
		}
		if a.Value < tier.Range[0] || a.Value > tier.Range[1] {
			t.Fatalf("%s value %d outside %v", a.ID, a.Value, tier.Range)
		}
	}
	if types["prefix"] > maxPrefixes || types["suffix"] > maxSuffixes {
		t.Fatalf("%s has %v", def.ID, types)
	}
	if inst.Rarity == Epic {
		if want := affixRoom(def, inst.Level, byID); len(inst.Affixes) != want {
			t.Fatalf("epic %s has %d affixes, want %d", def.ID, len(inst.Affixes), want)
		}
	}
}

// affixRoom is how many affixes an item can hold: 4 at most, fewer when the
// pool for its slot and level runs out.
func affixRoom(def Def, level int, byID map[string]AffixDef) int {
	groups := map[string]map[string]bool{"prefix": {}, "suffix": {}}
	for _, a := range byID {
		if slices.Contains(a.Slots, def.Slot) && a.Tiers[0].Level <= level {
			groups[a.Type][a.Group] = true
		}
	}
	return min(len(groups["prefix"]), maxPrefixes) + min(len(groups["suffix"]), maxSuffixes)
}

func TestUniqueDropsOncePerRun(t *testing.T) {
	r := NewRoller(data, 3)
	lanterns := 0
	for range 20000 {
		for _, inst := range r.Drop("cave1_gear", 5, 1) {
			if inst.DefID == "lantern_of_the_deep" {
				lanterns++
				if inst.Rarity != Unique || len(inst.Affixes) != 3 {
					t.Fatalf("lantern = %+v", inst)
				}
			}
		}
	}
	if lanterns != 1 {
		t.Fatalf("got %d lanterns in one run, want 1", lanterns)
	}
}

func TestMinLevelAndQuantity(t *testing.T) {
	r := NewRoller(data, 5)
	for range 5000 {
		for _, inst := range r.Drop("cave1_chest", 1, 1) {
			if def := data.Defs[inst.DefID]; def.MinLevel > 1 {
				t.Fatalf("level 1 chest dropped %s (min level %d)", inst.DefID, def.MinLevel)
			}
			if inst.DefID == "iron_ore" && (inst.Quantity < 3 || inst.Quantity > 6) {
				t.Fatalf("ore quantity %d, want 3 to 6", inst.Quantity)
			}
			if inst.DefID != "iron_ore" && inst.Quantity != 1 {
				t.Fatalf("%s quantity %d", inst.DefID, inst.Quantity)
			}
		}
	}
}

func TestMakeAndStats(t *testing.T) {
	r := NewRoller(data, 9)
	if _, err := r.Make("nope", 1, Common); err == nil {
		t.Fatal("want error for unknown item")
	}
	if _, err := r.Make("iron_sword", 1, Unique); err == nil {
		t.Fatal("want error for unique iron sword")
	}
	inst, err := r.Make("iron_sword", 1, Uncommon)
	if err != nil {
		t.Fatal(err)
	}
	want := inst.Base[Damage]
	for _, a := range inst.Affixes {
		if a.Stat == Damage {
			want += a.Value
		}
	}
	if got := inst.Stats()[Damage]; got != want {
		t.Fatalf("damage stat = %d, want %d", got, want)
	}
	if inst.Name != "Iron Sword" || inst.Slot != Weapon {
		t.Fatalf("name %q, slot %q", inst.Name, inst.Slot)
	}
	if len(inst.ID) != 36 {
		t.Fatalf("id %q is not a UUID", inst.ID)
	}
}

func TestStarterKitExists(t *testing.T) {
	r := NewRoller(data, 1)
	for _, id := range StarterKit {
		if _, err := r.Make(id, 1, Common); err != nil {
			t.Fatal(err)
		}
	}
}
