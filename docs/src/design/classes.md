# Classes

A class sets how a player fights. This page sets the first three classes:
Warrior, Rogue and Mage. It also sets the shared stat, resource and ability
model they all use. FOM-100 (data driven character system) turns that model
into data files. FOM-98 (character progression) builds leveling on top.

## Rules that do not bend

- The cave server runs every ability. The client sends "I pressed this, aimed
  here". The server checks cost, cooldown and range, and makes every hit.
- Classes and abilities are data, not code. A new ability should be a new
  row of numbers, unless it adds a new effect type.
- Every ability fits the 20 Hz tick. Times are whole milliseconds, counted
  down by `dt` like enemy timers.
- Every ability that hits hard has a windup. The windup hides latency and
  gives enemies, and later players, a tell.
- Gear matters more than levels. A class gives a kit and a small base. Most
  of a player's numbers come from what they carry, and they can lose it.
- Combat never rolls. Hits and damage are fixed, the same as for enemies.

## What exists today

From `src/internal/sim` and `src/internal/items`:

- A player has 100 health (`Config.PlayerHealth`) and top speed 220.
- One attack. The client sends `AttackRequest` with a `target_id` and
  `ability_id: "basic"`. The client picks the nearest enemy.
- The server hits that one enemy if its center is within 64 px
  (`AttackRange`). No windup. Cooldown is 500 ms, cut by `attack_speed`.
- Damage is the weapon's `damage` stat. Unarmed it is 10 plus any
  `damage` from gear.
- Gear stats are summed in `applyGear`. Stats are `damage`,
  `attack_speed`, `armor`, `max_health`, `move_speed`, `extract_speed`,
  `heal` and `detect_radius`. 50 armor halves a hit. `move_speed` caps at
  +20%.
- Weapons are `rusty_sword`, `iron_sword` and `war_axe`. Items have no
  weapon type yet.
- Enemies wind up before they hit. Ranged enemies fire projectiles. The
  elite has a telegraphed charge. See [Enemy AI](enemy-ai.md).

So the game today is already a Warrior with one button.

## Shared model

### Base stats

A class has a base stat block. It uses the same stat names as items, and
`applyGear` adds it in first, so there is one code path. Caps such as the
`move_speed` cap apply to the total.

| Stat | Warrior | Rogue | Mage |
| -- | -- | -- | -- |
| `max_health` | 120 | 90 | 80 |
| `armor` | 5 | 0 | 0 |
| `move_speed` | 0 | 10 | 0 |

- `Config.PlayerHealth` goes away. Health comes from the class.
- The differences are small on purpose. A Mage in good armor should be as
  tough as a Warrior in rags.
- Rogue speed counts against the 20% cap. That leaves less room for speed
  gear, which is fine. It stops a Rogue from being far faster than enemies
  can ever handle.

### Resources

Each class has one resource. The resource is the main thing that makes the
classes feel different, so each one is picked for how it pushes play.

| | Warrior: rage | Rogue: energy | Mage: mana |
| -- | -- | -- | -- |
| Max | 100 | 100 | 100 |
| Start of run | 0 | 100 | 100 |
| Regen | none | 20 / s | 4 / s |
| Gain | +8 per swing that hits, +6 per hit taken | none | none |
| Decay | 10 / s after 4 s with no hit dealt or taken | none | none |

Why:

- **Rage** rewards standing in melee and taking hits. That is the Warrior's
  job. It starts at 0, so the first pull is always a plain fight. It decays,
  so it cannot be banked between rooms.
- **Energy** refills fast. A Rogue spends in short bursts, then waits a few
  seconds. That gives the Rogue a hit and run rhythm.
- **Mana** refills slow. A Mage plans their spells across a whole room.
  In an extraction run that makes mana potions worth carrying out, and worth
  losing. Potions come later.
- One generic resource shape covers all three: max, start, regen, gain on
  hit dealt, gain on hit taken, decay and decay delay. FOM-100 needs only
  one type.

Basic attacks cost nothing. No class is ever stuck with no button to press.

### Ability shape

Every ability has the same fields.

| Field | Example | Notes |
| -- | -- | -- |
| `id` | `cleave` | Unique. Sent as `ability_id` |
| `name` | Cleave | |
| `cost` | 30 | Resource spent when the windup starts |
| `cooldown_ms` | 4000 | Starts when the windup starts |
| `windup_ms` | 250 | Time from press to effect. 0 is instant |
| `root` | true | Player cannot move during the windup |
| `target` | see below | Shape and size |
| `effects` | list | What happens to each thing hit |

