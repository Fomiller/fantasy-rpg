package classes

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func TestDefaultDataLoads(t *testing.T) {
	d := Default()
	w, ok := d.Classes[Starter]
	if !ok || w.Base[items.MaxHealth] != 120 || w.Resource.Kind != "rage" {
		t.Fatalf("warrior = %+v", w)
	}
	for _, id := range []string{"slash", "cleave", "brace"} {
		if !slices.Contains(w.Abilities, id) {
			t.Errorf("warrior is missing %q", id)
		}
	}
	if !w.CanWield(items.Sword) || !w.CanWield(items.Axe) || w.CanWield(items.Staff) {
		t.Fatalf("warrior weapons = %v", w.Weapons)
	}
	// Unlock levels from docs/src/design/progression.md.
	for id, level := range map[string]int{"slash": 1, "cleave": 2, "brace": 3} {
		if got := d.Abilities[id].UnlockLevel; got != level {
			t.Errorf("%s unlocks at %d, want %d", id, got, level)
		}
	}
	if w.PerLevel[items.MaxHealth] != 5 {
		t.Fatalf("warrior per_level = %v", w.PerLevel)
	}
}

// The curve from docs/src/design/progression.md.
func TestXPCurve(t *testing.T) {
	p := Default().Progression
	if p.MaxLevel != 10 {
		t.Fatalf("max level = %d", p.MaxLevel)
	}
	reach := []int{0, 100, 250, 450, 700, 1000, 1350, 1750, 2200, 2700}
	for i, want := range reach {
		level := i + 1
		if got := p.XPToReach(level); got != want {
			t.Errorf("XPToReach(%d) = %d, want %d", level, got, want)
		}
		if got := p.LevelFor(want); got != level {
			t.Errorf("LevelFor(%d) = %d, want %d", want, got, level)
		}
		if want > 0 {
			if got := p.LevelFor(want - 1); got != level-1 {
				t.Errorf("LevelFor(%d) = %d, want %d", want-1, got, level-1)
			}
		}
	}
	if got := p.LevelFor(1_000_000); got != 10 {
		t.Errorf("LevelFor past the cap = %d, want 10", got)
	}
	if p.ToNext(1) != 100 || p.ToNext(9) != 500 || p.ToNext(10) != 0 {
		t.Errorf("ToNext = %d, %d, %d", p.ToNext(1), p.ToNext(9), p.ToNext(10))
	}
}

