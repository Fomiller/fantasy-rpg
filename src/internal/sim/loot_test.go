package sim

import (
	"slices"
	"testing"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func item(id string, slot items.Slot, stats map[items.Stat]int) items.Instance {
	inst := gear(slot, stats)
	inst.ID, inst.DefID, inst.Stack = id, id, 1
	return inst
}

func potion(id string, n int) items.Instance {
	inst := item(id, items.Consumable, map[items.Stat]int{items.Heal: 30})
	inst.DefID, inst.Quantity, inst.Stack = "health_potion", n, 5
	return inst
}

// do submits one item command and reports whether the step said the
// player's items changed.
func do(w *World, cmd ItemCommand) bool {
	w.Submit(cmd)
	return slices.Contains(w.Step(dt).Inventory, cmd.player())
}

func ids(list []items.Instance) []string {
	var out []string
	for _, inst := range list {
		out = append(out, inst.ID)
	}
	return out
}

func TestPickupNeedsRange(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	far := w.AddPile(Vec2{X: 60}, []items.Instance{item("far", items.Head, nil)})
	near := w.AddPile(Vec2{X: 40}, []items.Instance{item("near", items.Head, nil)})

	if do(w, PickupCommand{Player: id, Target: far}) {
		t.Fatal("picked up from out of range")
	}
	if !do(w, PickupCommand{Player: id, Target: near}) {
		t.Fatal("pickup in range failed")
	}
	p, _ := w.Player(id)
	if got := ids(p.Inventory.Items); !slices.Equal(got, []string{"near"}) {
		t.Fatalf("bag = %v", got)
	}
	if _, ok := w.Pile(near); ok {
		t.Fatal("empty loot pile is still on the ground")
	}
}

func TestPickupLeavesWhatDoesNotFit(t *testing.T) {
	cfg := passive()
	cfg.InventorySize = 1
	w := NewWorld(cfg, nil)
	id := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{}, []items.Instance{item("a", items.Head, nil), item("b", items.Feet, nil)})

	do(w, PickupCommand{Player: id, Target: pile})
	if left, _ := w.Pile(pile); !slices.Equal(ids(left.Items), []string{"b"}) {
		t.Fatalf("pile = %v", ids(left.Items))
	}
	if do(w, PickupCommand{Player: id, Target: pile}) {
		t.Fatal("full bag reported a change")
	}
}

func TestEmptyChestStays(t *testing.T) {
	w := NewWorld(passive(), nil)
	w.SetLoot(items.NewRoller(items.Default(), 1))
	id := w.AddPlayer(Vec2{})
	chest := w.AddChest(Vec2{X: 10})
	if c, _ := w.Pile(chest); len(c.Items) < 2 {
		t.Fatalf("chest = %+v, want it filled from cave1_chest", c)
	}

	do(w, PickupCommand{Player: id, Target: chest})
	c, ok := w.Pile(chest)
	if !ok || !c.Chest || len(c.Items) != 0 {
		t.Fatalf("chest = %+v, %v, want an empty chest left behind", c, ok)
	}
	if do(w, PickupCommand{Player: id, Target: chest}) {
		t.Fatal("looted an empty chest")
	}
}

func TestEquipFromBagSwaps(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	equip(t, w, id, item("old", items.Weapon, map[items.Stat]int{items.Damage: 5}))
	w.Restore(id, inventory.Loadout{Items: []items.Instance{item("new", items.Weapon, map[items.Stat]int{items.Damage: 20})}})

	if !do(w, EquipCommand{Player: id, Item: "new"}) {
		t.Fatal("equip failed")
	}
	p, _ := w.Player(id)
	if p.Equipment[items.Weapon].ID != "new" || !slices.Equal(ids(p.Inventory.Items), []string{"old"}) {
		t.Fatalf("weapon = %v, bag = %v", p.Equipment[items.Weapon].ID, ids(p.Inventory.Items))
	}
	if w.attackDamage(w.players[id]) != 20 {
		t.Fatalf("damage = %d, want the new weapon's", w.attackDamage(w.players[id]))
	}
}

func TestEquipRejectsWhatIsNotGear(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 1)}})
	if do(w, EquipCommand{Player: id, Item: "p"}) || do(w, EquipCommand{Player: id, Item: "missing"}) {
		t.Fatal("equipped a potion or a missing item")
	}
}

