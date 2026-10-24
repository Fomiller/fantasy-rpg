package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const (
	toStash = gamev1.ItemPlace_ITEM_PLACE_STASH
	toBag   = gamev1.ItemPlace_ITEM_PLACE_BAG
	toGear  = gamev1.ItemPlace_ITEM_PLACE_GEAR
)

func helm(id string) items.Instance {
	return items.Instance{ID: id, DefID: "iron_helm", Slot: items.Head, Quantity: 1, Stack: 1}
}

func potions(id string, n int) items.Instance {
	return items.Instance{ID: id, DefID: "health_potion", Slot: items.Consumable, Quantity: n, Stack: 5}
}

func anyGear(inst items.Instance) error {
	if !inst.Slot.IsGear() {
		return errors.New("not gear")
	}
	return nil
}

func ids(insts []items.Instance) []string {
	var out []string
	for _, inst := range insts {
		out = append(out, fmt.Sprintf("%s:%d", inst.ID, inst.Quantity))
	}
	return out
}

func TestLobbyMoves(t *testing.T) {
	lb := newLobby(0, 2, inventory.Loadout{Equipment: []items.Instance{helm("old")}, Items: []items.Instance{potions("p", 2)}}, []items.Instance{helm("new")})

	if r := lb.move("p", toStash, anyGear); r != "" || len(lb.bag.Items) != 0 || fmt.Sprint(ids(lb.stash.Items)) != "[new:1 p:2]" {
		t.Fatalf("bag to stash: %q, bag %v, stash %v", r, ids(lb.bag.Items), ids(lb.stash.Items))
	}
	// The old helm goes where the new one came from.
	if r := lb.move("new", toGear, anyGear); r != "" || lb.gear[items.Head].ID != "new" || fmt.Sprint(ids(lb.stash.Items)) != "[p:2 old:1]" {
		t.Fatalf("stash to gear: %q, gear %v, stash %v", r, lb.gear, ids(lb.stash.Items))
	}
	if r := lb.move("new", toBag, anyGear); r != "" || len(lb.gear) != 0 || fmt.Sprint(ids(lb.bag.Items)) != "[new:1]" {
		t.Fatalf("gear to bag: %q, gear %v, bag %v", r, lb.gear, ids(lb.bag.Items))
	}
	if r := lb.move("p", toGear, anyGear); r != MoveCannotEquip || len(lb.gear) != 0 {
		t.Fatalf("equipped a potion: %q", r)
	}
	if r := lb.move("nope", toBag, anyGear); r != MoveUnknownItem {
		t.Fatalf("unknown item: %q", r)
	}
	if r := lb.move("p", gamev1.ItemPlace_ITEM_PLACE_UNSPECIFIED, anyGear); r != MoveUnknownPlace {
		t.Fatalf("no place: %q", r)
	}

	l := lb.loadout()
	if len(l.Equipment) != 0 || fmt.Sprint(ids(l.Items)) != "[new:1]" {
		t.Fatalf("loadout = %+v", l)
	}
}

func TestLobbyMoveIntoFullPlace(t *testing.T) {
	lb := newLobby(0, 1, inventory.Loadout{Items: []items.Instance{helm("h")}}, []items.Instance{potions("p", 4), helm("x")})

	// The bag is full, so nothing moves.
	if r := lb.move("x", toBag, anyGear); r != MoveFull || fmt.Sprint(ids(lb.stash.Items)) != "[p:4 x:1]" || fmt.Sprint(ids(lb.bag.Items)) != "[h:1]" {
		t.Fatalf("full bag: %q, stash %v, bag %v", r, ids(lb.stash.Items), ids(lb.bag.Items))
	}

	// A stack only tops up what is already there and leaves the rest.
	lb = newLobby(0, 1, inventory.Loadout{Items: []items.Instance{potions("b", 3)}}, []items.Instance{potions("p", 4)})
	if r := lb.move("p", toBag, anyGear); r != "" || fmt.Sprint(ids(lb.bag.Items)) != "[b:5]" || fmt.Sprint(ids(lb.stash.Items)) != "[p:2]" {
		t.Fatalf("partial stack: %q, bag %v, stash %v", r, ids(lb.bag.Items), ids(lb.stash.Items))
	}

	// Moving gear out of its slot needs room too.
	lb = newLobby(0, 1, inventory.Loadout{Equipment: []items.Instance{helm("worn")}, Items: []items.Instance{helm("h")}}, nil)
	if r := lb.move("worn", toBag, anyGear); r != MoveFull || lb.gear[items.Head].ID != "worn" {
		t.Fatalf("unequip into a full bag: %q, gear %v", r, lb.gear)
	}
}

