package server

import (
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
	// MoveSaving is also why Enter Cave did nothing.
	MoveSaving = "saving"
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

// openLobby puts the player in the lobby with ch and sends it. ch is what
// the store has, or is about to have once the player's queued writes land.
func (s *Server) openLobby(c *client, ch inventory.Character) {
	c.lobby = newLobby(ch.XP, s.world.BagSize(), ch.Loadout, ch.Stash)
	c.runID, c.stash = "", nil
	// A lobby save from before this lobby must not count as what is saved now.
	c.lobbySeq++
	c.saved, c.savedSeq = cloneCharacter(ch), c.lobbySeq
	s.sendLobby(c)
	s.sendLobbyItems(c)
}

func (s *Server) sendLobby(c *client) {
	prog := s.world.Classes().Progression
	character := protocol.Character(prog, c.name, c.class, prog.LevelFor(c.lobby.xp), c.lobby.xp, 0)
	s.sendToClient(c, protocol.Lobby(character, protocol.ClassInfo(s.world.Classes(), c.class), c.pending != nil))
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
	switch {
	case c.lobby != nil && c.pending != nil:
		// Saving on top of a run that has not landed could lose the run.
		reason = MoveSaving
	case c.lobby != nil:
		reason = c.lobby.move(req.GetItemId(), req.GetTo(), func(inst items.Instance) error { return s.world.CanEquip(c.class, inst) })
	}
	if reason != "" {
		s.sendToClient(c, protocol.StashMoveRejected(req.GetItemId(), reason))
		return
	}
	s.saveLobby(c, req.GetItemId())
	s.sendLobbyItems(c)
}

// enterCave spawns a lobby player with everything they have on them.
func (s *Server) enterCave(c *client) {
	if c.lobby == nil {
		return
	}
	if c.pending != nil {
		s.sendToClient(c, protocol.EnterCaveRejected(MoveSaving))
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
	c.id, c.runID, c.stash, c.lobby = id, items.NewID(), c.lobby.stash.Items, nil
	s.clients[id] = c
	p, _ := s.world.Player(id)
	class := protocol.ClassInfo(s.world.Classes(), p.Class)
	character := protocol.Character(s.world.Classes().Progression, c.name, p.Class, p.Level, p.XP, p.RunXP)
	s.sendToClient(c, protocol.Joined(id, s.cave, spawn, s.TickRate, class, character))
	s.sendItems(id)
	s.log.Info("player entered the cave", "player", id, "name", c.name)
}

// stashKept adds an extracted bag to a copy of the stash. What does not fit
// stays in the bag, and is returned so the summary can say so.
func stashKept(saved, kept []items.Instance) ([]items.Instance, []items.Instance) {
	stash := inventory.Bag{Size: inventory.StashSize, Items: slices.Clone(saved)}
	var overflow []items.Instance
	for _, inst := range kept {
		if left := stash.Add(inst); left > 0 {
			inst.Quantity = left
			overflow = append(overflow, inst)
		}
	}
	return stash.Items, overflow
}

// toLobby takes a player whose run ended out of the cave.
func (s *Server) toLobby(id sim.EntityID, ch inventory.Character) {
	c, ok := s.clients[id]
	if !ok {
		return
	}
	delete(s.clients, id)
	c.id = 0
	s.openLobby(c, ch)
}
