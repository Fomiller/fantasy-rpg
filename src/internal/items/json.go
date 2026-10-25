package items

import (
	"encoding/json"
	"fmt"
)

// item is the saved shape of an Instance, in Postgres and on the inventory
// service's API. It is kept apart so that renaming a Go field never changes
// data already saved.
type item struct {
	ID      string         `json:"id"`
	Def     string         `json:"def"`
	Name    string         `json:"name"`
	Slot    string         `json:"slot"`
	Weapon  string         `json:"weapon,omitempty"`
	Rarity  string         `json:"rarity"`
	Level   int            `json:"level"`
	Base    map[string]int `json:"base,omitempty"`
	Affixes []affix        `json:"affixes,omitempty"`
	Qty     int            `json:"qty"`
	Stack   int            `json:"stack"`
}

type affix struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Stat  string `json:"stat"`
	Tier  int    `json:"tier"`
	Value int    `json:"value"`
}

// EncodeJSON writes insts in the saved shape. nil is an empty array.
func EncodeJSON(insts []Instance) ([]byte, error) {
	out := make([]item, 0, len(insts))
	for _, inst := range insts {
		it := item{
			ID: inst.ID, Def: inst.DefID, Name: inst.Name, Slot: string(inst.Slot), Weapon: string(inst.WeaponType),
			Rarity: inst.Rarity.String(), Level: inst.Level, Qty: inst.Quantity, Stack: inst.Stack,
		}
		if len(inst.Base) > 0 {
			it.Base = map[string]int{}
			for stat, v := range inst.Base {
				it.Base[string(stat)] = v
			}
		}
		for _, a := range inst.Affixes {
			it.Affixes = append(it.Affixes, affix{ID: a.ID, Name: a.Name, Stat: string(a.Stat), Tier: a.Tier, Value: a.Value})
		}
		out = append(out, it)
	}
	return json.Marshal(out)
}

func DecodeJSON(b []byte) ([]Instance, error) {
	var saved []item
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	var out []Instance
	for _, it := range saved {
		rarity, err := ParseRarity(it.Rarity)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", it.ID, err)
		}
		inst := Instance{
			ID: it.ID, DefID: it.Def, Name: it.Name, Slot: Slot(it.Slot), WeaponType: WeaponType(it.Weapon),
			Rarity: rarity, Level: it.Level, Base: map[Stat]int{}, Quantity: it.Qty, Stack: it.Stack,
		}
		for stat, v := range it.Base {
			inst.Base[Stat(stat)] = v
		}
		for _, a := range it.Affixes {
			inst.Affixes = append(inst.Affixes, Affix{ID: a.ID, Name: a.Name, Stat: Stat(a.Stat), Tier: a.Tier, Value: a.Value})
		}
		out = append(out, inst)
	}
	return out, nil
}
