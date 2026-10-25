# Progression

Progression is how a player gets stronger over many runs. This page sets
stats, leveling, gear growth, ability unlocks, and what gets saved. It
builds on [Loot](loot.md) and the hooks in [Classes](classes.md). FOM-113
and FOM-114 (persistence) take the save model from here.

## Goals

- **Gear is most of a player's power.** Gear is what you risk and what you
  lose. If levels gave most of the power, death would not matter much.
- **Levels give a little permanent growth.** A player always walks away
  from a run with something, even after a bad death.
- **New players catch up fast.** The level cap is low and the gains per
  level are small. A new player with a friend's spare gear can join a
  veteran's run on day one.
- **Death has stakes.** You lose all carried gear and most of the run's XP.
  Extracting is the only way to keep everything.

## Rules that do not bend

- The cave server grants all XP and levels. The client only shows them.
- Level, XP curve and unlocks are data, not code.
- Level never gates gear. Any character can equip any item its class can
  wield. Item level gates what drops, not who can use it.
- A max level character in starter gear loses to a level 1 character in
  good gear. The check under Stats proves this with today's numbers.

## Stats

The stat names are the `items.Stat` values. No new stats for MVP.

| Stat | Effect | Cap | Comes from |
| -- | -- | -- | -- |
| `max_health` | Health. Swapping gear never heals | none | class, level, gear |
| `armor` | Each hit is scaled by 50 / (50 + armor). At least 1 damage | none. The formula already flattens it | class, gear |
| `damage` | Armed: the weapon's damage. Unarmed: 10 plus this | none | gear |
| `attack_speed` | +N% basic attacks per second. Basic attack only | none now. Revisit with T3 affixes | gear |
| `move_speed` | +N% top speed | +20% total | class, gear |
| `extract_speed` | +N% extraction speed. +100% halves the time | propose +100% total | gear |
| `detect_radius` | Enemies notice you N% farther away. A downside stat | none | gear (uniques) |
| `heal` | Health a consumable gives back. Not summed into the player | n/a | consumables |

### How stats combine

`applyGear` already sums class base, then gear. Level adds one step between
them. The full order:

1. Class `base` block from `classes.json`.
2. Level block: the class's `per_level` values times (level - 1).
3. Every equipped item's rolled base stats and affixes.
4. Caps apply to the total, at the point of use. `maxSpeed` caps
   `move_speed` today. `extractTime` would cap `extract_speed` the same way.
5. Statuses (guard, slow) apply on top, at the point of use. They are not
   part of the summed stats.

All values stay whole numbers. One code path, so a level is just another
stat source.

### What a level gives

Warrior `per_level`: `{ "max_health": 5 }`. That is +45 health at level 10.

Health only, on purpose. Damage and armor stay with gear, so the
weapon you carry is still the thing that matters most. Rogue and Mage get
their own `per_level` block when they ship. Mage gets less health per level
than Warrior.

### Gap check

Level 10 Warrior in the starter kit against a level 1 Warrior in fair
cave 1 gear:

| | Level 10, starter kit | Level 1, found gear |
| -- | -- | -- |
| Gear | Rusty Sword (10), Leather Vest (5) | Iron Sword with Sharp (15 + 2), Iron Helm (5), Leather Vest (5), Gloves (2), Boots (2), Bone Charm (8) |
| Health | 120 + 45 = 165 | 120 + 8 = 128 |
| Armor | 5 + 5 = 10 | 5 + 14 = 19 |
| Damage taken from a 25 hit | 20 | 18 |
| Hits to die from 25 | 9 | 8 |
| Damage per slash | 10 | 17 |

The level 1 player deals 70% more damage and dies only one hit sooner. Gear
wins, as the rule asks.

## Leveling

### XP sources

| Source | XP | Who gets it |
| -- | -- | -- |
| Melee kill | 10 | Every player who hit it in the last 10 s |
| Ranged kill | 10 | Same |
| Elite kill | 40 | Same |
| Open a chest | 5 | The player who opens it, once per chest |
| Extract | 25, plus 25% of run XP | The player who extracts |

- Full XP to each player who helped, not a split. Co-op players should
  never fight over the last hit.
- Kill XP is a field on `Archetype`, next to `Loot`. Archetypes are in Go
  today, so it is a Go field, not JSON.
- XP scales with the enemy's level when caves get levels. For now every
  enemy is level 1 and gives the base number.
- No XP for picking up items. That would reward farming the ground instead
  of fighting and leaving.

A cleared loop cave run is about 6 normal kills, 1 elite and 3 chests:
60 + 40 + 15 = 115 run XP. Extracting makes it 115 + 25 + 28 = 168.

