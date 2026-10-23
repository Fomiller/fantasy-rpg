# Enemy AI

Enemies give the cave its danger. This page sets how they think. FOM-97
(enemy archetypes) builds on it.

## Rules that do not bend

- The cave server runs all enemy AI inside the sim tick. The client only
  draws what the snapshot says.
- Each enemy is a finite state machine (FSM). One state at a time, and a
  fixed list of transitions.
- Archetypes differ by data first. A new archetype should be a new row of
  numbers, not new code, unless it adds a special behavior.
- Enemies are slower than players. A player who runs can always get away,
  except from a telegraphed elite charge.
- The same seed and the same player actions give the same enemy behavior.

## Where the code is

FOM-97 built this design. It lives in `src/internal/sim`:

- `archetype.go`: the `Archetype` table.
- `enemy.go`: states, the transition table (`next`), what each state does
  (`act`), and the charge.
- `projectile.go`: ranged shots.

Facts the design keeps from the first AI (FOM-96):

- Line of sight is a segment test against every wall rect (`canSee`).
- Movement is a straight line at a capped speed, then `slide` against walls.
  There is no pathfinding.
- Enemies step in sorted ID order. Ties in `spot()` go to the lower player
  ID. Damage events come out in the same order every run.
- The player's `detect_radius` stat scales the detect range.

## States

| State | What the enemy does | Leaves to |
| -- | -- | -- |
| Idle | Stands still at a patrol end for `IdleTime` | Patrol, Detect |
| Patrol | Walks its patrol route at `PatrolSpeed` | Idle, Detect |
| Detect | Stops and faces the player for `ReactTime`. This is the "!" moment | Chase, Search |
| Chase | Moves toward the target at `Speed`. Melee closes to touching. A shooter stops once in `AttackRange` | Attack, Search, Retreat, Return, Charge |
| Attack | Winds up for `Windup`, then hits or fires. Does not move | Chase, Search, Return |
| Search | Walks to where it last saw the target, then looks around for `SearchTime` | Chase, Return |
| Retreat | Moves straight away from the target at `Speed` | Chase, Search, Return |
| Return | Walks home along its trail. Ignores players. Heals to full on arrival | Patrol |
| Charge | Elite only. Winds up, then dashes. See archetypes below | Chase |
| Dead | Nothing. Final | none |

## Transitions

Checked top to bottom each tick. The first match wins.

| From | Trigger | To |
| -- | -- | -- |
| any | `Health <= 0` | Dead |
| Idle, Patrol | `spot()` finds a player | Detect |
| Idle, Patrol, Search | hit by a player | Chase, on the attacker |
| Idle | `IdleTime` is up | Patrol |
| Patrol | reached a patrol end | Idle |
| Detect | `ReactTime` is up and target still seen | Chase |
| Detect | target lost during `ReactTime` | Search |
| Chase, Attack, Retreat, Search | farther than `LeashRadius` from home | Return |
| Retreat | low health and target lost | Return |
| Chase, Attack, Retreat | target lost: dead, past `LoseRadius`, or no line of sight | Search |
| Chase | health at or below `RetreatHealth` | Retreat |
| Chase | target closer than `KeepAway` | Retreat |
| Chase | special is ready and its trigger holds | Charge |
| Chase | in `AttackRange`, line of sight, off cooldown | Attack |
| Attack | windup done and the swing made | Chase |
| Retreat | target farther than `KeepAway`, and not low health | Chase |
| Search | `spot()` finds a player | Chase. It is already alert, so no Detect |
| Search | `SearchTime` is up after arriving | Return |
| Return | home within `arriveDistance` | Patrol |
| Charge | dash done, or the dash hit a wall | Chase |

Notes on the rules:

- A hit during Return does nothing. Return heals the enemy to full on
  arrival. That stops a player from hitting an enemy at its leash edge for
  free.
- An Attack in progress is not cancelled when the target walks away. The
  hit lands only if the target is still in range at the end of `Windup`.
  That is the dodge window.
- Search only walks to the last seen spot. It was in line of sight when it
  was seen, so a straight line gets there. This is why no pathfinding is
  needed yet.
- Home may be out of sight by the time the enemy gives up. So while it is
  away from home, the enemy drops a trail point each time it loses sight
  of the last one. Return walks the trail back, newest point first. Each
  leg is a straight line that was walked, so it gets home without A*.
- A wall in the way counts as arriving. A patrol end, a search point or a
  dash that runs into a wall ends there, so an enemy never stands pushing
  into one.
- Low health Retreat is a flee. The enemy does not come back to Chase until
  it has gone home and healed.

## What runs each tick

The order inside `World.Step` stays the same. Enemies step after players
move. Projectiles step right after enemies.

For each live enemy, in ID order:

1. Count down timers by `dt`: attack cooldown, special cooldown, and the
   state timer.
2. Run the transitions above. A transition sets the new state and resets
   the state timer. At most one transition per tick, so the client sees
   every state for at least one snapshot.
