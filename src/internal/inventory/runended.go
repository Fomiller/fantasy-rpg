package inventory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

type Outcome int

const (
	Extracted Outcome = iota + 1
	Died
	Disconnect
)

// RunEnd is one run's result, as run.ended carries it.
type RunEnd struct {
	RunID string
	// Instance is the id and boot id that held the lease.
	Instance  string
	Outcome   Outcome
	Character Character
	Time      time.Time
}

// Event is e as a run.ended payload.
func (e RunEnd) Event() *eventsv1.RunEnded {
	msg := &eventsv1.RunEnded{
		Version:   events.Version,
		RunId:     e.RunID,
		Account:   e.Character.Name,
		Class:     e.Character.Class,
		Outcome:   eventsv1.RunOutcome(e.Outcome),
		Xp:        int64(e.Character.XP),
		Instance:  e.Instance,
		Time:      timestamppb.New(e.Time),
		Equipment: protoItems(e.Character.Loadout.Equipment),
		Bag:       protoItems(e.Character.Loadout.Items),
		Stash:     protoItems(e.Character.Stash),
	}
	if e.Outcome == Died {
		msg.Deaths = 1
	}
	return msg
}

// RunEndOf reads a run.ended payload back.
func RunEndOf(msg *eventsv1.RunEnded) (RunEnd, error) {
	e := RunEnd{
		RunID:     msg.GetRunId(),
		Instance:  msg.GetInstance(),
		Outcome:   Outcome(msg.GetOutcome()),
		Time:      msg.GetTime().AsTime(),
		Character: Character{Name: msg.GetAccount(), Class: msg.GetClass(), XP: int(msg.GetXp())},
	}
	var err error
	if e.Character.Loadout.Equipment, err = goItems(msg.GetEquipment()); err != nil {
		return e, err
	}
	if e.Character.Loadout.Items, err = goItems(msg.GetBag()); err != nil {
		return e, err
	}
	e.Character.Stash, err = goItems(msg.GetStash())
	return e, err
}

// RunEndedHandler writes each run.ended to store. A run id already written
// is a no-op, so redelivery is safe. A record that can never apply is
// dropped, since retrying it would hold up the partition behind it.
func RunEndedHandler(store Store, log *slog.Logger) events.Handler {
	return func(ctx context.Context, _ string, value []byte) error {
		var msg eventsv1.RunEnded
		if err := proto.Unmarshal(value, &msg); err != nil {
			return events.Drop(err)
		}
		e, err := RunEndOf(&msg)
		if err != nil {
			return events.Drop(err)
		}
		ctx, cancel := context.WithTimeout(ctx, applyTimeout)
		defer cancel()
		err = store.EndRun(ctx, e.RunID, e.Character)
		switch {
		case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrNoCharacter), errors.Is(err, ErrNoRunID), errors.Is(err, ErrStashFull):
			return events.Drop(fmt.Errorf("run %s of %s: %w", e.RunID, e.Character.Name, err))
		case err != nil:
			return err
		}
		log.Info("run applied", "run", e.RunID, "name", e.Character.Name, "outcome", msg.GetOutcome(), "instance", e.Instance)
		return nil
	}
}

const applyTimeout = 5 * time.Second

func protoItems(insts []items.Instance) []*eventsv1.Item {
	out := make([]*eventsv1.Item, 0, len(insts))
	for _, inst := range insts {
		it := &eventsv1.Item{
			Id: inst.ID, Def: inst.DefID, Name: inst.Name, Slot: string(inst.Slot), Weapon: string(inst.WeaponType),
			Rarity: inst.Rarity.String(), Level: int32(inst.Level), Quantity: int32(inst.Quantity), Stack: int32(inst.Stack),
		}
		if len(inst.Base) > 0 {
			it.Base = map[string]int32{}
			for stat, v := range inst.Base {
				it.Base[string(stat)] = int32(v)
			}
		}
		for _, a := range inst.Affixes {
			it.Affixes = append(it.Affixes, &eventsv1.Affix{Id: a.ID, Name: a.Name, Stat: string(a.Stat), Tier: int32(a.Tier), Value: int32(a.Value)})
		}
		out = append(out, it)
	}
	return out
}

func goItems(in []*eventsv1.Item) ([]items.Instance, error) {
	var out []items.Instance
	for _, it := range in {
		rarity, err := items.ParseRarity(it.GetRarity())
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", it.GetId(), err)
		}
		inst := items.Instance{
			ID: it.GetId(), DefID: it.GetDef(), Name: it.GetName(), Slot: items.Slot(it.GetSlot()), WeaponType: items.WeaponType(it.GetWeapon()),
			Rarity: rarity, Level: int(it.GetLevel()), Base: map[items.Stat]int{}, Quantity: int(it.GetQuantity()), Stack: int(it.GetStack()),
		}
		for stat, v := range it.GetBase() {
			inst.Base[items.Stat(stat)] = int(v)
		}
		for _, a := range it.GetAffixes() {
			inst.Affixes = append(inst.Affixes, items.Affix{ID: a.GetId(), Name: a.GetName(), Stat: items.Stat(a.GetStat()), Tier: int(a.GetTier()), Value: int(a.GetValue())})
		}
		out = append(out, inst)
	}
	return out, nil
}