func TestStashHoldsAHundred(t *testing.T) {
	var full []items.Instance
	for i := range inventory.StashSize {
		full = append(full, helm(fmt.Sprint(i)))
	}
	lb := newLobby(0, 20, inventory.Loadout{Items: []items.Instance{helm("one more")}}, full)
	if r := lb.move("one more", toStash, anyGear); r != MoveFull || len(lb.stash.Items) != 100 {
		t.Fatalf("101st item: %q, stash has %d", r, len(lb.stash.Items))
	}
}

func moveTo(t *testing.T, conn *websocket.Conn, id string, to gamev1.ItemPlace) {
	t.Helper()
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_StashMove{StashMove: &gamev1.StashMoveRequest{ItemId: id, To: to}}})
}

// lobbyItems reads the gear, bag and stash the lobby sends after each change.
func lobbyItems(t *testing.T, conn *websocket.Conn) (*gamev1.EquipmentUpdated, *gamev1.InventoryUpdated, *gamev1.StashContents) {
	t.Helper()
	eq := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment()
	inv := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetInventory() != nil }).GetInventory()
	st := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetStash() != nil }).GetStash()
	return eq, inv, st
}

func rejected(t *testing.T, conn *websocket.Conn) *gamev1.StashMoveRejected {
	t.Helper()
	return waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetStashMoveRejected() != nil }).GetStashMoveRejected()
}

// A joined player is in the lobby, not the cave, until they enter.
func TestJoinLandsInLobby(t *testing.T) {
	h := start(t)
	h.srv.Store.SaveXP(context.Background(), "ann", 120)
	other := dial(t, h.url)
	otherID := join(t, other)

	conn := dial(t, h.url)
	lb := lobbyAs(t, conn, "ann")
	if c := lb.GetCharacter(); c.GetName() != "ann" || c.GetLevel() != 2 || lb.GetClassInfo().GetId() != "warrior" {
		t.Fatalf("lobby = %v", lb)
	}
	eq, inv, st := lobbyItems(t, conn)
	if len(eq.GetItems()) != len(items.StarterKit) || stat(eq, "max_health") != 120+cfgPerLevel(t, "max_health") {
		t.Fatalf("lobby equipment = %v", eq)
	}
	if inv.GetCapacity() != 20 || st.GetCapacity() != 100 || len(st.GetItems()) != 0 {
		t.Fatalf("bag = %v, stash = %v", inv, st)
	}

	// Cave commands from the lobby do nothing, and the cave has one player.
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Attack{
		Attack: &gamev1.AttackRequest{Seq: 1, TargetId: uint64(h.enemy), AbilityId: "basic"},
	}})
	time.Sleep(50 * time.Millisecond)
	snap := waitFor(t, other, func(m *gamev1.ServerMessage) bool { return m.GetSnapshot() != nil }).GetSnapshot()
	players := 0
	for _, e := range snap.GetEntities() {
		if e.GetKind() == gamev1.EntityKind_ENTITY_KIND_PLAYER {
			players++
		}
		if e.GetId() == uint64(h.enemy) && e.GetHealth() != 30 {
			t.Fatalf("a lobby player hit the enemy: %v", e)
		}
	}
	if players != 1 || entity(snap, otherID) == nil {
		t.Fatalf("snapshot has %d players, want only the one in the cave", players)
	}

	joined := enterCave(t, conn)
	waitFor(t, other, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), joined.GetPlayerId()) != nil })
}

func cfgPerLevel(t *testing.T, stat string) int32 {
	t.Helper()
	return int32(sim.DefaultConfig().Classes.Classes["warrior"].PerLevel[items.Stat(stat)])
}

func TestLobbyMovesOverTheWire(t *testing.T) {
	h := start(t)
	potion := mustMake(t, h.srv, "health_potion")
	h.srv.Store.SaveStash(context.Background(), "ann", []items.Instance{potion})
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	sword := eq.GetItems()[0]

	moveTo(t, conn, sword.GetId(), toStash)
	eq, _, st := lobbyItems(t, conn)
	if len(eq.GetItems()) != len(items.StarterKit)-1 || stat(eq, "damage") != int32(sim.DefaultConfig().AttackDamage) {
		t.Fatalf("equipment after stashing the sword = %v", eq)
	}
	if len(st.GetItems()) != 2 || st.GetItems()[1].GetId() != sword.GetId() {
		t.Fatalf("stash = %v", st.GetItems())
	}
	moveTo(t, conn, potion.ID, toBag)
	if _, inv, _ := lobbyItems(t, conn); len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != potion.ID {
		t.Fatalf("bag = %v", inv.GetItems())
	}

	ctx := context.Background()
	if stash, _ := h.srv.Store.LoadStash(ctx, "ann"); len(stash) != 1 || stash[0].ID != sword.GetId() {
		t.Fatalf("saved stash = %v", ids(stash))
	}
	if l, _, _ := h.srv.Store.Load(ctx, "ann"); len(l.Items) != 1 || l.Items[0].ID != potion.ID {
		t.Fatalf("saved loadout = %+v", l)
	}

	moveTo(t, conn, potion.ID, toGear)
	if r := rejected(t, conn); r.GetItemId() != potion.ID || r.GetReason() != MoveCannotEquip {
		t.Fatalf("rejected = %v", r)
	}
	moveTo(t, conn, "nope", toBag)
	if r := rejected(t, conn); r.GetReason() != MoveUnknownItem {
		t.Fatalf("rejected = %v", r)
	}

	// Whatever is packed goes into the cave, and the stash cannot be touched there.
	enterCave(t, conn)
	waitFor(t, conn, inventoryWith("health_potion"))
	moveTo(t, conn, sword.GetId(), toGear)
	if r := rejected(t, conn); r.GetReason() != MoveNotInLobby {
		t.Fatalf("rejected = %v", r)
	}
}

