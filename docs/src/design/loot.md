# Loot

Loot is the reason to go into a cave. Losing it on death is the risk. This
page sets the rules for how items are made and how they drop. FOM-102 (items)
and FOM-103 (inventory) build on it.

## Rules that do not bend

- The cave server makes every roll. The client never picks or changes an item.
- Item definitions are data files, not code.
- An item stores the values it rolled. Changing a definition later does not
  change items players already own.
- The client gets item names and stats in messages. It has no copy of the
  data files, the same way it has no copy of the cave map.

## Two kinds of item data

**Item definition.** Static. Written by hand in a data file.

| Field | Example | Notes |
| -- | -- | -- |
| `id` | `iron_sword` | Unique across all items |
| `name` | Iron Sword | |
| `slot` | `weapon` | `weapon`, `head`, `chest`, `hands`, `feet`, `trinket`, `consumable`, `material` |
| `base` | `{ damage: [8, 12] }` | Stats every copy has. A range rolls once per item |
| `min_level` | 1 | Lowest item level this base can drop at |
| `stack` | 1 | Above 1 for consumables and materials |
| `value` | 20 | Sell price. Also used for balance checks |

**Item instance.** Rolled. One per real item a player can hold.

| Field | Notes |
| -- | -- |
| `instance_id` | UUID, made by the server |
| `def_id` | Points at the definition |
| `rarity` | See below |
| `item_level` | Comes from where it dropped |
| `base` | The rolled base stats |
| `affixes` | List of `{ affix_id, tier, value }` |
| `quantity` | For stacks |

## Rarity

| Rarity | Affixes | Base weight | Color |
| -- | -- | -- | -- |
| Common | 0 | 700 | grey |
| Uncommon | 1 | 220 | green |
| Rare | 2 to 3 | 70 | blue |
| Epic | 4, or as many as the slot allows | 10 | purple |
| Unique | fixed | own table | orange |

Weights are out of 1000. A drop source can scale them. For example, an elite
enemy multiplies every weight above Common by 3. Consumables and materials
are always Common.

## Item level

Item level says how strong an item can be.

- An item's level is the level of the thing that dropped it. Enemies and
  chests get their level from the cave.
- Item level gates which bases can drop (`min_level`) and which affix tiers
  can roll.
- The first cave is level 1 to 5. Deeper or harder caves raise it later.

## Affixes

An affix is one extra stat on an item. Each affix is either a prefix or a
suffix.

- At most 2 prefixes and 2 suffixes.
- Each affix belongs to a group. An item cannot have two affixes from the
  same group, so no item gets two "+damage" lines.
- Each affix lists the slots it can roll on.
- Each affix has tiers. A tier sets the minimum item level and the value
  range.

First affix pool:

| Affix | Type | Group | Slots | T1 (lvl 1) | T2 (lvl 5) | T3 (lvl 10) |
| -- | -- | -- | -- | -- | -- | -- |
| Sharp | prefix | damage | weapon | +1 to 2 damage | +3 to 4 | +5 to 7 |
| Quick | prefix | attack_speed | weapon, hands | +3 to 5% | +6 to 9% | +10 to 14% |
| Sturdy | prefix | armor | head, chest, hands, feet | +2 to 4 armor | +5 to 8 | +9 to 14 |
| of Vigor | suffix | max_health | any gear | +5 to 10 hp | +11 to 20 | +21 to 35 |
| of Haste | suffix | move_speed | feet | +2 to 4% | +5 to 7% | +8 to 10% |
| of Escape | suffix | extract_speed | trinket, feet | +5 to 10% | +11 to 20% | +21 to 30% |

Move speed has a hard cap of +20% total, so enemies can still catch up with
a player.

## What stats do

Stats from all equipped items add up. The sim applies them like this:

| Stat | Effect |
| -- | -- |
| `damage` | With a weapon, attack damage is the damage stat. Without one, it is 10 plus the stat |
| `attack_speed` | +N% attacks per second. +100% halves the cooldown |
| `armor` | Each hit is scaled by 50 / (50 + armor), so 50 armor halves it. A hit always does at least 1 |
| `max_health` | Adds to the base 100. Swapping gear moves current health by the same amount, so it never heals |
| `move_speed` | +N% top speed, capped at +20% |
| `extract_speed` | +N% extraction speed. +100% halves the time in the exit zone |
| `detect_radius` | Enemies notice the player N% farther away. A downside stat for uniques |
| `heal` | Health a consumable gives back. Used by FOM-103 |

Every player gets a Common Rusty Sword and Leather Vest on join. This is a
stand-in until the stash can carry gear into a run.

## How one item is rolled

1. Pick an entry from the drop table (see below). If it is an item, go on.
2. Keep only the bases in that entry with `min_level` at or below the item
   level. Pick one by weight.
