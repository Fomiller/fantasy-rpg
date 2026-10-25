package server

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

func joinTown(t *testing.T, conn *websocket.Conn, name string, returning bool) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: name, Returning: returning},
	}})
}

func read(t *testing.T, conn *websocket.Conn) *gamev1.ServerMessage {
	t.Helper()
	return waitFor(t, conn, func(*gamev1.ServerMessage) bool { return true })
}

// A join into the town comes in a set order: the body, the character, the
// items, then a snapshot every tick.
func TestTownJoinOrder(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	joinTown(t, conn, "ann", false)

	joined := read(t, conn).GetJoined()
	town := testTown()
	l := joined.GetLayout()
	if !joined.GetTown() || joined.GetPlayerId() == 0 || l.GetName() != "test town" || l.GetEntrance().GetMin().GetY() != float32(town.Entrance.Min.Y) || l.GetStash().GetX() != float32(town.Stash.X) || l.GetExtraction() != nil {
		t.Fatalf("first message = %v, want JoinAccepted in the town", joined)
	}
	if joined.GetSpawn().GetX() != float32(town.PlayerSpawns[0].X) || joined.GetTickRate() != 20 {
		t.Fatalf("spawn %v tick rate %d", joined.GetSpawn(), joined.GetTickRate())
	}
	if lb := read(t, conn).GetLobby(); lb == nil || lb.GetCharacter().GetName() != "ann" {
		t.Fatalf("second message = %v, want Lobby", lb)
	}
	if read(t, conn).GetEquipment() == nil || read(t, conn).GetInventory() == nil || read(t, conn).GetStash() == nil {
		t.Fatal("want gear, bag and stash after Lobby")
	}
	snap := read(t, conn).GetSnapshot()
	if me := entity(snap, joined.GetPlayerId()); me == nil || me.GetHealth() != me.GetMaxHealth() || me.GetMaxSpeed() == 0 {
		t.Fatalf("snapshot = %v, want the player at full health", snap)
	}
}

func TestTownPlayersSeeEachOther(t *testing.T) {
	h := serveWorld(t)
	a := dial(t, h.url)
	lobbyAs(t, a, "ann")
	b := dial(t, h.url)
	lobbyAs(t, b, "bob")
	idA, idB := townID(t, a), townID(t, b)

	waitFor(t, a, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), idB) != nil })
	start := townPos(t, b, func(*gamev1.EntityState) bool { return true })
	walk(t, a, 1, 0)
	waitFor(t, b, func(m *gamev1.ServerMessage) bool {
		e := entity(m.GetSnapshot(), idA)
		return e != nil && e.GetPosition().GetX() > start.GetPosition().GetX()+20
	})

	b.Close(websocket.StatusNormalClosure, "")
	waitFor(t, a, func(m *gamev1.ServerMessage) bool {
		return m.GetSnapshot() != nil && entity(m.GetSnapshot(), idB) == nil
	})
}

// Nothing in the town fights or makes loot. An attack is never started, and
// a drop or a potion changes nothing.
func TestTownDropsRunCommands(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	sword := eq.GetItems()[0].GetId()

	for _, msg := range []*gamev1.ClientMessage{
		{Payload: &gamev1.ClientMessage_Attack{Attack: &gamev1.AttackRequest{Seq: 1, AbilityId: "basic", Aim: &gamev1.Vec2{X: 1}}}},
		{Payload: &gamev1.ClientMessage_Drop{Drop: &gamev1.DropRequest{ItemId: sword}}},
		{Payload: &gamev1.ClientMessage_Use{Use: &gamev1.UseRequest{ItemId: sword}}},
		{Payload: &gamev1.ClientMessage_Pickup{Pickup: &gamev1.PickupRequest{TargetId: 1}}},
		{Payload: &gamev1.ClientMessage_LootOpen{LootOpen: &gamev1.LootOpenRequest{TargetId: 1}}},
	} {
		sendItem(t, conn, msg)
	}
	snapshots := 0
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		if m.GetAbilityUsed() != nil || m.GetAbilityRejected() != nil || m.GetDamage() != nil || m.GetLoot() != nil || m.GetInventory() != nil {
			t.Fatalf("the town answered a run command with %v", m)
		}
		if m.GetSnapshot() != nil {
			snapshots++
		}
		return snapshots == 10
	})
	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "weapon"}}})
	if _, inv, _ := lobbyItems(t, conn); len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != sword {
		t.Fatalf("bag = %v, want the sword the drop left alone", inv.GetItems())
	}
}

// Moves from or to the stash need the chest in range. Bag and gear moves
// work anywhere.
func TestTownStashRange(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	eq, _, _ := lobbyItems(t, conn)
	sword := eq.GetItems()[0].GetId()
	moveTo(t, conn, sword, toStash)
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Stash) == 1 })
	conn.Close(websocket.StatusNormalClosure, "")

	far, _ := rejoinReturning(t, h.url, "ann")
	eq, _, _ = lobbyItems(t, far)
	helm := eq.GetItems()[0].GetId()
	if r := moveToRejected(t, far, sword, toBag); r != MoveFar {
		t.Fatalf("take from the stash far away: %q, want far", r)
	}
	if r := moveToRejected(t, far, helm, toStash); r != MoveFar {
		t.Fatalf("stash far away: %q, want far", r)
	}
	moveTo(t, far, helm, toBag)
	if _, inv, _ := lobbyItems(t, far); len(inv.GetItems()) != 1 || inv.GetItems()[0].GetId() != helm {
		t.Fatalf("bag = %v, want the unequipped gear", inv.GetItems())
	}
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Loadout.Items) == 1 && len(c.Stash) == 1 })
}

