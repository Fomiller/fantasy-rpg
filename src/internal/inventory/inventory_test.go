package inventory

import (
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