3. Roll each base stat once in its range.
4. Roll rarity with the source's weights. Skip for consumables and materials.
5. Roll the affix count for that rarity. Rare picks 2 or 3, half and half.
   If the pool for the slot runs out first, the item keeps what it has.
6. Pick affixes one at a time by weight. Skip any affix whose group is
   taken, whose type is full, or that cannot roll on this slot.
7. For each affix, use the highest tier the item level allows. Roll a whole
   number in that tier's range.

All values are whole numbers. Percent stats are stored as whole percents.

## Randomness

- Each cave run gets one seed. The cave server logs it on start.
- Every roll in that run takes numbers from one generator made from that
  seed. The tick loop is the only caller, so the order is fixed.
- The same seed and the same player actions give the same loot. That makes
  bugs easy to replay in a test.

## Unique items

A unique is a hand-made definition with fixed affixes.

- Its affix values can still roll in a range.
- It only drops from tables that name it. The normal rarity roll can never
  make one.
- Its rarity is always Unique.
- At most one copy of a given unique drops per cave run.

Example: **Lantern of the Deep**, trinket. +25% extract speed, +10 to 20 max
hp, and enemies detect the holder from 20% farther away. Faster exits, more
fights.

## Drop tables

A drop table is a list of weighted entries. A source rolls its table a set
number of times.

```json
{
  "id": "cave1_grunt",
  "rolls": 1,
  "entries": [
    { "weight": 60, "nothing": true },
    { "weight": 25, "item": "health_potion" },
    { "weight": 10, "table": "cave1_gear" },
    { "weight": 5,  "item": "iron_ore", "quantity": [1, 3] }
  ]
}
```

- An entry is an item, another table, or nothing.
- A table entry rolls that table once. Tables can nest, but must not loop.
  A test checks for loops.
- `quantity` only applies to items that stack.
- Each source names its table: an enemy type, a chest type, or a boss.

First tables:

| Table | Used by | Rolls | Nothing |
| -- | -- | -- | -- |
| `cave1_grunt` | normal enemy | 1 | 60% |
| `cave1_elite` | elite enemy | 2 | 10% |
| `cave1_chest` | chest | 2 to 3 | 0% |
| `cave1_gear` | other tables | 1 | 0% |

## Drops in the world

- A drop becomes a ground item at the spot it dropped, owned by the server.
- Any player can pick it up with interact within 48 px. First one there
  gets it. This is a PvP extractor, so loot is not reserved.
- Ground items go away when the cave run ends.
- Everything a player carries in the cave is at risk. On death, it drops as
  one bag at that spot, and anyone can loot it.
- Items are only safe after extraction. Then they move to the player's
  stash. Saving the stash is part of FOM-103 and the database work.

## Inventory

FOM-103 built the first version of this.

- A player has equipment, one item per gear slot, and a bag of 20 slots.
  A stack takes one slot. Stacks fill up before a new slot is used.
- Interact on a chest or ground pile takes everything that fits. The rest
  stays. An empty pile goes away. An empty chest stays, opened.
- The loop cave has three chests. Each rolls `cave1_chest` when the run
  starts. A killed enemy rolls `cave1_grunt` and drops a pile if it got
  anything.
- From the bag a player can equip, drop, or use a consumable. Equipping
  swaps the old item into the bag. Unequip needs a free bag slot. Drop puts
  the whole entry on the ground at the player's feet.
- On death, all equipment and bag items drop as one pile.
- The client sends requests. The server checks range, ownership and room,
  then sends the new `EquipmentUpdated` and `InventoryUpdated`. A request it
  cannot do is ignored.

Saving, for now:

- The server saves a player's loadout under their name after every change.
  Joining with that name again restores it.
- A player who loses everything, by death or by dropping it all, starts over
  with the starter kit.
- Only one connection per name at a time.
- The store is in memory, so a server restart forgets it. Postgres replaces
  it in FOM-114. The stash and the at-risk rules above still apply then.

## Data files

- Definitions, affixes, uniques and drop tables live as JSON in
  `src/internal/items/data/`. The server embeds them at build time.
- JSON for now, because Go reads it with no extra library. Switching to
  YAML or TOML later only changes the loader.
- Tests check the data on every build:
  - all ids are unique
  - every reference points at something that exists
  - every weight is above 0
  - tables do not loop
  - every tier range has min at or below max

## Decisions for now

These can change once the game is playable. Until then:

- No gold. The economy is items and materials only.
- No durability. Items do not wear out.
- No stash limit yet. Add one when the stash is saved to the database.
- No trading between players yet. Revisit after the stash exists.
- All gear a player brings into a cave is at risk. Nothing is safe on death.
