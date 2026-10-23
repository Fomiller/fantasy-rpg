// Package inventory holds the items a player carries and the store that
// keeps them between connections.
package inventory

import (
	"slices"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Bag is what a player carries but does not wear. Each entry takes one of
// Size slots, and a stack of potions or ore is one entry.
type Bag struct {
	Size  int
	Items []items.Instance
}

// Add puts inst in the bag, topping up matching stacks before using a new
// slot. It returns how many did not fit.
func (b *Bag) Add(inst items.Instance) int {
	left := inst.Quantity
	for i := range b.Items {
		it := &b.Items[i]
		if left == 0 || it.DefID != inst.DefID || it.Stack <= 1 {
			continue
		}
		n := min(it.Stack-it.Quantity, left)
		it.Quantity += n
		left -= n
	}
	if left > 0 && !b.Full() {
		inst.Quantity = left
		b.Items = append(b.Items, inst)
		left = 0
	}
	return left
}

func (b *Bag) Get(id string) (items.Instance, bool) {
	i := b.index(id)
	if i < 0 {
		return items.Instance{}, false
	}
	return b.Items[i], true
}

// Take removes a whole entry.
func (b *Bag) Take(id string) (items.Instance, bool) {
	i := b.index(id)
	if i < 0 {
		return items.Instance{}, false
	}
	inst := b.Items[i]
	b.Items = slices.Delete(b.Items, i, i+1)
	return inst, true
}

// TakeOne removes one from a stack, and the entry once the stack is empty.
func (b *Bag) TakeOne(id string) (items.Instance, bool) {
	i := b.index(id)
	if i < 0 {
		return items.Instance{}, false
	}
	one := b.Items[i]
	one.Quantity = 1
	if b.Items[i].Quantity--; b.Items[i].Quantity <= 0 {
		b.Items = slices.Delete(b.Items, i, i+1)
	}
	return one, true
}

func (b *Bag) Full() bool { return len(b.Items) >= b.Size }

func (b *Bag) index(id string) int {
	return slices.IndexFunc(b.Items, func(it items.Instance) bool { return it.ID == id })
}

// Loadout is everything a player owns: what they wear and what is in the bag.
type Loadout struct {
	Equipment []items.Instance
	Items     []items.Instance
}

func (l Loadout) Empty() bool { return len(l.Equipment) == 0 && len(l.Items) == 0 }
