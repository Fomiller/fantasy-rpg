# Hub town

The hub town replaces the lobby menu. A player joins the world server and
walks around a small town with other players. They use a stash chest to
pack gear, and walk into a cave entrance to start a run. After the run they
come back to the town. The full world map (FOM-110) stays for later.

It builds on [Instances](instances.md), [Persistence](persistence.md) and
the stash loop in [Progression](progression.md). FOM-230 builds it.

## Rules that do not bend

- Everything in [Instances](instances.md) still holds. One socket at a
  time, the join token, the run lease, the instance server writes run
  results.
- The town is a safe place. No enemies, no combat, no loot, no death.
- The world server stays the only writer of lobby state. The town changes
  where the player stands, not how items are saved.
- One world server, one town. No shards yet.

## Today

- `world-server` runs `Lobby`. It has no tick loop. `Lobby.Run` drains the
  inbox on one goroutine.
- It builds a `sim.World` for the rules and never steps it.
- The client shows `LobbyWindow`: gear, bag, stash, and an "Enter cave"
  button that sends `EnterInstanceRequest`.
- `Lobby` puts the client in `in_lobby`. It sets `player_id` to 0, so the
  client sends no moves.

## Town map

### Built from the caves layout format

The town is a hand-drawn layout parsed by `caves.Parse`, like `loop` and
`yard`.

- One format and one parser for every map. The client already draws
  `InstanceLayout`. Collision is the same `slide` against merged walls.
- `caves` now holds a town too. The package name is a little off. A rename
  to `layouts` is not worth the churn in this ticket.

New tiles:

| Tile | Meaning |
| -- | -- |
| `D` | Cave entrance. One solid rectangle, like `X` |
| `T` | Stash chest. Exactly one. Floor, not a wall |
| `R` | Return spawn, where a player lands after a run |

Parse rules:

- A town has `S`, `R`, `T` and `D`. It has no `m`, `r`, `E`, `c` or `X`.
  Parse rejects a town that breaks this, so a town can never hold an
  enemy, a chest or an exit.
- A cave has `X` and no `D`, `T` or `R`, as now.
- `Cave` gains `Town bool`, `Entrance sim.Rect`, `Stash sim.Vec2` and
  `ReturnSpawns []sim.Vec2`.

### The layout

```
########################
##########DD############
##########..############
#.........R............#
#..##.............##...#
#..##.............##...#
#......................#
#....T....SSS..........#
#......................#
#..##.............##...#
#..##.............##...#
#......................#
########################
```

- 24 by 13 tiles, 1536 by 832 px. Small enough to cross in about 7 s at
  base speed. Big enough that 30 players do not stand on each other.
- **Spawn:** the three `S` tiles in the middle. A first join uses them in
  turn, like a cave's spawns.
- **Stash chest:** `T`, on the same row as the spawn and 5 tiles left.
- **Cave entrance:** `D` at the top, at the end of a one tile corridor.
  The player has to walk into the recess on purpose. Walking past it in
  the square never sends anyone into a run.
- **Return spawn:** `R`, just below the corridor. It is outside `D`, so a
  returning player is not sent straight back in.
- The straight lines from `S` to `T` and from `R` to `D` have no walls.
  Tests can walk them with one direction.
- The town has no position saved. A join always lands on `S` or `R`.

## Town sim

### The world server steps the town

The world server's `rules` world becomes the town and gets stepped.

- `Lobby.Step(dt)` replaces `Lobby.Run`. It drains the inbox, steps the
  world and broadcasts a `WorldSnapshot`, like `Server.Step`.
- `world-server` runs a `tick.Loop`, with metrics labeled `world-server`.
- `sim.World` fits as it is. A town world has walls and players, no
  enemies, no piles, and no extraction zone, so `stepExtraction` does
  nothing.
- `Server` embeds `Lobby` and has its own `Step`, so the instance server
  does not change.

### What runs in the town

- **Movement:** `MoveInput` goes to the sim, as in a cave. Client
  prediction and reconcile work unchanged.
- **Other players:** every player is in the snapshot. The client draws
  them with `remote_player.tscn`, as in a cave.
- **No enemies, no loot:** the layout cannot hold them. Nothing in the
  town can make a pile.