func TestValidateCatchesBadData(t *testing.T) {
	good := func() *Data {
		return &Data{
			Classes: map[string]Class{
				Starter: {
					ID: Starter, Name: "Warrior",
					Base:      map[items.Stat]int{items.MaxHealth: 100},
					PerLevel:  map[items.Stat]int{items.MaxHealth: 5},
					Resource:  Resource{Kind: "rage", Max: 100},
					Weapons:   []items.WeaponType{items.Sword},
					Basic:     "hit",
					Abilities: []string{"hit", "guard"},
				},
			},
			Abilities: map[string]Ability{
				"hit": {ID: "hit", Name: "Hit", UnlockLevel: 1, CooldownMS: 500, WindupMS: 100, Target: Target{Shape: Arc, Range: 64, ArcDeg: 90}, Effects: []Effect{{Type: Damage, Power: 100}}},
				"guard": {ID: "guard", Name: "Guard", Cost: 20, UnlockLevel: 3, Target: Target{Shape: Self}, Effects: []Effect{
					{Type: Status, Status: Guard, DurationMS: 1000, Amount: 50},
				}},
			},
			Progression: Progression{MaxLevel: 3, XPToNext: []int{100, 150}},
		}
	}
	if err := good().validate(); err != nil {
		t.Fatalf("good data failed: %v", err)
	}

	class := func(d *Data, f func(c *Class)) {
		c := d.Classes[Starter]
		f(&c)
		d.Classes[Starter] = c
	}
	ability := func(d *Data, id string, f func(a *Ability)) {
		a := d.Abilities[id]
		f(&a)
		d.Abilities[id] = a
	}
	tests := []struct {
		name   string
		break_ func(d *Data)
		want   string
	}{
		{"no starter", func(d *Data) {
			d.Classes["rogue"] = d.Classes[Starter]
			delete(d.Classes, Starter)
		}, "no \"warrior\""},
		{"unknown stat", func(d *Data) { class(d, func(c *Class) { c.Base["luck"] = 5 }) }, "unknown stat"},
		{"unknown per_level stat", func(d *Data) { class(d, func(c *Class) { c.PerLevel["luck"] = 1 }) }, "unknown per_level stat"},
		{"negative per_level", func(d *Data) { class(d, func(c *Class) { c.PerLevel[items.Armor] = -1 }) }, "per_level armor is -1"},
		{"no max level", func(d *Data) { d.Progression = Progression{} }, "max_level"},
		{"short curve", func(d *Data) { d.Progression.XPToNext = []int{100} }, "max_level - 1 entries"},
		{"zero step", func(d *Data) { d.Progression.XPToNext[1] = 0 }, "must be above 0"},
		{"negative chest xp", func(d *Data) { d.Progression.ChestXP = -5 }, "cannot be negative"},
		{"death keep over 100", func(d *Data) { d.Progression.DeathKeepPercent = 150 }, "death_keep_percent"},
		{"no health", func(d *Data) { class(d, func(c *Class) { delete(c.Base, items.MaxHealth) }) }, "max_health"},
		{"start over max", func(d *Data) { class(d, func(c *Class) { c.Resource.Start = 200 }) }, "start from 0 to max"},
		{"negative decay", func(d *Data) { class(d, func(c *Class) { c.Resource.Decay = -1 }) }, "negative"},
		{"unknown weapon", func(d *Data) { class(d, func(c *Class) { c.Weapons = []items.WeaponType{"whip"} }) }, "unknown weapon type"},
		{"unknown ability", func(d *Data) { class(d, func(c *Class) { c.Abilities = append(c.Abilities, "fireball") }) }, "unknown ability"},
		{"basic not listed", func(d *Data) { class(d, func(c *Class) { c.Basic = "kick" }) }, "basic \"kick\""},
		{"listed twice", func(d *Data) { class(d, func(c *Class) { c.Abilities = append(c.Abilities, "hit") }) }, "twice"},
		{"no unlock level", func(d *Data) { ability(d, "guard", func(a *Ability) { a.UnlockLevel = 0 }) }, "unlock_level must be from 1 to max_level"},
		{"unlock past max level", func(d *Data) { ability(d, "guard", func(a *Ability) { a.UnlockLevel = 4 }) }, "unlock_level must be from 1 to max_level"},
		{"basic locked", func(d *Data) { ability(d, "hit", func(a *Ability) { a.UnlockLevel = 2 }) }, "must unlock at level 1"},
		{"basic costs", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Cost = 5 }) }, "cost nothing"},
		{"cost over max", func(d *Data) { ability(d, "guard", func(a *Ability) { a.Cost = 150 }) }, "costs more"},
		{"negative cooldown", func(d *Data) { ability(d, "hit", func(a *Ability) { a.CooldownMS = -1 }) }, "negative"},
		{"root without windup", func(d *Data) { ability(d, "guard", func(a *Ability) { a.Root = true }) }, "root needs a windup"},
		{"arc without range", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Target.Range = 0 }) }, "arc needs"},
		{"arc with speed", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Target.Speed = 300 }) }, "only range and arc_deg"},
		{"self with range", func(d *Data) { ability(d, "guard", func(a *Ability) { a.Target.Range = 10 }) }, "no fields"},
		{"unknown target", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Target = Target{Shape: "cone"} }) }, "unknown target"},
		{"projectile not yet", func(d *Data) {
			ability(d, "hit", func(a *Ability) { a.Target = Target{Shape: Projectile, Speed: 300, Radius: 5, Range: 200} })
		}, "not supported yet"},
		{"no effects", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Effects = nil }) }, "at least one effect"},
		{"unknown effect", func(d *Data) { ability(d, "hit", func(a *Ability) { a.Effects = []Effect{{Type: "heal"}} }) }, "unknown effect"},
		{"damage on self", func(d *Data) {
			ability(d, "guard", func(a *Ability) { a.Effects = []Effect{{Type: Damage, Power: 50}} })
		}, "cannot target self"},
		{"stun not yet", func(d *Data) {
			ability(d, "guard", func(a *Ability) { a.Effects = []Effect{{Type: Status, Status: Stun, DurationMS: 1000, Amount: 1}} })
		}, "not supported yet"},
		{"status on arc", func(d *Data) {
			ability(d, "hit", func(a *Ability) { a.Effects = []Effect{{Type: Status, Status: Slow, DurationMS: 1000, Amount: 30}} })
		}, "only work on self"},
		{"guard over 100", func(d *Data) { ability(d, "guard", func(a *Ability) { a.Effects[0].Amount = 150 }) }, "amount from 1 to 100"},
		{"blink not yet", func(d *Data) {
			ability(d, "guard", func(a *Ability) { a.Effects = []Effect{{Type: Blink, Distance: 140}} })
		}, "not supported yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := good()
			tt.break_(d)
			err := d.validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error with %q", err, tt.want)
			}
		})
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	fsys := fstest.MapFS{
		"data/classes.json":     {Data: []byte(`[]`)},
		"data/abilities.json":   {Data: []byte(`[{"id": "hit", "cooldwn_ms": 500}]`)},
		"data/progression.json": {Data: []byte(`{}`)},
	}
	if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), "cooldwn_ms") {
		t.Fatalf("got %v, want an unknown field error", err)
	}
}

func TestLoadRejectsDuplicates(t *testing.T) {
	fsys := fstest.MapFS{
		"data/classes.json":     {Data: []byte(`[{"id": "warrior"}, {"id": "warrior"}]`)},
		"data/abilities.json":   {Data: []byte(`[]`)},
		"data/progression.json": {Data: []byte(`{}`)},
	}
	if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("got %v, want a duplicate error", err)
	}
}
