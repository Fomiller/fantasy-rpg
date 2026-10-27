// Package storetest is the contract every inventory.Store must meet. Each
// store's tests call Run with a constructor for an empty store, which gets
// the subtest it is for.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func Run(t *testing.T, newStore func(*testing.T) inventory.Store) {
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
		{"StartRunTakesTheLease", startRunTakesTheLease},
		{"OneRunPerAccount", oneRunPerAccount},
		{"NoLobbySaveInARun", noLobbySaveInARun},
		{"EndRunNeedsTheLease", endRunNeedsTheLease},
		{"RunIDIsUsedOnce", runIDIsUsedOnce},
		{"ClearRun", clearRun},
		{"OneClaimWinsARace", oneClaimWinsARace},
		{"ClaimTakenOverAfterExpiry", claimTakenOverAfterExpiry},
		{"NewBootEndsOldClaims", newBootEndsOldClaims},
		{"StaleSessionCannotSave", staleSessionCannotSave},
		{"ReleaseThenReclaim", releaseThenReclaim},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, newStore(t)) })
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

const instance = "instance-0@3f9a1c"

// mustStart takes the lease, as an instance server does on entry.
func mustStart(t *testing.T, s inventory.Store, name, runID string) inventory.Character {
	t.Helper()
	c, err := s.StartRun(ctx, name, "warrior", runID, instance)
	if err != nil {
		t.Fatalf("start run %s for %s: %v", runID, name, err)
	}
	return c
}

func noLease(t *testing.T, s inventory.Store, name string) {
	t.Helper()
	if c := mustLoad(t, s, name, "warrior"); c.Run.Held() {
		t.Fatalf("%s still has a lease: %+v", name, c.Run)
	}
}

