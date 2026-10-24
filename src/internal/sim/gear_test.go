package sim

import (
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func gear(slot items.Slot, stats map[items.Stat]int) items.Instance {
	inst := items.Instance{Name: string(slot), Slot: slot, Base: stats, Quantity: 1}
	if slot == items.Weapon {
		inst.WeaponType = items.Sword
	}
	return inst
}

func equip(t *testing.T, w *World, id EntityID, inst items.Instance) {
	t.Helper()
	if _, _, err := w.Equip(id, inst); err != nil {
		t.Fatal(err)
	}
}

func TestWeaponSetsAttackDamage(t *testing.T) {
	w := NewWorld(passive(), nil)
	player := w.AddPlayer(Vec2{})
	enemy := w.AddEnemy(Vec2{40, 0}, Melee)

	if ev := swing(w, AttackCommand{Player: player, Seq: 1, Target: enemy}); len(ev) != 1 || ev[0].Amount != DefaultConfig().AttackDamage {
		t.Fatalf("unarmed hit = %+v", ev)
	}

	equip(t, w, player, gear(items.Weapon, map[items.Stat]int{items.Damage: 17}))
	steps(w, 20)
	if ev := swing(w, AttackCommand{Player: player, Seq: 2, Target: enemy}); len(ev) != 1 || ev[0].Amount != 17 {
		t.Fatalf("armed hit = %+v", ev)
	}
}

func TestEquipReplacesSlot(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	first := gear(items.Head, map[items.Stat]int{items.Armor: 2})
	first.ID = "first"
	if _, had, _ := w.Equip(id, first); had {
		t.Fatal("empty slot reported an old item")
	}
	old, had, err := w.Equip(id, gear(items.Head, map[items.Stat]int{items.Armor: 5}))
	if err != nil || !had || old.ID != "first" {
		t.Fatalf("old = %+v, %v, %v", old, had, err)
	}
	if p, _ := w.Player(id); p.Equipment[items.Head].Base[items.Armor] != 5 {
		t.Fatalf("head = %+v", p.Equipment[items.Head])
	}
}

func TestEquipRejects(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	if _, _, err := w.Equip(id, gear(items.Consumable, nil)); err == nil {
		t.Fatal("equipped a potion")
	}
	if _, _, err := w.Equip(999, gear(items.Head, nil)); err == nil {
		t.Fatal("equipped on a missing player")
	}
	w.players[id].Health = 0
	if _, _, err := w.Equip(id, gear(items.Head, nil)); err == nil {
		t.Fatal("dead player equipped")
	}
}

func TestMaxHealthGearNeverHeals(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.players[id].Health = 50

	equip(t, w, id, gear(items.Trinket, map[items.Stat]int{items.MaxHealth: 20}))
	if p, _ := w.Player(id); p.MaxHealth != 140 || p.Health != 70 {
		t.Fatalf("health %d/%d, want 70/140", p.Health, p.MaxHealth)
	}
	equip(t, w, id, gear(items.Trinket, nil))
	if p, _ := w.Player(id); p.MaxHealth != 120 || p.Health != 50 {
		t.Fatalf("health %d/%d, want 50/120", p.Health, p.MaxHealth)
	}
}

func TestGearStats(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	p := w.players[id]
	equip(t, w, id, gear(items.Hands, map[items.Stat]int{items.AttackSpeed: 100, items.Armor: 45}))
	equip(t, w, id, gear(items.Feet, map[items.Stat]int{items.MoveSpeed: 50, items.ExtractSpeed: 100}))
	equip(t, w, id, gear(items.Trinket, map[items.Stat]int{items.DetectRadius: 50}))

	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: BasicAbility, Aim: Vec2{1, 0}})
	w.Step(dt)
	if got := p.cooldowns["slash"]; got != 250*time.Millisecond {
		t.Errorf("basic cooldown = %v, want 250ms", got)
	}
	// The Warrior's base 5 armor makes 50.
	if got := w.damageTaken(p, 10); got != 5 {
		t.Errorf("damage taken = %d, want 5", got)
	}
	if got := w.maxSpeed(p); got != 264 {
		t.Errorf("max speed = %v, want 264 (capped at +20%%)", got)
	}
	if got := w.extractTime(p); got != 1500*time.Millisecond {
		t.Errorf("extract time = %v, want 1.5s", got)
	}
	if got := w.detectRadius(DefaultConfig().Archetypes[Melee], p); got != 300 {
		t.Errorf("detect radius = %v, want 300", got)
	}
}

