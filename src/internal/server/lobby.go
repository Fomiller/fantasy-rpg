package server

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/coder/websocket"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

// Why a lobby move did nothing.
const (
	MoveNotInLobby   = "not_in_lobby"
	MoveUnknownItem  = "unknown_item"
	MoveFull         = "full"
	MoveCannotEquip  = "cannot_equip"
	MoveUnknownPlace = "unknown_place"
	// MoveFar is a move from or to the stash with the chest out of range.
	MoveFar = "far"
	// MoveSaving and MoveInRun are also why the entrance did nothing.
	MoveSaving = "saving"
	// MoveInRun is a run holding the account somewhere else.
	MoveInRun = "in_run"
	// EnterFull is no instance with room for the player.
	EnterFull = "full"
)

// StashRange is how far from the stash chest's center a player can stand to
// use it, by their server position. The client mirrors it as STASH_RANGE.
const StashRange = 64

// reasonTownFull is the close when the town is at TownMaxPlayers.
const reasonTownFull = "town full"

// lobby is a player's items between runs. They live here, not in the town's
// sim, until the player enters the cave.
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

// enterTown gives a joining player a body in the town, then opens their
// lobby on ch. A returning player lands outside the entrance.
func (s *Lobby) enterTown(c *client, ch inventory.Character, returning bool) {
	if len(s.inTown) >= s.TownMaxPlayers {
		c.close(websocket.StatusTryAgainLater, reasonTownFull)
		return
	}
	spawns := s.town.PlayerSpawns
	if returning {
		spawns = s.town.ReturnSpawns
	}
	spawn := spawns[s.townSpawns%len(spawns)]
	s.townSpawns++
	id, err := s.rules.AddPlayerAs(spawn, c.class, ch.XP)
	if err != nil {
		s.log.Error("enter town", "name", c.name, "err", err)
		c.close(websocket.StatusInternalError, "characters unavailable")
		return
	}
	c.id = id
	s.inTown[id] = c
	p, _ := s.rules.Player(id)
	prog := s.rules.Classes().Progression
	character := protocol.Character(prog, c.name, c.class, p.Level, p.XP, 0)
	s.sendToClient(c, protocol.Joined(id, s.town, spawn, s.TickRate, protocol.ClassInfo(s.rules.Classes(), c.class), character))
	s.sessionEvent(c, eventsv1.PlayerSessionKind_PLAYER_SESSION_KIND_ENTER)
	s.openLobby(c, ch)
}

// leaveTown takes the player's body out of the town. Town run XP is always
// 0, so the run end it reports means nothing.
func (s *Lobby) leaveTown(c *client) {
	if c.id == 0 || s.inTown[c.id] != c {
		return
	}
	s.rules.RemovePlayer(c.id)
	delete(s.inTown, c.id)
	c.id = 0
	s.sessionEvent(c, eventsv1.PlayerSessionKind_PLAYER_SESSION_KIND_LEAVE)
}

// checkEntrance sends a player who just stepped into the entrance toward a
// run. Only the step in counts, so standing in it tries once.
func (s *Lobby) checkEntrance() {
	for _, p := range s.rules.Players() {
		c, ok := s.inTown[p.ID]
		if !ok {
			continue
		}
		in := s.town.Entrance.Contains(p.Pos)
		if in && !c.atEntrance {
			s.enterInstance(c)
		}
		c.atEntrance = in
	}
}

func (s *Lobby) nearStash(c *client) bool {
	p, ok := s.rules.Player(c.id)
	return ok && p.Pos.DistanceTo(s.town.Stash) <= StashRange
}

// openLobby opens the player's lobby with ch and sends it. ch is what the
// store has, or is about to have once the player's queued writes land.
func (s *Lobby) openLobby(c *client, ch inventory.Character) {
	c.lobby = newLobby(ch.XP, s.rules.BagSize(), ch.Loadout, ch.Stash)
	c.runID, c.stash = "", nil
	// A lobby save from before this lobby must not count as what is saved now.
	c.lobbySeq++
	c.saved, c.savedSeq = cloneCharacter(ch), c.lobbySeq
	s.setInRun(c, ch.Run.Held())
	s.sendLobby(c)
	s.sendLobbyItems(c)
}

func (s *Lobby) sendLobby(c *client) {
	prog := s.rules.Classes().Progression
	character := protocol.Character(prog, c.name, c.class, prog.LevelFor(c.lobby.xp), c.lobby.xp, 0)
	s.sendToClient(c, protocol.Lobby(character, protocol.ClassInfo(s.rules.Classes(), c.class), c.pending != nil || c.inRun))
}

