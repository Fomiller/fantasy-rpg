package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

type worldHarness struct {
	url   string
	lobby *Lobby
	store *spyStore
}

// serveWorld runs a world server with no instances, with lease checks
// every 20 ms instead of 2 s.
func serveWorld(t *testing.T, configure ...func(*Lobby)) worldHarness {
	t.Helper()
	loot := items.NewRoller(items.Default(), 1)
	l := NewLobby(sim.NewWorld(sim.DefaultConfig(), nil), loot, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spy := newSpy()
	l.Store = spy
	l.leaseRetry = 20 * time.Millisecond
	for _, f := range configure {
		f(l)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go l.Run(ctx)
	hs := httptest.NewServer(http.HandlerFunc(l.HandleWS))
	t.Cleanup(func() {
		hs.Close()
		cancel()
		l.Close()
	})
	return worldHarness{url: "ws" + strings.TrimPrefix(hs.URL, "http"), lobby: l, store: spy}
}

func (h worldHarness) saved(t *testing.T, name string, match func(inventory.Character) bool) inventory.Character {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, ok, err := h.store.Load(context.Background(), name, classes.Starter)
		if err == nil && ok && match(c) {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved %s = %+v, %v, %v", name, c, ok, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func enterRejected(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_EnterInstance{EnterInstance: &gamev1.EnterInstanceRequest{}}})
	return waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEnterInstanceRejected() != nil }).GetEnterInstanceRejected().GetReason()
}

// A new name on the world server gets a starter kit, the lobby and the
// character screen's gear and stats, with no sim behind it.
func TestWorldJoinOpensTheLobby(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobby := lobbyAs(t, conn, "ann")
	if lobby.GetSaving() || lobby.GetCharacter().GetName() != "ann" || lobby.GetCharacter().GetLevel() != 1 || lobby.GetClassInfo().GetId() != classes.Starter {
		t.Fatalf("lobby = %v", lobby)
	}
	eq, inv, st := lobbyItems(t, conn)
	if len(eq.GetItems()) != len(items.StarterKit) || len(eq.GetStats()) == 0 || len(inv.GetItems()) != 0 || len(st.GetItems()) != 0 || st.GetCapacity() != inventory.StashSize {
		t.Fatalf("lobby items: gear %v, bag %v, stash %v", eq, inv, st)
	}
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Loadout.Equipment) == len(items.StarterKit) })
}

func TestWorldStashMoveIsSaved(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	id := eq.GetItems()[0].GetId()

	moveTo(t, conn, id, toStash)
	if _, _, st := lobbyItems(t, conn); len(st.GetItems()) != 1 || st.GetItems()[0].GetId() != id {
		t.Fatalf("stash = %v, want the moved item", st.GetItems())
	}
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Stash) == 1 && c.Stash[0].ID == id })

	moveTo(t, conn, id, toGear)
	h.saved(t, "ann", func(c inventory.Character) bool {
		return len(c.Stash) == 0 && len(c.Loadout.Equipment) == len(items.StarterKit)
	})
}

// With no instance to send anyone to, Enter Cave is full and no lease is
// ever taken.
func TestWorldEnterIsFull(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	if r := enterRejected(t, conn); r != EnterFull {
		t.Fatalf("enter rejected = %q, want full", r)
	}
	if slices.Contains(h.store.calls(), "start run") {
		t.Fatalf("store calls = %v, want no lease", h.store.calls())
	}
}

// A join while a run holds the account shows the lobby saving and turns
// moves away. Once the run end lands the lobby opens on what it banked. A
// world server that knows nothing about the lease's instance never clears it.
func TestWorldWaitsForTheRun(t *testing.T) {
	h := serveWorld(t)
	ctx := context.Background()
	if err := h.store.Create(ctx, inventory.Character{Name: "ann", Class: classes.Starter, Loadout: h.lobby.starterKit()}); err != nil {
		t.Fatal(err)
	}
	run := items.NewID()
	c, err := h.store.Memory.StartRun(ctx, "ann", classes.Starter, run, "instance-0@3f9a1c")
	if err != nil {
		t.Fatal(err)
	}

	conn := dial(t, h.url)
	if lobby := lobbyAs(t, conn, "ann"); !lobby.GetSaving() {
		t.Fatalf("lobby = %v, want saving", lobby)
	}
	eq, _, _ := lobbyItems(t, conn)
	moveTo(t, conn, eq.GetItems()[0].GetId(), toStash)
	if r := rejected(t, conn); r.GetReason() != MoveInRun {
		t.Fatalf("move rejected = %v, want in_run", r)
	}
	if r := enterRejected(t, conn); r != MoveInRun {
		t.Fatalf("enter rejected = %q, want in_run", r)
	}

	c.XP = 120
	if err := h.store.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	lobby := waitFor(t, conn, saving(false)).GetLobby()
	if lobby.GetCharacter().GetXp() != 120 {
		t.Fatalf("lobby after the run = %v, want the banked xp", lobby)
	}
	eq, _, _ = lobbyItems(t, conn)
	moveTo(t, conn, eq.GetItems()[0].GetId(), toStash)
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Stash) == 1 })
	if slices.Contains(h.store.calls(), "clear run") {
		t.Fatalf("store calls = %v, want the lease left alone", h.store.calls())
	}
}