3. Run the state's action. It sets `Vel`, and may make a `DamageEvent` or a
   projectile.
4. `slide` moves the enemy against walls. A dashing elite then hits each
   player it overlaps.
5. Drop a trail point if needed.

Then each projectile moves, and hits the first wall or player in its path.
Walls use the same segment test as line of sight. Players count as their
box grown by the projectile radius. On a tie the lower player ID is hit.

Timers are `time.Duration`, counted down by `dt`, like the code today. The
tick rate is fixed for a run (20 by default, `TICK_RATE`).

## Randomness

- AI gets its own generator, `w.ai`, a `math/rand/v2` PCG. `World.SeedAI`
  seeds it from the run seed with a fixed second word (`aiStream`), so it
  never shares a stream with the loot roller. Adding an AI roll does not
  change which loot drops.
- The run seed is the one `cave-server` already logs. `LOOT_SEED` replays
  both loot and AI.
- AI rolls only happen in the tick loop, in enemy ID order.
- What uses it, for now: `IdleTime` jitter (plus or minus 25%), and the
  look-around points during Search (0 to 2 points within 64 px of the
  last seen spot, walked at `PatrolSpeed`). Combat never rolls. Hits and
  damage are fixed so fights read clearly.

## Where the data lives

All in `src/internal/sim`. Go types, no JSON yet. Archetypes only matter to
the sim, and a Go table lets tests change one number. If designers need to
edit them without a build, they move to `src/internal/items/data/`-style
JSON later. Only the loader changes.

```go
type ArchetypeID string // "melee", "ranged", "elite"

type Archetype struct {
	Health        int
	Speed         float64
	PatrolSpeed   float64
	PatrolRadius  float64
	DetectRadius  float64
	LoseRadius    float64
	LeashRadius   float64
	ReactTime     time.Duration
	IdleTime      time.Duration
	SearchTime    time.Duration
	AttackRange   float64
	Damage        int
	Windup        time.Duration
	Cooldown      time.Duration
	RetreatHealth float64 // fraction of max health. 0 never retreats
	KeepAway      float64 // 0 means no kiting
	Projectile    *ProjectileSpec
	Special       Special // SpecialNone or SpecialCharge
	Loot          string  // drop table id

	ChargeMin, ChargeMax float64 // the range band that starts a charge
	ChargeWindup         time.Duration
	ChargeTime           time.Duration
	ChargeSpeed          float64
	ChargeDamage         int
	ChargeCooldown       time.Duration
}

type ProjectileSpec struct {
	Speed  float64
	Radius float64
	Range  float64
}
```

- `Config.Archetypes map[ArchetypeID]Archetype` holds the table.
  `DefaultConfig` fills it. The `Enemy*`, `PatrolSpeed`, `DetectRadius`,
  `LoseRadius`, `LeashRadius` and `EnemyLoot` fields on `Config` move into
  the melee row and go away.
- `Enemy` gets `Archetype ArchetypeID`, `MaxHealth`, a state `timer`,
  `specialCooldown`, `lastSeen`, the Return `trail`, and `Aim` for windups
  and charges. `Aim` is exported because the snapshot sends it.
- `EnemyState` gets the full list: `EnemyIdle`, `EnemyPatrol`,
  `EnemyDetect`, `EnemyChase`, `EnemyAttack`, `EnemySearch`,
  `EnemyRetreat`, `EnemyReturn`, `EnemyCharge`, `EnemyDead`. Keep
  `EnemyPatrol` as the zero value, so a new enemy starts patrolling.
- `AddEnemy(pos, archetype)` takes the archetype and reads health from it.
- Two switches on state: `next` holds the transition table and `act` holds
  what each state does. Archetype numbers come from
  `w.cfg.Archetypes[e.Archetype]`. Code that only one archetype needs,
  like the charge hits, goes in its own function.
- Cave maps: `m` melee, `r` ranged, `E` elite. `caves.EnemySpawn` swaps
  `Health` for `Archetype`.

## Archetypes

**Melee.** Today's enemy with a windup. Walks up and swings.

**Ranged.** Keeps distance and shoots. Backs off when a player gets close,
and stands still while it has a shot.

Ranged uses projectiles, not hitscan:

- A projectile can be dodged. Hitscan cannot, and with 50 to 150 ms of
  latency a hitscan hit would feel unfair.
- A projectile shows on screen, so the player sees where the danger is.
- Walls block it the same way they block movement, with the same segment
  test as line of sight.
- The cost is one more entity kind in the sim and the snapshot. That is
  small.

Projectile rules:

- Fired at the end of `Windup`, aimed at where the target is then. No
  leading, so moving sideways dodges it.
- Moves in a straight line at its speed. It is removed when it hits a
  wall, hits a player, or travels its `Range`.
- Hits players only. No friendly fire on enemies.
- Damage goes through `damageTaken`, so armor applies. The `DamageEvent`
  source is the enemy that fired it.
- Owned by the server. A dead shooter's projectiles keep flying.

