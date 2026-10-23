package protocol

import (
	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
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

func Joined(player sim.EntityID) *gamev1.ServerMessage {
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Joined{
		Joined: &gamev1.JoinAccepted{PlayerId: uint64(player)},
	}}
}

func Snapshot(w *sim.World) *gamev1.ServerMessage {
	snap := &gamev1.WorldSnapshot{Tick: w.Tick}
	for _, p := range w.Players() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:       uint64(p.ID),
			Kind:     gamev1.EntityKind_ENTITY_KIND_PLAYER,
			Position: vec(p.Pos),
			Velocity: vec(p.Vel),
			Health:   int32(p.Health),
		})
	}
	for _, e := range w.Enemies() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:       uint64(e.ID),
			Kind:     gamev1.EntityKind_ENTITY_KIND_ENEMY,
			Position: vec(e.Pos),
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

func vec(v sim.Vec2) *gamev1.Vec2 {
	return &gamev1.Vec2{X: float32(v.X), Y: float32(v.Y)}
}
