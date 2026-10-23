package sim

import (
	"cmp"
	"maps"
	"slices"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Pile is loot on the ground: an enemy drop, a dead player's bag, something
// a player dropped, or a chest. Anyone close enough can pick it up.
type Pile struct {
	ID    EntityID
	Pos   Vec2
	Chest bool
	Items []items.Instance
	// version goes up whenever Items changes, so open loot windows know to update.
	version uint64
}

// Why a pickup took nothing.
const (
	PickupRange   = "range"
	PickupGone    = "gone"
	PickupBagFull = "bag_full"
)

type PickupRejection struct {
	Player, Pile EntityID
	Item         string
	Reason       string
}

// LootEvent is what one player's loot window shows now. Closed means the
// window is shut and Items is empty.
type LootEvent struct {
	Player, Pile EntityID
	Items        []items.Instance
	Closed       bool
}

// OpenLootCommand opens a loot window. The world sends the pile's contents
// on the next Step, and again whenever they change.
type OpenLootCommand struct {
	Player EntityID
	Target EntityID
}

type CloseLootCommand struct {
	Player EntityID
	Target EntityID
}

// ItemCommand is any command that changes what a player carries.
type ItemCommand interface{ player() EntityID }

// PickupCommand takes Item from the pile, or everything that fits when Item
// is empty.
type PickupCommand struct {
	Player EntityID
	Target EntityID
	Item   string
}

type EquipCommand struct {
	Player EntityID
	Item   string
}

type UnequipCommand struct {
	Player EntityID
	Slot   items.Slot
}

type DropCommand struct {
	Player EntityID
	Item   string
}

type UseCommand struct {
	Player EntityID
	Item   string
}

func (c PickupCommand) player() EntityID  { return c.Player }
func (c EquipCommand) player() EntityID   { return c.Player }
func (c UnequipCommand) player() EntityID { return c.Player }
func (c DropCommand) player() EntityID    { return c.Player }
func (c UseCommand) player() EntityID     { return c.Player }

// SetLoot gives the world a roller for enemy drops and chests. Without one
// nothing drops.
func (w *World) SetLoot(r *items.Roller) {
	w.loot = r
}

// AddChest places a chest and fills it right away, so its contents come from
// the run seed in a fixed order.
func (w *World) AddChest(pos Vec2) EntityID {
	var contents []items.Instance
	if w.loot != nil {
		contents = w.loot.Drop(w.cfg.ChestLoot, w.cfg.LootLevel, 1)
	}
	return w.addPile(pos, true, contents)
}

func (w *World) AddPile(pos Vec2, contents []items.Instance) EntityID {
	return w.addPile(pos, false, contents)
}

func (w *World) addPile(pos Vec2, chest bool, contents []items.Instance) EntityID {
	w.nextID++
	w.piles[w.nextID] = &Pile{ID: w.nextID, Pos: pos, Chest: chest, Items: contents, version: 1}
	return w.nextID
}

func (w *World) Piles() []Pile {
	out := make([]Pile, 0, len(w.piles))
	for _, p := range w.piles {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b Pile) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (w *World) Pile(id EntityID) (Pile, bool) {
	p, ok := w.piles[id]
	if !ok {
		return Pile{}, false
	}
	return *p, true
}

// Restore gives a player a saved loadout. Anything that does not fit is lost.
func (w *World) Restore(id EntityID, l inventory.Loadout) error {
	p, ok := w.players[id]
	if !ok {
		return errNoPlayer
	}
	// Gear the player cannot wear, like a staff saved by another class, goes in the bag.
	for _, inst := range l.Equipment {
		if _, _, err := w.Equip(id, inst); err != nil {
			p.Inventory.Add(inst)
		}
	}
	for _, inst := range l.Items {
		p.Inventory.Add(inst)
	}
	return nil
}

// Loadout is what the player owns right now, equipment in slot order.
func (p Player) Loadout() inventory.Loadout {
	var l inventory.Loadout
	for _, slot := range items.GearSlots {
		if inst, ok := p.Equipment[slot]; ok {
			l.Equipment = append(l.Equipment, inst)
		}
	}
	l.Items = slices.Clone(p.Inventory.Items)
	return l
}

func (ev *Events) changed(id EntityID) {
	if !slices.Contains(ev.Inventory, id) {
		ev.Inventory = append(ev.Inventory, id)
	}
}

// applyItem reports whether the player's items changed. A command that
// cannot apply, like picking up from too far away, does nothing.
func (w *World) applyItem(c ItemCommand, ev *Events) bool {
	p, ok := w.players[c.player()]
	if !ok || p.Health <= 0 {
		return false
	}
	switch c := c.(type) {
	case PickupCommand:
		reason := w.pickup(p, c)
		if reason != "" {
			ev.PickupRejected = append(ev.PickupRejected, PickupRejection{Player: p.ID, Pile: c.Target, Item: c.Item, Reason: reason})
		}
		return reason == ""
	case EquipCommand:
		inst, ok := p.Inventory.Get(c.Item)
		if !ok {
			return false
		}
		old, had, err := w.Equip(p.ID, inst)
		if err != nil {
			return false
		}
		p.Inventory.Take(c.Item)
		if had {
			p.Inventory.Add(old)
		}
		return true
	case UnequipCommand:
		inst, ok := p.Equipment[c.Slot]
		if !ok || p.Inventory.Full() {
			return false
		}
		delete(p.Equipment, c.Slot)
		p.Inventory.Add(inst)
		w.applyGear(p)
		return true
	case DropCommand:
		inst, ok := p.Inventory.Take(c.Item)
		if ok {
			w.AddPile(p.Pos, []items.Instance{inst})
		}
		return ok
	case UseCommand:
		inst, ok := p.Inventory.Get(c.Item)
		if !ok || inst.Slot != items.Consumable {
			return false
		}
		p.Inventory.TakeOne(c.Item)
		p.Health = min(p.Health+inst.Stats()[items.Heal], p.MaxHealth)
		return true
	}
	return false
}

// pickup takes one item, or everything that fits. Whatever does not fit
// stays. It returns why it took nothing, or "" when it took something.
func (w *World) pickup(p *Player, c PickupCommand) string {
	pile, ok := w.piles[c.Target]
	if !ok || len(pile.Items) == 0 {
		return PickupGone
	}
	if p.Pos.DistanceTo(pile.Pos) > w.cfg.PickupRange {
		return PickupRange
	}
	if c.Item != "" && !slices.ContainsFunc(pile.Items, func(inst items.Instance) bool { return inst.ID == c.Item }) {
		return PickupGone
	}
	var rest []items.Instance
	took := false
	for _, inst := range pile.Items {
		if c.Item == "" || inst.ID == c.Item {
			left := p.Inventory.Add(inst)
			took = took || left < inst.Quantity
			inst.Quantity = left
		}
		if inst.Quantity > 0 {
			rest = append(rest, inst)
		}
	}
	if !took {
		return PickupBagFull
	}
	pile.Items = rest
	pile.version++
	if len(rest) == 0 && !pile.Chest {
		delete(w.piles, c.Target)
	}
	return ""
}

func (w *World) applyLoot(cmd any) {
	switch c := cmd.(type) {
	case OpenLootCommand:
		if p, ok := w.players[c.Player]; ok {
			// Zero is never a pile's version, so the next Step sends the contents.
			p.looting, p.lootSeen = c.Target, 0
		}
	case CloseLootCommand:
		if p, ok := w.players[c.Player]; ok && p.looting == c.Target {
			p.looting = 0
		}
	}
}

// stepLoot sends each open loot window its pile's contents when they have
// changed, and closes it once the pile is empty, gone or out of range.
func (w *World) stepLoot(ev *Events) {
	for _, id := range slices.Sorted(maps.Keys(w.players)) {
		p := w.players[id]
		if p.looting == 0 {
			continue
		}
		pile, ok := w.piles[p.looting]
		if !ok || len(pile.Items) == 0 || p.Health <= 0 || p.Pos.DistanceTo(pile.Pos) > w.cfg.PickupRange {
			ev.Loot = append(ev.Loot, LootEvent{Player: id, Pile: p.looting, Closed: true})
			p.looting = 0
			continue
		}
		if p.lootSeen != pile.version {
			p.lootSeen = pile.version
			ev.Loot = append(ev.Loot, LootEvent{Player: id, Pile: pile.ID, Items: slices.Clone(pile.Items)})
		}
	}
}

// dropOnKill drops loot where enemies die, and drops everything a dead
// player carried as one bag where they fell.
func (w *World) dropOnKill(ev *Events) {
	for _, d := range ev.Damage {
		if !d.Killed {
			continue
		}
		if e, ok := w.enemies[d.Target]; ok && w.loot != nil {
			if drop := w.loot.Drop(w.cfg.Archetypes[e.Archetype].Loot, w.cfg.LootLevel, 1); len(drop) > 0 {
				w.AddPile(e.Pos, drop)
			}
		}
		if p, ok := w.players[d.Target]; ok {
			l := p.Loadout()
			if !l.Empty() {
				w.AddPile(p.Pos, append(l.Equipment, l.Items...))
			}
			p.Equipment = map[items.Slot]items.Instance{}
			p.Inventory.Items = nil
			p.stats = baseStats(p.class)
			p.MaxHealth = p.stats[items.MaxHealth]
			ev.changed(p.ID)
		}
	}
}
