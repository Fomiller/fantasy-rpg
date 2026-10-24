package server

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus/testutil"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// seed saves a character before it joins, as a returning player.
func seed(t *testing.T, h harness, c inventory.Character) {
	t.Helper()
	if c.Class == "" {
		c.Class = classes.Starter
	}
	if err := h.srv.Store.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

// waitSaved polls the store, since writes land after the tick that made them.
func waitSaved(t *testing.T, h harness, name string, match func(inventory.Character) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, ok, err := h.srv.Store.Load(context.Background(), name, classes.Starter)
		if err == nil && ok && match(c) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved %s = %+v, %v, %v", name, c, ok, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var errDown = errors.New("store is down")

// spyStore records each call and can fail or hold them.
type spyStore struct {
	*inventory.Memory
	mu sync.Mutex
	// ops is every call in order.
	ops            []string
	loadDown       bool
	endRunFails    int
	saveLobbyFails int
	// hold, when set, makes EndRun wait until it is closed.
	hold chan struct{}
}

func newSpy() *spyStore { return &spyStore{Memory: inventory.NewMemory()} }

func (s *spyStore) record(op string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, op)
}

func (s *spyStore) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ops)
}

func (s *spyStore) Load(ctx context.Context, name, class string) (inventory.Character, bool, error) {
	s.record("load")
	s.mu.Lock()
	down := s.loadDown
	s.mu.Unlock()
	if down {
		return inventory.Character{}, false, errDown
	}
	return s.Memory.Load(ctx, name, class)
}

func (s *spyStore) Create(ctx context.Context, c inventory.Character) error {
	s.record("create")
	return s.Memory.Create(ctx, c)
}

func (s *spyStore) SaveLobby(ctx context.Context, name, class string, l inventory.Loadout, stash []items.Instance) error {
	s.record("save lobby")
	s.mu.Lock()
	fail := s.saveLobbyFails > 0
	if fail {
		s.saveLobbyFails--
	}
	s.mu.Unlock()
	if fail {
		return errDown
	}
	return s.Memory.SaveLobby(ctx, name, class, l, stash)
}

func (s *spyStore) failEndRuns(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endRunFails = n
}

func (s *spyStore) count(op string) int {
	return len(slices.DeleteFunc(s.calls(), func(o string) bool { return o != op }))
}

func (s *spyStore) EndRun(ctx context.Context, runID string, c inventory.Character) error {
	s.record("end run")
	s.mu.Lock()
	hold, fail := s.hold, s.endRunFails > 0
	if fail {
		s.endRunFails--
	}
	s.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if fail {
		return errDown
	}
	return s.Memory.EndRun(ctx, runID, c)
}

func waitCalls(t *testing.T, spy *spyStore, want []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !slices.Equal(spy.calls(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("store calls = %v, want %v", spy.calls(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A run writes nothing until it ends. A disconnect ends it with one write
// that keeps what the player carried.
func TestDisconnectInRunIsOneWrite(t *testing.T) {
	spy := newSpy()
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	sword := waitFor(t, conn, inventoryWith("rusty_sword")).GetInventory().GetItems()[0]
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Equip{Equip: &gamev1.EquipRequest{ItemId: sword.GetId()}}})
	waitFor(t, conn, equipmentWith("rusty_sword"))
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	waitFor(t, conn, inventoryWith("rusty_sword"))
	if got := spy.calls(); !slices.Equal(got, []string{"load", "create", "load"}) {
		t.Fatalf("store calls in a run = %v, want only the join", got)
	}

	conn.Close(websocket.StatusNormalClosure, "")
	waitCalls(t, spy, []string{"load", "create", "load", "end run"})
	waitSaved(t, h, "ann", func(c inventory.Character) bool {
		return len(c.Loadout.Items) == 1 && c.Loadout.Items[0].ID == sword.GetId() && len(c.Loadout.Equipment) == len(items.StarterKit)-1
	})
}

func TestRunEndRetries(t *testing.T) {
	spy := newSpy()
	spy.endRunFails = 2
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	h := serveWith(t, cave, calm(cfg), spy)

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil })
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return c.XP == cfg.Classes.Progression.ExtractBonus })
	ends := slices.DeleteFunc(spy.calls(), func(op string) bool { return op != "end run" })
	if len(ends) != 3 {
		t.Fatalf("run end calls = %d, want two that fail and one that lands", len(ends))
	}
}

// A store that cannot load turns the join away. It never hands out a
// starter kit that would later save over real gear.
func TestJoinFailsWhenStoreIsDown(t *testing.T) {
	spy := newSpy()
	spy.loadDown = true
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	_, _, err := conn.Read(context.Background())
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusTryAgainLater || closed.Reason != "characters unavailable" {
		t.Fatalf("got %v, want try again later", err)
	}
	if slices.Contains(spy.calls(), "create") {
		t.Fatalf("store calls = %v, want no starter kit", spy.calls())
	}

	spy.mu.Lock()
	spy.loadDown = false
	spy.mu.Unlock()
	rejoin(t, h.url, "ann")
}

// A slow run end write stalls nobody else. A rejoin before it lands gets the
// lobby from the run, not the old character from the store.
func TestSlowRunEndServesTheRejoin(t *testing.T) {
	spy := newSpy()
	spy.hold = make(chan struct{})
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	other := dial(t, h.url)
	join(t, other)

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	waitFor(t, conn, inventoryWith("rusty_sword"))
	conn.Close(websocket.StatusNormalClosure, "")
	waitCalls(t, spy, []string{"load", "create", "load", "load", "create", "load", "end run"})

	for range 5 {
		waitFor(t, other, func(m *gamev1.ServerMessage) bool { return m.GetSnapshot() != nil })
	}
	back, lobby := rejoinLobby(t, h.url, "ann")
	if !lobby.GetSaving() {
		t.Fatalf("lobby on rejoin = %v, want saving", lobby)
	}
	if inv := waitFor(t, back, func(m *gamev1.ServerMessage) bool { return m.GetInventory() != nil }).GetInventory(); len(inv.GetItems()) != 1 {
		t.Fatalf("bag after rejoin = %v, want the sword from the run", inv.GetItems())
	}

	close(spy.hold)
	waitFor(t, back, saving(false))
	if n := spy.count("load"); n != 4 {
		t.Fatalf("loads = %d, want only the two first joins", n)
	}
}

