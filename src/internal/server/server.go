// Package server connects WebSocket clients to the game. Lobby is the world
// server: join, characters, the lobby, the stash, and tickets into an
// instance. Server is an instance server: one sim.World and its tick loop,
// which a player joins only with a ticket from the world server. They share
// how connections and store writes are handled.
//
// State is only touched from one goroutine: connections hand work to it
// through an inbox, and it hands encoded messages back through each
// client's outbox.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// Close reasons an instance server sends.
const (
	reasonJoinOnWorld = "join on the world server"
	reasonInRun       = "already in a run"
	reasonFull        = "instance full"
	reasonRunOver     = "run over"
)

var (
	errJoinOnWorld = errors.New(reasonJoinOnWorld)
	errInRun       = errors.New(reasonInRun)
	errFull        = errors.New(reasonFull)
)

// Server is an instance server. A player joins with a token from the world
// server and plays one run. When it ends the socket closes and the client
// goes back to the world server.
type Server struct {
	*Lobby
	// TickRate is sent to clients on join. It must match the tick loop.
	TickRate int
	// ID names this instance in tokens and the run lease, with a boot id that
	// is new on every start.
	ID string
	// MaxPlayers counts players in the cave and on their way in.
	MaxPlayers int
	// Tokens checks join tokens. Set it before serving.
	Tokens *jointoken.Verifier
	// Reset builds the world for the next group once the instance empties,
	// so nobody walks into a looted cave. Nil keeps the old world.
	Reset   func() (*sim.World, *items.Roller)
	boot    string
	world   *sim.World
	cave    caves.Cave
	spawns  int
	clients map[sim.EntityID]*client
	// used is set once anyone has played in this world.
	used bool
}

