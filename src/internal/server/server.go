// Package server connects WebSocket clients to the game. Its lobby half,
// Lobby, is the world server: join, characters, the lobby and the stash. Its
// run half, Server, is the instance server: one sim.World and its tick loop,
// on top of a lobby of its own until FOM-228 moves players over from the
// world server.
//
// State is only touched from one goroutine: connections hand work to it
// through an inbox, and it hands encoded messages back through each
// client's outbox.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// Server is an instance server. A player joins into its lobby and enters
// the cave from there. When the run ends they go back to the lobby on the
// same connection.
type Server struct {
	*Lobby
	// TickRate is sent to clients on join. It must match the tick loop.
	TickRate int
	// ID names this instance in the run lease, with a boot id that is new on
	// every start.
	ID      string
	boot    string
	world   *sim.World
	cave    caves.Cave
	spawns  int
	clients map[sim.EntityID]*client
}

func New(world *sim.World, cave caves.Cave, loot *items.Roller, log *slog.Logger) *Server {
	s := &Server{
		Lobby:    NewLobby(world, loot, log),
		TickRate: 20,
		ID:       "instance-0",
		boot:     bootID(),
		world:    world,
		cave:     cave,
		clients:  map[sim.EntityID]*client{},
	}
	s.run = s
	return s
}

func bootID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Instance is the id and boot id, like instance-0@3f9a1c.
func (s *Server) Instance() string {
	return s.ID + "@" + s.boot
}

// Step runs on the tick loop goroutine.
func (s *Server) Step(dt time.Duration) {
	s.drain()

	events := s.world.Step(dt)

	s.broadcast(protocol.Snapshot(s.world))
	for _, ev := range events.Abilities {
		s.broadcast(protocol.AbilityUsed(ev))
	}
	for _, r := range events.Rejected {
		s.sendTo(r.Player, protocol.AbilityRejected(r))
	}
	for _, ev := range events.Damage {
		s.broadcast(protocol.Damage(ev))
	}
	for _, ev := range events.XP {
		s.sendTo(ev.Player, protocol.XPGained(ev))
	}
	// An extracting player's summary rides on their Extracted, which has to
	// come after the XP above and before the lobby.
	summaries := map[sim.EntityID]*gamev1.RunSummary{}
	after := map[sim.EntityID]inventory.Character{}
	for _, end := range events.RunEnded {
		summaries[end.Player], after[end.Player] = s.endRun(end)
	}
	for _, id := range events.Extracted {
		s.sendTo(id, protocol.Extracted(id, summaries[id]))
		s.broadcastExcept(id, protocol.Extracted(id, nil))
		s.toLobby(id, after[id])
		s.log.Info("player extracted", "player", id)
	}
	for _, id := range events.Inventory {
		s.sendItems(id)
	}
	for _, r := range events.PickupRejected {
		s.sendTo(r.Player, protocol.PickupRejected(r))
	}
	for _, ev := range events.Loot {
		s.sendTo(ev.Player, protocol.Loot(ev))
	}
}

// sendItems tells the player what they carry. Nothing is saved until the run ends.
func (s *Server) sendItems(id sim.EntityID) {
	c, ok := s.clients[id]
	p, alive := s.world.Player(id)
	if !ok || !alive {
		return
	}
	s.sendToClient(c, protocol.Equipment(s.world, p))
	s.sendToClient(c, protocol.Inventory(p))
}

// endRun tells the player about any new level and saves the run's result in
// one write. A death's summary goes out here and the player goes back to the
// lobby. An extraction's summary goes out with Extracted instead, and the
// character returned is the lobby to open after it.
func (s *Server) endRun(end sim.RunEnd) (*gamev1.RunSummary, inventory.Character) {
	c, ok := s.clients[end.Player]
	if !ok {
		return nil, inventory.Character{}
	}
	if end.Levels > 0 {
		s.sendTo(end.Player, protocol.LevelUp(end.Level, protocol.ClassInfo(s.world.Classes(), end.Class)))
	}
	summary := protocol.RunSummary(s.world.Classes().Progression, c.name, end)
	ch := inventory.Character{Name: c.name, Class: c.class, XP: end.XP}
	if end.Extracted {
		var overflow []items.Instance
		ch.Stash, overflow = stashKept(c.stash, end.Kept)
		ch.Loadout = inventory.Loadout{Equipment: end.Gear, Items: overflow}
		summary.Overflow = protocol.Items(overflow)
		s.saveRun(c, ch)
		return summary, ch
	}
	s.sendTo(end.Player, protocol.RunEnded(summary))
	// The gear and bag already lie on the ground, so the next lobby starts
	// from the starter kit. The stash is untouched.
	ch.Loadout, ch.Stash = s.starterKit(), c.stash
	s.saveRun(c, ch)
	s.world.RemovePlayer(end.Player)
	s.toLobby(end.Player, ch)
	return summary, ch
}