// A run that starts after the join stops the next lobby save. The move is
// undone and the lobby waits for the run.
func TestWorldSaveInRunIsUndone(t *testing.T) {
	h := serveWorld(t)
	ctx := context.Background()
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	id := eq.GetItems()[0].GetId()
	run := items.NewID()
	c, err := h.store.Memory.StartRun(ctx, "ann", classes.Starter, run, "instance-0@3f9a1c")
	if err != nil {
		t.Fatal(err)
	}

	moveTo(t, conn, id, toStash)
	waitFor(t, conn, saving(true))
	if r := rejected(t, conn); r.GetItemId() != id || r.GetReason() != MoveInRun {
		t.Fatalf("rejected = %v, want in_run", r)
	}
	if eq, _, st := lobbyItems(t, conn); len(eq.GetItems()) != len(items.StarterKit) || len(st.GetItems()) != 0 {
		t.Fatalf("after the rejected save: gear %v, stash %v, want it back", eq.GetItems(), st.GetItems())
	}

	if err := h.store.EndRun(ctx, run, c); err != nil {
		t.Fatal(err)
	}
	waitFor(t, conn, saving(false))
}

func TestWorldNameAlreadyPlaying(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	again := dial(t, h.url)
	send(t, again, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	_, _, err := again.Read(context.Background())
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusPolicyViolation || closed.Reason != "name already playing" {
		t.Fatalf("second join: %v, want name already playing", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
	rejoin(t, h.url, "ann")
}

func TestWorldJoinFailsWhenStoreIsDown(t *testing.T) {
	h := serveWorld(t)
	h.store.loadDown = true
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	_, _, err := conn.Read(context.Background())
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusTryAgainLater || closed.Reason != "characters unavailable" {
		t.Fatalf("got %v, want try again later", err)
	}
}

// Run commands mean nothing on the world server and change nothing.
func TestWorldIgnoresRunCommands(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	lobbyItems(t, conn)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Move{Move: &gamev1.MoveInput{Seq: 1, Direction: &gamev1.Vec2{X: 1}}}})
	if r := enterRejected(t, conn); r != EnterFull {
		t.Fatalf("enter rejected = %q, want full", r)
	}
}

// A lease on an instance that does not answer is waited on, never cleared.
// Once the instance answers with a new boot id, that run is gone and the
// lease is cleared.
func TestWorldClearsALeaseOnlyOnProof(t *testing.T) {
	var boot atomic.Value
	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := boot.Load().(string)
		if b == "" {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(InstanceStatus{ID: "instance-0", Boot: b, Capacity: 8, Runs: []string{}})
	}))
	t.Cleanup(status.Close)
	dir := NewDirectory([]Instance{{ID: "instance-0", Internal: status.URL, Public: "ws://unused"}})
	h := serveWorld(t, func(l *Lobby) { l.Instances = dir })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dir.Run(ctx, 10*time.Millisecond)

	if err := h.store.Create(ctx, inventory.Character{Name: "ann", Class: classes.Starter, Loadout: h.lobby.starterKit()}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Memory.StartRun(ctx, "ann", classes.Starter, items.NewID(), "instance-0@3f9a1c"); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.url)
	if lobby := lobbyAs(t, conn, "ann"); !lobby.GetSaving() {
		t.Fatalf("lobby = %v, want saving", lobby)
	}
	time.Sleep(100 * time.Millisecond)
	if slices.Contains(h.store.calls(), "clear run") {
		t.Fatalf("store calls = %v, want no clear while the instance is down", h.store.calls())
	}

	boot.Store("000000")
	waitFor(t, conn, saving(false))
	if !slices.Contains(h.store.calls(), "clear run") {
		t.Fatalf("store calls = %v, want the dead lease cleared", h.store.calls())
	}
}