func New(world *sim.World, cave caves.Cave, loot *items.Roller, log *slog.Logger) *Server {
	return &Server{
		Lobby:      NewLobby(world, loot, log),
		TickRate:   20,
		ID:         "instance-0",
		MaxPlayers: 8,
		boot:       bootID(),
		world:      world,
		cave:       cave,
		clients:    map[sim.EntityID]*client{},
	}
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
	s.resetIfEmpty()

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
	// come after the XP above and before the close.
	summaries := map[sim.EntityID]*gamev1.RunSummary{}
	for _, end := range events.RunEnded {
		summaries[end.Player] = s.endRun(end)
	}
	for _, id := range events.Extracted {
		s.sendTo(id, protocol.Extracted(id, summaries[id]))
		s.broadcastExcept(id, protocol.Extracted(id, nil))
		s.finish(id)
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

// endRun tells the player about any new level and queues the run's result
// as one write, without waiting for it. A death's summary goes out here and
// the run is over. An extraction's summary goes out with Extracted instead.
func (s *Server) endRun(end sim.RunEnd) *gamev1.RunSummary {
	c, ok := s.clients[end.Player]
	if !ok {
		return nil
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
		return summary
	}
	s.sendTo(end.Player, protocol.RunEnded(summary))
	// The gear and bag already lie on the ground, so the next lobby starts
	// from the starter kit. The stash is untouched.
	ch.Loadout, ch.Stash = s.starterKit(), c.stash
	s.saveRun(c, ch)
	s.world.RemovePlayer(end.Player)
	s.finish(end.Player)
	return summary
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

// HandleWS lets in a player the world server sent. The first message must
// be InstanceJoinRequest with a token for this process.
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.log.Warn("websocket accept", "err", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	claims, err := s.readToken(ctx, conn)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	c := newClient(claims.Name, claims.Class, ctx, cancel)
	c.runID = claims.Run
	claimed := make(chan error, 1)
	ok := s.enqueue(func() {
		// A name still here has a run that has not ended, so its lease is held.
		if _, ok := s.conns[c.name]; ok {
			claimed <- errInRun
			return
		}
		if s.players() >= s.MaxPlayers {
			claimed <- errFull
			return
		}
		s.conns[c.name] = c
		c.entering = true
		claimed <- nil
	})
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	// Wait even if the client goes, or a late claim would hold the name forever.
	select {
	case err = <-claimed:
	case <-s.done:
		return
	}
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	log := s.log.With("name", c.name, "run", c.runID)
	go s.saveLoop(c.name, c.writes)
	defer func() {
		s.enqueue(func() { s.leave(c) })
		log.Info("player left")
	}()
	if !s.enqueue(func() { s.startRun(c) }) {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}

	go s.writeLoop(ctx, conn, c, cancel)
	s.readLoop(ctx, conn, log, func(msg *gamev1.ClientMessage) { s.handle(c, msg) })
}

func (s *Server) readToken(ctx context.Context, conn *websocket.Conn) (jointoken.Claims, error) {
	ctx, cancel := context.WithTimeout(ctx, joinTimeout)
	defer cancel()
	_, b, err := conn.Read(ctx)
	if err != nil {
		return jointoken.Claims{}, err
	}
	msg, err := protocol.DecodeClient(b)
	switch {
	case err != nil:
		return jointoken.Claims{}, err
	case msg.GetJoin() != nil:
		return jointoken.Claims{}, errJoinOnWorld
	case msg.GetInstanceJoin() == nil:
		return jointoken.Claims{}, errors.New("first message must be instance join")
	}
	return s.Tokens.Check(msg.GetInstanceJoin().GetToken(), s.Instance(), time.Now())
}

// players counts everyone in the cave or taking the lease to get in.
func (s *Server) players() int {
	n := 0
	for _, c := range s.conns {
		if c.entering || c.id != 0 {
			n++
		}
	}
	return n
}

// startRun takes the lease for the token's run on the player's writer, off
// the tick. It is the check that holds across restarts: a run id that
// already ended is refused.
func (s *Server) startRun(c *client) {
	name, class, runID, instance := c.name, c.class, c.runID, s.Instance()
	var ch inventory.Character
	c.writes.push(storeWrite{op: "start run", call: func(ctx context.Context) (err error) {
		ch, err = s.Store.StartRun(ctx, name, class, runID, instance)
		return err
	}, done: func(_ *client, err error) {
		c.entering = false
		switch {
		case errors.Is(err, inventory.ErrInRun):
			c.close(websocket.StatusPolicyViolation, reasonInRun)
			return
		case errors.Is(err, inventory.ErrRunUsed):
			c.close(websocket.StatusPolicyViolation, jointoken.ErrUsed.Error())
			return
		case err != nil:
			c.close(websocket.StatusTryAgainLater, "characters unavailable")
			return
		}
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
	s.used = true
	c.id, c.stash = id, ch.Stash
	s.clients[id] = c
	p, _ := s.world.Player(id)
	class := protocol.ClassInfo(s.world.Classes(), p.Class)
	character := protocol.Character(s.world.Classes().Progression, c.name, p.Class, p.Level, p.XP, p.RunXP)
	s.sendToClient(c, protocol.Joined(id, s.cave, spawn, s.TickRate, class, character))
	s.sendItems(id)
	s.log.Info("player entered the cave", "player", id, "name", c.name, "run", c.runID)
}

// handle runs on the loop. The stash is the world server's, so a move is
// turned away here.
func (s *Server) handle(c *client, msg *gamev1.ClientMessage) {
	if move := msg.GetStashMove(); move != nil {
		s.sendToClient(c, protocol.StashMoveRejected(move.GetItemId(), MoveNotInLobby))
		return
	}
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
	c.gone = true
	if c.id != 0 {
		p, _ := s.world.Player(c.id)
		if end, ok := s.world.RemovePlayer(c.id); ok {
			s.saveRun(c, inventory.Character{Name: c.name, Class: c.class, XP: end.XP, Loadout: p.Loadout(), Stash: c.stash})
		}
		delete(s.clients, c.id)
		c.id = 0
	}
	c.writes.close()
}

// finish closes the socket of a player whose run is over. The run end is
// queued already and lands without them.
func (s *Server) finish(id sim.EntityID) {
	c, ok := s.clients[id]
	if !ok {
		return
	}
	delete(s.clients, id)
	c.id = 0
	time.AfterFunc(s.closeGrace, func() { c.close(websocket.StatusNormalClosure, reasonRunOver) })
}

// resetIfEmpty builds a fresh world once the last player is gone and their
// writes have landed. The boot id stays, since it is the same process.
func (s *Server) resetIfEmpty() {
	if !s.used || s.Reset == nil || len(s.conns) > 0 {
		return
	}
	s.world, s.loot = s.Reset()
	s.rules = s.world
	s.used, s.spawns = false, 0
	s.log.Info("instance reset")
}

// HandleStatus answers the world server's poll. It is on the internal port
// only, never behind the public route.
func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	st := make(chan InstanceStatus, 1)
	if !s.enqueue(func() { st <- s.status() }) {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	select {
	case v := <-st:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	case <-r.Context().Done():
	case <-s.done:
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
	}
}

func (s *Server) status() InstanceStatus {
	st := InstanceStatus{ID: s.ID, Boot: s.boot, Players: s.players(), Capacity: s.MaxPlayers, Runs: []string{}}
	for _, c := range s.conns {
		if c.runID != "" {
			st.Runs = append(st.Runs, c.runID)
		}
	}
	slices.Sort(st.Runs)
	return st
}
