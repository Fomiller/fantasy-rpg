package server

import (
	"context"
	"fmt"
	"slices"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// Why a lobby move did nothing.
const (
	MoveNotInLobby   = "not_in_lobby"
	MoveUnknownItem  = "unknown_item"
	MoveFull         = "full"
	MoveCannotEquip  = "cannot_equip"
	MoveUnknownPlace = "unknown_place"
)

// lobby is a player between runs. Their items live here, not in the world,
// until they enter the cave.
type lobby struct {
	xp    int
	gear  map[items.Slot]items.Instance
	bag   inventory.Bag
	stash inventory.Bag
}

func newLobby(xp, bagSize int, l inventory.Loadout, stash []items.Instance) *lobby {
	lb := &lobby{
		xp:    xp,
		gear:  map[items.Slot]items.Instance{},
		bag:   inventory.Bag{Size: bagSize},
		stash: inventory.Bag{Size: inventory.StashSize, Items: stash},
	}
	for _, inst := range l.Equipment {
		lb.gear[inst.Slot] = inst
	}
	for _, inst := range l.Items {
		lb.bag.Add(inst)
	}
	return lb
}

// loadout is gear in slot order and the bag, as the store and the world take it.
func (lb *lobby) loadout() inventory.Loadout {
	var l inventory.Loadout
	for _, slot := range items.GearSlots {
		if inst, ok := lb.gear[slot]; ok {
			l.Equipment = append(l.Equipment, inst)
		}
	}
	l.Items = slices.Clone(lb.bag.Items)
	return l
}

// move puts a whole entry somewhere else. It returns why nothing moved, or
// "" when something did. A stack that only partly fits leaves the rest where
// it was.
func (lb *lobby) move(id string, to gamev1.ItemPlace, canEquip func(items.Instance) error) string {
	from, inst, ok := lb.find(id)
	if !ok {
		return MoveUnknownItem
	}
	switch to {
	case gamev1.ItemPlace_ITEM_PLACE_GEAR:
		if from == to {
			return ""
		}
		if canEquip(inst) != nil {
			return MoveCannotEquip
		}
		lb.bagAt(from).Take(id)
		// The old gear always fits, since the new gear just left a slot.
		if old, had := lb.gear[inst.Slot]; had {
			lb.bagAt(from).Add(old)
		}
		lb.gear[inst.Slot] = inst
		return ""
	case gamev1.ItemPlace_ITEM_PLACE_STASH, gamev1.ItemPlace_ITEM_PLACE_BAG:
		if from == to {
			return ""
		}
		dst := lb.bagAt(to)
		// Trial run on a copy, so a stash that is full changes nothing.
		trial := inventory.Bag{Size: dst.Size, Items: slices.Clone(dst.Items)}
		left := trial.Add(inst)
		if left == inst.Quantity {
			return MoveFull
		}
		*dst = trial
		lb.remove(from, inst, left)
		return ""
	}
	return MoveUnknownPlace
}

func (lb *lobby) find(id string) (gamev1.ItemPlace, items.Instance, bool) {
	for _, place := range []gamev1.ItemPlace{gamev1.ItemPlace_ITEM_PLACE_STASH, gamev1.ItemPlace_ITEM_PLACE_BAG} {
		if inst, ok := lb.bagAt(place).Get(id); ok {
			return place, inst, true
		}
	}
	for _, inst := range lb.gear {
		if inst.ID == id {
			return gamev1.ItemPlace_ITEM_PLACE_GEAR, inst, true
		}
	}
	return 0, items.Instance{}, false
}

// remove takes inst out of from, leaving left of the stack behind.
func (lb *lobby) remove(from gamev1.ItemPlace, inst items.Instance, left int) {
	if from == gamev1.ItemPlace_ITEM_PLACE_GEAR {
		delete(lb.gear, inst.Slot)
		return
	}
	b := lb.bagAt(from)
	i := slices.IndexFunc(b.Items, func(it items.Instance) bool { return it.ID == inst.ID })
	if left == 0 {
		b.Items = slices.Delete(b.Items, i, i+1)
	} else {
		b.Items[i].Quantity = left
	}
}

func (lb *lobby) bagAt(place gamev1.ItemPlace) *inventory.Bag {
	if place == gamev1.ItemPlace_ITEM_PLACE_STASH {
		return &lb.stash
	}
	return &lb.bag
}

// openLobby loads the player's items and sends them. A player with no saved
// loadout gets the starter kit, saved right away so it is theirs to stash.
func (s *Server) openLobby(c *client) error {
	ctx := context.Background()
	xp, err := s.Store.LoadXP(ctx, c.name)
	if err != nil {
		return fmt.Errorf("load xp: %w", err)
	}
	l, ok, err := s.Store.Load(ctx, c.name)
	if err != nil {
		return fmt.Errorf("load loadout: %w", err)
	}
	if !ok {
		l = s.starterKit()
		if err := s.Store.Save(ctx, c.name, l); err != nil {
			return fmt.Errorf("save starter kit: %w", err)
		}
	}
	stash, err := s.Store.LoadStash(ctx, c.name)
	if err != nil {
		return fmt.Errorf("load stash: %w", err)
	}
	c.lobby = newLobby(xp, s.world.BagSize(), l, stash)

	prog := s.world.Classes().Progression
	level := prog.LevelFor(xp)
	s.sendToClient(c, protocol.Lobby(protocol.Character(prog, c.name, c.class, level, xp, 0), protocol.ClassInfo(s.world.Classes(), c.class)))
	s.sendLobbyItems(c)
	return nil
}

func (s *Server) sendLobbyItems(c *client) {
	p, err := s.world.Preview(c.class, c.lobby.xp, c.lobby.loadout())
	if err != nil {
		s.log.Error("preview loadout", "name", c.name, "err", err)
		return
	}
	s.sendToClient(c, protocol.Equipment(s.world, p))
	s.sendToClient(c, protocol.Inventory(p))
	s.sendToClient(c, protocol.Stash(c.lobby.stash))
}

// moveItem checks a lobby move and saves what changed.
func (s *Server) moveItem(c *client, req *gamev1.StashMoveRequest) {
	reason := MoveNotInLobby
	if c.lobby != nil {
		reason = c.lobby.move(req.GetItemId(), req.GetTo(), func(inst items.Instance) error { return s.world.CanEquip(c.class, inst) })
	}
	if reason != "" {
		s.sendToClient(c, protocol.StashMoveRejected(req.GetItemId(), reason))
		return
	}
	ctx := context.Background()
	// An empty loadout is saved, not deleted, or stashing everything would
	// hand out a new starter kit.
	if err := s.Store.Save(ctx, c.name, c.lobby.loadout()); err != nil {
		s.log.Error("save loadout", "name", c.name, "err", err)
	}
	if err := s.Store.SaveStash(ctx, c.name, c.lobby.stash.Items); err != nil {
		s.log.Error("save stash", "name", c.name, "err", err)
	}
	s.sendLobbyItems(c)
}

// enterCave spawns a lobby player with everything they have on them.
func (s *Server) enterCave(c *client) {
	if c.lobby == nil {
		return
	}
	spawn := s.cave.PlayerSpawns[s.spawns%len(s.cave.PlayerSpawns)]
	id, err := s.world.AddPlayerAs(spawn, c.class, c.lobby.xp)
	if err != nil {
		s.log.Error("enter cave", "name", c.name, "err", err)
		return
	}
	if err := s.world.Restore(id, c.lobby.loadout()); err != nil {
		s.log.Error("restore loadout", "player", id, "err", err)
	}
	s.spawns++
	c.id, c.lobby = id, nil
	s.clients[id] = c
	p, _ := s.world.Player(id)
	class := protocol.ClassInfo(s.world.Classes(), p.Class)
	character := protocol.Character(s.world.Classes().Progression, c.name, p.Class, p.Level, p.XP, p.RunXP)
	s.sendToClient(c, protocol.Joined(id, s.cave, spawn, s.TickRate, class, character))
	s.sendItems(id)
	s.log.Info("player entered the cave", "player", id, "name", c.name)
}

// stashKept moves an extracted bag into the stash. What does not fit stays
// in the bag, and is returned so the summary can say so.
func (s *Server) stashKept(name string, kept, gear []items.Instance) []items.Instance {
	ctx := context.Background()
	saved, err := s.Store.LoadStash(ctx, name)
	if err != nil {
		// Saving over a stash that did not load would wipe it, so keep it all in the bag.
		s.log.Error("load stash", "name", name, "err", err)
		return kept
	}
	stash := inventory.Bag{Size: inventory.StashSize, Items: saved}
	var overflow []items.Instance
	for _, inst := range kept {
		if left := stash.Add(inst); left > 0 {
			inst.Quantity = left
			overflow = append(overflow, inst)
		}
	}
	if err := s.Store.SaveStash(ctx, name, stash.Items); err != nil {
		s.log.Error("save stash", "name", name, "err", err)
		return kept
	}
	if err := s.Store.Save(ctx, name, inventory.Loadout{Equipment: gear, Items: overflow}); err != nil {
		s.log.Error("save loadout", "name", name, "err", err)
	}
	return overflow
}

// toLobby takes a player whose run ended out of the cave.
func (s *Server) toLobby(id sim.EntityID) {
	c, ok := s.clients[id]
	if !ok {
		return
	}
	delete(s.clients, id)
	c.id = 0
	if err := s.openLobby(c); err != nil {
		s.log.Error("open lobby", "name", c.name, "err", err)
		c.cancel()
	}
}