// A second connection for a name that is still playing is turned away.
func TestNameAlreadyPlaying(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	again := dial(t, h.url)
	send(t, again, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	_, _, err := again.Read(context.Background())
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusPolicyViolation || closed.Reason != "name already playing" {
		t.Fatalf("second join: %v, want name already playing", err)
	}
}

// extractAtSpawn starts a server where every run extracts at once, and run
// ends go pending after 50 ms instead of 10 s.
func extractAtSpawn(t *testing.T, spy *spyStore) (harness, sim.Config) {
	t.Helper()
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	h := serveWith(t, cave, calm(cfg), spy, func(s *Server) {
		s.endRunPatience, s.pendingRetry = 50*time.Millisecond, 10*time.Millisecond
	})
	return h, cfg
}

func waitPending(t *testing.T, h harness, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for testutil.ToFloat64(h.srv.PendingSaves) != want {
		if time.Now().After(deadline) {
			t.Fatalf("pending saves = %v, want %v", testutil.ToFloat64(h.srv.PendingSaves), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func saving(saving bool) func(*gamev1.ServerMessage) bool {
	return func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil && m.GetLobby().GetSaving() == saving }
}

// A run end that fails past its patience is kept and retried. Until it lands
// the lobby says saving and turns away moves and Enter Cave.
func TestPendingRunEndHoldsTheLobby(t *testing.T) {
	spy := newSpy()
	spy.endRunFails = math.MaxInt
	h, cfg := extractAtSpawn(t, spy)

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil })
	waitFor(t, conn, saving(true))
	eq, _, _ := lobbyItems(t, conn)
	waitPending(t, h, 1)
	tries := spy.count("end run")
	deadline := time.Now().Add(2 * time.Second)
	for spy.count("end run") < tries+3 {
		if time.Now().After(deadline) {
			t.Fatalf("run end calls = %d, want retries past the patience", spy.count("end run"))
		}
		time.Sleep(10 * time.Millisecond)
	}

	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_EnterInstance{EnterInstance: &gamev1.EnterInstanceRequest{}}})
	if r := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEnterInstanceRejected() != nil }).GetEnterInstanceRejected(); r.GetReason() != MoveSaving {
		t.Fatalf("enter instance rejected = %v, want saving", r)
	}
	moveTo(t, conn, eq.GetItems()[0].GetId(), toStash)
	if r := rejected(t, conn); r.GetReason() != MoveSaving {
		t.Fatalf("move rejected = %v, want saving", r)
	}
	if slices.Contains(spy.calls(), "save lobby") {
		t.Fatalf("store calls = %v, want no lobby save on top of the run", spy.calls())
	}

	spy.failEndRuns(0)
	waitFor(t, conn, saving(false))
	waitPending(t, h, 0)
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return c.XP == cfg.Classes.Progression.ExtractBonus })
	enterInstance(t, conn)
}

// A rejoin while the run end is pending opens the lobby from it, without a
// load, and the save keeps going.
func TestRejoinWithPendingSave(t *testing.T) {
	spy := newSpy()
	spy.endRunFails = math.MaxInt
	h, cfg := extractAtSpawn(t, spy)

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	waitFor(t, conn, saving(true))
	waitPending(t, h, 1)
	conn.Close(websocket.StatusNormalClosure, "")
	spy.mu.Lock()
	spy.loadDown = true
	spy.mu.Unlock()
	loads := spy.count("load")

	back, lobby := rejoinLobby(t, h.url, "ann")
	if !lobby.GetSaving() || lobby.GetCharacter().GetXp() != uint32(cfg.Classes.Progression.ExtractBonus) {
		t.Fatalf("lobby on rejoin = %v, want the pending run, still saving", lobby)
	}
	if n := spy.count("load"); n != loads {
		t.Fatalf("loads = %d, want none on a rejoin with a pending save", n-loads)
	}

	spy.mu.Lock()
	spy.loadDown = false
	spy.mu.Unlock()
	spy.failEndRuns(0)
	waitFor(t, back, saving(false))
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return c.XP == cfg.Classes.Progression.ExtractBonus })
}

// A lobby save that fails puts the item back and says why.
func TestFailedLobbySaveIsUndone(t *testing.T) {
	spy := newSpy()
	spy.saveLobbyFails = 1
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	id := eq.GetItems()[0].GetId()

	moveTo(t, conn, id, toStash)
	if st := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetStash() != nil }).GetStash(); len(st.GetItems()) != 1 {
		t.Fatalf("stash after the move = %v", st.GetItems())
	}
	if r := rejected(t, conn); r.GetItemId() != id || r.GetReason() != MoveSaving {
		t.Fatalf("rejected = %v, want saving", r)
	}
	if eq, _, st := lobbyItems(t, conn); len(eq.GetItems()) != len(items.StarterKit) || len(st.GetItems()) != 0 {
		t.Fatalf("after the failed save: gear %v, stash %v, want it back", eq.GetItems(), st.GetItems())
	}

	moveTo(t, conn, id, toStash)
	waitSaved(t, h, "ann", func(c inventory.Character) bool { return len(c.Stash) == 1 && c.Stash[0].ID == id })
}
