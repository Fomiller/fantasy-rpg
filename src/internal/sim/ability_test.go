package sim

import (
	"math"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// swing submits an attack and steps until its windup is over. It returns the
// damage from those steps.
func swing(w *World, cmd AttackCommand) []DamageEvent {
	w.Submit(cmd)
	events := steps(w, 1)
	for range 20 {
		if p := w.players[cmd.Player]; p == nil || p.cast == nil {
			break
		}
		events = append(events, steps(w, 1)...)
	}
	return events
}

var right = Vec2{1, 0}

func TestBasicIsTheClassBasic(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: BasicAbility, Aim: right})
	ev := w.Step(dt)
	want := AbilityEvent{Source: id, Ability: "slash", Aim: right, Windup: 100 * time.Millisecond, Cooldown: 500 * time.Millisecond}
	if len(ev.Abilities) != 1 || ev.Abilities[0] != want {
		t.Fatalf("abilities = %+v, want %+v", ev.Abilities, want)
	}
}

func TestAbilityRejected(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(w *World, p *Player)
		cmd     AttackCommand
		ability string
		reason  string
	}{
		{"unknown id", func(*World, *Player) {}, AttackCommand{Ability: "fireball", Aim: right}, "fireball", RejectUnknown},
		{"on cooldown", func(_ *World, p *Player) { p.cooldowns["slash"] = time.Second }, AttackCommand{Ability: "slash", Aim: right}, "slash", RejectCooldown},
		{"no rage", func(_ *World, p *Player) { p.Resource = 29 }, AttackCommand{Ability: "cleave", Aim: right}, "cleave", RejectResource},
		{"mid windup", func(w *World, p *Player) {
			p.Resource = 100
			w.applyAttack(AttackCommand{Player: p.ID, Ability: "cleave", Aim: right}, &Events{})
		}, AttackCommand{Ability: "slash", Aim: right}, "slash", RejectBusy},
		{"no aim or target", func(*World, *Player) {}, AttackCommand{Ability: "slash"}, "slash", RejectAim},
		{"non-finite aim", func(*World, *Player) {}, AttackCommand{Ability: "slash", Aim: Vec2{X: math.NaN(), Y: 1}}, "slash", RejectAim},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorld(passive(), nil)
			id := w.AddPlayer(Vec2{})
			enemy := w.AddEnemy(Vec2{40, 0}, Melee)
			p := w.players[id]
			tt.setup(w, p)
			resource := p.Resource

			tt.cmd.Player, tt.cmd.Seq = id, 7
			w.Submit(tt.cmd)
			ev := w.Step(dt)
			want := Rejection{Player: id, Seq: 7, Ability: tt.ability, Reason: tt.reason}
			if len(ev.Rejected) != 1 || ev.Rejected[0] != want || len(ev.Abilities) != 0 {
				t.Fatalf("rejected = %+v abilities = %+v, want %+v", ev.Rejected, ev.Abilities, want)
			}
			if p.Resource != resource {
				t.Fatalf("resource %v -> %v, want unchanged", resource, p.Resource)
			}
			if e, _ := w.Enemy(enemy); e.Health != e.MaxHealth {
				t.Fatalf("enemy health = %d", e.Health)
			}
		})
	}
}

func TestDeadPlayerCannotCast(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	w.AddEnemy(Vec2{40, 0}, Melee)
	w.Submit(AttackCommand{Player: id, Ability: BasicAbility, Aim: right})
	w.Step(dt)
	w.players[id].Health = 0
	if ev := steps(w, 5); len(ev) != 0 {
		t.Fatalf("a dead player's windup landed: %+v", ev)
	}
	w.Submit(AttackCommand{Player: id, Ability: BasicAbility, Aim: right})
	if ev := w.Step(dt); len(ev.Abilities)+len(ev.Rejected) != 0 {
		t.Fatalf("dead player got %+v", ev)
	}
}

