// Package storetest is the contract every inventory.Store must meet. Each
// store's tests call Run with a constructor for an empty store.
package storetest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func Run(t *testing.T, newStore func() inventory.Store) {
	tests := []struct {
		name string
		run  func(*testing.T, inventory.Store)
	}{
		{"LoadUnknown", loadUnknown},
		{"CreateThenLoad", createThenLoad},
		{"CreateTwiceKeepsTheFirst", createTwice},
		{"ClassesShareTheStash", classesShareTheStash},
		{"SaveLobby", saveLobby},
		{"SaveLobbyKeepsAnEmptyLoadout", saveLobbyEmpty},
		{"EndRun", endRun},
		{"EndRunSameIDIsNoOp", endRunSameID},
		{"WritesNeedACharacter", writesNeedACharacter},
		{"StashLimit", stashLimit},
		{"NamesAreApart", namesAreApart},
		{"ParallelPlayers", parallelPlayers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, newStore()) })
	}
}

var ctx = context.Background()

func sword(id string) items.Instance {
	return items.Instance{ID: id, DefID: "iron_sword", Name: "Iron Sword", Slot: items.Weapon, WeaponType: "sword", Rarity: items.Uncommon, Level: 2,
		Base: map[items.Stat]int{"damage": 15}, Affixes: []items.Affix{{ID: "sharp", Name: "Sharp", Stat: "damage", Tier: 1, Value: 2}}, Quantity: 1, Stack: 1}
}

func potions(id string, n int) items.Instance {
	return items.Instance{ID: id, DefID: "health_potion", Name: "Health Potion", Slot: items.Consumable, Rarity: items.Common, Level: 1, Quantity: n, Stack: 5}
}

func character(name string) inventory.Character {
	return inventory.Character{
		Name:    name,
		Class:   "warrior",
		XP:      10,
		Loadout: inventory.Loadout{Equipment: []items.Instance{sword(name + "-sword")}, Items: []items.Instance{potions(name+"-potions", 2)}},
		Stash:   []items.Instance{potions(name+"-stashed", 4)},
	}
}

// mustLoad also checks that no item is in two places, which the server
// relies on to never duplicate an item.
func mustLoad(t *testing.T, s inventory.Store, name, class string) inventory.Character {
	t.Helper()
	c, ok, err := s.Load(ctx, name, class)
	if err != nil || !ok {
		t.Fatalf("load %s %s: %v, %v", name, class, ok, err)
	}
	seen := map[string]bool{}
	for _, list := range [][]items.Instance{c.Loadout.Equipment, c.Loadout.Items, c.Stash} {
		for _, inst := range list {
			if seen[inst.ID] {
				t.Fatalf("item %s is saved twice: %+v", inst.ID, c)
			}
			seen[inst.ID] = true
		}
	}
	return c
}

func mustCreate(t *testing.T, s inventory.Store, c inventory.Character) {
	t.Helper()
	if err := s.Create(ctx, c); err != nil {
		t.Fatalf("create %s: %v", c.Name, err)
	}
}

func idsOf(insts []items.Instance) string {
	var out []string
	for _, inst := range insts {
		out = append(out, fmt.Sprintf("%s:%d", inst.ID, inst.Quantity))
	}
	return fmt.Sprint(out)
}

func sameItems(t *testing.T, what string, got, want []items.Instance) {
	t.Helper()
	if idsOf(got) != idsOf(want) {
		t.Fatalf("%s = %s, want %s", what, idsOf(got), idsOf(want))
	}
}

func loadUnknown(t *testing.T, s inventory.Store) {
	c, ok, err := s.Load(ctx, "ann", "warrior")
	if err != nil || ok || c.XP != 0 || !c.Loadout.Empty() || len(c.Stash) != 0 {
		t.Fatalf("load of a new name = %+v, %v, %v", c, ok, err)
	}
}

