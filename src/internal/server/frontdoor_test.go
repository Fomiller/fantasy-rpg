package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/towns"
)

const otherTown = "ws://other/towns/world-1/ws"

// pickDir picks to for everyone, or is full when to has no id.
type pickDir struct {
	towns.Off
	to towns.Town
}

func (d pickDir) Pick(_ context.Context, _ string, self towns.Town) (towns.Town, error) {
	switch d.to.ID {
	case "":
		return towns.Town{}, towns.ErrFull
	case "self":
		return self, nil
	}
	return d.to, nil
}

// picking runs the shard as world-0 with dir picking to.
func picking(to towns.Town) func(*Lobby) {
	return func(l *Lobby) {
		asShard(l)
		l.closeGrace = 50 * time.Millisecond
		l.Towns = &towns.Shard{Dir: pickDir{to: to}, ID: l.WorldID, Players: l.TownPlayers, Log: quiet()}
	}
}

var world1 = towns.Town{ID: "world-1", URL: otherTown}

func (h worldHarness) shardURL(id string) string {
	return h.url + "/towns/" + id + "/ws"
}

func townTicket(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	return waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetTownTicket() != nil }).GetTownTicket().GetUrl()
}

// The front door sends the player on and claims nothing. The target claims.
func TestFrontDoorSendsATownTicket(t *testing.T) {
	h := serveWorld(t, picking(world1))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.lobby.RunHeartbeat(ctx)

	conn := dial(t, h.url+"/ws")
	joinTown(t, conn, "ann", false)
	if got := townTicket(t, conn); got != otherTown {
		t.Fatalf("ticket url = %q, want %q", got, otherTown)
	}
	wantClose(t, conn, websocket.StatusNormalClosure, reasonChangingTown)
	if calls := h.store.calls(); len(calls) != 0 {
		t.Fatalf("store calls = %v, want none on a redirect", calls)
	}
	if err := h.store.Claim(ctx, elsewhere(t, h.store)); err != nil {
		t.Fatalf("claim on the target = %v, want the name free", err)
	}
}

func TestFrontDoorSeatsWhenThePickIsThisShard(t *testing.T) {
	h := serveWorld(t, picking(towns.Town{ID: "self"}))
	lobbyAs(t, dial(t, h.url+"/ws"), "ann")
}

func TestFrontDoorWithNoRoomIsFull(t *testing.T) {
	h := serveWorld(t, picking(towns.Town{}))
	conn := dial(t, h.url+"/ws")
	joinTown(t, conn, "ann", false)
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonTownFull)
}

// A shard path seats the player even when the directory would pick another
// shard. It never forwards.
func TestShardPathNeverForwards(t *testing.T) {
	h := serveWorld(t, picking(world1))
	lobbyAs(t, dial(t, h.shardURL("world-0")), "ann")
}

func TestShardPathForAnotherShard(t *testing.T) {
	h := serveWorld(t, picking(world1))
	conn := dial(t, h.shardURL("world-1"))
	joinTown(t, conn, "ann", false)
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonWrongTown)
}

func TestShardPathFull(t *testing.T) {
	h := serveWorld(t, picking(world1), func(l *Lobby) { l.TownMaxPlayers = 1 })
	lobbyAs(t, dial(t, h.shardURL("world-0")), "ann")
	deadline := time.Now().Add(2 * time.Second)
	for h.lobby.TownPlayers() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("ann never counted in the town")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn := dial(t, h.shardURL("world-0"))
	joinTown(t, conn, "bob", false)
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonTownFull)
}

func TestShardPathDraining(t *testing.T) {
	h := serveWorld(t, picking(world1))
	h.lobby.townDraining.Store(true)
	conn := dial(t, h.shardURL("world-0"))
	joinTown(t, conn, "ann", false)
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonTownFull)
	if h.lobby.Ready() {
		t.Fatal("a draining shard is ready")
	}
}

// A drain waits for the player's queued lobby save, releases the claim, then
// sends the TownTicket.
func TestDrainMovesPlayersAfterTheirSaves(t *testing.T) {
	h := serveWorld(t, picking(world1))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.lobby.RunHeartbeat(ctx)

	conn := dial(t, h.shardURL("world-0"))
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	moveTo(t, conn, eq.GetItems()[0].GetId(), toStash)
	other := elsewhere(t, h.store)
	if err := h.store.Claim(ctx, other); !errors.Is(err, inventory.ErrPlaying) {
		t.Fatalf("claim while ann plays = %v, want ErrPlaying", err)
	}

	drained := make(chan struct{})
	go func() {
		h.lobby.Drain(ctx)
		close(drained)
	}()
	if got := townTicket(t, conn); got != otherTown {
		t.Fatalf("ticket url = %q, want %q", got, otherTown)
	}
	if c, _, err := h.store.Load(ctx, "ann", classes.Starter); err != nil || len(c.Stash) != 1 {
		t.Fatalf("stash at the ticket = %v, %v, want the queued save landed", c.Stash, err)
	}
	if err := h.store.Claim(ctx, other); err != nil {
		t.Fatalf("claim on the target = %v, want the name released", err)
	}
	wantClose(t, conn, websocket.StatusNormalClosure, reasonChangingTown)
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("the drain did not end once the town was empty")
	}
}

// With nowhere to send them, the drain ends and the players are closed.
func TestDrainWithNoOtherShardEnds(t *testing.T) {
	h := serveWorld(t, picking(towns.Town{ID: "self"}))
	lobbyAs(t, dial(t, h.shardURL("world-0")), "ann")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h.lobby.Drain(ctx)
	if ctx.Err() != nil {
		t.Fatal("the drain waited out its timeout with no shard to move to")
	}
}
