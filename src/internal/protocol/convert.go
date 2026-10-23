package protocol

import (
	"maps"
	"math"
	"slices"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
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
			Aim:     sim.Vec2{X: float64(p.Attack.GetAim().GetX()), Y: float64(p.Attack.GetAim().GetY())},
		}, true
	case *gamev1.ClientMessage_Pickup:
		return sim.PickupCommand{Player: player, Target: sim.EntityID(p.Pickup.GetTargetId())}, true
	case *gamev1.ClientMessage_Equip:
		return sim.EquipCommand{Player: player, Item: p.Equip.GetItemId()}, true
	case *gamev1.ClientMessage_Unequip:
		return sim.UnequipCommand{Player: player, Slot: items.Slot(p.Unequip.GetSlot())}, true
	case *gamev1.ClientMessage_Drop:
		return sim.DropCommand{Player: player, Item: p.Drop.GetItemId()}, true
	case *gamev1.ClientMessage_Use:
		return sim.UseCommand{Player: player, Item: p.Use.GetItemId()}, true
	}
	return nil, false
}

func Joined(player sim.EntityID, cave caves.Cave, spawn sim.Vec2, tickRate int, class *gamev1.ClassInfo) *gamev1.ServerMessage {
	layout := &gamev1.CaveLayout{
		Name:       cave.Name,
		Bounds:     rect(cave.Bounds),
		Extraction: rect(cave.Extraction),
	}
	for _, w := range cave.Walls {
		layout.Walls = append(layout.Walls, rect(w))
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Joined{
		Joined: &gamev1.JoinAccepted{PlayerId: uint64(player), Cave: layout, Spawn: vec(spawn), TickRate: uint32(tickRate), ClassInfo: class},
	}}
}

// ClassInfo lists the class's abilities in the order the data gives them.
func ClassInfo(data *classes.Data, id string) *gamev1.ClassInfo {
	c := data.Classes[id]
	info := &gamev1.ClassInfo{Id: c.ID, Name: c.Name, Resource: c.Resource.Kind, MaxResource: int32(c.Resource.Max), Basic: c.Basic}
	for _, aid := range c.Abilities {
		a := data.Abilities[aid]
		info.Abilities = append(info.Abilities, &gamev1.AbilityInfo{
			Id:         a.ID,
			Name:       a.Name,
			Cost:       int32(a.Cost),
			CooldownMs: uint32(a.CooldownMS),
			WindupMs:   uint32(a.WindupMS),
			Root:       a.Root,
		})
	}
	return info
}

func AbilityUsed(ev sim.AbilityEvent) *gamev1.ServerMessage {
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_AbilityUsed{AbilityUsed: &gamev1.AbilityUsed{
		SourceId:   uint64(ev.Source),
		AbilityId:  ev.Ability,
		Aim:        vec(ev.Aim),
		WindupMs:   uint32(ev.Windup.Milliseconds()),
		CooldownMs: uint32(ev.Cooldown.Milliseconds()),
	}}}
}

func AbilityRejected(r sim.Rejection) *gamev1.ServerMessage {
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_AbilityRejected{AbilityRejected: &gamev1.AbilityRejected{
		Seq:       r.Seq,
		AbilityId: r.Ability,
		Reason:    r.Reason,
	}}}
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
			LastInputSeq:    p.LastInputSeq,
			MaxSpeed:        float32(w.MaxSpeed(p)),
			ClassId:         p.Class,
			// Rounded down, so the bar never shows enough for an ability the server would refuse.
			Resource:    int32(math.Floor(p.Resource)),
			MaxResource: int32(p.MaxResource),
		})
	}
	for _, e := range w.Enemies() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:         uint64(e.ID),
			Kind:       gamev1.EntityKind_ENTITY_KIND_ENEMY,
			Position:   vec(e.Pos),
			Velocity:   vec(e.Vel),
			Health:     int32(e.Health),
			MaxHealth:  int32(e.MaxHealth),
			Archetype:  string(e.Archetype),
			EnemyState: enemyStates[e.State],
			Aim:        vec(e.Aim),
		})
	}
	for _, s := range w.Projectiles() {
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:       uint64(s.ID),
			Kind:     gamev1.EntityKind_ENTITY_KIND_PROJECTILE,
			Position: vec(s.Pos),
			Velocity: vec(s.Vel),
		})
	}
	for _, p := range w.Piles() {
		kind := gamev1.EntityKind_ENTITY_KIND_LOOT
		if p.Chest {
			kind = gamev1.EntityKind_ENTITY_KIND_CHEST
		}
		snap.Entities = append(snap.Entities, &gamev1.EntityState{
			Id:        uint64(p.ID),
			Kind:      kind,
			Position:  vec(p.Pos),
			ItemCount: uint32(len(p.Items)),
		})
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Snapshot{Snapshot: snap}}
}

var enemyStates = map[sim.EnemyState]gamev1.EnemyState{
	sim.EnemyIdle:    gamev1.EnemyState_ENEMY_STATE_IDLE,
	sim.EnemyPatrol:  gamev1.EnemyState_ENEMY_STATE_PATROL,
	sim.EnemyDetect:  gamev1.EnemyState_ENEMY_STATE_DETECT,
	sim.EnemyChase:   gamev1.EnemyState_ENEMY_STATE_CHASE,
	sim.EnemyAttack:  gamev1.EnemyState_ENEMY_STATE_ATTACK,
	sim.EnemySearch:  gamev1.EnemyState_ENEMY_STATE_SEARCH,
	sim.EnemyRetreat: gamev1.EnemyState_ENEMY_STATE_RETREAT,
	sim.EnemyReturn:  gamev1.EnemyState_ENEMY_STATE_RETURN,
	sim.EnemyCharge:  gamev1.EnemyState_ENEMY_STATE_CHARGE,
	sim.EnemyDead:    gamev1.EnemyState_ENEMY_STATE_DEAD,
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

func Inventory(p sim.Player) *gamev1.ServerMessage {
	msg := &gamev1.InventoryUpdated{Capacity: uint32(p.Inventory.Size)}
	for _, inst := range p.Inventory.Items {
		msg.Items = append(msg.Items, Item(inst))
	}
	return &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Inventory{Inventory: msg}}
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