- **No combat:** the world server's `handle` sends only `MoveInput` to the
  sim. `AttackRequest`, `PickupRequest`, `LootOpenRequest`,
  `LootCloseRequest`, `DropRequest` and `UseRequest` are dropped. The sim
  never sees an `AttackCommand`, so there is no `AbilityUsed` and no
  `DamageEvent`. The client also skips ability keys in the town, so it
  does not play a windup the server will never answer.
- **No death:** nothing can deal damage, so health stays full.

### Gear on the sim player

The lobby stays the source of truth for items. The sim player is a mirror,
so the snapshot shows the right level and speed.

- New in `sim`: `Reload(id, xp, loadout)`. It sets XP and level, clears
  gear and bag, then restores the loadout. `Restore` only adds, so it
  cannot be used for this.
- The world server calls it after each lobby move and when a lease clears
  with the banked character.
- `RemovePlayer` takes a player out of the town. It returns a `RunEnd`,
  which the town ignores. Town run XP is always 0.

### Tick rate and cost

- **20 Hz**, the same as an instance. `TICK_RATE` on the world server, as
  on the instance server. Client prediction and the movement fixture are
  tuned at 20 Hz. A slower town would feel different from a cave.
- A player's `EntityState` is about 60 bytes. A snapshot is encoded once
  per tick and sent to everyone.
- At 32 players: about 2 KB a snapshot, 40 KB/s to each client, 1.3 MB/s
  out in total. That is about what a full cave sends each client today
  (8 players and about 20 enemies).
- The sim cost is movement against about 20 walls. It is small next to an
  instance's enemy AI.

### Player cap

- `TOWN_MAX_PLAYERS`, default 32. Players in a run do not count.
- A join past the cap closes with 1013 `town full`. The client shows
  "The town is full. Trying again..." and retries every 2 s, as it does
  for any drop.
- A player coming back from a run is also turned away when the town is
  full. Their run is already saved, so nothing is lost while they wait.
- More than one town is shards, which is FOM-110.

## Stash chest

- Walking up to `T` and pressing interact opens the stash window. It is
  the same key that opens a loot window in a cave.
- Range is 64 px from the chest's center, server position, like loot.
  The client mirrors it as `STASH_RANGE`.
- The window is today's `LobbyWindow`, renamed `StashWindow`. It keeps the
  gear, bag and stash lists and their move buttons. The title and the
  "Enter cave" button go.
- It joins the `windows` group, so Escape closes it. It closes when the
  player walks out of range, like the loot window.
- Moves are `StashMoveRequest`, as now. The world server checks range for
  any move from or to the stash. Too far is rejected with a new reason,
  `far`.
- Moves between bag and gear do not need the chest. They work anywhere in
  the town.

### Equipping in the town

- The inventory panel and character screen work in the town as in a cave.
- `EquipRequest` in the town is a lobby move of that item to gear.
  `UnequipRequest` is a lobby move of the item in that slot to the bag.
  Both go through `lobby.move` and `SaveLobby`, as stash moves do.
- `DropRequest` and `UseRequest` are dropped in the town. The inventory
  panel hides Drop and Use there. A potion is for a run.
- A rejected equip or unequip gets `StashMoveRejected`, so the client
  shows the reason the same way.

## Cave entrance

### Walking in

- Each tick, the world server checks each town player's server position
  against `D`.
- The trigger is on the edge. A player who steps in triggers it once.
  They must step out and back in to try again.
- The trigger runs today's `enterInstance`: wait behind queued lobby
  saves, read the lease, reserve an instance, send `InstanceTicket`.
- On a ticket the player leaves the town sim and stops getting snapshots.
  The client closes and dials the ticket URL, as now.
- `EnterInstanceRequest` goes away. Field 20 is reserved. A walk-in needs
  no request, and one way in is easier to test.

### While waiting

- The client sees its own predicted position enter `D` and shows
  "Entering the cave..." on the status line.
- The player can still move. The wait is one `Load`, so it is short.
- The loading screen is the same as today: the town clears when the
  socket closes, and the cave draws on `JoinAccepted`.

### On reject

`EnterInstanceRejected` with `saving`, `in_run` or `full`, as now.

- The player stays where they are. The server does not push them out.
  Moving a player by force would fight client prediction for no gain.
