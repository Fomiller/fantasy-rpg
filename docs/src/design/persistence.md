# Persistence

Persistence is what the game keeps between runs and across restarts. This
page sets where each kind of data lives, the Postgres schema, the store
interface, what happens when the database is down, and how it all deploys.
It builds on the save model in [Progression](progression.md). FOM-114
(schema, migrations) and FOM-216 (Postgres store) build on it.

## Rules that do not bend

- Postgres is the only source of truth for anything a player owns. No
  other system holds items, XP or the stash.
- The cave server writes. The client never writes and never sees the
  database.
- A run end is one transaction. The stash and the character never disagree
  after a crash.
- No database call runs on the tick loop. A slow query must never stall the
  cave for everyone.
- Migrations only go forward. Each one works with both the old and the new
  server.
- Do not add a system until a feature needs it. One instance-server process and
  one Postgres is the MVP.

## Data ownership

| Store | Owns | MVP |
| -- | -- | -- |
| PostgreSQL | Accounts (by name until auth), characters (class, XP, equipped gear, bag), the stash | Yes |
| Instance-server memory | Run state: positions, health, run XP, ground items, chests, enemies, the seed. Also who is connected | Yes. Lost on a crash, by design |
| Prometheus | Metrics only. Tick time, players online, store latency and errors | Yes. Already scraped |
| Redis | Short-lived shared state once there is more than one server: sessions, presence, a matchmaking queue | Not needed for MVP |
| NATS JetStream | Events between the world server and caves: run started, run ended, player moved | Not needed for MVP |
| MinIO | Large files: client builds, asset packs, run replays | Not needed for MVP |

- Run state stays in memory. It changes 20 times a second and is worthless
  once the run ends. Writing it anywhere would cost a lot and buy nothing.
- Redis and NATS only make sense once there are two processes to share
  state between. Today the lobby and the cave live in one instance-server, and
  `Server.conns` already does what presence would.
- NATS is in `deploy/compose/compose.yaml`, as core NATS. Only chat
  connects to it, see [Chat](chat.md). It carries messages, never the
  truth.
- Backups go to the homelab's S3 bucket, not MinIO. See Backups.
- Each of the later systems gets its own design note when a feature needs
  it. This page does not size or deploy them.

## Where the code saved before FOM-220

All `Store` calls ran on the tick goroutine, either in `Step` or in a
closure from the inbox:

| Call | Where | When |
| -- | -- | -- |
| `Load`, `LoadXP`, `LoadStash` | `openLobby` in `lobby.go` | Join and after every run |
| `Save` (starter kit) | `openLobby` | A name with no loadout |
| `Save` + `SaveStash` | `moveItem` in `lobby.go` | Every lobby move. Two writes, not one |
| `Save` | `sendItems` in `server.go` | Every inventory change in a run |
| `SaveXP` | `saveXP` in `server.go` | Run end and disconnect |
| `LoadStash`, `SaveStash`, `Save` | `stashKept` in `lobby.go` | Extraction |
| `Delete` | `endRun` in `server.go` | Death |

With the memory store this was fine. With Postgres every one of these would
block the tick. A run end is up to four separate writes, so a crash between
them can bank the XP and lose the stash. The store changes below fix both.

## Schema

Two tables. Level is not stored. It comes from XP and `progression.json`,
the same way `LevelFor` works today, so the two can never disagree.

```sql
CREATE TABLE accounts (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    stash       jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stash_is_array CHECK (jsonb_typeof(stash) = 'array'),
    CONSTRAINT stash_size CHECK (jsonb_array_length(stash) <= 100)
);

CREATE TABLE characters (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   bigint NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    class_id     text NOT NULL,
    xp           integer NOT NULL DEFAULT 0,
    equipment    jsonb NOT NULL DEFAULT '[]',
    bag          jsonb NOT NULL DEFAULT '[]',
    last_run_id  uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, class_id),
    CONSTRAINT xp_not_negative CHECK (xp >= 0),
    CONSTRAINT equipment_is_array CHECK (jsonb_typeof(equipment) = 'array'),
    CONSTRAINT bag_is_array CHECK (jsonb_typeof(bag) = 'array'),
    CONSTRAINT bag_size CHECK (jsonb_array_length(bag) <= 20)
);
```

- **Keys.** `accounts.name` is the join name until auth exists. A character
  is one per class per account, as Progression says. MVP is Warrior only,
  so one row each.
- **The stash is on the account.** It is account scope, so every class
  shares it later.
- **Equipment and bag are on the character.** A character always has a
  loadout row, even an empty one. A death writes the starter kit right away,
  so "no loadout" never means "give a starter kit" in the database.
