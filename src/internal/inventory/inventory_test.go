package inventory

import (
	"context"
	"testing"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func potions(id string, n int) items.Instance {
	return items.Instance{ID: id, DefID: "health_potion", Slot: items.Consumable, Quantity: n, Stack: 5}
}

func sword(id string) items.Instance {
	return items.Instance{ID: id, DefID: "iron_sword", Slot: items.Weapon, Quantity: 1, Stack: 1}
}

func TestAddTopsUpStacksFirst(t *testing.T) {
	b := Bag{Size: 3}
	b.Add(potions("a", 4))
	if left := b.Add(potions("b", 3)); left != 0 {
		t.Fatalf("left = %d", left)
	}
	if len(b.Items) != 2 || b.Items[0].Quantity != 5 || b.Items[1].Quantity != 2 || b.Items[1].ID != "b" {
		t.Fatalf("items = %+v", b.Items)
	}
}

func TestAddNeverStacksGear(t *testing.T) {
	b := Bag{Size: 2}
	b.Add(sword("a"))
	b.Add(sword("b"))
	if left := b.Add(sword("c")); left != 1 || len(b.Items) != 2 {
		t.Fatalf("left = %d, items = %+v", left, b.Items)
	}
}

func TestFullBagStillTopsUpStacks(t *testing.T) {
	b := Bag{Size: 1}
	b.Add(potions("a", 3))
	if left := b.Add(potions("b", 4)); left != 2 || b.Items[0].Quantity != 5 {
		t.Fatalf("left = %d, items = %+v", left, b.Items)
	}
}

func TestTake(t *testing.T) {
	b := Bag{Size: 3}
	b.Add(sword("s"))
	b.Add(potions("p", 2))

	if one, ok := b.TakeOne("p"); !ok || one.Quantity != 1 || b.Items[1].Quantity != 1 {
		t.Fatalf("took %+v, %v, items = %+v", one, ok, b.Items)
	}
	b.TakeOne("p")
	if _, ok := b.Get("p"); ok {
		t.Fatal("empty stack is still in the bag")
	}
	if got, ok := b.Take("s"); !ok || got.ID != "s" || len(b.Items) != 0 {
		t.Fatalf("took %+v, %v, items = %+v", got, ok, b.Items)
	}
	if _, ok := b.Take("s"); ok {
		t.Fatal("took the same item twice")
	}
}

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	if _, ok, _ := m.Load(ctx, "ann"); ok {
		t.Fatal("loaded a player that was never saved")
	}

	l := Loadout{Equipment: []items.Instance{sword("s")}, Items: []items.Instance{potions("p", 2)}}
	m.Save(ctx, "ann", l)
	l.Items[0].Quantity = 99

	got, ok, err := m.Load(ctx, "ann")
	if err != nil || !ok || got.Equipment[0].ID != "s" || got.Items[0].Quantity != 2 {
		t.Fatalf("loaded %+v, %v, %v", got, ok, err)
	}
	if _, ok, _ := m.Load(ctx, "bob"); ok {
		t.Fatal("players share a loadout")
	}

	m.Delete(ctx, "ann")
	if _, ok, _ := m.Load(ctx, "ann"); ok {
		t.Fatal("loadout survived delete")
	}
}

func TestMemoryStoreXP(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	if xp, err := m.LoadXP(ctx, "ann"); err != nil || xp != 0 {
		t.Fatalf("new player xp = %d, %v", xp, err)
	}
	m.SaveXP(ctx, "ann", 168)
	m.Save(ctx, "ann", Loadout{Items: []items.Instance{potions("p", 1)}})
	m.Delete(ctx, "ann")
	if xp, _ := m.LoadXP(ctx, "ann"); xp != 168 {
		t.Fatalf("xp = %d, want it kept after the loadout is deleted", xp)
	}
	if xp, _ := m.LoadXP(ctx, "bob"); xp != 0 {
		t.Fatalf("players share xp: bob has %d", xp)
	}
}

func TestMemoryStoreStash(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	if stash, err := m.LoadStash(ctx, "ann"); err != nil || len(stash) != 0 {
		t.Fatalf("new player stash = %v, %v", stash, err)
	}
	stash := []items.Instance{sword("s"), potions("p", 3)}
	m.SaveStash(ctx, "ann", stash)
	stash[1].Quantity = 99

	got, _ := m.LoadStash(ctx, "ann")
	if len(got) != 2 || got[0].ID != "s" || got[1].Quantity != 3 {
		t.Fatalf("stash = %+v", got)
	}
	m.Delete(ctx, "ann")
	if got, _ := m.LoadStash(ctx, "ann"); len(got) != 2 {
		t.Fatalf("stash = %+v, want it kept after the loadout is deleted", got)
	}
	if got, _ := m.LoadStash(ctx, "bob"); len(got) != 0 {
		t.Fatalf("players share a stash: bob has %+v", got)
	}
}