func (s *Server) sendTo(id sim.EntityID, msg *gamev1.ServerMessage) {
	if c, ok := s.clients[id]; ok {
		s.sendToClient(c, msg)
	}
}

func (s *Server) broadcast(msg *gamev1.ServerMessage) {
	s.broadcastExcept(0, msg)
}

func (s *Server) broadcastExcept(skip sim.EntityID, msg *gamev1.ServerMessage) {
	if len(s.clients) == 0 {
		return
	}
	b, err := protocol.EncodeServer(msg)
	if err != nil {
		s.log.Error("encode server message", "err", err)
		return
	}
	for id, c := range s.clients {
		if id != skip {
			c.send(b)
		}
	}
}

// enter takes the run lease on the player's writer, off the tick, and
// spawns them with what the store handed back. Lobby saves queued before it
// land first, so that is the lobby the player sees.
func (s *Server) enter(c *client) {
	runID := items.NewID()
	name, class, instance := c.name, c.class, s.Instance()
	var ch inventory.Character
	c.entering = true
	c.writes.push(storeWrite{op: "start run", call: func(ctx context.Context) (err error) {
		ch, err = s.Store.StartRun(ctx, name, class, runID, instance)
		return err
	}, done: func(_ *client, err error) {
		c.entering = false
		if err != nil {
			reason := MoveSaving
			if errors.Is(err, inventory.ErrInRun) {
				reason = MoveInRun
			}
			if !c.gone {
				s.sendToClient(c, protocol.EnterInstanceRejected(reason))
			}
			return
		}
		c.runID = runID
		if c.gone {
			// The player left while the lease was taken. Give it back with what
			// they took in.
			s.saveRun(c, ch)
			return
		}
		s.spawn(c, ch)
	}})
}

// spawn puts a player in the cave with everything they have on them.
func (s *Server) spawn(c *client, ch inventory.Character) {
	spawn := s.cave.PlayerSpawns[s.spawns%len(s.cave.PlayerSpawns)]
	id, err := s.world.AddPlayerAs(spawn, c.class, ch.XP)
	if err != nil {
		s.log.Error("enter instance", "name", c.name, "err", err)
		s.saveRun(c, ch)
		return
	}
	if err := s.world.Restore(id, ch.Loadout); err != nil {
		s.log.Error("restore loadout", "player", id, "err", err)
	}
	s.spawns++
	c.id, c.stash, c.lobby = id, ch.Stash, nil
	s.clients[id] = c
	p, _ := s.world.Player(id)
	class := protocol.ClassInfo(s.world.Classes(), p.Class)
	character := protocol.Character(s.world.Classes().Progression, c.name, p.Class, p.Level, p.XP, p.RunXP)
	s.sendToClient(c, protocol.Joined(id, s.cave, spawn, s.TickRate, class, character))
	s.sendItems(id)
	s.log.Info("player entered the cave", "player", id, "name", c.name, "run", c.runID)
}

func (s *Server) command(c *client, msg *gamev1.ClientMessage) {
	if c.id == 0 {
		return
	}
	if cmd, ok := protocol.Command(msg, c.id); ok {
		s.world.Submit(cmd)
	}
}

// leave ends a run in progress as a death would for XP, but keeps what the
// player carried until the 30 s rule in FOM-217.
func (s *Server) leave(c *client) {
	if c.id == 0 {
		return
	}
	p, _ := s.world.Player(c.id)
	if end, ok := s.world.RemovePlayer(c.id); ok {
		s.saveRun(c, inventory.Character{Name: c.name, Class: c.class, XP: end.XP, Loadout: p.Loadout(), Stash: c.stash})
	}
	delete(s.clients, c.id)
}

// staleLease is true for any lease but this process's own. Until FOM-228
// there is one instance server, so a lease from another process is one that
// died with its run.
func (s *Server) staleLease(l inventory.Lease) bool {
	return l.Instance != s.Instance()
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
