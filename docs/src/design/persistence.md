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
- Do not add a system until a feature needs it. One cave-server process and
  one Postgres is the MVP.

## Data ownership

| Store | Owns | MVP |
| -- | -- | -- |
| PostgreSQL | Accounts (by name until auth), characters (class, XP, equipped gear, bag), the stash | Yes |
| Cave-server memory | Run state: positions, health, run XP, ground items, chests, enemies, the seed. Also who is connected | Yes. Lost on a crash, by design |
| Prometheus | Metrics only. Tick time, players online, store latency and errors | Yes. Already scraped |
| Redis | Short-lived shared state once there is more than one server: sessions, presence, a matchmaking queue | Not needed for MVP |
| NATS JetStream | Events between the world server and caves: run started, run ended, player moved | Not needed for MVP |
| MinIO | Large files: client builds, asset packs, run replays | Not needed for MVP |

- Run state stays in memory. It changes 20 times a second and is worthless
  once the run ends. Writing it anywhere would cost a lot and buy nothing.
- Redis and NATS only make sense once there are two processes to share
  state between. Today the lobby and the cave live in one cave-server, and
  `Server.conns` already does what presence would.
- NATS is in `deploy/compose/compose.yaml` already. Leave it there, but
  nothing connects to it for MVP. When it is used, it carries events, never
  the truth. A consumer that misses an event reads Postgres.
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
  character that a queued `EndRun` is about to change.
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
  on both stores. Each retry counts as its own call. `store_pending_saves`
  waits for FOM-223.

FOM-220 does the join row: close code 1013 and no starter kit. `EndRun`
retries for 10 s, then logs the error and drops the result. A failed
`SaveLobby` is only logged. The pending save, the undo and the `saving`
reason wait for FOM-223.

The `saving` state needs two protocol changes: a `saving` field on `Lobby`
and a new `saving` reason on `StashMoveRejected`. The client also needs to
show the WebSocket close reason on the join screen. Today
`server_connection.gd` only prints the close code.

## Migrations

- `pressly/goose`, used as a Go library, with the pgx driver
  (`github.com/jackc/pgx/v5/stdlib` for goose's `*sql.DB`, `pgxpool` for the
  store).
- SQL files in `src/internal/db/migrations/`, numbered `00001_accounts.sql`
  and up, embedded with `embed`.
- A `cave-server migrate` subcommand runs them. It reads `DATABASE_URL`,
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
| `ExternalSecret` (existing template) | `fantasy-rpg-db-backup` | an entry in `externalSecrets` |
| barman-cloud `ObjectStore` | `fantasy-rpg-db-backup` | `postgres.backup.enabled` |
| `ScheduledBackup` | `fantasy-rpg-db-nightly` | `postgres.backup.enabled` |

`postgres.enabled` is false in the chart's `values.yaml` and true in
`argocd/overlays/values.app.base.yaml`. That follows the chart's rule that a
plain render should not need anything from the cluster, like
`externalSecrets: {}`. `postgres.backup.enabled` is false in both until the
backup credentials exist.

The Cluster follows `attic-db-cnpg` in the homelab:

- `instances: 1`. Durability comes from backups, not a replica. An outage
  stops joins, not runs.
- `imageName` pinned to `ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie`.
  CNPG will not do an implicit major upgrade, so an unpinned image can
  strand the cluster on an operator bump.
- `bootstrap.initdb` with database `fantasy`, owner `fantasy`.
- `storage.size: 2Gi` on `longhorn`. One character is about 10 KB. Longhorn
  can grow a volume but not shrink it, so start small.
- `monitoring.enablePodMonitor: true`.
- The annotation `argocd.argoproj.io/sync-options: Delete=false,Prune=false`.
  CNPG owns the PVC through the Cluster, so pruning the Cluster deletes the
  data. Removing it has to be done by hand.

### Secrets

- CNPG makes `fantasy-rpg-db-app` itself, with a ready `uri` key. The
  Deployment and the migrate Job both set `DATABASE_URL` from it with
  `secretKeyRef`. No password is in git, Doppler or AWS.
- The server uses Postgres when `DATABASE_URL` is set, and the memory store
  when it is not. The chart sets it only when `postgres.enabled` is true.
- Backup credentials come through the chart's existing `externalSecrets`
  map. See Backups.

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
`cave-server migrate` still waits 2 minutes for the database, so the Job is
slower but does not fail.

### Backups

Off by default. When turned on, they follow `attic-db-cnpg`:

- The Cluster gets the `barman-cloud.cloudnative-pg.io` plugin with
  `isWALArchiver: true` and `barmanObjectName: fantasy-rpg-db-backup`.
- The `ObjectStore` writes to
  `s3://fomiller-dev-homelab-cnpg-backups/fantasy-rpg-db`, gzip for WAL and
  data, `retentionPolicy: 30d`.
- `ScheduledBackup` runs at `0 0 3 * * *`, 03:00. The cron has six fields
  because CNPG counts seconds. 03:00 keeps it clear of attic at 02:30.
- Credentials are an access key, not IRSA. The homelab's IRSA role pins
  each cluster's ServiceAccount in its trust policy, and changing that is a
  homelab change. Instead an IAM user in this repo's `infra/` gets write
  access to the `fantasy-rpg-db/` prefix only. Its key goes into AWS Secrets
  Manager at `dev/fomiller/homelab/fantasy-rpg-db-backup`. The overlay adds
  that as an `externalSecrets` entry, and `ObjectStore.s3Credentials` reads
  `ACCESS_KEY_ID` and `ACCESS_SECRET_KEY` from it.
- A long-lived key is the cost. Rotate it by making a new key in `infra/`.
  The ExternalSecret picks it up within an hour.
- A backup that was never restored is not a backup. The backup ticket ends
  with a restore into a scratch Cluster, as in the attic README.

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
    DATABASE_URL=postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable go run ./src/cmd/cave-server migrate

cave-db: migrate
    DATABASE_URL=postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable go run ./src/cmd/cave-server
```

- `just cave` stays on the memory store, so client work needs no Docker.
- The `full` Compose profile gets a one-shot `migrate` service. The
  `cave-server` service depends on it with
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
- **More than one cave-server.** "One connection per name" lives in one
  process's memory today. Two processes need a lease on the account row,
  for example an `active_server` column taken in the join transaction. That
  is also when Redis for presence might earn its place.
- **The lobby on the world server.** It writes `SaveLobby`, the cave writes
  `EndRun`. The lease above has to exist first.
- **`runs` and `run_results`.** When balance work needs the numbers. A NATS
  event at run end is one way to feed them without slowing the run end
  write.
- **Read replica or a second instance.** When an outage that stops joins
  costs more than the extra volume.

## Follow-up tickets

In order. Each one can merge on its own.

1. **Store interface: one write per change, off the tick loop.** The four
   calls above on `Memory`. Server builds whole results, writer goroutine
   per client, no writes during a run, `runID`, the contract suite. No
   Postgres yet. This is the risky part of FOM-216, so it lands first,
   where it is easy to test.
2. **FOM-114: schema, migrations, local Postgres.** `src/internal/db` with
   the embedded goose migrations above, `cave-server migrate`, the schema
   version check at start, Compose on `postgres:18` with the `migrate`
   service, `just migrate` and `just cave-db`.
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