- **`last_run_id`** makes a run-end write safe to retry. See Store interface.
- **Limits.** 100 matches `inventory.StashSize`. 20 matches
  `Config.InventorySize`. Go enforces both first. The checks are a backstop
  against a bug that writes too much. Changing either size needs a
  migration that changes the check.
- **Names** match exactly, as `Server.conns` does today. Anyone who types
  your name gets your stash until auth exists. That is already true with the
  memory store.

### Items are JSONB, not rows

Each list (equipment, bag, stash) is one JSONB array of item instances.

- The server always reads and writes a whole list. Load the stash, change
  it in Go, save the stash. It never asks for one item by id.
- An instance is a nested value: rolled base stats as a map, a list of
  affixes. As rows that is two or three tables and a join on every load.
- Items are immutable after the roll, per [Loot](loot.md). There is no
  partial update that rows would make cheaper.
- Bag order is slot order. A JSON array keeps it for free.
- Balance queries still work, for example
  `SELECT i->>'def' FROM accounts, jsonb_array_elements(stash) i`.

What rows would buy: a unique index on `instance_id` across every list, so
the database itself stops a duplicated item. The server already stops it,
since only one connection per name can play and every write replaces whole
lists. A contract test checks that no instance id appears twice across a
character's equipment, bag and stash after every store operation.

The JSON shape is fixed by a struct with explicit tags in the Postgres
store, never by `items.Instance` field names. Renaming a Go field must not
change saved data. Rarity is saved by name, not by its `iota` number, so
adding a rarity never shifts the ones already saved.

```json
{
  "id": "5f0c...", "def": "iron_sword", "name": "Iron Sword",
  "slot": "weapon", "weapon": "sword", "rarity": "uncommon", "level": 2,
  "base": { "damage": 15 },
  "affixes": [{ "id": "sharp", "name": "Sharp", "stat": "damage", "tier": 1, "value": 2 }],
  "qty": 1, "stack": 1
}
```

- The struct is `item` in `src/internal/db/items.go`. `TestItemJSONShape`
  pins the exact bytes, so a change to it fails a test.
- `weapon` is left out for anything but a weapon. `base` and `affixes` are
  left out when empty.
- A rarity name the server does not know fails the `Load`. The join is then
  rejected like any other load error, instead of handing out a wrong item.

A data change to items is a migration that rewrites the JSON in SQL, for
example with `jsonb_set`.

### Not in MVP

`runs` and `run_results` from Progression's "What to save" are for balance
numbers and support, not for play. They wait until someone needs the
numbers. Progression's `character_equipment` and `stash_items` row tables
are replaced by the JSONB columns above.

## Store interface

FOM-220 replaced the seven calls with four. Each write is one call and one
transaction, and it writes whole values, never a change against what is
stored.

```go
type Character struct {
	Name    string
	Class   string
	XP      int
	Loadout Loadout
	Stash   []items.Instance
}

type Store interface {
	// Load returns false for a name and class that have never joined.
	Load(ctx context.Context, name, class string) (Character, bool, error)
	// Create saves a new character with its starter kit. It is a no-op if the
	// character already exists, so two racing joins cannot both hand out a kit.
	Create(ctx context.Context, c Character) error
	// SaveLobby writes the loadout and stash after a lobby move.
	SaveLobby(ctx context.Context, name, class string, l Loadout, stash []items.Instance) error
	// EndRun writes XP, loadout and stash after a run. Writing the same run id
	// twice is a no-op, so a retry after a lost reply cannot apply it twice.
	EndRun(ctx context.Context, runID string, c Character) error
}
```

- The server builds the whole result before it calls the store. On
  extraction it adds the bag to the stash with `Bag.Add`, in `stashKept`
  does now. On death the loadout is a fresh starter kit. Then `EndRun`
  writes the lot.
- The server keeps the stash in memory from the lobby through the run, so
  run end does not have to read it again. One connection per name makes the
  server's copy the latest one.
- `runID` is a UUID the server makes when the player enters the cave.
  `EndRun` updates the character only where `last_run_id` is not already
  that id. Zero rows updated with a matching id means the write already
  landed.
- `Load` takes the class now, so Rogue and Mage need no interface change.
- `Delete`, `LoadXP`, `SaveXP`, `LoadStash` and `SaveStash` go away.
- `Memory` implements the same four calls with the same rules. One
  contract test suite runs against both stores.
- `db.Store` is the Postgres one, on a `pgxpool.Pool`. `SaveLobby` and
  `EndRun` each run in one transaction. `EndRun` locks the character row,
  and returns early when `last_run_id` already equals `runID`.

### When the server writes

| Moment | Call | Notes |
| -- | -- | -- |
| Join, first time | `Load`, then `Create` | The starter kit |
| Join, after that | `Load` | |
| Lobby move | `SaveLobby` | Replaces `Save` + `SaveStash` |
| Extraction | `EndRun` | Banked XP, gear plus overflow, the new stash |
| Death | `EndRun` | Banked XP, starter kit, stash unchanged |
| Disconnect in a run | `EndRun` | Banked XP as now, the carried loadout, stash unchanged. Same result as today until the 30 s rule lands |
| Inventory change in a run | none | |

