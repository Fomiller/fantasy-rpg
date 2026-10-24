package db

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/server"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
	"github.com/Fomiller/fantasy-rpg/src/internal/tick"
)

// instanceServer is an instance-server on store whose players spawn inside
// the exit, so every run extracts. stop shuts it down as a restart would.
func instanceServer(t *testing.T, store inventory.Store) (url string, stop func()) {
	t.Helper()
	cave := caves.Cave{
		Name:         "restart",
		Bounds:       sim.Rect{Min: sim.Vec2{X: -600, Y: -320}, Max: sim.Vec2{X: 600, Y: 320}},
		PlayerSpawns: []sim.Vec2{{}},
		Extraction:   sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}},
	}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 100 * time.Millisecond
	loot := items.NewRoller(items.Default(), 1)
	srv := server.New(cave.NewWorld(cfg, loot), cave, loot, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.Store = store

	ctx, cancel := context.WithCancel(context.Background())
	loop := &tick.Loop{Interval: 10 * time.Millisecond, Metrics: tick.NewMetrics(prometheus.NewRegistry(), "test"), Step: srv.Step}
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()
	hs := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		hs.CloseClientConnections()
		hs.Close()
		cancel()
		<-done
		srv.Close()
	}
	t.Cleanup(stop)
	return "ws" + strings.TrimPrefix(hs.URL, "http"), stop
}

func send(t *testing.T, conn *websocket.Conn, msg *gamev1.ClientMessage) {
	t.Helper()
	msg.ProtocolVersion = protocol.Version
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(t.Context(), websocket.MessageBinary, b); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, conn *websocket.Conn, match func(*gamev1.ServerMessage) bool) *gamev1.ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no matching message: %v", err)
		}
		var msg gamev1.ServerMessage
		if err := proto.Unmarshal(b, &msg); err != nil {
			t.Fatal(err)
		}
		if match(&msg) {
			return &msg
		}
	}
}

func joinLobby(t *testing.T, url, name string) (*websocket.Conn, *gamev1.Lobby, *gamev1.StashContents) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	send(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: name}}})
	lobby := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil }).GetLobby()
	stash := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetStash() != nil }).GetStash()
	return conn, lobby, stash
}

func waitLoaded(t *testing.T, s inventory.Store, name string, match func(inventory.Character) bool) inventory.Character {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, ok, err := s.Load(t.Context(), name, classes.Starter)
		if err == nil && ok && match(c) {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved %s = %+v, %v, %v", name, c, ok, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func make1(t *testing.T, r *items.Roller, def string, rarity items.Rarity) items.Instance {
	t.Helper()
	inst, err := r.Make(def, 3, rarity)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// A character, its gear and its stash outlive the server that saved them.
func TestRestartKeepsCharacters(t *testing.T) {
	t.Parallel()
	url := newDBURL(t, true)
	roll := items.NewRoller(items.Default(), 7)
	gear := []items.Instance{make1(t, roll, "rusty_sword", items.Common), make1(t, roll, "leather_vest", items.Common)}
	carried := []items.Instance{make1(t, roll, "health_potion", items.Common), make1(t, roll, "iron_sword", items.Rare)}
	stashed := make1(t, roll, "iron_sword", items.Uncommon)

	first := newStore(t, url)
	if err := first.Create(t.Context(), inventory.Character{
		Name: "ann", Class: classes.Starter, XP: 40,
		Loadout: inventory.Loadout{Equipment: gear, Items: carried},
		Stash:   []items.Instance{stashed},
	}); err != nil {
		t.Fatal(err)
	}
	wsURL, stop := instanceServer(t, first)
	conn, _, _ := joinLobby(t, wsURL, "ann")
	send(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_EnterInstance{EnterInstance: &gamev1.EnterInstanceRequest{}}})
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil })
	// Extraction banks XP and moves the bag to the stash.
	saved := waitLoaded(t, first, "ann", func(c inventory.Character) bool { return c.XP > 40 && len(c.Stash) == 3 })
	stop()
	first.Close()

	second := newStore(t, url)
	wsURL, _ = instanceServer(t, second)
	_, lobby, stash := joinLobby(t, wsURL, "ann")
	if got := lobby.GetCharacter().GetXp(); int(got) != saved.XP {
		t.Fatalf("lobby xp after restart = %d, want %d", got, saved.XP)
	}
	var ids []string
	for _, it := range stash.GetItems() {
		ids = append(ids, it.GetId())
	}
	if want := []string{stashed.ID, carried[0].ID, carried[1].ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("lobby stash after restart = %v, want %v", ids, want)
	}

	got, ok, err := second.Load(t.Context(), "ann", classes.Starter)
	if err != nil || !ok {
		t.Fatalf("load after restart: %v, %v", ok, err)
	}
	if got.XP != saved.XP || len(got.Loadout.Items) != 0 {
		t.Fatalf("after restart xp %d, bag %d, want xp %d and an empty bag", got.XP, len(got.Loadout.Items), saved.XP)
	}
	if !reflect.DeepEqual(got.Loadout.Equipment, gear) {
		t.Fatalf("gear after restart = %+v, want %+v", got.Loadout.Equipment, gear)
	}
	if want := []items.Instance{stashed, carried[0], carried[1]}; !reflect.DeepEqual(got.Stash, want) {
		t.Fatalf("stash after restart = %+v, want %+v", got.Stash, want)
	}
}

