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

// lootFor returns the player's loot window event from one step.
func lootFor(ev Events, player EntityID) (LootEvent, bool) {
	for _, l := range ev.Loot {
		if l.Player == player {
			return l, true
		}
	}
	return LootEvent{}, false
}

func TestOpenLootOutOfRangeCloses(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{X: 60}, []items.Instance{item("a", items.Head, nil)})

	w.Submit(OpenLootCommand{Player: id, Target: pile})
	l, ok := lootFor(w.Step(dt), id)
	if !ok || !l.Closed || len(l.Items) != 0 {
		t.Fatalf("loot = %+v, %v, want a closed window with nothing in it", l, ok)
	}
	if _, ok := lootFor(w.Step(dt), id); ok {
		t.Fatal("closed window still gets updates")
	}
}

func TestTakeOneItem(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil), potion("p", 3)})

	w.Submit(OpenLootCommand{Player: id, Target: pile})
	if l, _ := lootFor(w.Step(dt), id); !slices.Equal(ids(l.Items), []string{"a", "p"}) {
		t.Fatalf("window = %+v", l)
	}
	if _, ok := lootFor(w.Step(dt), id); ok {
		t.Fatal("window got contents that did not change")
	}

	w.Submit(PickupCommand{Player: id, Target: pile, Item: "p"})
	ev := w.Step(dt)
	if !slices.Contains(ev.Inventory, id) {
		t.Fatal("take did not change the bag")
	}
	if p, _ := w.Player(id); !slices.Equal(ids(p.Inventory.Items), []string{"p"}) || p.Inventory.Items[0].Quantity != 3 {
		t.Fatalf("bag = %+v", p.Inventory.Items)
	}
	if l, _ := lootFor(ev, id); !slices.Equal(ids(l.Items), []string{"a"}) || l.Closed {
		t.Fatalf("window = %+v, want only a left", l)
	}
}

func TestTakeAllEmptiesPileAndClosesWindow(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil), item("b", items.Feet, nil)})
	w.Submit(OpenLootCommand{Player: id, Target: pile})
	w.Step(dt)

	w.Submit(PickupCommand{Player: id, Target: pile})
	ev := w.Step(dt)
	if p, _ := w.Player(id); !slices.Equal(ids(p.Inventory.Items), []string{"a", "b"}) {
		t.Fatalf("bag = %v", ids(p.Inventory.Items))
	}
	if l, ok := lootFor(ev, id); !ok || !l.Closed {
		t.Fatalf("window = %+v, %v, want it closed once the pile is gone", l, ok)
	}
}

func TestTakeIntoFullBagLeavesItem(t *testing.T) {
	cfg := passive()
	cfg.InventorySize = 1
	w := NewWorld(cfg, nil)
	id := w.AddPlayer(Vec2{})
	w.Restore(id, inventory.Loadout{Items: []items.Instance{item("mine", items.Hands, nil)}})
	pile := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil)})
	w.Submit(OpenLootCommand{Player: id, Target: pile})
	w.Step(dt)

	for _, take := range []string{"a", ""} {
		w.Submit(PickupCommand{Player: id, Target: pile, Item: take})
		ev := w.Step(dt)
		if slices.Contains(ev.Inventory, id) {
			t.Fatalf("take %q into a full bag changed it", take)
		}
		if len(ev.PickupRejected) != 1 || ev.PickupRejected[0] != (PickupRejection{Player: id, Pile: pile, Item: take, Reason: PickupBagFull}) {
			t.Fatalf("rejected = %+v", ev.PickupRejected)
		}
	}
	if left, _ := w.Pile(pile); !slices.Equal(ids(left.Items), []string{"a"}) {
		t.Fatalf("pile = %v", ids(left.Items))
	}
}

func TestTakeNeedsRange(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{X: 60}, []items.Instance{item("a", items.Head, nil)})

	w.Submit(PickupCommand{Player: id, Target: pile, Item: "a"})
	if ev := w.Step(dt); len(ev.PickupRejected) != 1 || ev.PickupRejected[0].Reason != PickupRange {
		t.Fatalf("rejected = %+v", ev.PickupRejected)
	}
}

func TestTwoPlayersRaceForOneItem(t *testing.T) {
	w := NewWorld(passive(), nil)
	ann := w.AddPlayer(Vec2{})
	bob := w.AddPlayer(Vec2{X: 20})
	pile := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil), item("b", items.Feet, nil)})
	w.Submit(OpenLootCommand{Player: ann, Target: pile})
	w.Submit(OpenLootCommand{Player: bob, Target: pile})
	w.Step(dt)

	w.Submit(PickupCommand{Player: ann, Target: pile, Item: "a"})
	w.Submit(PickupCommand{Player: bob, Target: pile, Item: "a"})
	ev := w.Step(dt)

	if !slices.Equal(ev.Inventory, []EntityID{ann}) {
		t.Fatalf("changed = %v, want only ann", ev.Inventory)
	}
	if len(ev.PickupRejected) != 1 || ev.PickupRejected[0] != (PickupRejection{Player: bob, Pile: pile, Item: "a", Reason: PickupGone}) {
		t.Fatalf("rejected = %+v", ev.PickupRejected)
	}
	if p, _ := w.Player(bob); len(p.Inventory.Items) != 0 {
		t.Fatalf("bob's bag = %v", ids(p.Inventory.Items))
	}
	for _, id := range []EntityID{ann, bob} {
		if l, _ := lootFor(ev, id); !slices.Equal(ids(l.Items), []string{"b"}) {
			t.Fatalf("player %d window = %+v, want only b", id, l)
		}
	}
}

func TestLootWindowClosesWhenPlayerWalksAway(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	chest := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil)})
	w.Submit(OpenLootCommand{Player: id, Target: chest})
	w.Step(dt)

	w.players[id].Pos = Vec2{X: -60}
	if l, ok := lootFor(w.Step(dt), id); !ok || !l.Closed {
		t.Fatalf("window = %+v, %v, want it closed", l, ok)
	}
}

func TestCloseLootStopsUpdates(t *testing.T) {
	w := NewWorld(passive(), nil)
	ann := w.AddPlayer(Vec2{})
	bob := w.AddPlayer(Vec2{})
	pile := w.AddPile(Vec2{X: 10}, []items.Instance{item("a", items.Head, nil), item("b", items.Feet, nil)})
	w.Submit(OpenLootCommand{Player: ann, Target: pile})
	w.Step(dt)

	w.Submit(CloseLootCommand{Player: ann, Target: pile})
	w.Submit(PickupCommand{Player: bob, Target: pile, Item: "a"})
	if _, ok := lootFor(w.Step(dt), ann); ok {
		t.Fatal("closed window got an update")
	}
}