Writes during a run stop. Today `sendItems` saves on every pickup so a
disconnect keeps everything. A disconnect now goes through `EndRun`, so it
keeps the same items with one write instead of dozens. A server crash in a
run loses the run: the character is back to what it took in. That matches
Progression, "a server crash mid-run then loses the run, not banked
progress".

### Off the tick loop

- Each client gets a writer goroutine with a queue. Writes for one player
  run in order. Writes for different players run side by side.
- The tick goroutine copies what it needs into the request. The writer never
  touches sim state.
- When a write finishes, the writer sends the result back through
  `Server.inbox`. The lobby opens from the `Character` the server already
  has, not from a fresh `Load`.
- A join claims the name in `conns` on the tick first. Then `Load`, and
  `Create` for a new character, run on the connection goroutine in
  `HandleWS`. Only the starter kit is made on the tick, since the item
  roller is not safe to share.
- The name stays claimed until the writer has run the player's last write.
  A rejoin before then gets "name already playing", so it can never load a
  character that a queued `EndRun` is about to change. The one exception is
  a run end that has not landed: the rejoin takes over the writer and opens
  the lobby from that result.
- Each call gets a 2 s timeout. Join has 5 s in total today
  (`joinTimeout`), which still fits.

## Failure behavior

The database being down never stops a run in progress. It stops joins, and
it delays saves.

| When | What the server does | What the player sees |
| -- | -- | -- |
| Server start | Pings the database and checks the schema version for 30 s, then exits non-zero | Nothing. Kubernetes restarts the pod |
| Join | `Load` fails. The server closes the socket with code 1013 (try again later) and reason `characters unavailable`. It never falls back to a starter kit, which would overwrite real gear later | "Can't load your character. Try again in a minute." on the join screen |
| Lobby move | `SaveLobby` fails. The move is undone in memory and rejected with reason `saving` | The item goes back. A short "Couldn't save, try again" |
| Run end | `EndRun` retries with backoff. After 10 s it keeps the result as a pending save and retries every 5 s in the background | The normal run summary. The lobby shows "Saving..." and Enter Cave is off until it lands |
| Reconnect with a pending save | `Load` is served from the pending save, not the database | The same lobby as before the disconnect |
| Server dies with a pending save | The run result is lost. The character is as it was at cave entry | Their last run is gone. Rare: needs a database outage and a crash together |

- Rejecting a join is better than a starter kit. A starter kit saved later
  would wipe the gear the database still holds.
- Lobby moves and entering a cave wait for a pending save. Otherwise a lobby
  move could save on top of a run that has not landed yet.
- The database is not in `/healthz`. With one replica, a failing readiness
  probe takes away the only endpoint, and a failing liveness probe restarts
  the cave and kills every run. Neither helps a database outage.
- Metrics: `store_duration_seconds` by call, `store_errors_total` by call,
  and `store_pending_saves`. An alert on pending saves above 0 for 5 minutes
  catches an outage. FOM-216 adds the first two with `inventory.Metered`,
  on both stores. Each retry counts as its own call. FOM-223 adds
  `store_pending_saves`, a gauge on `Server` of run ends past their 10 s.

FOM-220 did the join row: close code 1013 and no starter kit. FOM-223 did
the rest:

- `Lobby.saving` is true from the run end until `EndRun` lands, not only
  after the 10 s. The server sends `Lobby` again when it lands.
- Lobby moves while saving get `StashMoveRejected` with reason `saving`.
  Enter Cave gets the new `EnterInstanceRejected`, also with reason `saving`.
- A failed `SaveLobby` puts the lobby back to what the store has and sends
  `StashMoveRejected` with reason `saving`. A newer queued save decides
  instead, since it writes the whole lobby again.
- The join screen shows the close reason. `characters unavailable` reads
  "Can't load your character. Try again in a minute." Other reasons show as
  the server sent them.

## Migrations

- `pressly/goose`, used as a Go library, with the pgx driver
  (`github.com/jackc/pgx/v5/stdlib` for goose's `*sql.DB`, `pgxpool` for the
  store).
- SQL files in `src/internal/db/migrations/`, numbered `00001_accounts.sql`
  and up, embedded with `embed`.
- An `instance-server migrate` subcommand runs them. It reads `DATABASE_URL`,
  waits up to 2 minutes for the database to accept a connection, applies
  every pending migration and exits. The same binary and image as the
  server, so the migration and the code always match.
- Forward only. No down migrations. A rollback is promoting the older image,
  which then runs on the newer schema.