// Stashing every item saves an empty loadout, so a rejoin does not hand out
// another starter kit.
func TestStashingEverythingKeepsNoStarterKit(t *testing.T) {
	h := start(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	for _, it := range eq.GetItems() {
		moveTo(t, conn, it.GetId(), toStash)
	}
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		return m.GetStash() != nil && len(m.GetStash().GetItems()) == len(items.StarterKit)
	})
	conn.Close(websocket.StatusNormalClosure, "")

	again := rejoin(t, h.url, "ann")
	if eq := waitFor(t, again, func(m *gamev1.ServerMessage) bool { return m.GetEquipment() != nil }).GetEquipment(); len(eq.GetItems()) != 0 {
		t.Fatalf("equipment on rejoin = %v, want none", eq.GetItems())
	}
}

// rejoin retries until the server has let go of the name.
func rejoin(t *testing.T, url, name string) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn := dial(t, url)
		send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{Name: name}}})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _, err := conn.Read(ctx)
		cancel()
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not rejoin as %s: %v", name, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startExtract spawns players inside the exit, so every run extracts.
func startExtract(t *testing.T) harness {
	t.Helper()
	cave := testCave()
	cave.Extraction = sim.Rect{Min: sim.Vec2{X: -20, Y: -20}, Max: sim.Vec2{X: 20, Y: 20}}
	cfg := sim.DefaultConfig()
	cfg.ExtractTime = 300 * time.Millisecond
	return startCave(t, cave, cfg)
}

// Extraction moves the bag to the stash. Gear stays on the character.
func TestExtractionMovesBagToStash(t *testing.T) {
	h := startExtract(t)
	potion := mustMake(t, h.srv, "health_potion")
	h.srv.Store.Save(context.Background(), "ann", inventory.Loadout{Equipment: h.srv.starterKit().Equipment, Items: []items.Instance{potion}})

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	summary := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil }).GetExtracted().GetSummary()
	if len(summary.GetKept()) != 1 || len(summary.GetOverflow()) != 0 {
		t.Fatalf("summary = %v", summary)
	}

	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil })
	eq, inv, st := lobbyItems(t, conn)
	if len(eq.GetItems()) != len(items.StarterKit) || len(inv.GetItems()) != 0 || len(st.GetItems()) != 1 || st.GetItems()[0].GetId() != potion.ID {
		t.Fatalf("lobby after extraction: gear %v, bag %v, stash %v", eq.GetItems(), inv.GetItems(), st.GetItems())
	}
	if stash, _ := h.srv.Store.LoadStash(context.Background(), "ann"); len(stash) != 1 {
		t.Fatalf("saved stash = %v", ids(stash))
	}
}

// What a full stash cannot take stays in the bag, and the summary says so.
func TestExtractionIntoFullStash(t *testing.T) {
	h := startExtract(t)
	ctx := context.Background()
	var full []items.Instance
	for i := range inventory.StashSize - 1 {
		full = append(full, helm(fmt.Sprint(i)))
	}
	h.srv.Store.SaveStash(ctx, "ann", full)
	fits, spills := mustMake(t, h.srv, "health_potion"), mustMake(t, h.srv, "rusty_sword")
	h.srv.Store.Save(ctx, "ann", inventory.Loadout{Items: []items.Instance{fits, spills}})

	conn := dial(t, h.url)
	joinAs(t, conn, "ann")
	summary := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetExtracted() != nil }).GetExtracted().GetSummary()
	if len(summary.GetKept()) != 2 || len(summary.GetOverflow()) != 1 || summary.GetOverflow()[0].GetId() != spills.ID {
		t.Fatalf("summary kept %v, overflow %v", summary.GetKept(), summary.GetOverflow())
	}
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil })
	_, inv, _ := lobbyItems(t, conn)
	if len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != spills.ID {
		t.Fatalf("bag = %v, want the overflow", inv.GetItems())
	}
	if stash, _ := h.srv.Store.LoadStash(ctx, "ann"); len(stash) != inventory.StashSize || stash[len(stash)-1].ID != fits.ID {
		t.Fatalf("saved stash has %d, want it full with the potion last", len(stash))
	}
}