func createThenLoad(t *testing.T, s inventory.Store) {
	want := character("ann")
	mustCreate(t, s, want)
	// The store keeps its own copy.
	want.Loadout.Items[0].Quantity = 99
	want.Stash[0].Quantity = 99

	got := mustLoad(t, s, "ann", "warrior")
	if got.Name != "ann" || got.Class != "warrior" || got.XP != 10 {
		t.Fatalf("loaded %+v", got)
	}
	sameItems(t, "equipment", got.Loadout.Equipment, []items.Instance{sword("ann-sword")})
	sameItems(t, "bag", got.Loadout.Items, []items.Instance{potions("ann-potions", 2)})
	sameItems(t, "stash", got.Stash, []items.Instance{potions("ann-stashed", 4)})

	inst := got.Loadout.Equipment[0]
	if want := sword("ann-sword"); inst.Name != want.Name || inst.Slot != want.Slot || inst.WeaponType != want.WeaponType || inst.Rarity != want.Rarity ||
		inst.Level != want.Level || inst.Base["damage"] != 15 || len(inst.Affixes) != 1 || inst.Affixes[0] != want.Affixes[0] || inst.Stack != 1 {
		t.Fatalf("sword = %+v, want every field kept", inst)
	}
}

func createTwice(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	again := character("ann")
	again.XP = 500
	again.Loadout = inventory.Loadout{Equipment: []items.Instance{sword("another")}}
	again.Stash = nil
	mustCreate(t, s, again)

	got := mustLoad(t, s, "ann", "warrior")
	if got.XP != 10 {
		t.Fatalf("xp = %d, want the first create's", got.XP)
	}
	sameItems(t, "equipment", got.Loadout.Equipment, []items.Instance{sword("ann-sword")})
	sameItems(t, "stash", got.Stash, []items.Instance{potions("ann-stashed", 4)})
}

func classesShareTheStash(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	if c, ok, err := s.Load(ctx, "ann", "rogue"); err != nil || ok || len(c.Stash) != 1 {
		t.Fatalf("load of a new class = %+v, %v, %v, want the account's stash", c, ok, err)
	}
	rogue := inventory.Character{Name: "ann", Class: "rogue", XP: 3, Stash: []items.Instance{sword("ignored")}}
	mustCreate(t, s, rogue)

	got := mustLoad(t, s, "ann", "rogue")
	if got.XP != 3 || !got.Loadout.Empty() {
		t.Fatalf("rogue = %+v", got)
	}
	sameItems(t, "rogue stash", got.Stash, []items.Instance{potions("ann-stashed", 4)})

	if err := s.SaveLobby(ctx, "ann", "rogue", inventory.Loadout{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); len(got.Stash) != 0 || got.XP != 10 {
		t.Fatalf("warrior after the rogue emptied the stash = %+v", got)
	}
}

func saveLobby(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	l := inventory.Loadout{Items: []items.Instance{sword("ann-sword")}}
	stash := []items.Instance{potions("ann-stashed", 4), potions("ann-potions", 2)}
	if err := s.SaveLobby(ctx, "ann", "warrior", l, stash); err != nil {
		t.Fatal(err)
	}
	l.Items[0].Quantity = 99
	stash[0].Quantity = 99

	got := mustLoad(t, s, "ann", "warrior")
	if got.XP != 10 || len(got.Loadout.Equipment) != 0 {
		t.Fatalf("loaded %+v, want xp kept and no gear", got)
	}
	sameItems(t, "bag", got.Loadout.Items, []items.Instance{sword("ann-sword")})
	sameItems(t, "stash", got.Stash, []items.Instance{potions("ann-stashed", 4), potions("ann-potions", 2)})
}

func saveLobbyEmpty(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	all := []items.Instance{potions("ann-stashed", 4), sword("ann-sword"), potions("ann-potions", 2)}
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, all); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); !got.Loadout.Empty() || len(got.Stash) != 3 {
		t.Fatalf("loaded %+v", got)
	}
}