Target shapes:

| Shape | Fields | Hit test |
| -- | -- | -- |
| `arc` | `range`, `arc_deg` | Enemy center within `range` of the player, and within half of `arc_deg` of the aim |
| `projectile` | `speed`, `radius`, `range`, optional `burst` | Same rules as enemy shots in `projectile.go`. Hits enemies only. `burst` makes it hit an area where it stops |
| `area` | `radius` | Enemy center within `radius` of the player |
| `self` | none | The caster |

Effect types:

| Effect | Fields | Notes |
| -- | -- | -- |
| `damage` | `power` | Percent of the player's weapon damage. 100 is one basic hit |
| `status` | `status`, `duration_ms`, `amount` | `stun`, `slow` (percent), `guard` (percent less damage taken) |
| `move` | `distance`, `duration_ms` | Dash along the aim. Stops on a wall. Uses `slide` |
| `blink` | `distance` | Teleport along the aim. Stops short of the first wall |

Rules:

- Damage is `weapon damage * power / 100`, at least 1. Weapon damage is the
  number `attackDamage` gives today. So a Mage's staff and a Warrior's
  sword scale the same way.
- `attack_speed` makes the basic attack's cooldown shorter, as today. It
  does not touch other cooldowns.
- The hit is checked at the end of the windup, against where enemies are
  then. Same rule as enemy attacks. Stepping out during the windup dodges.
- An ability resolves once. No channels or damage over time for now.
- One ability at a time. A press during a windup is dropped.
- Gaining rage on hit counts once per swing, not once per target.

### Aim

Arcs, projectiles, dashes and blinks need a direction, not a target. So
`AttackRequest` gets an `aim` vector. The client sends where the mouse is,
or the move direction when there is no mouse.

- The server uses `aim` when it is set.
- When `aim` is zero, the server aims at `target_id`. That keeps the
  current client working until it sends `aim`.
- Range is center to center, the same as the 64 px today. With both boxes
  16 px from center, a 64 px arc already reaches past touching.

### Latency

At 50 to 150 ms of latency, the player sees enemies a little behind where
the server has them. The design leans on windups instead of rewinding time:

- The client plays the windup animation the moment the key is pressed.
- The server resolves at the end of its windup. A 100 ms windup and a
  generous arc cover most of the gap.
- Cooldown and resource on screen come from the server. The client can grey
  out the button early, but never shows a hit the server did not send.

### Weapons and class

Items get a `weapon_type` field on weapon defs. A class lists the types it
can equip.

| Weapon type | Items today | Warrior | Rogue | Mage |
| -- | -- | -- | -- | -- |
| `sword` | `rusty_sword`, `iron_sword` | yes | yes | |
| `axe` | `war_axe` | yes | | |
| `dagger` | none yet | | yes | |
| `staff` | none yet | | | yes |

- A hard limit, not a penalty. `Equip` rejects a weapon the class cannot
  use, with a clear error. Simple to explain and simple to test.
- Armor and trinkets have no class limit.
- A weapon the player cannot use still drops. It can be carried out and
  sold, and later stashed for another character. In an extraction game
  every item is worth something.
- Drop tables do not weight by class for now. Revisit if one class finds
  too few weapons.
- Unarmed is always allowed. Every class can still fight after losing a
  weapon.
- New items: at least one `dagger` (damage 8 to 11) and one `staff` (damage
  11 to 15) at level 1, before Rogue or Mage ship.

## Warrior

**Role.** Front line. Walks into the fight, holds enemies on itself, and
hits more than one at a time.

**Strengths.** Most health and some base armor. Cleave hits a group. Brace
turns a charge or a volley into a small hit. Rage grows while fighting, so
long fights get easier.

**Weaknesses.** Short reach. Has to walk up to a shooter. Starts every run
with 0 rage, so the opening is weak. No way to close distance fast.

**Resource.** Rage.

| Ability | Cost | Cooldown | Windup | Target | Effects |
| -- | -- | -- | -- | -- | -- |
| `slash` (basic) | 0 | 500 ms | 100 ms | arc, 64 px, 90° | damage 100 |
| `cleave` | 30 | 4 s | 250 ms, root | arc, 72 px, 180° | damage 150 |
| `brace` | 25 | 12 s | 0 | self | guard 50% for 2 s, slow 30% for 2 s |
| `bash` | 20 | 8 s | 150 ms | arc, 56 px, 60° | damage 50, stun 1 s |