- The client clears "Entering the cave..." and shows the reason as a toast
  over the town: "Still saving your last run", "Your character is in a
  run", or "All instances are full."
- They step out and back in to try again.
- A failed join on the instance side (token expired, instance full) sends
  the player back to the world, as now. They land on `R` with the notice.

## Return

- After `Extracted` or `RunEnded` the client rejoins the world, as now. It
  sends `JoinRequest` with `returning` set.
- The world server spawns a returning player on `R`, and a fresh join on
  `S`. The flag is only where to stand. A wrong value costs nothing.
- The run summary window stays open over the town. `_on_joined` no longer
  hides it. It closes on Escape or on the next run.
- While the lease is held, the town works. The player can walk. Stash and
  gear moves are rejected with `saving`, and the stash window shows
  "Saving...". The entrance is rejected with `saving`.
- When the lease clears, the world server sends `Lobby` with `saving`
  false and the banked character, calls `Reload` on the sim player, and
  sends the items. The player does not respawn.

## Saving

- The run lease rules do not change. `SaveLobby` fails with `ErrInRun`
  while a lease is held.
- Every town action that changes the character is a lobby move: stash
  moves, equip and unequip. They all go through `moveItem` and
  `saveLobby`, with the same `saving` and `in_run` checks.
- Moving in the town writes nothing.
- The sim player is never saved. It is rebuilt from the lobby.

## Proto changes

```proto
message ClientMessage {
  // EnterInstanceRequest was 20. The town's entrance replaced it.
  reserved 20;
  oneof payload {
    // ...
  }
}

message JoinRequest {
  string name = 1;
  string class_id = 2;
  // Set on the first join after a run, so the town spawns the player at
  // the cave entrance.
  bool returning = 3;
}

message JoinAccepted {
  // ...
  // The world server's town. Moves only: no abilities, pickups, drops or
  // potions.
  bool town = 7;
}

message InstanceLayout {
  string name = 1;
  Rect bounds = 2;
  repeated Rect walls = 3;
  // Empty in the town.
  Rect extraction = 4;
  // Town only. Walking in sends the player to an instance.
  Rect entrance = 5;
  // Town only. Where the stash chest stands.
  Vec2 stash = 6;
}
```

- `EnterInstanceRequest` and `ClientMessage.enter_instance` are deleted.
- `StashMoveRejected.reason` gains `far`.
- `Lobby` stays, with a new comment. It now means "the town state
  changed": the character, the class, and `saving`. It no longer means the
  player has no body. The name is not on the wire, so a rename can come
  later.
- `JoinAccepted`, `WorldSnapshot`, `InstanceLayout`, `StashContents`,
  `EquipmentUpdated`, `InventoryUpdated`, `InstanceTicket` and
  `EnterInstanceRejected` are reused as they are.
- No new message types.

### Join order on the world server

1. `JoinAccepted` with `town` true and the town layout.
2. `Lobby` with the character and `saving`.
3. `EquipmentUpdated`, `InventoryUpdated`, `StashContents`.
4. `WorldSnapshot` every tick from then on.

## Client

### How it tells town from instance

`JoinAccepted.town`. `ServerConnection.in_town` takes its value.

- An explicit field beats guessing from the URL or the layout name.
- `in_lobby` goes away. `in_town` replaces its checks.
- `Lobby` no longer sets `player_id` to 0. It updates `character`,
  `class_info` and `saving`.

### What changes

| File | Change |
| -- | -- |
| `server_connection.gd` | `in_town` from `JoinAccepted`. `Lobby` keeps the body. `send_stash_move` needs `in_town`. `send_attack`, `send_pickup`, `send_loot_open`, `send_drop` and `send_use` do nothing in the town. `send_enter_instance` goes. `returning` on the rejoin after a run. `town full` close text |
| `main.gd` | One joined path for both. In the town: interact opens the stash window in range, ability keys do nothing, the summary is not hidden, "Entering the cave..." when the predicted position is in the entrance. `_on_lobby` becomes `_on_town_state`, which updates the XP bar, character screen and `saving` |
| `lobby_window.gd` | Renamed `stash_window.gd`, class `StashWindow`. Drop the Enter button. Join the `windows` group. Add `far` to the reject text |
| `cave.gd` | Draw `entrance` with a "CAVE" label when it is set, and `extraction` with "EXIT" only when it is set. Draw the stash chest at `stash`, with the chest sprite |
| `inventory_panel.gd` | Hide Drop and Use in the town |
| `main.tscn` | `HUD/Lobby` becomes `HUD/Stash` |