### Run XP, death and extraction

XP earned in a cave is **run XP**. It is held on the player until the run
ends.

| Run ends by | Banked XP |
| -- | -- |
| Extraction | All run XP, plus the extraction bonus |
| Death | 25% of run XP, rounded down |
| Disconnect | Same as death. See Persistence |

- 25% on death means a bad run still moves the bar. That keeps new
  players coming back.
- 100% plus a bonus on extraction makes leaving alive worth 6 times
  dying with the same kills, 168 against 28. That is the benefit extraction
  lacks today.
- Banked XP is never lost. Levels never go down.

### XP curve

Cap is level 10 for MVP. Each level needs 50 more XP than the last.

| Level | XP to next | Total XP to reach | Good runs to reach (about) |
| -- | -- | -- | -- |
| 1 | 100 | 0 | 0 |
| 2 | 150 | 100 | 1 |
| 3 | 200 | 250 | 2 |
| 4 | 250 | 450 | 3 |
| 5 | 300 | 700 | 5 |
| 6 | 350 | 1000 | 6 |
| 7 | 400 | 1350 | 9 |
| 8 | 450 | 1750 | 11 |
| 9 | 500 | 2200 | 14 |
| 10 | max | 2700 | 17 |

"Good run" is the 168 XP extracted run above. Deaths stretch this out.
About 17 good runs, or around 20 to 25 real ones, is a few evenings of play.
A short climb is the catch-up plan. The real long game is gear.

XP past the cap still counts and is saved. It can feed a later system,
such as account rewards, without a migration.

## Equipment progression

The loot design already has the pieces. Progression decides how they
line up with cave depth.

- **Item level comes from the cave.** Today `Config.LootLevel` is 1 for
  every drop. Caves, and later zones inside a cave, get a level range.
- **Item level gates bases and affix tiers.** T1 at 1, T2 at 5, T3 at 10.
  Bases have `min_level`, such as War Axe and Iron Helm at 3.
- **Rarity adds affixes.** Elites scale the rare weights by 3.
- **Depth is the risk dial.** Deeper means higher item level, more elites,
  and a longer walk back to the exit.

Proposed cave levels:

| Area | Item level | Affix tiers | What it adds |
| -- | -- | -- | -- |
| Cave 1, near the entrance | 1 to 2 | T1 | Starter upgrades. Iron Sword, leather |
| Cave 1, deep end | 3 to 5 | T1, T2 at 5 | War Axe, Iron Helm, more elites |
| Cave 2 (later) | 5 to 9 | T2 | New bases at 5 and 7 |
| Cave 3 (later) | 10 | T3 | New bases at 10. Uniques that only drop here |

Rough power per tier, to guide new bases:

| Tier | Weapon damage | Total armor, full set | Bonus health from gear |
| -- | -- | -- | -- |
| Item level 1 to 4 | 9 to 20 | 10 to 25 | 0 to 30 |
| Item level 5 to 9 | 18 to 28 | 25 to 50 | 20 to 80 |
| Item level 10 | 26 to 38 | 45 to 80 | 60 to 140 |

Each step is a bigger jump than all 10 levels give. That keeps gear the
thing worth risking.

Item level is not tied to character level. A level 2 player can go deep and
bring back level 5 gear if they live.

## Skills

Abilities unlock by level. Later they get ranks. Both are data in
`abilities.json` and `classes.json`, using the hooks in [Classes](classes.md).

### Unlocks

An ability gets `unlock_level`. A locked ability is in the class list but
the server rejects it with reason `locked`.

Warrior:

| Level | Unlock | Why here |
| -- | -- | -- |
| 1 | `slash` | The basic attack. Always on |
| 2 | `cleave` | After the first run. Groups show up fast |
| 3 | `brace` | Before the player meets many elites |
| 5 | `bash` (later) | Needs the stun FSM state first |
| 7 | `cleave` rank 2 (later) | |
| 10 | one talent pick (later) | |

The full MVP kit is open by level 3. A new player has every button after
one or two runs. That is part of the catch-up plan.

### Ranks (later)

A rank overrides some fields of the same ability. Only the changed fields
are listed:

```json
{
  "id": "cleave",
  "unlock_level": 2,
  "ranks": [
    { "level": 7, "cost": 25, "effects": [{ "type": "damage", "power": 170 }] }
  ]
}
```

- The highest rank at or below the character's level wins.
- Load checks: rank levels go up, and each is above `unlock_level`.
- Ranks are automatic. No points to spend.

### Talents (later)