Against each archetype:

- **Melee.** Trade hits. An iron sword kills one in three slashes. Two
  melee enemies together are what cleave is for.
- **Ranged.** The hard one. Walk at it and let it back off into a wall.
  Brace through a volley if it has friends.
- **Elite.** Brace during the charge windup to take half of 25. Or bash
  during the windup to stun it and cancel the charge. Then trade hits.

## Rogue

**Role.** Burst and movement. Picks off a single target fast, and gets out.

**Strengths.** Fastest class. Fast basic attack. Dash gets through gaps and
away from a charge. Backstab kills a ranged enemy in one or two hits.

**Weaknesses.** Least armor and low health. Bad against groups, with no
area damage. Energy runs dry after two abilities, then the Rogue has to
wait.

**Resource.** Energy.

| Ability | Cost | Cooldown | Windup | Target | Effects |
| -- | -- | -- | -- | -- | -- |
| `stab` (basic) | 0 | 350 ms | 50 ms | arc, 52 px, 60° | damage 70 |
| `backstab` | 40 | 6 s | 150 ms | arc, 52 px, 60° | damage 120, or 250 from behind |
| `dash` | 25 | 5 s | 0 | self | move 160 px over 200 ms |
| `throwing_knife` | 20 | 3 s | 150 ms | projectile 500 px/s, radius 5, range 300 | damage 60 |

"From behind" means the target is not facing the Rogue. Facing is the
enemy's `Aim` in Detect, Attack and Charge, and its velocity otherwise. The
angle between that and the line to the Rogue is over 90°.

Against each archetype:

- **Melee.** Stab it down, step back when it winds up, step in again.
- **Ranged.** The Rogue's best target. Dash in past its shot, backstab.
- **Elite.** Dash sideways out of the charge line. Backstab from behind
  after the dash ends. Do not stand and trade.

## Mage

**Role.** Range and control. Hits from a distance and slows what gets close.

**Strengths.** Every attack is at range. Fireball hits a group. Frost nova
buys time. Blink is the best escape in the game.

**Weaknesses.** Lowest health. Slowest damage up close. The basic attack's
200 ms windup means a melee enemy in its face wins the trade. Mana runs out
in a long fight.

**Resource.** Mana.

| Ability | Cost | Cooldown | Windup | Target | Effects |
| -- | -- | -- | -- | -- | -- |
| `spark` (basic) | 0 | 600 ms | 200 ms | projectile 450 px/s, radius 6, range 320 | damage 60 |
| `fireball` | 30 | 5 s | 500 ms, root | projectile 380 px/s, radius 8, range 320, burst 56 px | damage 180 |
| `frost_nova` | 35 | 10 s | 150 ms | area 96 px | damage 40, slow 50% for 2 s |
| `blink` | 25 | 8 s | 0 | self | blink 140 px |

Against each archetype:

- **Melee.** Spark it as it walks in. Nova when it arrives, then walk away
  while it is slowed.
- **Ranged.** A shootout the Mage wins on range (320 vs 240), if they dodge
  its shots.
- **Elite.** Keep it at range with spark. Blink out of the charge. Fireball
  while it stands still in its charge windup.

## Starting balance

These are first numbers to play with, not a final balance.

- Basic attack damage per second with a 13 damage weapon: Warrior 26, Rogue
  about 26, Mage 13. Range costs damage.
- The Rogue's higher basic rate is paid for by a shorter reach and less
  health.
- No player ability out-speeds a player. Dash is 800 px/s for 200 ms. Blink
  is one jump. Enemies can still be run from, and so can players later.

## MVP class

**Warrior.** It is what the game already is: melee, swords and an axe, one
attack at 64 px. The MVP mostly turns that attack into the ability model.

- It needs no new item types and no new content in the loot tables.
- It tests the hard parts of the shared model: arcs, windups, cost,
  cooldown, a resource and a self buff.
- It has the best answers to all three enemy archetypes with the fewest new
  systems.

Rogue and Mage need player projectiles, dash or blink movement, a facing
check, and new weapons. That work is a better fit once the model has been
proven on the Warrior.

**In MVP scope:**

- Class and ability data files, and the loader (FOM-100).
- `warrior` class with its base stats and rage.
- `slash`, `cleave` and `brace`.
- `aim` on `AttackRequest`, arc hit test, windups, `root`, cost and
  cooldown.
- The `damage` and `status: guard` and `status: slow` effects, on the
  player only.