**Elite.** A stronger melee enemy with one special: a charge.

- Triggers from Chase when the target is between `ChargeMin` (100) and
  `ChargeMax` (260) px away, in line of sight, and the special is off
  cooldown.
- Charge state: locks `Aim` at the target as it starts, then stands still
  for `ChargeWindup`. Then dashes along `Aim` at `ChargeSpeed` for
  `ChargeTime`.
- The dash does not steer. It stops early on a wall.
- It hits each player it touches at most once per charge, for
  `ChargeDamage`.
- After the dash it goes back to Chase, and the special goes on
  `ChargeCooldown`.
- The elite never retreats and has a longer leash. It drops from
  `cave1_elite` in the loot data.

The Loop cave has two of each common archetype and one elite. Map tiles:
`m` melee, `r` ranged, `E` elite.

## Tunables

Starting numbers. Player top speed is 220 and a player has 100 health.

| Tunable | Melee | Ranged | Elite |
| -- | -- | -- | -- |
| `Health` | 30 | 20 | 90 |
| `Speed` | 140 | 110 | 150 |
| `PatrolSpeed` | 60 | 50 | 60 |
| `PatrolRadius` | 80 | 64 | 80 |
| `DetectRadius` | 200 | 260 | 220 |
| `LoseRadius` | 320 | 380 | 360 |
| `LeashRadius` | 450 | 450 | 600 |
| `ReactTime` | 400 ms | 400 ms | 250 ms |
| `IdleTime` | 1 s | 1.5 s | 1 s |
| `SearchTime` | 3 s | 3 s | 5 s |
| `AttackRange` | 40 | 240 | 44 |
| `Damage` | 10 | 8 | 18 |
| `Windup` | 250 ms | 400 ms | 300 ms |
| `Cooldown` | 1 s | 1.6 s | 1.2 s |
| `RetreatHealth` | 0.25 | 0 | 0 |
| `KeepAway` | 0 | 140 | 0 |
| Projectile speed / radius / range | none | 320 / 6 / 320 | none |
| Special | none | none | charge |
| `ChargeWindup` / `ChargeTime` | | | 600 ms / 400 ms |
| `ChargeSpeed` / `ChargeDamage` | | | 420 / 25 |
| `ChargeMin` / `ChargeMax` | | | 100 / 260 |
| `ChargeCooldown` | | | 6 s |
| `Loot` | `cave1_grunt` | `cave1_grunt` | `cave1_elite` |

Why some numbers are where they are:

- Every `Speed` is under 220, so running works.
- The ranged `KeepAway` (140) is well under its `AttackRange` (240). It
  backs off to a spot where it can still shoot, instead of flip-flopping.
- A projectile at 320 px/s takes about 0.75 s to cross its range. A player
  moving sideways at 220 clears its 6 px radius plus their 16 px half size
  in about 0.1 s. Dodging is easy if the player is watching.
- The charge covers 168 px. A player who sees the 600 ms windup has time to
  step out of the line.

The charge fields live on `Archetype` next to `Special`. They are zero for
the other two.

## What the client gets

`EntityState` in `proto/game/v1/game.proto` carries, for enemies:

| Field | Type | For |
| -- | -- | -- |
| `archetype` | `string` | Picks the look. A string, like item `def_id`, since archetypes are data |
| `enemy_state` | `EnemyState` enum | Picks the animation: walk, alert "!", windup, charge, dead |
| `aim` | `Vec2` | Facing during Detect, Attack and Charge. The client draws the telegraph line from it |
| `max_health` | `int32` | The health bar. Players already sent it |

Also:

- `ENTITY_KIND_PROJECTILE` in `EntityKind`. A projectile sends `id`,
  `position` and `velocity`. The client moves it between snapshots from
  velocity and removes it when it leaves the snapshot.
- No new events. `DamageEvent` covers hits from enemies and projectiles.
  Its source is the enemy, even for a projectile.
- The client tells a charge windup from the dash by velocity. The elite
  stands still during the windup.

The client never runs AI. It only draws the state it is given
(`client/godot/scripts/enemy_view.gd`). The looks are placeholders until
FOM-94.

## When behavior trees are worth it

Not yet. An FSM with a data table covers melee, ranged and elite. Switch
when one of these shows up:

- A new archetype needs a new state, not new numbers, for the second time.
  That means the switches in `next` and `act` are growing per archetype.
- A boss with phases, where each phase is its own sequence of moves.
- Group behavior: flanking, calling for help, taking turns to attack.
- The transitions table stops fitting on one screen.

If that happens, the states above become leaf actions in the tree. The
per-tick order, the RNG and the data table stay the same.

## Decisions for now

These can change once the game is playable. Until then:

- No pathfinding. Straight lines and `slide`.
- No alerting other enemies. Each enemy finds players on its own.
- No hearing. Detection is sight only.
- Enemies do not pick up loot or open chests.
- One target at a time. An enemy does not switch targets mid-chase, even
  if another player hits it.