At level 10, one pick from two or three talents per class. A talent is an
effect modifier keyed by ability id, such as "brace also gives 10 rage".
Picks are saved on the character and can be changed in the lobby for free.
No respec cost, so a bad pick is never a trap.

## Persistence model

Today the inventory store saves a `Loadout` by player name, in memory. This
section is the target for FOM-113 and FOM-114. [Persistence](persistence.md)
sets the schema and store interface. It keeps items as JSONB lists, not the
row tables in "What to save" below.

### Three scopes

| Scope | Lives | Holds |
| -- | -- | -- |
| Account | Forever | Name (until auth), stash, account-wide unlocks later |
| Character | Until deleted | Class, level, banked XP, equipped loadout, talent picks |
| Run | One cave run | Run XP, the bag, ground items, chest state, seed |

- One character per class per account for MVP. Since MVP is Warrior only,
  that is one character.
- Equipment belongs to the character but is at risk in every run.

### Stash and the extraction loop

1. **Town.** The player walks the [hub town](hub-town.md) on the world
   server and opens the stash chest there. They move gear between stash
   and equipment. Bag items can be packed
   too, such as potions.
2. **Enter.** Everything equipped and packed goes into the cave. All of it
   is at risk.
3. **In the cave.** Picked up items go into the bag. XP goes to run XP.
4. **Extract.** Bag items move to the stash. Equipment stays on the
   character. Run XP plus the bonus is banked.
5. **Die.** Equipment and bag drop as one pile, as now. 25% of run XP is
   banked. The character keeps its level. If it has no gear left, it gets
   the starter kit, as now.

Only extraction writes items to the stash. Nothing picked up in a run is
safe before that.

Stash size: 100 slots for MVP, stacks as in the bag. Loot design said no
limit until it is saved to the database. That time is now, so set one.

FOM-215 built this loop. Where it differs from the plan above:

- **The lobby is in the instance-server for now.** The world server is still
  only a health check, so a join lands in a lobby on the same connection.
  Moving it to the world server later changes where the lobby lives, not
  the messages. FOM-227 moved it to the world server, and FOM-230 made it
  the [hub town](hub-town.md).
- **A full stash.** What does not fit stays in the bag, so it goes into
  the next run at risk. `RunSummary.overflow` lists it.
- **After a run the player goes back to the lobby** on the same
  connection, after extraction and after death. A dead player leaves the
  cave right after `RunEnded`.
- The stash is kept by name, like the rest, until accounts exist.
- In the lobby a move takes a whole entry to the stash, the bag or its
  gear slot. Gear already in the slot goes where the new gear came from.
  The server rejects a move with `not_in_lobby`, `unknown_item`, `full`
  or `cannot_equip`.
- Stashing every item saves an empty loadout. Only a death replaces it,
  with a new starter kit, so the kit is not handed out again for an empty
  bag.

### Disconnects

Today a disconnect keeps all loot. That makes "pull the plug" a free escape.

- Proposal: on disconnect the body stays in the cave for 30 s. A reconnect
  in that time puts the player back. After 30 s it counts as a death.
- The 30 s window is in config, so it can change after playtests.

### What to save

| Record | Fields | Written when |
| -- | -- | -- |
| `accounts` | id, name, created_at | First join |
| `characters` | id, account_id, class_id, level, xp, talents | Run end, lobby change |
| `character_equipment` | character_id, slot, item instance | Every equip change, run end |
| `stash_items` | account_id, slot index, item instance | Extraction, lobby moves |
| `runs` | id, seed, cave, started_at, ended_at | Run start and end |
| `run_results` | run_id, character_id, outcome, run_xp, banked_xp, items_out | Run end |

- Item instances keep the shape the loot design set: `instance_id`,
  `def_id`, rarity, item level, rolled base, affixes, quantity.
- Save level and XP only at run end. Run XP is in memory until then. A
  server crash mid-run then loses the run, not banked progress.
- `run_results` is for balance numbers and for support. It is not needed to
  play.
- Run end writes level, XP, equipment and stash in one transaction. The
  stash and the character must never disagree after a crash.

## Data file changes

FOM-100 owns the format, this adds fields. FOM-211 built `per_level` and
`progression.json`. FOM-212 added the `Archetype` field. `unlock_level` is
still a proposal.

`classes.json`, new fields on a class:

```json
{
  "id": "warrior",
  "base": { "max_health": 120, "armor": 5 },
  "per_level": { "max_health": 5 }
}
```

`abilities.json`, new fields on an ability: `unlock_level` (default 1), and
later `ranks`.

New `src/internal/classes/data/progression.json`:

```json
{
  "max_level": 10,
  "xp_to_next": [100, 150, 200, 250, 300, 350, 400, 450, 500],
  "chest_xp": 5,
  "assist_window_ms": 10000,
  "extract_bonus": 25,
  "extract_bonus_percent": 25,
  "death_keep_percent": 25
}
```

`Archetype` in `src/internal/sim/archetype.go` gets an `XP int` field.

Load checks:

- `xp_to_next` has `max_level - 1` entries, all above 0.
- `per_level` uses only known stats.
- `unlock_level` is between 1 and `max_level`. The basic unlocks at 1.

## Protocol changes

FOM-213 and FOM-214 built these.

| Message | Field | For |
| -- | -- | -- |
| `JoinAccepted` | `CharacterInfo character` | name, class_id, level, xp, level_xp, xp_to_next, run_xp (0 at start) |
| `AbilityInfo` | `uint32 unlock_level` | Client greys out locked abilities and shows the level |
| `AbilityRejected` | reason `locked` | New reason |
| new `XpGained` | amount, source (`kill`, `chest`), run_xp | To that player only |
| new `LevelUp` | level, new `ClassInfo` | To that player, after the run ends |
| `Extracted` | `RunSummary summary` | Only in the copy sent to the player who left |
| new `RunEnded` on death | `RunSummary summary` | Same summary for a death |

`RunSummary` holds run_xp, banked_xp, levels_gained, the `CharacterInfo`
after banking, and `kept`: the bag on extraction, empty on death.
`overflow` is the part of `kept` the stash had no room for.

FOM-215 added the lobby:

| Message | Field | For |
| -- | -- | -- |
| new `Lobby` | `CharacterInfo`, `ClassInfo` | On join and after every run. Gear, bag and stash follow |
| new `StashContents` | items, capacity | Sent in the lobby whenever the stash changes |
| new `StashMoveRequest` | item_id, `ItemPlace to` | Stash, bag or gear. Lobby only |
| new `StashMoveRejected` | item_id, reason | `not_in_lobby`, `unknown_item`, `full`, `cannot_equip` |
| new `EnterInstanceRequest` | none | Leaves the lobby. The answer is `JoinAccepted` |

- `CharacterInfo` sends `level_xp` (XP past the start of the level) and
  `xp_to_next`. The client draws the XP bar from them and never needs the
  curve.
- Level changes only at run end. During a run, run XP shows as a second
  segment on the XP bar.
- Order at run end: any `XpGained` from that tick, then `LevelUp` if a level
  was gained, then the summary. On extraction the summary is on `Extracted`.
  `Lobby` comes after it.
- `EntityState.level` stays. Snapshots carry every player's level, so other
  players' levels can be shown later. The ability bar still reads its own
  level from it.
- No stat changes mid-run, so health never jumps in a fight. A death can
  never take away a level the player already saw.

## MVP and later

**MVP:**

- `per_level` health, levels 1 to 10, the XP curve above.
- Kill, chest and extraction XP. Run XP banked by the death and extract
  rules.
- `unlock_level` for `cleave` and `brace`.
- Character record with level and XP, saved with the loadout.
- `CharacterInfo`, `XpGained`, `RunSummary` and the `locked` reason in the
  protocol. An XP bar and a run summary screen in the client.
- The 30 s disconnect rule.

**Later:**

- The lobby on the world server, and the stash in Postgres. FOM-215 put
  both in the instance-server and the memory store.
- Cave levels and deeper zones with higher item level.
- Ability ranks and talents.
- Enemy level scaling for XP.
- Rogue and Mage `per_level` blocks.
- `extract_speed` cap. Only needed once more than one source can stack it.

## Follow-up tickets

- **Add character level and XP to the sim.** `per_level` in classes.json,
  progression.json, level step in `applyGear`, run XP on the player.
- **Grant XP for kills, chests and extraction.** Archetype `XP` field,
  assist window, death and extract banking rules.
- **Gate abilities by unlock_level.** Data field, load check, `locked`
  rejection, `AbilityInfo.unlock_level`.
- **Send progression to the client.** `CharacterInfo`, `XpGained`,
  `RunSummary`, `LevelUp`. XP bar and run summary screen in Godot.
- **Save characters with level and XP.** Character record next to the
  loadout in the store interface, then in Postgres (FOM-114).
- **Stash and extraction flow.** Stash store, bag to stash on extract,
  lobby loadout screen on the world server.
- **Disconnect counts as death after 30 s.** Keep the body in the cave and
  allow reconnect within the window.
- **Cave levels and deep zones.** Per area item level, more elites deeper,
  new bases at item level 5.
- **Ability ranks and level 10 talents.** Rank overrides in abilities.json,
  talent picks on the character.