func moveToRejected(t *testing.T, conn *websocket.Conn, id string, to gamev1.ItemPlace) string {
	t.Helper()
	moveTo(t, conn, id, to)
	return rejected(t, conn).GetReason()
}

// rejoinReturning joins as a player back from a run, once the server has
// let go of the name.
func rejoinReturning(t *testing.T, url, name string) (*websocket.Conn, *gamev1.JoinAccepted) {
	t.Helper()
	for range 100 {
		conn := dial(t, url)
		joinTown(t, conn, name, true)
		_, b, err := conn.Read(context.Background())
		if err != nil {
			continue
		}
		var msg gamev1.ServerMessage
		if err := proto.Unmarshal(b, &msg); err != nil || msg.GetJoined() == nil {
			t.Fatalf("first message = %v, %v", &msg, err)
		}
		townIDs.Store(conn, msg.GetJoined().GetPlayerId())
		waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetLobby() != nil })
		return conn, msg.GetJoined()
	}
	t.Fatalf("could not rejoin as %s", name)
	return nil, nil
}

func TestTownReturningSpawnsOutsideTheEntrance(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	conn.Close(websocket.StatusNormalClosure, "")

	_, joined := rejoinReturning(t, h.url, "ann")
	ret := testTown().ReturnSpawns[0]
	if s := joined.GetSpawn(); s.GetX() != float32(ret.X) || s.GetY() != float32(ret.Y) {
		t.Fatalf("returning spawn = %v, want %v", s, ret)
	}
}

// Equip and unequip in the town are lobby moves: saved, and mirrored on the
// town's player so the snapshot shows the speed.
func TestTownEquipIsALobbyMove(t *testing.T) {
	h := serveWorld(t)
	boots := items.Instance{ID: "boots", DefID: "boots", Name: "Boots", Slot: items.Feet, Quantity: 1, Stack: 1, Base: map[items.Stat]int{items.MoveSpeed: 10}}
	if err := h.store.Create(context.Background(), inventory.Character{Name: "ann", Class: classes.Starter, Loadout: inventory.Loadout{Items: []items.Instance{boots}}}); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	slow := townPos(t, conn, func(*gamev1.EntityState) bool { return true }).GetMaxSpeed()

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Equip{Equip: &gamev1.EquipRequest{ItemId: "boots"}}})
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Loadout.Equipment) == 1 && len(c.Loadout.Items) == 0 })
	townPos(t, conn, func(e *gamev1.EntityState) bool { return e.GetMaxSpeed() > slow })

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "feet"}}})
	h.saved(t, "ann", func(c inventory.Character) bool { return len(c.Loadout.Equipment) == 0 && len(c.Loadout.Items) == 1 })
	townPos(t, conn, func(e *gamev1.EntityState) bool { return e.GetMaxSpeed() == slow })

	sendItem(t, conn, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Unequip{Unequip: &gamev1.UnequipRequest{Slot: "feet"}}})
	if r := rejected(t, conn); r.GetReason() != MoveUnknownItem {
		t.Fatalf("unequip an empty slot: %v", r)
	}
}

// Stepping into the entrance tries once. Standing in it does not try again,
// and stepping out and back in does.
func TestTownEntranceTriesOncePerStep(t *testing.T) {
	h := serveWorld(t)
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	walk(t, conn, 0, -1)
	if r := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetEnterInstanceRejected() != nil }).GetEnterInstanceRejected(); r.GetReason() != EnterFull {
		t.Fatalf("rejected = %v, want full", r)
	}
	snapshots := 0
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		if m.GetEnterInstanceRejected() != nil {
			t.Fatal("standing in the entrance tried again")
		}
		if m.GetSnapshot() != nil {
			snapshots++
		}
		return snapshots == 20
	})
	walk(t, conn, 0, 1)
	townPos(t, conn, func(e *gamev1.EntityState) bool { return float64(e.GetPosition().GetY()) > testTown().Entrance.Max.Y })
	if r := enterRejected(t, conn); r != EnterFull {
		t.Fatalf("second try = %q, want full", r)
	}
}

// A ticket takes the player out of the town.
func TestTicketLeavesTheTown(t *testing.T) {
	h := start(t)
	stay := dial(t, h.url)
	lobbyAs(t, stay, "stay")
	leaver := dial(t, h.url)
	lobbyAs(t, leaver, "leaver")
	id := townID(t, leaver)
	waitFor(t, stay, func(m *gamev1.ServerMessage) bool { return entity(m.GetSnapshot(), id) != nil })

	enterInstance(t, leaver)
	waitFor(t, stay, func(m *gamev1.ServerMessage) bool {
		return m.GetSnapshot() != nil && entity(m.GetSnapshot(), id) == nil
	})
}

// A town at its cap turns the next join away until someone leaves. Players
// in a run do not count.
func TestTownFull(t *testing.T) {
	h := start(t)
	// On the loop, since the town reads it there.
	h.world.enqueue(func() { h.world.TownMaxPlayers = 1 })
	first := dial(t, h.url)
	lobbyAs(t, first, "ann")

	turned := dial(t, h.url)
	joinTown(t, turned, "bob", false)
	_, _, err := turned.Read(context.Background())
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusTryAgainLater || closed.Reason != reasonTownFull {
		t.Fatalf("join past the cap: %v, want 1013 town full", err)
	}

	enterInstance(t, first)
	rejoin(t, h.url, "bob")
}