// sendLobbyItems follows every lobby change, so it also reloads the town's
// player. The snapshot then shows the level and speed the gear gives.
func (s *Lobby) sendLobbyItems(c *client) {
	if c.id != 0 {
		if err := s.rules.Reload(c.id, c.lobby.xp, c.lobby.loadout()); err != nil {
			s.log.Error("reload town player", "name", c.name, "err", err)
		}
	}
	p, err := s.rules.Preview(c.class, c.lobby.xp, c.lobby.loadout())
	if err != nil {
		s.log.Error("preview loadout", "name", c.name, "err", err)
		return
	}
	s.sendToClient(c, protocol.Equipment(s.rules, p))
	s.sendToClient(c, protocol.Inventory(p))
	s.sendToClient(c, protocol.Stash(c.lobby.stash))
}

// moveItem checks a lobby move and saves what changed. Moves between bag
// and gear work anywhere in the town. Moves from or to the stash need the
// chest in range.
func (s *Lobby) moveItem(c *client, id string, to gamev1.ItemPlace) {
	reason := MoveNotInLobby
	switch {
	case c.lobby == nil || c.entering:
	case c.pending != nil:
		// Saving on top of a run that has not landed could lose the run.
		reason = MoveSaving
	case c.inRun:
		reason = MoveInRun
	case c.lobby.touchesStash(id, to) && !s.nearStash(c):
		reason = MoveFar
	default:
		reason = c.lobby.move(id, to, func(inst items.Instance) error { return s.rules.CanEquip(c.class, inst) })
	}
	if reason != "" {
		s.sendToClient(c, protocol.StashMoveRejected(id, reason))
		return
	}
	s.saveLobby(c, id)
	s.sendLobbyItems(c)
}

func (lb *lobby) touchesStash(id string, to gamev1.ItemPlace) bool {
	from, _, ok := lb.find(id)
	return to == gamev1.ItemPlace_ITEM_PLACE_STASH || (ok && from == gamev1.ItemPlace_ITEM_PLACE_STASH)
}

// enterInstance checks the lobby can be left. The check waits behind the
// player's queued lobby saves, so the instance loads the lobby they see,
// and reads the lease, so a run that started elsewhere since the join is
// caught here and not on the instance.
func (s *Lobby) enterInstance(c *client) {
	if c.lobby == nil || c.entering {
		return
	}
	if reason := s.enterBlocked(c); reason != "" {
		s.sendToClient(c, protocol.EnterInstanceRejected(reason))
		return
	}
	c.entering = true
	name, class := c.name, c.class
	var lease inventory.Lease
	c.writes.push(storeWrite{op: "enter instance", call: func(ctx context.Context) error {
		ch, _, err := s.Store.Load(ctx, name, class)
		lease = ch.Run
		return err
	}, done: func(owner *client, err error) {
		c.entering = false
		if owner != c || c.gone || c.lobby == nil {
			return
		}
		if err != nil {
			s.sendToClient(c, protocol.EnterInstanceRejected(MoveSaving))
			return
		}
		if lease.Held() {
			s.setInRun(c, true)
			s.sendLobby(c)
		}
		if reason := s.enterBlocked(c); reason != "" {
			s.sendToClient(c, protocol.EnterInstanceRejected(reason))
			return
		}
		s.sendTicket(c)
	}})
}

func (s *Lobby) enterBlocked(c *client) string {
	switch {
	case c.pending != nil:
		return MoveSaving
	case c.inRun:
		return MoveInRun
	}
	return ""
}

// sendTicket picks an instance, sends the player there and closes. The
// instance server takes the lease, so a ticket never used leaves nothing
// behind. The pick can be a call to the instance manager, so it runs off the
// loop and the player stays entering until it answers.
func (s *Lobby) sendTicket(c *client) {
	if s.Instances == nil {
		s.sendToClient(c, protocol.EnterInstanceRejected(EnterFull))
		return
	}
	c.entering = true
	runID := items.NewID()
	go func() {
		ctx, cancel := context.WithTimeout(c.ctx, managerTimeout)
		p, err := s.Instances.Reserve(ctx, []string{runID}, time.Now())
		cancel()
		s.enqueue(func() { s.ticket(c, runID, p, err) })
	}()
}

func (s *Lobby) ticket(c *client, runID string, p Placement, err error) {
	c.entering = false
	if c.gone || c.lobby == nil {
		return
	}
	if err != nil {
		if !errors.Is(err, ErrFull) {
			s.log.Error("reserve instance", "name", c.name, "err", err)
		}
		s.sendToClient(c, protocol.EnterInstanceRejected(EnterFull))
		return
	}
	now := time.Now()
	token, err := jointoken.Sign(s.JoinKey, jointoken.Claims{Name: c.name, Class: c.class, Inst: p.ID + "@" + p.Boot, Run: runID}, now)
	if err != nil {
		s.log.Error("sign join token", "name", c.name, "err", err)
		s.sendToClient(c, protocol.EnterInstanceRejected(EnterFull))
		return
	}
	c.lobby = nil
	s.leaveTown(c)
	s.sendToClient(c, protocol.Ticket(p.URL, token, p.ID, jointoken.Lifetime))
	time.AfterFunc(s.closeGrace, func() { c.close(websocket.StatusNormalClosure, "entering instance") })
	s.log.Info("sent to instance", "name", c.name, "instance", p.ID, "run", runID)
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
