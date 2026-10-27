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
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

// elsewhere is ann's session on another live shard.
func elsewhere(t *testing.T, s inventory.Store) inventory.Session {
	t.Helper()
	if err := s.Heartbeat(context.Background(), "world-1", "b1", time.Minute); err != nil {
		t.Fatal(err)
	}
	return inventory.Session{Name: "ann", ID: "other", Shard: "world-1", Boot: "b1"}
}

func asShard(l *Lobby) { l.WorldID = "world-0" }

func TestWorldJoinTurnedAwayWhileAnotherShardPlays(t *testing.T) {
	h := serveWorld(t, asShard)
	other := elsewhere(t, h.store)
	if err := h.store.Claim(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: "ann"}}})
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonPlaying)
	if calls := h.store.calls(); len(calls) != 0 {
		t.Fatalf("store calls = %v, want no load before the claim", calls)
	}

	if err := h.store.Release(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	rejoin(t, h.url, "ann")
}

func TestWorldReleasesTheClaimOnLeave(t *testing.T) {
	h := serveWorld(t, asShard)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.lobby.RunHeartbeat(ctx)
	other := elsewhere(t, h.store)

	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	if err := h.store.Claim(ctx, other); !errors.Is(err, inventory.ErrPlaying) {
		t.Fatalf("claim from another shard while ann plays = %v, want ErrPlaying", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := h.store.Claim(ctx, other)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("claim after ann left = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A shard that missed its heartbeat lost the name. Its next lobby save is
// turned away and the player is closed, so they rejoin where the name is.
func TestWorldLostClaimClosesThePlayer(t *testing.T) {
	h := serveWorld(t, asShard)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	if err := h.store.Claim(context.Background(), elsewhere(t, h.store)); err != nil {
		t.Fatalf("claim over a shard with no heartbeat: %v", err)
	}

	moveTo(t, conn, eq.GetItems()[0].GetId(), toStash)
	wantClose(t, conn, websocket.StatusTryAgainLater, reasonPlaying)
	if c, _, err := h.store.Load(context.Background(), "ann", classes.Starter); err != nil || len(c.Stash) != 0 {
		t.Fatalf("stash = %v, %v, want the stale save turned away", c.Stash, err)
	}
}