- **Expand, then contract.** Every migration must work with the server
  before it and the server after it. To rename a column: add the new one and
  write both in release N, read the new one in release N + 1, drop the old
  one in release N + 2 or later. A `NOT NULL` column gets a default in the
  same migration.
- At start the server checks that the database version is at least the
  newest migration it embeds. An older server on a newer schema starts fine.
  A newer server on an older schema refuses to start, so a skipped migration
  is loud.

## Deploy

All of it is in this repo's Helm chart, `helm/fantasy-rpg/`. The CNPG
operator and the barman-cloud plugin are already installed in the homelab.
The `fantasy-rpg` Argo CD app is in project `services`, which allows every
namespaced kind, so no homelab change is needed.

### Chart resources

| Resource | Name | Values gate |
| -- | -- | -- |
| CNPG `Cluster` | `fantasy-rpg-db` | `postgres.enabled` |
| `Job`, Argo CD Sync hook | `fantasy-rpg-migrate` | `postgres.enabled` |
| barman-cloud `ObjectStore` | `fantasy-rpg-db-backup` | `postgres.backup.enabled` |
| `ScheduledBackup` | `fantasy-rpg-db-nightly` | `postgres.backup.enabled` |

FOM-221 built the Cluster and the migrate Job. FOM-255 built the backups.

`postgres.enabled` is false in the chart's `values.yaml` and true in
`argocd/overlays/dev/values.app.yaml`. That follows the chart's rule that a
plain render should not need anything from the cluster, like
`externalSecrets: {}`. It is on per environment, so a new environment
starts on the memory store until someone turns it on.

The Cluster follows `attic-db-cnpg` in the homelab:

- `instances: 2`, a primary and one async streaming replica. FOM-254. A
  restart or a node loss fails over in tens of seconds instead of stopping
  joins until the pod is back. See Two instances.
- `imageName` pinned to `ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie`.
  CNPG will not do an implicit major upgrade, so an unpinned image can
  strand the cluster on an operator bump.
- `bootstrap.initdb` with database `fantasy`, owner `fantasy`.
- `storage.size: 2Gi` on `longhorn-single-replica`. One character is about
  10 KB. Longhorn can grow a volume but not shrink it, so start small.
- `monitoring.enablePodMonitor: true`. CNPG marks this field deprecated
  and warns on apply. attic and directus use it too. Swap all three for a
  hand-written `PodMonitor` before CNPG removes it.
- The annotation `argocd.argoproj.io/sync-options: Delete=false,Prune=false`.
  CNPG owns the PVC through the Cluster, so pruning the Cluster deletes the
  data. Removing it has to be done by hand.

### Secrets

- CNPG makes `fantasy-rpg-db-app` itself, with a ready `uri` key. The
  Deployment and the migrate Job both set `DATABASE_URL` from it with
  `secretKeyRef`. No password is in git, Doppler or AWS.
- The server uses Postgres when `DATABASE_URL` is set. The chart sets it
  only when `postgres.enabled` is true. The memory store is used only with
  `STORE=memory`. Otherwise a server with no store configured exits.
- Backups need no secret. They use IRSA, like attic. See Backups.

### Sync waves

Argo CD applies one wave, waits for it to be healthy, then starts the next.

| Wave | Resources |
| -- | -- |
| -2 | `ExternalSecret`s, `ObjectStore` |
| -1 | `Cluster` |
| 0 | migrate `Job` (Sync hook), `ScheduledBackup`, `Service`, `IngressRoute` |
| 1 | `Deployment` |

- The migrate Job is a `Sync` hook, not `PreSync`. On first install the
  Cluster does not exist during PreSync, so a PreSync Job would fail every
  time.
- `hook-delete-policy: BeforeHookCreation`. The Job runs on every sync and
  the last one stays for its logs. Goose skips applied migrations, so a sync
  with nothing new is a fast no-op.
- `backoffLimit: 2`, `activeDeadlineSeconds: 300`. A failed migration fails
  the sync. The Deployment in wave 1 is never updated, so the old server
  keeps running.
- The Deployment is `Recreate`. The old pod runs during the migration and
  stops only in wave 1. That is one more reason a migration has to work
  with the old server.
- Kargo promotes the image tag and the chart version together, so the Job
  always runs the new image's migrations.

**Cluster health.** The wave only waits if Argo CD knows when a `Cluster`
is healthy. Argo CD has a built-in check at
`resource_customizations/postgresql.cnpg.io/Cluster/health.lua` since
v3.1.0. The homelab runs v3.5.6, so no resource customization is needed.
It reports Healthy only for phase "Cluster in healthy state", and
Progressing while the cluster is set up. CNPG writes the app Secret during
setup, so it exists before wave 0. `ExternalSecret` also has a built-in
check. `ObjectStore` and `ScheduledBackup` have none, so Argo CD counts them
healthy once they are applied.