func TestCostAndCooldownArePaidWhenTheWindupStarts(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	p := w.players[id]
	p.Resource = 70

	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: "cleave", Aim: right})
	w.Step(dt)
	if p.Resource != 40 || p.cooldowns["cleave"] != 4*time.Second || p.cast == nil {
		t.Fatalf("rage %v cooldown %v cast %v, want 40, 4s and a windup", p.Resource, p.cooldowns["cleave"], p.cast)
	}
	steps(w, ticks(time.Second))
	w.Submit(AttackCommand{Player: id, Seq: 2, Ability: "cleave", Aim: right})
	if ev := w.Step(dt); len(ev.Rejected) != 1 || ev.Rejected[0].Reason != RejectCooldown {
		t.Fatalf("rejected = %+v, want cleave still on cooldown", ev.Rejected)
	}
	steps(w, ticks(3*time.Second))
	w.Submit(AttackCommand{Player: id, Seq: 3, Ability: "cleave", Aim: right})
	if ev := w.Step(dt); len(ev.Abilities) != 1 {
		t.Fatalf("cleave after its cooldown: %+v", ev)
	}
}

func TestStepOutOfTheWindupToDodge(t *testing.T) {
	for _, dodge := range []bool{false, true} {
		w := NewWorld(passive(), nil)
		id := w.AddPlayer(Vec2{})
		enemy := w.AddEnemy(Vec2{40, 0}, Melee)
		w.Submit(AttackCommand{Player: id, Seq: 1, Ability: BasicAbility, Target: enemy})
		events := steps(w, 2)
		if len(events) != 0 {
			t.Fatalf("hit before the windup ended: %+v", events)
		}
		if dodge {
			w.enemies[enemy].Pos = Vec2{100, 0}
		}
		events = steps(w, 1)
		if hit := len(events) == 1; hit == dodge {
			t.Fatalf("dodge %v: events %+v", dodge, events)
		}
	}
}

func TestArcs(t *testing.T) {
	at := []Vec2{
		{40, 0},   // ahead
		{30, 30},  // 45° off
		{0, 40},   // 90° off, on the edge of a 180° arc
		{-40, 0},  // behind
		{68, 0},   // past slash's 64, inside cleave's 72
		{100, 0},  // out of reach
		{0, -500}, // far away
	}
	tests := []struct {
		ability string
		hits    []int
		damage  int
	}{
		{"slash", []int{0, 1}, 10},
		{"cleave", []int{0, 1, 2, 4}, 15},
	}
	for _, tt := range tests {
		t.Run(tt.ability, func(t *testing.T) {
			w := NewWorld(passive(), nil)
			id := w.AddPlayer(Vec2{})
			w.players[id].Resource = 100
			var enemies []EntityID
			for _, pos := range at {
				enemies = append(enemies, w.AddEnemy(pos, Melee))
			}
			events := swing(w, AttackCommand{Player: id, Seq: 1, Ability: tt.ability, Aim: right})
			if len(events) != len(tt.hits) {
				t.Fatalf("got %d hits, want %d: %+v", len(events), len(tt.hits), events)
			}
			for i, n := range tt.hits {
				if events[i].Target != enemies[n] || events[i].Amount != tt.damage {
					t.Errorf("hit %d = %+v, want %d damage on enemy at %v", i, events[i], tt.damage, at[n])
				}
			}
		})
	}
}

func TestCleaveRootsDuringItsWindup(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	p := w.players[id]
	p.Resource = 100
	for seq := uint32(1); seq <= 10; seq++ {
		w.Submit(MoveCommand{Player: id, Seq: seq, Direction: right})
		w.Step(dt)
	}
	// Rooted acts like letting go: friction stops the player. The windup
	// ends on the sixth step and they speed up again.
	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: "cleave", Aim: right})
	var vels []float64
	for seq := uint32(11); seq <= 16; seq++ {
		w.Submit(MoveCommand{Player: id, Seq: seq, Direction: right})
		w.Step(dt)
		vels = append(vels, p.Vel.X)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if want := []float64{120, 20, 0, 0, 0, 80}; !slices.EqualFunc(vels, want, near) {
		t.Fatalf("vel.x = %v, want %v", vels, want)
	}
}