func endRun(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	after := inventory.Character{
		Name:    "ann",
		Class:   "warrior",
		XP:      131,
		Loadout: inventory.Loadout{Equipment: []items.Instance{sword("ann-sword")}, Items: []items.Instance{potions("overflow", 1)}},
		Stash:   []items.Instance{potions("ann-stashed", 5), potions("ann-potions", 1)},
	}
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000001", after); err != nil {
		t.Fatal(err)
	}
	got := mustLoad(t, s, "ann", "warrior")
	if got.XP != 131 {
		t.Fatalf("xp = %d", got.XP)
	}
	sameItems(t, "equipment", got.Loadout.Equipment, after.Loadout.Equipment)
	sameItems(t, "bag", got.Loadout.Items, after.Loadout.Items)
	sameItems(t, "stash", got.Stash, after.Stash)
}

func endRunSameID(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	const run = "6f1c2b3a-0000-4000-8000-000000000002"
	first := character("ann")
	first.XP = 50
	if err := s.EndRun(ctx, run, first); err != nil {
		t.Fatal(err)
	}
	// A lobby move after the run, then a late retry of the same run.
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, []items.Instance{sword("ann-sword")}); err != nil {
		t.Fatal(err)
	}
	retry := first
	retry.XP = 999
	if err := s.EndRun(ctx, run, retry); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got := mustLoad(t, s, "ann", "warrior")
	if got.XP != 50 || !got.Loadout.Empty() {
		t.Fatalf("after a retry = %+v, want the first run and the lobby move only", got)
	}
	sameItems(t, "stash", got.Stash, []items.Instance{sword("ann-sword")})

	next := got
	next.XP = 60
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000003", next); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.XP != 60 {
		t.Fatalf("xp after the next run = %d", got.XP)
	}
}

func writesNeedACharacter(t *testing.T, s inventory.Store) {
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, nil); err == nil {
		t.Fatal("saved a lobby for a character that does not exist")
	}
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000004", character("ann")); err == nil {
		t.Fatal("ended a run for a character that does not exist")
	}
	if _, ok, _ := s.Load(ctx, "ann", "warrior"); ok {
		t.Fatal("a failed write made a character")
	}
	mustCreate(t, s, character("ann"))
	if err := s.EndRun(ctx, "", character("ann")); err == nil {
		t.Fatal("ended a run with no run id")
	}
}

func stashLimit(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	var full []items.Instance
	for i := range inventory.StashSize {
		full = append(full, sword(fmt.Sprint("s", i)))
	}
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, full); err != nil {
		t.Fatalf("a full stash: %v", err)
	}
	over := append(full, sword("one more"))
	if err := s.SaveLobby(ctx, "ann", "warrior", inventory.Loadout{}, over); err == nil {
		t.Fatal("saved a stash over the limit")
	}
	c := character("ann")
	c.Stash = over
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000005", c); err == nil {
		t.Fatal("ended a run with a stash over the limit")
	}
	if got := mustLoad(t, s, "ann", "warrior"); len(got.Stash) != inventory.StashSize || got.XP != 10 {
		t.Fatalf("stash has %d, xp %d, want the full save kept", len(got.Stash), got.XP)
	}
}

func namesAreApart(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	mustCreate(t, s, character("bob"))
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000006", inventory.Character{Name: "ann", Class: "warrior", XP: 77}); err != nil {
		t.Fatal(err)
	}
	// Bob can end a run with the same id, since run ids are only kept per character.
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000006", inventory.Character{Name: "bob", Class: "warrior", XP: 88}); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "bob", "warrior"); got.XP != 88 || !got.Loadout.Empty() {
		t.Fatalf("bob = %+v", got)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.XP != 77 {
		t.Fatalf("ann = %+v", got)
	}
}

func parallelPlayers(t *testing.T, s inventory.Store) {
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			c := character(fmt.Sprint("p", i))
			if err := s.Create(ctx, c); err != nil {
				t.Error(err)
				return
			}
			for run := range 5 {
				c.XP++
				if err := s.EndRun(ctx, fmt.Sprintf("6f1c2b3a-0000-4000-8000-%012d", run), c); err != nil {
					t.Error(err)
				}
				if err := s.SaveLobby(ctx, c.Name, c.Class, c.Loadout, c.Stash); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	for i := range 8 {
		if got := mustLoad(t, s, fmt.Sprint("p", i), "warrior"); got.XP != 15 {
			t.Fatalf("p%d xp = %d, want 15", i, got.XP)
		}
	}
}