Check it on the first sync: `argocd app get fantasy-rpg` should show the
Job starting only after the Cluster turns Healthy. If it ever starts early,
`instance-server migrate` still waits 2 minutes for the database, so the Job is
slower but does not fail.

### Runbook

Do not delete the Cluster or its PVC. To get data back, restore from
the backups into a new Cluster. See Backups.

Check the database:

```sh
kubectl --context admin@homelab -n fantasy-rpg get cluster fantasy-rpg-db
nix shell nixpkgs#kubectl-cnpg -c \
  kubectl cnpg --context admin@homelab -n fantasy-rpg status fantasy-rpg-db
```

`STATUS` should read `Cluster in healthy state`.

Check the last migration:

```sh
kubectl --context admin@homelab -n fantasy-rpg logs job/fantasy-rpg-migrate
```

Open `psql` as the `postgres` user, inside the database pod:

```sh
nix shell nixpkgs#kubectl-cnpg -c \
  kubectl cnpg --context admin@homelab -n fantasy-rpg psql fantasy-rpg-db -- -d fantasy
```

Without the plugin, `kubectl exec` into the primary does the same. Either
instance can be the primary, so look it up:

```sh
primary=$(kubectl --context admin@homelab -n fantasy-rpg get cluster fantasy-rpg-db -o jsonpath='{.status.currentPrimary}')
kubectl --context admin@homelab -n fantasy-rpg exec -it $primary -c postgres -- psql -d fantasy
```

For a client on your machine, port-forward the read-write Service and use
the `uri` from `fantasy-rpg-db-app`, with the host changed to `localhost`:

```sh
kubectl --context admin@homelab -n fantasy-rpg port-forward svc/fantasy-rpg-db-rw 5432:5432
kubectl --context admin@homelab -n fantasy-rpg get secret fantasy-rpg-db-app -o jsonpath='{.data.uri}' | base64 -d
```

### Two instances

FOM-254. A restart of the only instance broke every join during a load
test. Two instances let CNPG fail over instead.

- `postgres.instances: 2`. One primary, one replica that streams WAL from
  it.
- **Pod anti-affinity is `required` on `kubernetes.io/hostname`.** The two
  pods never share a node. With two Longhorn nodes, a replica whose node is
  down stays `Pending` until that node is back. That is the point: two
  copies on one node are one copy.
- **`longhorn-single-replica`.** Postgres keeps its own copy, so each volume
  has one Longhorn replica. Two Longhorn replicas each would be four copies
  on two nodes. The class has `dataLocality: best-effort`, so Longhorn moves
  the replica to the node the pod runs on.
- **`primaryUpdateMethod: switchover`.** The default, `restart`, stops the
  primary in place for a spec change or an image bump. `switchover`
  promotes the replica first, so a rollout costs a few seconds of writes,
  not a restart.
- **Async replication.** No `synchronous` block, no `minSyncReplicas`. With
  one replica, `dataDurability: required` blocks every commit while the
  replica is down: each rollout, each drain, each node loss. That is the
  outage this ticket removes. `preferred` does not block, but it adds a
  round trip to every commit and falls back to async in exactly those
  moments. The cost of async: a failover can lose the last commits the
  replica had not received. On one LAN that is milliseconds of writes, and
  WAL also goes to S3.
- **Clients use `-rw`.** The `uri` in `fantasy-rpg-db-app` points at
  `fantasy-rpg-db-rw`, the Service for the current primary. inventory and
  the migrate Job are the only clients, and both read it. A failover needs
  no app change. inventory calls fail until the new primary is up. Players
  see "characters unavailable" or "Saving...".
- **PDBs are CNPG's.** It makes `fantasy-rpg-db-primary` with
  `minAvailable: 1`. It makes the replica PDB only at 3 or more instances.
  - Drain the replica's node: nothing blocks it. The replica goes `Pending`
    until the node is back.
  - Drain the primary's node: CNPG sees the node is cordoned and switches
    over first. Then the old primary is a replica and the drain goes on.
  - With one instance the primary PDB blocks the drain for good. That was
    true before this change.
- **A node that does not come back.** The instance on it stays `Pending`,
  because its only volume is there. Destroy it and CNPG builds a new one
  from the primary: `kubectl cnpg destroy fantasy-rpg-db <n>`.

#### Moving the first instance to the new class

CNPG uses `storage.storageClass` only when it creates a PVC. It never
changes an existing one. Only the size and the volume attributes class are
reconciled on existing PVCs, and `storageClassName` cannot change on a PVC
anyway. The webhook allows the change. So the merge does not touch
`fantasy-rpg-db-1`'s volume. It stays on `longhorn` until a person moves it.