func TestUnequipNeedsRoom(t *testing.T) {
	cfg := passive()
	cfg.InventorySize = 1
	w := NewWorld(cfg, nil)
	id := w.AddPlayer(Vec2{})
	equip(t, w, id, item("vest", items.Chest, map[items.Stat]int{items.MaxHealth: 20}))
	equip(t, w, id, item("cap", items.Head, nil))

	if !do(w, UnequipCommand{Player: id, Slot: items.Chest}) {
		t.Fatal("unequip failed")
	}
	if p, _ := w.Player(id); p.MaxHealth != 120 || len(p.Equipment) != 1 {
		t.Fatalf("max health %d, equipment %v", p.MaxHealth, p.Equipment)
	}
	if do(w, UnequipCommand{Player: id, Slot: items.Head}) {
		t.Fatal("unequipped into a full bag")
	}
}

func TestDropMakesAPile(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{X: 5, Y: 6})
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 3)}})

	if !do(w, DropCommand{Player: id, Item: "p"}) {
		t.Fatal("drop failed")
	}
	piles := w.Piles()
	if len(piles) != 1 || piles[0].Pos != (Vec2{X: 5, Y: 6}) || piles[0].Items[0].Quantity != 3 {
		t.Fatalf("piles = %+v", piles)
	}
	if p, _ := w.Player(id); len(p.Inventory.Items) != 0 {
		t.Fatalf("bag = %v", ids(p.Inventory.Items))
	}
}

func TestUsePotionHealsUpToMax(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 2)}})
	w.players[id].Health = 70

	do(w, UseCommand{Player: id, Item: "p"})
	p, _ := w.Player(id)
	if p.Health != 100 || p.Inventory.Items[0].Quantity != 1 {
		t.Fatalf("health %d, potions %d", p.Health, p.Inventory.Items[0].Quantity)
	}
	do(w, UseCommand{Player: id, Item: "p"})
	if p, _ = w.Player(id); p.Health != 120 || len(p.Inventory.Items) != 0 {
		t.Fatalf("health %d, bag %v", p.Health, ids(p.Inventory.Items))
	}
}

func TestDeadPlayersCannotUseItems(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 1)}})
	w.players[id].Health = 0
	if do(w, UseCommand{Player: id, Item: "p"}) || do(w, DropCommand{Player: id, Item: "p"}) {
		t.Fatal("dead player used an item")
	}
}

func TestDeathDropsEverythingAsOneBag(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{X: 20})
	w.AddEnemy(Vec2{}, Melee)
	w.Restore(id, inventory.Loadout{
		Equipment: []items.Instance{item("sword", items.Weapon, map[items.Stat]int{items.Damage: 10})},
		Items:     []items.Instance{potion("p", 2)},
	})
	w.players[id].Health = 1

	var ev Events
	for range 40 {
		if ev = w.Step(dt); len(ev.Damage) > 0 {
			break
		}
	}
	if len(ev.Damage) != 1 || !ev.Damage[0].Killed || !slices.Contains(ev.Inventory, id) {
		t.Fatalf("events = %+v", ev)
	}
	piles := w.Piles()
	if len(piles) != 1 || !slices.Equal(ids(piles[0].Items), []string{"sword", "p"}) {
		t.Fatalf("piles = %+v", piles)
	}
	if p, _ := w.Player(id); !p.Loadout().Empty() || p.Health != 0 {
		t.Fatalf("dead player kept %+v at %d hp", p.Loadout(), p.Health)
	}
}

func TestKilledEnemyDropsLoot(t *testing.T) {
	cfg := passive()
	melee := cfg.Archetypes[Melee]
	melee.Loot = "cave1_chest"
	cfg.Archetypes[Melee] = melee
	w := NewWorld(cfg, nil)
	w.SetLoot(items.NewRoller(items.Default(), 1))
	player := w.AddPlayer(Vec2{})
	enemy := w.AddEnemy(Vec2{X: 40}, Melee)
	w.enemies[enemy].Health = 1

	swing(w, AttackCommand{Player: player, Seq: 1, Target: enemy})
	piles := w.Piles()
	if len(piles) != 1 || piles[0].Chest || piles[0].Pos != (Vec2{X: 40}) || len(piles[0].Items) == 0 {
		t.Fatalf("piles = %+v", piles)
	}
}

func TestLoadoutRoundTrip(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	l := inventory.Loadout{
		Equipment: []items.Instance{item("sword", items.Weapon, nil), item("vest", items.Chest, nil)},
		Items:     []items.Instance{potion("p", 2)},
	}
	if err := w.Restore(id, l); err != nil {
		t.Fatal(err)
	}
	p, _ := w.Player(id)
	got := p.Loadout()
	if !slices.Equal(ids(got.Equipment), []string{"sword", "vest"}) || !slices.Equal(ids(got.Items), []string{"p"}) {
		t.Fatalf("loadout = %+v", got)
	}
}
