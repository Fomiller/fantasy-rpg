package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/gameserver"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// The world server's ticket points at this instance, and its token works
// once.
func TestTicketJoinsTheInstance(t *testing.T) {
	h := start(t)
	world := dial(t, h.url)
	lobbyAs(t, world, "ann")
	ticket := enterTicket(t, world)
	if ticket.GetUrl() != h.instanceURL() || ticket.GetInstance() != h.srv.ID || ticket.GetExpiresInMs() != uint32(jointoken.Lifetime.Milliseconds()) {
		t.Fatalf("ticket = %v", ticket)
	}
	conn := dial(t, ticket.GetUrl())
	sendToken(t, conn, ticket.GetToken())
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetJoined() != nil })

	again := dial(t, ticket.GetUrl())
	sendToken(t, again, ticket.GetToken())
	wantClose(t, again, websocket.StatusPolicyViolation, "token used")
}

func TestInstanceTurnsBadJoinsAway(t *testing.T) {
	h := start(t)
	seed(t, h, inventory.Character{Name: "ann", Loadout: h.srv.starterKit()})
	wrong, err := jointoken.Sign(testKey, jointoken.Claims{Name: "ann", Class: classes.Starter, Inst: "instance-1@" + h.srv.boot, Run: items.NewID()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	reboot, err := jointoken.Sign(testKey, jointoken.Claims{Name: "ann", Class: classes.Starter, Inst: h.srv.ID + "@000000", Run: items.NewID()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	forged, err := jointoken.Sign([]byte("not the key"), jointoken.Claims{Name: "ann", Class: classes.Starter, Inst: h.srv.Instance(), Run: items.NewID()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, token, reason string
	}{
		{"garbage", "nope", "bad token"},
		{"other key", forged, "bad token"},
		{"expired", h.token(t, "ann", items.NewID(), time.Now().Add(-jointoken.Lifetime)), "token expired"},
		{"other instance", wrong, "wrong instance"},
		{"old boot", reboot, "wrong instance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := dial(t, h.instanceURL())
			sendToken(t, conn, tc.token)
			wantClose(t, conn, websocket.StatusPolicyViolation, tc.reason)
		})
	}

	t.Run("join request", func(t *testing.T) {
		conn := dial(t, h.instanceURL())
		send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
		wantClose(t, conn, websocket.StatusPolicyViolation, "join on the world server")
	})
	t.Run("run command first", func(t *testing.T) {
		conn := dial(t, h.instanceURL())
		send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Move{Move: &gamev1.MoveInput{Seq: 1}}})
		wantClose(t, conn, websocket.StatusPolicyViolation, "first message must be instance join")
	})
	if l := leaseOf(t, h.srv.Store, "ann"); l.Held() {
		t.Fatalf("lease = %+v, want no bad join to take it", l)
	}
}

// A token for a run that already ended cannot start it again, even on a
// process that has never seen it.
func TestEndedRunCannotStartAgain(t *testing.T) {
	h := start(t)
	seed(t, h, inventory.Character{Name: "ann", Loadout: h.srv.starterKit()})
	ctx := context.Background()
	run := items.NewID()
	c, err := h.srv.Store.StartRun(ctx, "ann", classes.Starter, run, h.srv.Instance())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Store.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.instanceURL())
	sendToken(t, conn, h.token(t, "ann", run, time.Now()))
	wantClose(t, conn, websocket.StatusPolicyViolation, "token used")
}

// A lease held by a run elsewhere stops the join after the token checks out.
func TestJoinWhileInRunElsewhere(t *testing.T) {
	h := start(t)
	seed(t, h, inventory.Character{Name: "ann", Loadout: h.srv.starterKit()})
	if _, err := h.srv.Store.StartRun(context.Background(), "ann", classes.Starter, items.NewID(), "instance-1@3f9a1c"); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.instanceURL())
	sendToken(t, conn, h.token(t, "ann", items.NewID(), time.Now()))
	wantClose(t, conn, websocket.StatusPolicyViolation, "already in a run")
}

// A full instance turns a token away. The world server stops sending
// players to it once it counts them.
func TestFullInstance(t *testing.T) {
	h := startCave(t, testCave(), sim.DefaultConfig(), func(s *Server) { s.MaxPlayers = 1 })
	join(t, h)
	seed(t, h, inventory.Character{Name: "bob", Loadout: h.srv.starterKit()})
	conn := dial(t, h.instanceURL())
	sendToken(t, conn, h.token(t, "bob", items.NewID(), time.Now()))
	wantClose(t, conn, websocket.StatusPolicyViolation, "instance full")

	world := dial(t, h.url)
	lobbyAs(t, world, "bob")
	if r := enterRejected(t, world); r != EnterFull {
		t.Fatalf("enter rejected = %q, want full", r)
	}
}