What the merge does by itself, once Kargo promotes the chart and Argo CD
syncs:

1. CNPG creates `fantasy-rpg-db-2` on `longhorn-single-replica`, on the
   other node, and clones it from `-1`. Writes go on.
2. `-1`'s pod spec changed, because the anti-affinity went from `preferred`
   to `required`. CNPG rolls it with a switchover: `-2` becomes primary, a
   few seconds without writes, then `-1` comes back as a replica on its old
   `longhorn` volume.
3. Argo CD waits for the Cluster to be healthy, in wave -1, then runs the
   migrate Job.

The steps below move `-1` off the old volume. Both classes are `Retain`, so
a deleted PVC leaves its PV and Longhorn volume behind until a person
deletes them.

```sh
k() { kubectl --context admin@homelab -n fantasy-rpg "$@"; }
cnpg() { nix shell nixpkgs#kubectl-cnpg -c kubectl cnpg --context admin@homelab -n fantasy-rpg "$@"; }
```

**Before the merge.**

1. Check the class exists and Longhorn has two nodes:

   ```sh
   kubectl --context admin@homelab get storageclass longhorn-single-replica
   kubectl --context admin@homelab -n longhorn-system get nodes.longhorn.io
   ```

2. Check WAL archiving works, then take a base backup and wait for it. See
   Backups. This backup is the real rollback.

   ```sh
   k get backup -w
   ```

   Wait for `PHASE` `completed`.

3. Write down `-1`'s PV:

   ```sh
   k get pvc fantasy-rpg-db-1 -o jsonpath='{.spec.volumeName}{"\n"}'
   ```

**After the merge.**

4. Wait for the Cluster, then look at it:

   ```sh
   k get cluster fantasy-rpg-db -w
   cnpg status fantasy-rpg-db
   k get pods -l cnpg.io/cluster=fantasy-rpg-db -L cnpg.io/instanceRole -o wide
   k get pvc -l cnpg.io/cluster=fantasy-rpg-db \
     -o custom-columns=NAME:.metadata.name,CLASS:.spec.storageClassName,PV:.spec.volumeName
   ```

   Go on when all of these hold:
   - `STATUS` is `Cluster in healthy state`.
   - The two pods are on different nodes.
   - `status` shows `-2` streaming, with lag near 0.
   - `-1` is on `longhorn` and `-2` is on `longhorn-single-replica`.

5. If `-1` is still the primary, switch over:

   ```sh
   cnpg promote fantasy-rpg-db 2
   ```

   Wait for `Cluster in healthy state`, and check that `currentPrimary` is
   `fantasy-rpg-db-2`. inventory calls fail for a few seconds.

6. Destroy `-1`. This deletes its pod and its PVC. Only do it when `-1` is
   a replica:

   ```sh
   k get cluster fantasy-rpg-db -o jsonpath='{.status.currentPrimary}{"\n"}'
   cnpg destroy fantasy-rpg-db 1
   ```

7. CNPG makes a new replica on `longhorn-single-replica`, on the node
   without the primary, and clones it from `-2`. CNPG reuses free serials,
   so it is usually `-1` again. Run step 4 again. Both PVCs should now be on
   `longhorn-single-replica`.

8. Check each volume's one Longhorn replica is on its pod's node. Run it
   for each PV from step 4:

   ```sh
   kubectl --context admin@homelab -n longhorn-system get replicas.longhorn.io \
     -l longhornvolume=<pv> -o custom-columns=NAME:.metadata.name,NODE:.spec.nodeID
   ```

   `best-effort` can take a few minutes to move it after the pod starts.

9. Fail over once by hand, as the ticket asks, and watch the game. Promote
   whichever instance is the replica:

   ```sh
   cnpg promote fantasy-rpg-db <replica serial>
   ```

10. After a few days, delete the old volume. The PV from step 3 is
    `Released`:

    ```sh
    kubectl --context admin@homelab get pv <old pv>
    kubectl --context admin@homelab delete pv <old pv>
    kubectl --context admin@homelab -n longhorn-system delete volumes.longhorn.io <old pv>
    ```

**Rollback.**

- Before step 6, nothing is lost. To go back to one instance on the old
  volume, promote `-1` with `cnpg promote fantasy-rpg-db 1`, then revert
  the PR. CNPG removes a replica on scale down, never the primary, so it
  deletes `-2`. Its PV stays, because of `Retain`. Reverting the
  anti-affinity restarts the primary in place once.
- After step 6, the old PV holds data only up to the destroy. Do not bring
  it back. If the new replica will not join, the cluster keeps running on
  `-2` alone. `cnpg destroy fantasy-rpg-db <n>` again to retry.
- If `-2` is lost too, restore from the backup in step 2. See Restore.

### Connections

The Pooler is deferred. Add a CNPG `Pooler` in transaction mode when
connections pass half of `max_connections`. Check it on the primary:

```sh
cnpg psql fantasy-rpg-db -- -tAc \
  "select count(*), current_setting('max_connections') from pg_stat_activity where backend_type = 'client backend'"
```

Grafana has the same number as `cnpg_backends_total` against
`cnpg_pg_settings_setting{name="max_connections"}`. The replica has its own
`max_connections`, and no app connects to it.

### Backups

`postgres.backup.enabled` is false in the chart and true in dev. They
follow `attic-db-cnpg` in the homelab:

- The Cluster gets the `barman-cloud.cloudnative-pg.io` plugin with
  `isWALArchiver: true`. WAL goes to S3 all the time, so a restore can reach
  any point in time, not only the last base backup.
- The `ObjectStore` writes to
  `s3://fomiller-dev-homelab-cnpg-backups/fantasy-rpg-db`, gzip for WAL and
  data. Barman deletes backups older than 30 days. The bucket has no
  lifecycle rule on purpose: one could delete WAL that a base backup needs.
- `ScheduledBackup` takes a base backup at `0 0 3 * * *`, 03:00. The cron
  has six fields because CNPG counts seconds. 03:00 keeps it clear of attic
  at 02:30.
- Credentials are IRSA, the homelab role `FomillerCnpgBackupS3Access`. The
  chart puts its ARN on the Cluster's ServiceAccount. CNPG names that
  ServiceAccount after the Cluster, and the role's trust policy pins
  `system:serviceaccount:fantasy-rpg:fantasy-rpg-db`. **Renaming the Cluster
  breaks backups.** The trust policy also allows `fantasy-rpg-db-restore`,
  for the restore below.

Check that WAL archiving works and a base backup exists:

```sh
kubectl --context admin@homelab -n fantasy-rpg get objectstore,scheduledbackup,backup
primary=$(kubectl --context admin@homelab -n fantasy-rpg get cluster fantasy-rpg-db -o jsonpath='{.status.currentPrimary}')
kubectl --context admin@homelab -n fantasy-rpg exec $primary -c postgres -- \
  psql -U postgres -tAc \
  'select archived_count, failed_count, last_archived_time, last_failed_time from pg_stat_archiver'
```

`last_archived_time` should be newer than `last_failed_time`. The first
base backup comes at 03:00 after the first sync. To take one now:

```sh
kubectl --context admin@homelab apply -f - <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: fantasy-rpg-db-manual-1
  namespace: fantasy-rpg
spec:
  cluster:
    name: fantasy-rpg-db
  method: plugin
  pluginConfiguration:
    name: barman-cloud.cloudnative-pg.io
EOF
```

Argo CD prunes that `Backup` object later. The backup in S3 stays.

#### Restore

Always restore into a new Cluster. Recovery builds an empty cluster, so
never point it at the live one.

1. Apply a Cluster named `fantasy-rpg-db-restore`. Use that name: the
   role's trust policy allows only it and `fantasy-rpg-db`.

   ```sh
   kubectl --context admin@homelab apply -f - <<EOF
   apiVersion: postgresql.cnpg.io/v1
   kind: Cluster
   metadata:
     name: fantasy-rpg-db-restore
     namespace: fantasy-rpg
   spec:
     instances: 1
     imageName: ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie
     storage:
       size: 2Gi
       storageClass: longhorn
     serviceAccountTemplate:
       metadata:
         annotations:
           eks.amazonaws.com/role-arn: arn:aws:iam::695434033664:role/FomillerCnpgBackupS3Access
     bootstrap:
       recovery:
         source: fantasy-rpg-db
         # For a point in time, add: recoveryTarget: {targetTime: "2026-10-27 12:00:00+00"}
     externalClusters:
       - name: fantasy-rpg-db
         plugin:
           name: barman-cloud.cloudnative-pg.io
           parameters:
             barmanObjectName: fantasy-rpg-db-backup
             serverName: fantasy-rpg-db
   EOF
   ```

   It has no `plugins` entry on purpose. It reads the backups but never
   writes to them, so it cannot mix its WAL into the live cluster's.

2. Wait for it to be healthy:

   ```sh
   kubectl --context admin@homelab -n fantasy-rpg get cluster fantasy-rpg-db-restore -w
   ```

   `STATUS` should read `Cluster in healthy state`.

3. Check the data came back. Compare the counts with the live cluster:

   ```sh
   for c in fantasy-rpg-db fantasy-rpg-db-restore; do
     p=$(kubectl --context admin@homelab -n fantasy-rpg get cluster $c -o jsonpath='{.status.currentPrimary}')
     kubectl --context admin@homelab -n fantasy-rpg exec $p -c postgres -- \
       psql -U postgres -d fantasy -tAc \
       'select (select count(*) from accounts), (select count(*) from characters), (select max(version_id) from goose_db_version)'
   done
   ```