func TestBraceGuardsAndSlows(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	p := w.players[id]
	p.Resource = 25
	if got := w.damageTaken(p, 25); got != 22 {
		t.Fatalf("unbraced charge = %d, want 22 after 5 armor", got)
	}

	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: "brace"})
	if ev := w.Step(dt); len(ev.Abilities) != 1 || p.Resource != 0 {
		t.Fatalf("brace = %+v, rage %v", ev, p.Resource)
	}
	if got := w.damageTaken(p, 25); got != 11 {
		t.Errorf("braced charge = %d, want 11", got)
	}
	if got := w.maxSpeed(p); got != 154 {
		t.Errorf("braced speed = %v, want 154", got)
	}
	if d := w.hurt(0, p, 25); d.Amount != 11 {
		t.Errorf("hurt while braced = %+v", d)
	}

	steps(w, ticks(2*time.Second))
	if got, speed := w.damageTaken(p, 25), w.maxSpeed(p); got != 22 || speed != 220 {
		t.Fatalf("after 2s: damage %d speed %v, want brace gone", got, speed)
	}
}

func TestRageGainAndDecay(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	p := w.players[id]
	if p.Resource != 0 || p.MaxResource != 100 {
		t.Fatalf("rage %v/%d, want a run to start at 0/100", p.Resource, p.MaxResource)
	}
	w.AddEnemy(Vec2{40, 0}, Melee)
	w.AddEnemy(Vec2{40, 10}, Melee)

	if hits := swing(w, AttackCommand{Player: id, Seq: 1, Ability: BasicAbility, Aim: right}); len(hits) != 2 || p.Resource != 8 {
		t.Fatalf("hits %d rage %v, want two hits and 8 rage once per swing", len(hits), p.Resource)
	}
	w.hurt(0, p, 5)
	if p.Resource != 14 {
		t.Fatalf("rage = %v after a hit taken, want 14", p.Resource)
	}

	steps(w, ticks(4*time.Second))
	if p.Resource != 14 {
		t.Fatalf("rage = %v, want no decay for 4s", p.Resource)
	}
	steps(w, ticks(time.Second))
	if p.Resource < 4 || p.Resource > 5 {
		t.Fatalf("rage = %v, want about 10 lost in the next second", p.Resource)
	}
	steps(w, ticks(time.Second))
	if p.Resource != 0 {
		t.Fatalf("rage = %v, want it to stop at 0", p.Resource)
	}

	for range 30 {
		w.hurt(0, p, 1)
	}
	if p.Resource != 100 {
		t.Fatalf("rage = %v, want capped at 100", p.Resource)
	}
}

func TestClassLimitsWeapons(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	weapon := func(id string, wt items.WeaponType) items.Instance {
		inst := item(id, items.Weapon, map[items.Stat]int{items.Damage: 12})
		inst.WeaponType = wt
		return inst
	}
	for _, wt := range []items.WeaponType{items.Sword, items.Axe} {
		if _, _, err := w.Equip(id, weapon(string(wt), wt)); err != nil {
			t.Fatalf("warrior cannot use a %s: %v", wt, err)
		}
	}
	if _, _, err := w.Equip(id, weapon("staff", items.Staff)); err == nil || err.Error() != "a Warrior cannot use weapon" {
		t.Fatalf("err = %v, want a clear refusal", err)
	}

	// Through the bag the staff stays put and the axe stays on.
	w.players[id].Inventory.Add(weapon("staff", items.Staff))
	if do(w, EquipCommand{Player: id, Item: "staff"}) {
		t.Fatal("equip staff reported a change")
	}
	p, _ := w.Player(id)
	if p.Equipment[items.Weapon].ID != "axe" || len(p.Inventory.Items) != 1 {
		t.Fatalf("weapon %q bag %v", p.Equipment[items.Weapon].ID, ids(p.Inventory.Items))
	}

	// Unarmed is always allowed.
	if !do(w, UnequipCommand{Player: id, Slot: items.Weapon}) {
		t.Fatal("could not go unarmed")
	}
}