func TestArmorNeverBlocksAllDamage(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	equip(t, w, id, gear(items.Chest, map[items.Stat]int{items.Armor: 10000}))
	if got := w.damageTaken(w.players[id], 10); got != 1 {
		t.Fatalf("damage taken = %d, want 1", got)
	}
}

func TestLoudGearIsSpottedFarther(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{250, 0})
	enemy := w.AddEnemy(Vec2{}, Melee)
	w.Step(dt)
	if e, _ := w.Enemy(enemy); e.State != EnemyPatrol {
		t.Fatal("enemy saw a player outside its radius")
	}
	equip(t, w, id, gear(items.Trinket, map[items.Stat]int{items.DetectRadius: 50}))
	w.Step(dt)
	if e, _ := w.Enemy(enemy); e.State != EnemyDetect {
		t.Fatal("enemy missed a player wearing a beacon")
	}
}

func TestStatsSumClassAndGear(t *testing.T) {
	cfg := passive()
	w := NewWorld(cfg, nil)
	id := w.AddPlayer(Vec2{})

	p, _ := w.Player(id)
	got := w.Stats(p)
	if got[items.MaxHealth] != 120 || got[items.Armor] != 5 || got[items.Damage] != cfg.AttackDamage {
		t.Fatalf("warrior stats = %v", got)
	}
	if _, ok := got[items.Heal]; ok {
		t.Fatalf("stats include heal: %v", got)
	}

	equip(t, w, id, gear(items.Weapon, map[items.Stat]int{items.Damage: 17}))
	equip(t, w, id, gear(items.Chest, map[items.Stat]int{items.Armor: 8, items.MoveSpeed: 50}))
	p, _ = w.Player(id)
	got = w.Stats(p)
	if got[items.Damage] != 17 || got[items.Armor] != 13 || got[items.MoveSpeed] != cfg.MoveSpeedCap {
		t.Fatalf("geared stats = %v", got)
	}
}

func TestCanEquip(t *testing.T) {
	w := NewWorld(passive(), nil)
	staff := gear(items.Weapon, nil)
	staff.WeaponType = items.Staff
	for _, tc := range []struct {
		class string
		inst  items.Instance
		ok    bool
	}{
		{"warrior", gear(items.Head, nil), true},
		{"warrior", gear(items.Weapon, nil), true},
		{"warrior", staff, false},
		{"warrior", gear(items.Consumable, nil), false},
		{"necromancer", gear(items.Head, nil), false},
	} {
		if err := w.CanEquip(tc.class, tc.inst); (err == nil) != tc.ok {
			t.Errorf("CanEquip(%s, %s %s) = %v, want ok %v", tc.class, tc.inst.Slot, tc.inst.WeaponType, err, tc.ok)
		}
	}
}

// A preview has the stats a run would start with, and is not in the world.
func TestPreview(t *testing.T) {
	cfg := passive()
	w := NewWorld(cfg, nil)
	xp := cfg.Classes.Progression.XPToReach(2)
	l := inventory.Loadout{
		Equipment: []items.Instance{gear(items.Chest, map[items.Stat]int{items.Armor: 8})},
		Items:     []items.Instance{potion("p", 2)},
	}
	p, err := w.Preview("warrior", xp, l)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != 2 || w.Stats(p)[items.Armor] != 13 || len(p.Inventory.Items) != 1 || p.Inventory.Size != cfg.InventorySize {
		t.Fatalf("preview = %+v, stats %v", p, w.Stats(p))
	}
	if p.Health != p.MaxHealth || p.MaxHealth != w.Stats(p)[items.MaxHealth] {
		t.Fatalf("preview health = %d/%d", p.Health, p.MaxHealth)
	}
	if len(w.Players()) != 0 {
		t.Fatal("preview added a player to the world")
	}
	if _, err := w.Preview("necromancer", 0, l); err == nil {
		t.Fatal("previewed an unknown class")
	}
}