No new scenes. The town draws through the same `Cave` node, `Player` and
`remote_player.tscn`.

## Tests

### Go

- `caves`: the town parses. A town with an enemy, a chest or an `X` is
  rejected. A cave with a `D` is rejected. `D` must be a rectangle.
- `sim`: `Reload` replaces gear and bag, and sets the level.
- `server`, town side:
  - A join gets `JoinAccepted` with `town`, then `Lobby`, then items, then
    snapshots.
  - Two players see each other in the snapshot.
  - `AttackRequest` gives no `AbilityUsed`. `DropRequest` changes nothing.
  - A stash move from range works and saves. From far away it is rejected
    with `far`.
  - Equip and unequip save as lobby moves and change `max_speed` in the
    snapshot.
  - Walking into the entrance sends one ticket. Staying in it sends no
    second one. Walking in while `saving` is rejected.
  - A join with `returning` spawns on `R`.
  - A join past `TOWN_MAX_PLAYERS` closes with `town full`.

### net-test

`net-test` keeps its two world servers and three instances.

- **Join the town:** `JoinAccepted` with `town` true and layout `town`.
  The client's own player is in the snapshot.
- **See a second player:** `two_clients_test.gd` joins two clients to the
  town. Each sees the other in the snapshot and sees them move.
- **Stash chest:** walk from `S` to `T`. Move an item bag to stash. Rejoin
  and find it in the stash.
- **Enter via the entrance:** walk from `R` (or `S`, then to `R`) up into
  `D`. Get the ticket and join the instance.
- **Extract:** `world_test.gd` on the `yard` world loots the chest and
  extracts, as now.
- **Back in town with loot:** the rejoin spawns on `R` with `town` true.
  `saving` turns false. The looted item is in the stash. The summary is
  still kept.

Tests reach `T` and `D` by sending one direction until the server position
is there. The layout keeps those lines clear for this.

## What is left for FOM-110

- **Shards:** more than one town, or more than one world server, and
  picking one at join. Until then the cap is 32.
- **Interest management:** send only players near you, so a town can hold
  more than a snapshot of everyone allows.
- **PvP:** zones outside the town where combat is on.
- **NPCs:** vendors, quest givers, a trainer for talents.
- **Chat and name labels.** The snapshot has no names yet.
- **Parties formed in the town,** and a party walking into the entrance
  together. The world side of parties is still open from Instances.
- **More than one entrance,** one per cave, once there is more than one
  kind of cave.
- **A world map,** with the town as one place on it.

## Implementation plan

It fits one ticket, FOM-230. No new process, no migration, no new chart
resource. It is about the size of FOM-228.

In order, each step building and passing tests:

1. **Layout.** `caves`: the `D`, `T` and `R` tiles, the town rules, the
   new `Cave` fields, `Town()` with the art above. Tests.
2. **Proto.** The fields above, `reserved 20`, the `far` reason. Run
   `just proto`. Fix the compile errors from the deleted
   `EnterInstanceRequest`.
3. **Sim.** `Reload`. Test.
4. **Server.** `Lobby.Step` with the town world. Spawn on join, `S` or
   `R`. Route only moves to the sim. Equip and unequip as lobby moves.
   The stash range check. The entrance edge trigger into `enterInstance`.
   Leave the sim on ticket and on leave. `Reload` on a cleared lease. The
   cap. `world-server` main: `tick.Loop`, `TICK_RATE`, `TOWN_MAX_PLAYERS`.
   Tests.
5. **Client.** `in_town`, `StashWindow`, `cave.gd`, `main.gd`, the
   inventory panel, `returning`. Move the lobby tests to the stash window.
6. **net-test.** The cases above.
7. **Docs.** "What FOM-230 built" here. The lobby lines in Progression and
   Instances point to this page.

If it has to split, the cut is after step 4: the server half behind a
client that still sends `EnterInstanceRequest`. That needs field 20 kept
for one more ticket, so splitting only pays if the client work slips.
