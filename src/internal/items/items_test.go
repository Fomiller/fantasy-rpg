package items

import (
	"strings"
	"testing"
)

func TestDefaultDataLoads(t *testing.T) {
	d := Default()
	if len(d.Defs) == 0 || len(d.Affixes) == 0 || len(d.Tables) == 0 {
		t.Fatalf("defs %d, affixes %d, tables %d", len(d.Defs), len(d.Affixes), len(d.Tables))
	}
}

func TestValidateCatchesBadData(t *testing.T) {
	good := func() *Data {
		return &Data{
			Defs: map[string]Def{
				"sword": {ID: "sword", Slot: Weapon, WeaponType: Sword, Stack: 1, Base: map[Stat]Range{Damage: {1, 2}}},
				"ore":   {ID: "ore", Slot: Material, Stack: 10},
			},
			Affixes: []AffixDef{{ID: "sharp", Type: "prefix", Group: "damage", Stat: Damage, Slots: []Slot{Weapon}, Weight: 1, Tiers: []Tier{{1, Range{1, 2}}}}},
			Tables: map[string]Table{
				"a": {ID: "a", Rolls: Range{1, 1}, Entries: []Entry{{Weight: 1, Item: "sword"}, {Weight: 1, Table: "b"}}},
				"b": {ID: "b", Rolls: Range{1, 1}, Entries: []Entry{{Weight: 1, Item: "ore", Quantity: Range{1, 5}}}},
			},
		}
	}
	if err := good().validate(); err != nil {
		t.Fatalf("good data failed: %v", err)
	}

	tests := []struct {
		name   string
		break_ func(d *Data)
		want   string
	}{
		{"unknown slot", func(d *Data) { d.Defs["sword"] = Def{ID: "sword", Slot: "neck", Stack: 1} }, "unknown slot"},
		{"weapon without type", func(d *Data) { d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, Stack: 1} }, "weapon type"},
		{"unknown weapon type", func(d *Data) { d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, WeaponType: "whip", Stack: 1} }, "weapon type"},
		{"weapon type on armor", func(d *Data) { d.Defs["ore"] = Def{ID: "ore", Slot: Material, WeaponType: Sword, Stack: 10} }, "weapon type"},
		{"unknown stat", func(d *Data) {
			d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, WeaponType: Sword, Stack: 1, Base: map[Stat]Range{"luck": {1, 2}}}
		}, "unknown stat"},
		{"backwards range", func(d *Data) {
			d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, WeaponType: Sword, Stack: 1, Base: map[Stat]Range{Damage: {5, 2}}}
		}, "range"},
		{"stacking gear", func(d *Data) { d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, WeaponType: Sword, Stack: 3} }, "stack"},
		{"unique without fixed", func(d *Data) {
			d.Defs["sword"] = Def{ID: "sword", Slot: Weapon, WeaponType: Sword, Stack: 1, Unique: true}
		}, "uniques"},
		{"duplicate affix", func(d *Data) { d.Affixes = append(d.Affixes, d.Affixes[0]) }, "twice"},
		{"affix on consumable", func(d *Data) { d.Affixes[0].Slots = []Slot{Consumable} }, "not gear"},
		{"tiers go down", func(d *Data) { d.Affixes[0].Tiers = []Tier{{5, Range{1, 2}}, {1, Range{3, 4}}} }, "tier levels"},
		{"unknown item", func(d *Data) { d.Tables["a"].Entries[0].Item = "axe" }, "unknown item"},
		{"unknown table", func(d *Data) { d.Tables["a"].Entries[1].Table = "c" }, "unknown table"},
		{"zero weight", func(d *Data) { d.Tables["a"].Entries[0].Weight = 0 }, "exactly one"},
		{"two kinds", func(d *Data) { d.Tables["a"].Entries[0].Nothing = true }, "exactly one"},
		{"quantity on gear", func(d *Data) { d.Tables["a"].Entries[0].Quantity = Range{1, 2} }, "quantity"},
		{"quantity over stack", func(d *Data) { d.Tables["b"].Entries[0].Quantity = Range{1, 50} }, "quantity"},
		{"loop", func(d *Data) {
			b := d.Tables["b"]
			b.Entries = append(b.Entries, Entry{Weight: 1, Table: "a"})
			d.Tables["b"] = b
		}, "loops"},
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