// worldServer is a world-server on store. stop shuts it down as a restart would.
func worldServer(t *testing.T, store inventory.Store) (url string, stop func()) {
	t.Helper()
	lobby := server.NewLobby(sim.NewWorld(sim.DefaultConfig(), nil), items.NewRoller(items.Default(), 1), slog.New(slog.NewTextHandler(io.Discard, nil)))
	lobby.Store = store
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		lobby.Run(ctx)
		close(done)
	}()
	hs := httptest.NewServer(http.HandlerFunc(lobby.HandleWS))
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		hs.CloseClientConnections()
		hs.Close()
		cancel()
		<-done
		lobby.Close()
	}
	t.Cleanup(stop)
	return "ws" + strings.TrimPrefix(hs.URL, "http"), stop
}

// A character made and changed on the world server outlives it. The
// instance server then sees the same character, since both share the store.
func TestWorldRestartKeepsCharacters(t *testing.T) {
	t.Parallel()
	url := newDBURL(t, true)

	first := newStore(t, url)
	wsURL, stop := worldServer(t, first)
	conn, lobby, _ := joinLobby(t, wsURL, "ann")
	if lobby.GetSaving() {
		t.Fatalf("lobby = %v", lobby)
	}
	sword := waitLoaded(t, first, "ann", func(c inventory.Character) bool { return len(c.Loadout.Equipment) > 0 }).Loadout.Equipment[0].ID
	send(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_StashMove{StashMove: &gamev1.StashMoveRequest{ItemId: sword, To: gamev1.ItemPlace_ITEM_PLACE_STASH}}})
	saved := waitLoaded(t, first, "ann", func(c inventory.Character) bool { return len(c.Stash) == 1 && c.Stash[0].ID == sword })
	stop()
	first.Close()

	second := newStore(t, url)
	wsURL, _ = worldServer(t, second)
	_, lobby, stash := joinLobby(t, wsURL, "ann")
	if lobby.GetCharacter().GetName() != "ann" || len(stash.GetItems()) != 1 || stash.GetItems()[0].GetId() != sword {
		t.Fatalf("after restart: lobby %v, stash %v, want the stashed sword", lobby, stash.GetItems())
	}
	got := waitLoaded(t, second, "ann", func(inventory.Character) bool { return true })
	if !reflect.DeepEqual(got.Loadout, saved.Loadout) || !reflect.DeepEqual(got.Stash, saved.Stash) {
		t.Fatalf("after restart = %+v, want %+v", got, saved)
	}

}