4. Use it or remove it. For a drill, delete it:

   ```sh
   kubectl --context admin@homelab -n fantasy-rpg delete cluster fantasy-rpg-db-restore
   ```

   Making it the live database is not covered here. The chart names the
   Cluster `fantasy-rpg-db`, so it needs a chart change, not a hand edit.

> This restore is written from the plugin's API, not from a drill. Run it
> once after the first base backup, before you need it.

## Local dev and tests

### Local Postgres

`deploy/compose/compose.yaml` already runs Postgres for `just deps`.
FOM-114 changes it:

- Image `postgres:18`, the same major as the Cluster. The 18 image keeps its
  data under `/var/lib/postgresql`, so the volume mount moves there. The old
  17 volume will not start on 18. Delete it with `docker compose down -v`.
- New recipes in `repo.nix`, since the justfile is generated:

```just
migrate: deps
    DATABASE_URL=postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable go run ./src/cmd/instance-server migrate

instance-db: migrate
    DATABASE_URL=postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable go run ./src/cmd/instance-server
```

- `just instance` stays on the memory store, so client work needs no Docker.
- The `full` Compose profile gets a one-shot `migrate` service. The
  `instance-server` service depends on it with
  `condition: service_completed_successfully` and sets `DATABASE_URL`.
- The k3d setup (`just deploy-local`) stays on the memory store. It tests
  pods and images, not the database. The real chart path is tested by the
  homelab dev environment.

### Tests

- Unit tests and the contract suite run against `Memory` with no Docker.
- The Postgres store tests use a real Postgres, never a mock.
- They find one this way:
  1. If `TEST_DATABASE_URL` is set, use it. `just deps` gives you one.
  2. Else start `postgres:18` with testcontainers-go's postgres module.
  3. If Docker is not there, skip, unless `CI` is set. Then fail, so CI can
     never skip them by accident.
- Testcontainers fits CI without changing it. `ci.yml` is generated and
  runs `go test ./src/... -race -cover` on `ubuntu-latest`, which has
  Docker. A GitHub Actions `services:` block would need a change to the
  golden-github pack.
- One container per test binary, started in `TestMain`. Migrations run once
  into a template database. Each test gets its own database with
  `CREATE DATABASE ... TEMPLATE`, so tests can run in parallel and never
  share rows.
- All Postgres tests live in one package, `src/internal/db`, so CI starts
  one container, not one per package.
- A migration test applies every migration to an empty database, then runs
  the contract suite. That catches a migration that breaks the store.

## Later

- **Auth.** Add an auth subject column to `accounts` (expand), fill it on
  the next login, then make it the key. Names become display names.
- **More than one instance-server.** "One connection per name" lives in one
  process's memory today. Two processes need a lease on the account row,
  for example an `active_server` column taken in the join transaction. That
  is also when Redis for presence might earn its place.
- **The lobby on the world server.** It writes `SaveLobby`, the cave writes
  `EndRun`. The lease above has to exist first.
- **`runs` and `run_results`.** When balance work needs the numbers. A NATS
  event at run end is one way to feed them without slowing the run end
  write.
- **Read replicas for the game.** The replica takes no reads today. Every
  call goes to `-rw`. Reads from `-ro` would see lag, which the stash path
  cannot take.

## Follow-up tickets

In order. Each one can merge on its own.

1. **Store interface: one write per change, off the tick loop.** The four
   calls above on `Memory`. Server builds whole results, writer goroutine
   per client, no writes during a run, `runID`, the contract suite. No
   Postgres yet. This is the risky part of FOM-216, so it lands first,
   where it is easy to test.
2. **FOM-114: schema, migrations, local Postgres.** `src/internal/db` with
   the embedded goose migrations above, `instance-server migrate`, the schema
   version check at start, Compose on `postgres:18` with the `migrate`
   service, `just migrate` and `just instance-db`.
3. **FOM-216: Postgres store.** `pgxpool` store behind the interface from
   ticket 1, the JSON item shape, `DATABASE_URL` switch in `main.go`,
   testcontainers setup, contract and migration tests, store metrics.
4. **Deploy Postgres with the chart.** `Cluster`, migrate Job, sync waves,
   `DATABASE_URL` from `fantasy-rpg-db-app`, `postgres.enabled` on in the
   overlay. Check the wave order on the first sync.
5. **Postgres backups.** IAM user and policy in `infra/`, key in Secrets
   Manager, `ObjectStore`, plugin on the Cluster, `ScheduledBackup`, a
   restore drill, and a backup section in the README.
6. **Saving state in the protocol and client.** `Lobby.saving`, reason
   `saving`, pending saves on the server, "Saving..." in the lobby, and the
   close reason on the join screen.