// mustClaim claims name for a session on a live world-0 and returns the
// session id that SaveLobby takes.
func mustClaim(t *testing.T, s inventory.Store, name string) string {
	t.Helper()
	if err := s.Heartbeat(ctx, "world-0", "b0", time.Minute); err != nil {
		t.Fatal(err)
	}
	sess := inventory.Session{Name: name, ID: name + "-session", Shard: "world-0", Boot: "b0"}
	if err := s.Claim(ctx, sess); err != nil {
		t.Fatalf("claim %s: %v", name, err)
	}
	return sess.ID
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

	if err := s.SaveLobby(ctx, "ann", "rogue", mustClaim(t, s, "ann"), inventory.Loadout{}, nil); err != nil {
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
	if err := s.SaveLobby(ctx, "ann", "warrior", mustClaim(t, s, "ann"), l, stash); err != nil {
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
	if err := s.SaveLobby(ctx, "ann", "warrior", mustClaim(t, s, "ann"), inventory.Loadout{}, all); err != nil {
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
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000001")
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
	mustStart(t, s, "ann", run)
	if err := s.EndRun(ctx, run, first); err != nil {
		t.Fatal(err)
	}
	// A lobby move after the run, then a late retry of the same run.
	if err := s.SaveLobby(ctx, "ann", "warrior", mustClaim(t, s, "ann"), inventory.Loadout{}, []items.Instance{sword("ann-sword")}); err != nil {
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
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000003")
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000003", next); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.XP != 60 {
		t.Fatalf("xp after the next run = %d", got.XP)
	}
}

func writesNeedACharacter(t *testing.T, s inventory.Store) {
	if err := s.SaveLobby(ctx, "ann", "warrior", mustClaim(t, s, "ann"), inventory.Loadout{}, nil); err == nil {
		t.Fatal("saved a lobby for a character that does not exist")
	}
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000004", character("ann")); err == nil {
		t.Fatal("ended a run for a character that does not exist")
	}
	if _, err := s.StartRun(ctx, "ann", "warrior", "6f1c2b3a-0000-4000-8000-000000000004", instance); err == nil {
		t.Fatal("started a run for a character that does not exist")
	}
	if _, ok, _ := s.Load(ctx, "ann", "warrior"); ok {
		t.Fatal("a failed write made a character")
	}
	mustCreate(t, s, character("ann"))
	if err := s.EndRun(ctx, "", character("ann")); err == nil {
		t.Fatal("ended a run with no run id")
	}
	if _, err := s.StartRun(ctx, "ann", "warrior", "", instance); err == nil {
		t.Fatal("started a run with no run id")
	}
}

func stashLimit(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	sess := mustClaim(t, s, "ann")
	var full []items.Instance
	for i := range inventory.StashSize {
		full = append(full, sword(fmt.Sprint("s", i)))
	}
	if err := s.SaveLobby(ctx, "ann", "warrior", sess, inventory.Loadout{}, full); err != nil {
		t.Fatalf("a full stash: %v", err)
	}
	over := append(full, sword("one more"))
	if err := s.SaveLobby(ctx, "ann", "warrior", sess, inventory.Loadout{}, over); err == nil {
		t.Fatal("saved a stash over the limit")
	}
	c := character("ann")
	c.Stash = over
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000005")
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
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000006")
	mustStart(t, s, "bob", "6f1c2b3a-0000-4000-8000-000000000006")
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
			sess := inventory.Session{Name: c.Name, ID: c.Name + "-session", Shard: "world-0", Boot: "b0"}
			if err := s.Claim(ctx, sess); err != nil {
				t.Error(err)
				return
			}
			for run := range 5 {
				c.XP++
				id := fmt.Sprintf("6f1c2b3a-0000-4000-8000-%012d", run)
				if _, err := s.StartRun(ctx, c.Name, c.Class, id, instance); err != nil {
					t.Error(err)
				}
				if err := s.EndRun(ctx, id, c); err != nil {
					t.Error(err)
				}
				if err := s.SaveLobby(ctx, c.Name, c.Class, sess.ID, c.Loadout, c.Stash); err != nil {
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

func startRunTakesTheLease(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	const run = "6f1c2b3a-0000-4000-8000-000000000010"
	got := mustStart(t, s, "ann", run)
	if got.XP != 10 || got.Run.RunID != run || got.Run.Instance != instance || got.Run.Since.IsZero() {
		t.Fatalf("start run = %+v", got)
	}
	sameItems(t, "equipment", got.Loadout.Equipment, []items.Instance{sword("ann-sword")})
	sameItems(t, "bag", got.Loadout.Items, []items.Instance{potions("ann-potions", 2)})
	sameItems(t, "stash", got.Stash, []items.Instance{potions("ann-stashed", 4)})

	loaded := mustLoad(t, s, "ann", "warrior")
	if loaded.Run.RunID != run || loaded.Run.Instance != instance {
		t.Fatalf("lease on load = %+v", loaded.Run)
	}
	// The lease is the account's, so another class sees it before it has a character.
	if c, ok, err := s.Load(ctx, "ann", "rogue"); err != nil || ok || c.Run.RunID != run {
		t.Fatalf("load of a new class = %+v, %v, %v, want the lease", c, ok, err)
	}
}

func oneRunPerAccount(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	mustCreate(t, s, inventory.Character{Name: "ann", Class: "rogue"})
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000011")
	if _, err := s.StartRun(ctx, "ann", "warrior", "6f1c2b3a-0000-4000-8000-000000000012", instance); !errors.Is(err, inventory.ErrInRun) {
		t.Fatalf("second run = %v, want ErrInRun", err)
	}
	if _, err := s.StartRun(ctx, "ann", "rogue", "6f1c2b3a-0000-4000-8000-000000000012", "instance-1@aa"); !errors.Is(err, inventory.ErrInRun) {
		t.Fatalf("run for another class = %v, want ErrInRun", err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.Run.RunID != "6f1c2b3a-0000-4000-8000-000000000011" {
		t.Fatalf("lease = %+v, want the first run's", got.Run)
	}
	mustCreate(t, s, character("bob"))
	mustStart(t, s, "bob", "6f1c2b3a-0000-4000-8000-000000000012")
}

func noLobbySaveInARun(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	sess := mustClaim(t, s, "ann")
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000013")
	if err := s.SaveLobby(ctx, "ann", "warrior", sess, inventory.Loadout{}, nil); !errors.Is(err, inventory.ErrInRun) {
		t.Fatalf("lobby save in a run = %v, want ErrInRun", err)
	}
	got := mustLoad(t, s, "ann", "warrior")
	sameItems(t, "equipment", got.Loadout.Equipment, []items.Instance{sword("ann-sword")})
	sameItems(t, "stash", got.Stash, []items.Instance{potions("ann-stashed", 4)})

	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000013", got); err != nil {
		t.Fatal(err)
	}
	noLease(t, s, "ann")
	if err := s.SaveLobby(ctx, "ann", "warrior", sess, inventory.Loadout{}, nil); err != nil {
		t.Fatalf("lobby save after the run: %v", err)
	}
}

func endRunNeedsTheLease(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	after := character("ann")
	after.XP = 99
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000014", after); !errors.Is(err, inventory.ErrLeaseLost) {
		t.Fatalf("run end with no lease = %v, want ErrLeaseLost", err)
	}
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000015")
	if err := s.EndRun(ctx, "6f1c2b3a-0000-4000-8000-000000000014", after); !errors.Is(err, inventory.ErrLeaseLost) {
		t.Fatalf("run end under another run's lease = %v, want ErrLeaseLost", err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.XP != 10 || got.Run.RunID != "6f1c2b3a-0000-4000-8000-000000000015" {
		t.Fatalf("after lost run ends = %+v, want nothing written", got)
	}
}

func runIDIsUsedOnce(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	const run = "6f1c2b3a-0000-4000-8000-000000000016"
	c := mustStart(t, s, "ann", run)
	c.XP = 20
	if err := s.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "ann", "warrior", run, instance); !errors.Is(err, inventory.ErrRunUsed) {
		t.Fatalf("start of an ended run = %v, want ErrRunUsed", err)
	}
	noLease(t, s, "ann")
	// The retry no-op still holds once the lease is gone.
	c.XP = 999
	if err := s.EndRun(ctx, run, c); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.XP != 20 {
		t.Fatalf("xp = %d, want the first run end", got.XP)
	}
}

func clearRun(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	const run = "6f1c2b3a-0000-4000-8000-000000000017"
	c := mustStart(t, s, "ann", run)
	if err := s.ClearRun(ctx, "ann", "6f1c2b3a-0000-4000-8000-000000000018"); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s, "ann", "warrior"); got.Run.RunID != run {
		t.Fatalf("lease after clearing another run = %+v, want it kept", got.Run)
	}
	if err := s.ClearRun(ctx, "ann", run); err != nil {
		t.Fatal(err)
	}
	noLease(t, s, "ann")
	// The run that lost its lease cannot write over what comes after.
	c.XP = 500
	if err := s.EndRun(ctx, run, c); !errors.Is(err, inventory.ErrLeaseLost) {
		t.Fatalf("run end after a clear = %v, want ErrLeaseLost", err)
	}
	if err := s.ClearRun(ctx, "nobody", run); err != nil {
		t.Fatalf("clear for an unknown name: %v", err)
	}
	mustStart(t, s, "ann", "6f1c2b3a-0000-4000-8000-000000000019")
}

func session(shard, boot, id string) inventory.Session {
	return inventory.Session{Name: "ann", ID: id, Shard: shard, Boot: boot}
}

func beat(t *testing.T, s inventory.Store, shard, boot string, ttl time.Duration) {
	t.Helper()
	if err := s.Heartbeat(ctx, shard, boot, ttl); err != nil {
		t.Fatalf("heartbeat %s@%s: %v", shard, boot, err)
	}
}

func saveWith(s inventory.Store, id string) error {
	return s.SaveLobby(ctx, "ann", "warrior", id, inventory.Loadout{}, []items.Instance{sword(id)})
}

// Two tabs, or two shards, joining one name at once.
func oneClaimWinsARace(t *testing.T, s inventory.Store) {
	const n = 8
	for i := range n {
		beat(t, s, fmt.Sprint("world-", i), "b0", time.Minute)
	}
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			err := s.Claim(ctx, session(fmt.Sprint("world-", i), "b0", fmt.Sprint("s", i)))
			switch {
			case err == nil:
				won.Add(1)
			case !errors.Is(err, inventory.ErrPlaying):
				t.Errorf("claim %d = %v, want ErrPlaying", i, err)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("%d claims won, want 1", won.Load())
	}
	if err := s.Claim(ctx, inventory.Session{Name: "bob", ID: "bob-1", Shard: "world-1", Boot: "b0"}); err != nil {
		t.Fatalf("claim of another name: %v", err)
	}
}

func claimTakenOverAfterExpiry(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	beat(t, s, "world-0", "b0", 200*time.Millisecond)
	beat(t, s, "world-1", "b0", time.Minute)
	if err := s.Claim(ctx, session("world-0", "b0", "s1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, session("world-1", "b0", "s2")); !errors.Is(err, inventory.ErrPlaying) {
		t.Fatalf("claim while world-0 is live = %v, want ErrPlaying", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := s.Claim(ctx, session("world-1", "b0", "s2")); err != nil {
		t.Fatalf("claim after world-0 missed its heartbeat: %v", err)
	}
	if err := saveWith(s, "s1"); !errors.Is(err, inventory.ErrSessionLost) {
		t.Fatalf("save from the expired session = %v, want ErrSessionLost", err)
	}
	if err := saveWith(s, "s2"); err != nil {
		t.Fatal(err)
	}
	// world-0 coming back does not take the name back.
	beat(t, s, "world-0", "b0", time.Minute)
	if err := s.Claim(ctx, session("world-0", "b0", "s3")); !errors.Is(err, inventory.ErrPlaying) {
		t.Fatalf("claim back = %v, want ErrPlaying", err)
	}
}

func newBootEndsOldClaims(t *testing.T, s inventory.Store) {
	beat(t, s, "world-0", "b0", time.Minute)
	beat(t, s, "world-1", "b0", time.Minute)
	if err := s.Claim(ctx, session("world-0", "b0", "s1")); err != nil {
		t.Fatal(err)
	}
	// world-0 restarted.
	beat(t, s, "world-0", "b1", time.Minute)
	if err := s.Claim(ctx, session("world-1", "b0", "s2")); err != nil {
		t.Fatalf("claim after the holder's shard restarted: %v", err)
	}
}

func staleSessionCannotSave(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	if err := saveWith(s, "s1"); !errors.Is(err, inventory.ErrSessionLost) {
		t.Fatalf("save with no claim = %v, want ErrSessionLost", err)
	}
	beat(t, s, "world-0", "b0", time.Minute)
	if err := s.Claim(ctx, session("world-0", "b0", "s1")); err != nil {
		t.Fatal(err)
	}
	// The same shard and boot can take over its own claim. Its local check
	// already made sure s1 is gone.
	if err := s.Claim(ctx, session("world-0", "b0", "s2")); err != nil {
		t.Fatalf("claim by the same shard: %v", err)
	}
	if err := saveWith(s, "s1"); !errors.Is(err, inventory.ErrSessionLost) {
		t.Fatalf("save from the old session = %v, want ErrSessionLost", err)
	}
	if err := saveWith(s, "s2"); err != nil {
		t.Fatal(err)
	}
	got := mustLoad(t, s, "ann", "warrior")
	sameItems(t, "stash", got.Stash, []items.Instance{sword("s2")})
}

func releaseThenReclaim(t *testing.T, s inventory.Store) {
	mustCreate(t, s, character("ann"))
	beat(t, s, "world-0", "b0", time.Minute)
	beat(t, s, "world-1", "b0", time.Minute)
	first, second := session("world-0", "b0", "s1"), session("world-1", "b0", "s2")
	if err := s.Claim(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, second); !errors.Is(err, inventory.ErrPlaying) {
		t.Fatalf("claim after a release by a non-holder = %v, want ErrPlaying", err)
	}
	if err := s.Release(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := saveWith(s, "s1"); !errors.Is(err, inventory.ErrSessionLost) {
		t.Fatalf("save after release = %v, want ErrSessionLost", err)
	}
	if err := s.Claim(ctx, second); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if err := saveWith(s, "s2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, first); err != nil {
		t.Fatalf("reclaim by the first shard: %v", err)
	}
}
