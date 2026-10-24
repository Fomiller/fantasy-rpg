package sim

import (
	"testing"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
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