- `weapon_type` on weapon defs, and the equip check.
- Protocol changes below, and a rage bar and three buttons in the client.

**Later:**

- `bash`. A stun needs a new enemy state that cancels a windup or a charge.
  That is a change to the enemy FSM, so it gets its own ticket.
- Rogue and Mage, with daggers, staves, player projectiles, dash, blink and
  backstab.
- Class choice in a menu. In MVP every player is a Warrior.
- Mana potions, cooldown reduction and resource stats on gear.
- Friendly fire and PvP. Abilities hit enemies only for now.

## Hooks for progression

FOM-98 owns leveling. This design leaves room for it without picking:

- Base stats are a flat block today. Progression can add a per level block
  on top, still through `applyGear`.
- An ability can get an `unlock_level` field. The class list stays the
  same.
- An ability can get ranks that change its numbers. A rank is the same
  ability shape with new values.
- Talents or skill picks can be effect modifiers keyed by ability id.
- Keep the gap small. A max level character in starter gear should lose to
  a low level one in good gear.

## Data files

Proposed for FOM-100. JSON, embedded like `src/internal/items/data`, so one
loader style covers the repo. Durations are whole milliseconds. Stat names
are `items.Stat` values.

`classes.json`:

```json
[
  {
    "id": "warrior",
    "name": "Warrior",
    "base": { "max_health": 120, "armor": 5 },
    "resource": {
      "kind": "rage",
      "max": 100,
      "start": 0,
      "regen": 0,
      "on_hit_dealt": 8,
      "on_hit_taken": 6,
      "decay": 10,
      "decay_delay_ms": 4000
    },
    "weapons": ["sword", "axe"],
    "basic": "slash",
    "abilities": ["slash", "cleave", "brace"]
  }
]
```

`abilities.json`:

```json
[
  {
    "id": "cleave",
    "name": "Cleave",
    "cost": 30,
    "cooldown_ms": 4000,
    "windup_ms": 250,
    "root": true,
    "target": { "shape": "arc", "range": 72, "arc_deg": 180 },
    "effects": [{ "type": "damage", "power": 150 }]
  },
  {
    "id": "brace",
    "name": "Brace",
    "cost": 25,
    "cooldown_ms": 12000,
    "windup_ms": 0,
    "target": { "shape": "self" },
    "effects": [
      { "type": "status", "status": "guard", "duration_ms": 2000, "amount": 50 },
      { "type": "status", "status": "slow", "duration_ms": 2000, "amount": 30 }
    ]
  }
]
```

Load checks, at startup like items:

- Every ability id a class lists exists, and `basic` is in its list.
- Every `weapons` entry is a known weapon type.
- Every stat name is a known `items.Stat`.
- Each target has the fields its shape needs, and no others.

`weapon_type` in `items.json`:

```json
{ "id": "iron_sword", "name": "Iron Sword", "slot": "weapon", "weapon_type": "sword", ... }
```

## Protocol

Proposed additions to `proto/game/v1/game.proto`. No change yet.

| Message | Field | For |
| -- | -- | -- |
| `JoinRequest` | `string class_id` | Class pick. Empty means `warrior` |
| `JoinAccepted` | `ClassInfo class` | Own class: id, name, resource kind and max, and an `AbilityInfo` per ability (id, name, cost, cooldown, windup). The client has no data files, the same as items |
| `AttackRequest` | `Vec2 aim` | Direction. Zero falls back to `target_id` |
| `EntityState` | `string class_id` | Players only. Picks the look |
| `EntityState` | `int32 resource`, `int32 max_resource` | Players only. The resource bar |
| new `AbilityUsed` event | `source_id`, `ability_id`, `aim`, `windup_ms` | Sent to everyone. Other clients draw the windup and telegraph |
| new `AbilityRejected` | `seq`, `ability_id`, `reason` | Sent to the caster only: on cooldown, no resource, bad id |

- Cooldowns are not in the snapshot. The caster gets `AbilityUsed` and
  counts the cooldown down from the `AbilityInfo` it got on join.
- `DamageEvent` covers ability hits as it is. Its source is the player.
- Statuses on a player can wait for a `statuses` field on `EntityState`
  once something needs to draw them. For MVP the client knows brace is up
  from its own `AbilityUsed`.

## Decisions for now

- One class per character, picked at join. No switching mid-run.
- Three to four abilities per class. No hotbar customizing.
- No ability hits players.
- Statuses do not stack. A new one of the same kind replaces the old.
- Enemies do not react to statuses until `bash` and `frost_nova` land.