func TestRestoreBagsWeaponsTheClassCannotUse(t *testing.T) {
	w := NewWorld(passive(), nil)
	id := w.AddPlayer(Vec2{})
	staff := item("staff", items.Weapon, nil)
	staff.WeaponType = items.Staff
	if err := w.Restore(id, inventory.Loadout{Equipment: []items.Instance{staff, item("vest", items.Chest, nil)}}); err != nil {
		t.Fatal(err)
	}
	p, _ := w.Player(id)
	if _, armed := p.Equipment[items.Weapon]; armed || p.Equipment[items.Chest].ID != "vest" || len(p.Inventory.Items) != 1 {
		t.Fatalf("equipment %v bag %v, want the staff in the bag", p.Equipment, ids(p.Inventory.Items))
	}
}

func TestAddPlayerAs(t *testing.T) {
	w := NewWorld(passive(), nil)
	id, err := w.AddPlayerAs(Vec2{}, "", 0)
	if p, _ := w.Player(id); err != nil || p.Class != classes.Starter || p.MaxHealth != 120 || p.Health != 120 {
		t.Fatalf("player = %+v, %v", p, err)
	}
	if _, err := w.AddPlayerAs(Vec2{}, "necromancer", 0); err == nil {
		t.Fatal("joined as a class that does not exist")
	}
}

// Changing the data files changes the game, with no code change.
func TestClassComesFromData(t *testing.T) {
	data, err := classes.Load(fstest.MapFS{
		"data/classes.json": {Data: []byte(`[{
			"id": "warrior", "name": "Warrior",
			"base": { "max_health": 200, "damage": 4 },
			"resource": { "kind": "rage", "max": 50, "start": 50, "regen": 0, "on_hit_dealt": 0, "on_hit_taken": 0, "decay": 0, "decay_delay_ms": 0 },
			"weapons": ["sword"],
			"basic": "slash",
			"abilities": ["slash", "cleave"]
		}]`)},
		"data/abilities.json": {Data: []byte(`[
			{ "id": "slash", "name": "Slash", "cost": 0, "cooldown_ms": 500, "windup_ms": 0, "target": { "shape": "arc", "range": 64, "arc_deg": 90 }, "effects": [{ "type": "damage", "power": 100 }] },
			{ "id": "cleave", "name": "Cleave", "cost": 10, "cooldown_ms": 1000, "windup_ms": 0, "target": { "shape": "arc", "range": 200, "arc_deg": 360 }, "effects": [{ "type": "damage", "power": 300 }] }
		]`)},
		"data/progression.json": {Data: []byte(`{ "max_level": 1, "xp_to_next": [] }`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := passive()
	cfg.Classes = data
	w := NewWorld(cfg, nil)
	id := w.AddPlayer(Vec2{})
	w.AddEnemy(Vec2{-150, 0}, Elite)

	p := w.players[id]
	if p.MaxHealth != 200 || p.Health != 200 || p.Resource != 50 || p.MaxResource != 50 {
		t.Fatalf("player %d/%d hp %v/%d rage, want the data's numbers", p.Health, p.MaxHealth, p.Resource, p.MaxResource)
	}
	w.Submit(AttackCommand{Player: id, Seq: 1, Ability: "cleave", Aim: right})
	ev := w.Step(dt)
	// Unarmed 10 plus 4 base damage, at 300 power, reaching behind.
	if len(ev.Damage) != 1 || ev.Damage[0].Amount != 42 {
		t.Fatalf("damage = %+v, want one 42 damage hit", ev.Damage)
	}
	if p.Resource != 40 || p.cooldowns["cleave"] != time.Second {
		t.Fatalf("rage %v cooldown %v, want 40 and 1s", p.Resource, p.cooldowns["cleave"])
	}
	axe := item("axe", items.Weapon, nil)
	axe.WeaponType = items.Axe
	if _, _, err := w.Equip(id, axe); err == nil {
		t.Fatal("this warrior lost axes in the data but could still equip one")
	}
}
