package sim

import (
	"reflect"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func TestStatsPerLevel(t *testing.T) {
	prog := DefaultConfig().Classes.Progression
	for level, health := range map[int]int{1: 120, 2: 125, 5: 140, 10: 165} {
		w := NewWorld(passive(), nil)
		id, err := w.AddPlayerAs(Vec2{}, classes.Starter, prog.XPToReach(level))
		if err != nil {
			t.Fatal(err)
		}
		p, _ := w.Player(id)
		if p.Level != level || p.MaxHealth != health || p.Health != health || w.Stats(p)[items.MaxHealth] != health {
			t.Errorf("level %d: got level %d health %d/%d, want %d", level, p.Level, p.Health, p.MaxHealth, health)
		}
		if w.Stats(p)[items.Armor] != 5 {
			t.Errorf("level %d: armor %d, levels give health only", level, w.Stats(p)[items.Armor])
		}
	}
}

// The gap check from the design: level 10 in the starter kit, plus gear.
func TestLevelStacksWithGear(t *testing.T) {
	w := NewWorld(passive(), nil)
	id, _ := w.AddPlayerAs(Vec2{}, "", 2700)
	equip(t, w, id, gear(items.Trinket, map[items.Stat]int{items.MaxHealth: 8}))
	if p, _ := w.Player(id); p.MaxHealth != 173 {
		t.Fatalf("max health %d, want 120 + 45 + 8", p.MaxHealth)
	}
}

func TestRunXPDoesNotLevel(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	if run, err := w.AddRunXP(id, 500); err != nil || run != 500 {
		t.Fatalf("run xp = %d, %v", run, err)
	}
	if p, _ := w.Player(id); p.Level != 1 || p.XP != 0 || p.MaxHealth != 120 {
		t.Fatalf("level %d xp %d health %d, want run xp to wait for the run end", p.Level, p.XP, p.MaxHealth)
	}
}

func TestBankXPLevelsUp(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.players[id].Health = 100
	w.AddRunXP(id, 168)

	gained, err := w.BankXP(id, 168)
	p, _ := w.Player(id)
	if err != nil || gained != 1 || p.Level != 2 || p.XP != 168 || p.RunXP != 0 {
		t.Fatalf("gained %d, %v: level %d xp %d run %d", gained, err, p.Level, p.XP, p.RunXP)
	}
	// The new max health comes with the level, but nothing more is healed.
	if p.MaxHealth != 125 || p.Health != 105 {
		t.Fatalf("health %d/%d, want 105/125", p.Health, p.MaxHealth)
	}

	if gained, _ := w.BankXP(id, 600); gained != 3 {
		t.Fatalf("768 xp gained %d levels, want 3 to level 5", gained)
	}
	if gained, _ := w.BankXP(id, 100_000); gained != 5 {
		t.Fatalf("gained %d levels, want 5 to the cap", gained)
	}
	if p, _ := w.Player(id); p.Level != 10 || p.XP != 100_768 {
		t.Fatalf("level %d xp %d, want 10 and xp past the cap kept", p.Level, p.XP)
	}
	if gained, _ := w.BankXP(id, 1); gained != 0 {
		t.Fatalf("gained %d levels past the cap", gained)
	}
}

func TestXPRejects(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	if _, err := w.AddRunXP(id, -1); err == nil {
		t.Error("took negative run xp")
	}
	if _, err := w.BankXP(id, -1); err == nil {
		t.Error("banked negative xp")
	}
	if _, err := w.AddRunXP(999, 10); err == nil {
		t.Error("gave xp to a missing player")
	}
	if _, err := w.AddPlayerAs(Vec2{}, "", -5); err == nil {
		t.Error("joined with negative xp")
	}
}

func xpFor(ev []XPEvent, player EntityID) int {
	total := 0
	for _, x := range ev {
		if x.Player == player {
			total += x.Amount
		}
	}
	return total
}

func TestArchetypeXP(t *testing.T) {
	a := DefaultConfig().Archetypes
	if a[Melee].XP != 10 || a[Ranged].XP != 10 || a[Elite].XP != 40 {
		t.Fatalf("xp melee %d ranged %d elite %d", a[Melee].XP, a[Ranged].XP, a[Elite].XP)
	}
}

func TestKillXPGoesToEveryoneInTheAssistWindow(t *testing.T) {
	w := NewWorld(passive(), nil)
	killer := w.AddPlayer(Vec2{})
	helper := w.AddPlayer(Vec2{Y: 1})
	stale := w.AddPlayer(Vec2{Y: -1})
	bystander := w.AddPlayer(Vec2{Y: 2})
	enemy := w.AddEnemy(Vec2{X: 40}, Elite)

	swing(w, AttackCommand{Player: stale, Seq: 1, Target: enemy})
	w.enemies[enemy].hits[stale] -= DefaultConfig().Classes.Progression.AssistWindow() + time.Second
	swing(w, AttackCommand{Player: helper, Seq: 1, Target: enemy})
	w.enemies[enemy].Health = 1

	var ev []XPEvent
	w.Submit(AttackCommand{Player: killer, Seq: 1, Target: enemy})
	for range 20 {
		ev = append(ev, w.Step(dt).XP...)
	}
	for id, want := range map[EntityID]int{killer: 40, helper: 40, stale: 0, bystander: 0} {
		if got := xpFor(ev, id); got != want || playerOf(t, w, id).RunXP != want {
			t.Errorf("player %d: event xp %d run xp %d, want %d", id, got, playerOf(t, w, id).RunXP, want)
		}
	}
	if len(ev) != 2 || ev[0].Source != XPKill {
		t.Fatalf("events = %+v", ev)
	}
}

func TestChestXPOncePerPlayer(t *testing.T) {
	w := NewWorld(passive(), nil)
	a := w.AddPlayer(Vec2{})
	b := w.AddPlayer(Vec2{Y: 1})
	far := w.AddPlayer(Vec2{X: 200})
	chest := w.AddChest(Vec2{X: 20})
	pile := w.AddPile(Vec2{X: 20}, nil)

	open := func(id, target EntityID) []XPEvent {
		w.Submit(OpenLootCommand{Player: id, Target: target})
		return w.Step(dt).XP
	}
	if ev := open(a, chest); len(ev) != 1 || ev[0] != (XPEvent{Player: a, Amount: 5, Source: XPChest, RunXP: 5}) {
		t.Fatalf("first open = %+v", ev)
	}
	if ev := open(a, chest); len(ev) != 0 {
		t.Fatalf("second open = %+v, want no xp", ev)
	}
	if ev := open(b, chest); xpFor(ev, b) != 5 {
		t.Fatalf("other player = %+v, want their own chest xp", ev)
	}
	if ev := open(far, chest); len(ev) != 0 {
		t.Fatalf("open out of range = %+v", ev)
	}
	if ev := open(b, pile); len(ev) != 0 {
		t.Fatalf("open a pile = %+v, only chests give xp", ev)
	}
}

func TestExtractionBanksRunXPAndBonus(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExtractTime = dt
	w := NewWorld(cfg, nil)
	w.SetExtraction(Rect{Vec2{-50, -50}, Vec2{50, 50}})
	id := w.AddPlayer(Vec2{})
	w.AddRunXP(id, 115)
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 2)}})

	ev := w.Step(dt)
	want := RunEnd{Player: id, Class: "warrior", Extracted: true, RunXP: 115, Banked: 168, XP: 168, Level: 2, Levels: 1, Kept: []items.Instance{potion("p", 2)}}
	if len(ev.RunEnded) != 1 || !reflect.DeepEqual(ev.RunEnded[0], want) || len(ev.Extracted) != 1 {
		t.Fatalf("run ended = %+v, extracted %v, want %+v", ev.RunEnded, ev.Extracted, want)
	}
}