func instanceStatus(t *testing.T, h harness) InstanceStatus {
	t.Helper()
	resp, err := http.Get(h.world.Instances.(*Directory).list[0].Internal + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st InstanceStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

// /status counts players and lists every run the process holds, until the
// run end lands.
func TestStatusListsRuns(t *testing.T) {
	spy := newSpy()
	spy.hold = make(chan struct{})
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	if st := instanceStatus(t, h); st.ID != h.srv.ID || st.Boot != h.srv.boot || st.Players != 0 || st.Capacity != 8 || len(st.Runs) != 0 {
		t.Fatalf("empty status = %+v", st)
	}
	conn, _ := joinAs(t, h, "ann")
	lease := leaseOf(t, spy.Memory, "ann")
	if st := instanceStatus(t, h); st.Players != 1 || !slices.Equal(st.Runs, []string{lease.RunID}) {
		t.Fatalf("status in a run = %+v, want ann's run %s", st, lease.RunID)
	}

	conn.Close(websocket.StatusNormalClosure, "")
	waitCalls(t, spy, []string{"load", "create", "load", "load", "start run", "end run"})
	if st := instanceStatus(t, h); st.Players != 0 || !slices.Equal(st.Runs, []string{lease.RunID}) {
		t.Fatalf("status with the run end held = %+v, want the run still listed", st)
	}
	close(spy.hold)
	deadline := time.Now().Add(2 * time.Second)
	for len(instanceStatus(t, h).Runs) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("status = %+v, want the run gone once it landed", instanceStatus(t, h))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The instance builds a fresh world once the last player's run end lands,
// and not while anyone is still in.
func TestEmptyInstanceResets(t *testing.T) {
	var resets atomic.Int32
	h := startCave(t, testCave(), sim.DefaultConfig(), func(s *Server) {
		s.Reset = func() (*sim.World, *items.Roller) {
			resets.Add(1)
			loot := items.NewRoller(items.Default(), uint64(resets.Load()))
			return testCave().NewWorld(sim.DefaultConfig(), loot), loot
		}
	})
	a, _ := join(t, h)
	b, _ := join(t, h)
	a.Close(websocket.StatusNormalClosure, "")
	waitFor(t, b, func(m *gamev1.ServerMessage) bool {
		return m.GetSnapshot() != nil && len(m.GetSnapshot().GetEntities()) == 2
	})
	if n := resets.Load(); n != 0 {
		t.Fatalf("resets with a player in = %d", n)
	}

	b.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for resets.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("resets = %d, want one once the instance emptied", resets.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The new world has no test enemy, only what the cave places.
	conn, _ := join(t, h)
	snap := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetSnapshot() != nil }).GetSnapshot()
	if len(snap.GetEntities()) != 1 {
		t.Fatalf("entities in the new world = %v, want only the player", snap.GetEntities())
	}
	time.Sleep(50 * time.Millisecond)
	if n := resets.Load(); n != 1 {
		t.Fatalf("resets = %d, want no more while the new player is in", n)
	}
}

func listedRuns(t *testing.T, h harness) []string {
	t.Helper()
	runs, err := h.srv.GameServer.Runs()
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

// A run holds a seat on the runs list until its run end lands.
func TestRunsListHoldsTheSeat(t *testing.T) {
	spy := newSpy()
	spy.hold = make(chan struct{})
	h := serveWith(t, testCave(), calm(sim.DefaultConfig()), spy)
	conn, _ := joinAs(t, h, "ann")
	lease := leaseOf(t, spy.Memory, "ann")
	if runs := listedRuns(t, h); !slices.Equal(runs, []string{lease.RunID}) {
		t.Fatalf("runs in a run = %v, want %s", runs, lease.RunID)
	}
	conn.Close(websocket.StatusNormalClosure, "")
	waitCalls(t, spy, []string{"load", "create", "load", "load", "start run", "end run"})
	if runs := listedRuns(t, h); !slices.Equal(runs, []string{lease.RunID}) {
		t.Fatalf("runs with the run end held = %v, want the seat kept", runs)
	}
	close(spy.hold)
	deadline := time.Now().Add(2 * time.Second)
	for len(listedRuns(t, h)) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("runs = %v, want the seat freed once the run end landed", listedRuns(t, h))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A run started before SIGTERM lands its run end. The drain turns new joins
// away and, once it times out, ends the run as a disconnect does.
func TestDrainLandsTheRunEnd(t *testing.T) {
	h := start(t)
	conn, _ := joinAs(t, h, "ann")
	if !leaseOf(t, h.srv.Store, "ann").Held() {
		t.Fatal("ann has no lease")
	}

	drained := make(chan struct{})
	go func() {
		h.srv.Drain(context.Background(), 300*time.Millisecond)
		close(drained)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !instanceStatus(t, h).Draining {
		if time.Now().After(deadline) {
			t.Fatal("status never said draining")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if capacity := h.srv.GameServer.(*gameserver.Local).RunCapacity(); capacity != 0 {
		t.Fatalf("run capacity while draining = %d, want 0", capacity)
	}
	seed(t, h, inventory.Character{Name: "bob", Loadout: h.srv.starterKit()})
	late := dial(t, h.instanceURL())
	sendToken(t, late, h.token(t, "bob", items.NewID(), time.Now()))
	wantClose(t, late, websocket.StatusTryAgainLater, "instance draining")

	wantClose(t, conn, websocket.StatusGoingAway, "instance draining")
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("drain never returned")
	}
	if l := leaseOf(t, h.srv.Store, "ann"); l.Held() {
		t.Fatalf("lease after the drain = %+v, want the run end landed", l)
	}
	if runs := listedRuns(t, h); len(runs) != 0 {
		t.Fatalf("runs after the drain = %v, want none", runs)
	}
}

// With no one in, the drain is done at once.
func TestDrainOnEmptyReturns(t *testing.T) {
	h := start(t)
	done := make(chan struct{})
	go func() {
		h.srv.Drain(context.Background(), time.Minute)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain on an empty instance waited")
	}
}
