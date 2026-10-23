package protocol

import (
	"maps"
	"slices"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// Command turns a client message into a sim command for player. The player
// comes from the connection, never from the message, so a client cannot act
// as someone else.
func Command(msg *gamev1.ClientMessage, player sim.EntityID) (any, bool) {
	switch p := msg.GetPayload().(type) {
	case *gamev1.ClientMessage_Move:
		d := p.Move.GetDirection()
		return sim.MoveCommand{
			Player:    player,
			Seq:       p.Move.GetSeq(),
			Direction: sim.Vec2{X: float64(d.GetX()), Y: float64(d.GetY())},
		}, true
	case *gamev1.ClientMessage_Attack:
		return sim.AttackCommand{
			Player:  player,
			Seq:     p.Attack.GetSeq(),
			Target:  sim.EntityID(p.Attack.GetTargetId()),
			Ability: p.Attack.GetAbilityId(),
		}, true
	}
	return nil, false
}

func Joined(player sim.EntityID, cave caves.Cave, spawn sim.Vec2) *gamev1.ServerMessage {
	layout := &gamev1.CaveLayout{
		Name:       cave.Name,
		Bounds:     rect(cave.Bounds),
		Extraction: rect(cave.Extraction),
	}
	for _, w := range cave.Walls {
		layout.Walls = append(layout.Walls, rect(w))
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Joined{
		Joined: &gamev1.JoinAccepted{PlayerId: uint64(player), Cave: layout, Spawn: vec(spawn)},
	}}
}

func Extracted(player sim.EntityID) *gamev1.ServerMessage {
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Extracted{
		Extracted: &gamev1.Extracted{PlayerId: uint64(player)},
	}}
}

func Snapshot(w *sim.World) *gamev1.ServerMessage {
	snap := &gamev1.WorldSnapshot{Tick: w.Tick}
	for _, p := range w.Players() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:              uint64(p.ID),
			Kind:            gamev1.EntityKind_ENTITY_KIND_PLAYER,
			Position:        vec(p.Pos),
			Velocity:        vec(p.Vel),
			Health:          int32(p.Health),
			MaxHealth:       int32(p.MaxHealth),
			ExtractProgress: float32(p.ExtractProgress),
		})
	}
	for _, e := range w.Enemies() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:       uint64(e.ID),
			Kind:     gamev1.EntityKind_ENTITY_KIND_ENEMY,
			Position: vec(e.Pos),
			Velocity: vec(e.Vel),
			Health:   int32(e.Health),
		})
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Snapshot{Snapshot: snap}}
}

func Damage(ev sim.DamageEvent) *gamev1.ServerMessage {
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Damage{Damage: &gamev1.DamageEvent{
		SourceId:     uint64(ev.Source),
		TargetId:     uint64(ev.Target),
		Amount:       int32(ev.Amount),
		TargetHealth: int32(ev.TargetHealth),
		Killed:       ev.Killed,
	}}}
}

// Equipment lists a player's gear in slot order, so the client can show it
// the same way every time.
func Equipment(p sim.Player) *gamev1.ServerMessage {
	msg := &gamev1.EquipmentUpdated{}
	for _, slot := range items.GearSlots {
		if inst, ok := p.Equipment[slot]; ok {
			msg.Items = append(msg.Items, Item(inst))
		}
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Equipment{Equipment: msg}}
}

func Item(inst items.Instance) *gamev1.Item {
	out := &gamev1.Item{
		Id:        inst.ID,
		ItemDefId: inst.DefID,
		Quantity:  uint32(inst.Quantity),
		Name:      inst.Name,
		Slot:      string(inst.Slot),
		Rarity:    gamev1.Rarity(inst.Rarity + 1),
		Level:     uint32(inst.Level),
	}
	for _, s := range slices.Sorted(maps.Keys(inst.Base)) {
		out.Base = append(out.Base, &gamev1.ItemStat{Stat: string(s), Value: int32(inst.Base[s])})
	}
	for _, a := range inst.Affixes {
		out.Affixes = append(out.Affixes, &gamev1.ItemAffix{Name: a.Name, Stat: string(a.Stat), Value: int32(a.Value), Tier: uint32(a.Tier)})
	}
	return out
}

func rect(r sim.Rect) *gamev1.Rect {
	return &gamev1.Rect{Min: vec(r.Min), Max: vec(r.Max)}
}

func vec(v sim.Vec2) *gamev1.Vec2 {
	return &gamev1.Vec2{X: float32(v.X), Y: float32(v.Y)}
}