func TestDeathBanksAQuarterAndStaysDead(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{X: 20})
	w.AddEnemy(Vec2{}, Melee)
	w.players[id].Health = 1
	w.AddRunXP(id, 403)
	w.Restore(id, inventory.Loadout{Items: []items.Instance{potion("p", 2)}})

	var ends []RunEnd
	for range 40 {
		ends = append(ends, w.Step(dt).RunEnded...)
	}
	want := RunEnd{Player: id, Class: "warrior", RunXP: 403, Banked: 100, XP: 100, Level: 2, Levels: 1}
	if len(ends) != 1 || !reflect.DeepEqual(ends[0], want) {
		t.Fatalf("run ended = %+v, want %+v", ends, want)
	}
	if p := playerOf(t, w, id); p.Health != 0 || p.MaxHealth != 125 || p.RunXP != 0 {
		t.Fatalf("health %d/%d run xp %d, want dead at the level 2 max", p.Health, p.MaxHealth, p.RunXP)
	}
	if _, ok := w.RemovePlayer(id); ok {
		t.Fatal("leaving after death banked a second time")
	}
}

func TestLeavingBanksLikeDeath(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.AddRunXP(id, 41)
	if end, ok := w.RemovePlayer(id); !ok || end.Banked != 10 || end.XP != 10 || end.Extracted {
		t.Fatalf("run end = %+v, %v", end, ok)
	}
	if _, ok := w.RemovePlayer(id); ok {
		t.Fatal("removed twice")
	}
}

func TestNoXPAfterTheRunEnds(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	enemy := w.AddEnemy(Vec2{X: 40}, Melee)
	swing(w, AttackCommand{Player: id, Seq: 1, Target: enemy})
	w.players[id].Health = 0
	w.endRun(w.players[id], false)

	w.enemies[enemy].Health = 0
	ev := Events{Damage: []DamageEvent{{Target: enemy, Killed: true}}}
	w.onKill(&ev)
	if len(ev.XP) != 0 || playerOf(t, w, id).RunXP != 0 {
		t.Fatalf("dead player got %+v", ev.XP)
	}
}
