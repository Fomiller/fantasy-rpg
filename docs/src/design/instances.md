# Instances

An instance is one running copy of a play space: one sim, its own enemies,
loot and extraction zone. A cave is one kind of instance. This page splits
the game between the world server and instance servers, sets how a player
moves between them, and how it deploys before the instance manager exists.
It builds on [Persistence](persistence.md) and the lobby in
[Progression](progression.md). FOM-226, FOM-227, FOM-228 and FOM-112 build
on it.

`cave-server` is renamed `instance-server` in FOM-226. This page uses the
new name.

## Rules that do not bend

- Postgres stays the only source of truth for anything a player owns. The
  servers talk to each other through it, not around it.
- The client holds one connection at a time. It is either on the world
  server or on one instance server.
- An instance server only lets in a player the world server sent, with a
  token it can check without a database call.
- Only one run per account can be live at once. The database enforces it.
- No new system for this. No Redis, no NATS, no queue. Two Go binaries,
  Postgres and HTTP.

## Today

- `world-server` is only a `/healthz` handler.
- `cave-server` does everything: `/ws`, join, the lobby, stash moves,
  character load and create, the sim and its tick loop, and `EndRun`.
- One process runs one world. `main.go` builds one `sim.World` and one
  `server.Server` around it.
- The chart deploys only `cave-server`. `https://fantasy-rpg.fomiller.com`
  sends everything, including `/ws`, to it.
- "One connection per name" is `Server.conns`, in one process's memory.
  Persistence already says two processes need a lease on the account row
  first.

## Responsibilities

| World server | Instance server |
| -- | -- |
| Join: name and class, one connection per name | Checks the join token |
| Character load and create, the starter kit | Takes the run lease and loads the character in one call |
| The lobby, stash, `SaveLobby` | One sim and its tick loop |
| The character screen | Enemies, chests, loot, XP, extraction |
| Party grouping (later) | Run end: `EndRun` and pending saves |
| Picking an instance, minting the token | `/status` for the world server |
| Clearing a dead run lease | Resetting the instance when it empties |

- The world server has no tick loop and no sim. It reacts to messages and
  database results. A slow lobby can never stall a run.
- The instance server owns everything that changes during a run. It never
  does lobby moves. It rejects `StashMoveRequest`.
- The split follows what changes 20 times a second and what does not. The
  sim stays where the tick is. Everything a player does between runs moves
  out.

## One connection, not two

The client closes the world connection before it opens the instance one.

- It is what the client does today: one `ServerConnection`, one socket.
  Two sockets would need two message routers and a rule for which one owns
  the HUD.
- The world server has nothing to say during a run. No chat, no party
  messages yet. A second socket would sit idle.
- The cost is a reconnect at each end of a run, about one round trip plus
  the TLS handshake. It happens behind the loading screen.
- When chat or party messages need the world during a run, add the second
  socket then. Nothing below stops it.

## Handoff

### World to instance to world

1. The client connects to the world server at `/ws` and sends
   `JoinRequest`, as now. The world server claims the name, runs `Load`
   (or `Create`), and sends `Lobby`, `EquipmentUpdated`,
   `InventoryUpdated` and `StashContents`, as now.
2. The player makes lobby moves. Each is a `SaveLobby`, as now.
3. The client sends `EnterInstanceRequest`.
4. The world server rejects it with `EnterInstanceRejected` if the lobby is
   `saving`, if the account has a live run lease, or if no instance has
   room (reason `full`).
5. Otherwise it picks an instance (see Instance selection), makes a run id
   (UUID), and mints a join token for that character, instance and run.
6. It sends `InstanceTicket` with the URL and the token. Then it closes the
   socket with 1000 and reason `entering instance`, and frees the name.
7. The client opens the ticket URL and sends `InstanceJoinRequest` with
   the token as its first message.
8. The instance server checks the token (see Join token). On a bad token it
   closes with 1008 and the reason.
9. It claims the name in its own `conns`, then calls
   `StartRun(name, class, runID, instance)`. That takes the run lease and
   returns the character and stash in one transaction. If another run holds
   the lease, it closes with 1008 `already in a run`.
10. It spawns the player and sends `JoinAccepted`, `EquipmentUpdated` and
    `InventoryUpdated`. The run is the same as today's from here.
11. On extraction or death it sends `Extracted` or `RunEnded` with the
    `RunSummary`, as now. It queues `EndRun(runID, character)` on the
    player's writer, which also clears the lease. It does not wait for the
    write.
12. It flushes the outbox, then closes with 1000 and reason `run over`.
13. The client keeps the `RunSummary`, connects to the world server and
    sends `JoinRequest` again.
14. The world server runs `Load`. If the lease is still held, the run end
    has not landed. It sends `Lobby` with `saving` true, and checks the
    store every 2 s off the tick until the lease clears. Then it sends
    `Lobby` with `saving` false and the banked character.
15. The client shows the lobby with the run summary over it.

### Proto changes

Renames in FOM-226. Names are not on the wire, so these break nothing but
generated code:

| Old | New | Field |
| -- | -- | -- |
| `EnterCaveRequest` | `EnterInstanceRequest` | `ClientMessage` 20 |
| `EnterCaveRejected` | `EnterInstanceRejected` | `ServerMessage` 26 |
| `CaveLayout` | `InstanceLayout` | |
| `JoinAccepted.cave` | `JoinAccepted.layout` | 2 |

New in FOM-228:

```proto
message ClientMessage {
  oneof payload {
    // ...
    InstanceJoinRequest instance_join = 22;
  }
}

message ServerMessage {
  oneof payload {
    // ...
    InstanceTicket ticket = 27;
  }
}

// InstanceTicket is the world server's answer to EnterInstanceRequest. The
// world server closes the socket right after it.
message InstanceTicket {
  // The instance server's WebSocket URL, like
  // wss://fantasy-rpg.fomiller.com/instances/instance-0/ws.
  string url = 1;
  string token = 2;
  string instance_id = 3;
  // How long the token is good for, from when it was sent.
  uint32 expires_in_ms = 4;
}

// InstanceJoinRequest is the first message on an instance server. It is
// the only one an instance server takes before JoinAccepted.
message InstanceJoinRequest {
  string token = 1;
}
```

- `EnterInstanceRejected.reason` gains `full` and `in_run`, next to
  `saving`.
- `JoinRequest` stays world only. An instance server that gets it closes
  with 1008 `join on the world server`.
- `Extracted`, `RunEnded` and `RunSummary` do not change.

## Join token

### Format

`base64url(payload) "." base64url(HMAC-SHA256(key, payload))`. The payload
is compact JSON with fixed tags, made and checked by a small Go package,
`src/internal/jointoken`. The client never reads it.

| Field | JSON | Notes |
| -- | -- | -- |
| Version | `v` | 1. A server rejects a version it does not know |
| Account name | `name` | The store's key until auth. The account id after |
| Class | `class` | With the name, the character |
| Instance | `inst` | Instance id and boot id, like `instance-0@3f9a1c`. A token only works on that process |
| Run id | `run` | UUID. Also the nonce |
| Issued at | `iat` | Unix seconds |
| Expires | `exp` | `iat` + 30 |

### Why HMAC with a shared key

- Both servers are ours, in one namespace, from one image. One secret is
  enough. Ed25519 would only matter if instance servers were run by
  someone we do not trust with a signing key.
- The check is a hash and a compare. No database call, no call to the
  world server. A flood of bad tokens never reaches Postgres.
- The key is a Secret, `fantasy-rpg-join-key`, from AWS Secrets Manager at
  `dev/fomiller/homelab/fantasy-rpg-join-key` through the chart's
  `externalSecrets` map, like the backup key. Both Deployments read it as
  `JOIN_TOKEN_KEY`. At least 32 random bytes.
- Without `JOIN_TOKEN_KEY` both servers refuse to start. The local recipes
  set a fixed dev key.
- Rotation: change the secret and restart both. Tokens live 30 s, so at
  most one in-flight entry fails. No key ids until that hurts.

### Checks, in order

1. The signature matches, compared in constant time.
2. `v` is known, `exp` is in the future, and `iat` is not more than 5 s in
   the future (clock skew).
3. `inst` is this instance server's id and boot id. The world server
   reads the boot id from `/status`.
4. `run` has not been seen on this process. Seen run ids are kept in
   memory until their `exp` passes.
5. `StartRun` takes the lease for `run`. This is the check that holds
   across restarts.

Close reasons: `bad token`, `token expired`, `wrong instance`,
`token used`, `already in a run`, `instance full`. All with 1008.

### Why reuse cannot work

- During the run, step 4 rejects a second use on the same process, and
  `conns` would reject the name anyway.
- After the run, `EndRun` has cleared the lease and set `last_run_id` to
  that run. `StartRun` refuses a run id that equals `last_run_id`, so the
  token can never start a second run, even after a restart.
- After a restart, step 4's memory is gone, but the boot id is new. Step 3
  rejects every token made for the old process.

30 s covers the close, a new TLS connection and a slow phone. It is short
enough that a leaked token is useless almost at once.

## Run lease and run results

### The lease

Persistence said two writers need a lease on the account row. It goes on
`accounts`, not `characters`, because the stash is per account. Two
classes in two instances would both write it at run end.

```sql
ALTER TABLE accounts
    ADD COLUMN active_run_id   uuid,
    ADD COLUMN active_instance text,
    ADD COLUMN active_since    timestamptz;
```

`active_instance` is the instance id and its boot id, like
`instance-0@3f9a1c`. The boot id is random on each start.

Store changes:

```go
// StartRun takes the run lease and returns the character and stash, in one
// transaction. ErrInRun if a lease is held. ErrRunUsed if runID is the
// character's last_run_id.
StartRun(ctx context.Context, name, class, runID, instance string) (Character, error)
// EndRun as now, plus it clears the lease. ErrLeaseLost if the lease is not
// runID's.
EndRun(ctx context.Context, runID string, c Character) error
// ClearRun drops a dead lease. A no-op unless the lease is still runID's.
ClearRun(ctx context.Context, name, runID string) error
```

- `Load` also returns the lease, so the world server knows a run is live.
- `SaveLobby` fails with `ErrInRun` while a lease is held. The world server
  rejects the move with `in_run`. This is what stops a lobby move from
  landing under a run end.
- `EndRun` first checks `last_run_id`. A match is the retry no-op, as now.
  Then it needs the lease to be `runID`'s. Otherwise it returns
  `ErrLeaseLost`, writes nothing, logs it and counts it in
  `store_errors_total`. That only happens when the world server cleared a
  lease the instance server still held, which the rules below avoid.
- The instance server takes the lease, not the world server. A token that
  is never used then leaves nothing to clean up.

### The instance server writes run results

The instance server calls `EndRun` itself, with the code it has today.

- It already has the whole result: XP, loadout, the stash it loaded at
  entry. Sending it to the world server is one more hop that can fail.
- The writer goroutine, retries and pending saves from FOM-220 and FOM-223
  already work there. They move as they are.
- A world server restart cannot lose a run result, since the world server
  never holds one.
- The cost: both servers need `DATABASE_URL`. They are the same image, so
  migrations always match both.

### Idempotency

- The run id is made by the world server and travels in the token. The
  instance server uses it for `StartRun` and `EndRun`.
- `EndRun` with the same run id twice is a no-op, as now. A retry after a
  lost reply is safe.

### When an instance server dies mid-run

- Run state is in memory, so the run is lost, as Persistence says. The
  character and stash are what they were at `StartRun`. That is the
  loadout the player took in, so a crash never costs gear.
- A pending save that had not landed is lost too. Same rule as today.
- The lease is left behind. The world server clears it (see Dead leases).

### Dead leases

The world server clears a lease with `ClearRun` when it can prove the run
is gone:

- The lease's boot id is not the instance's current boot id. That process
  is dead, and its memory with it.
- The boot id matches, the instance's `/status` does not list the run,
  and the lease is older than 60 s. 60 s is past the token lifetime, so a
  join that is still on its way is not cut off.
- The lease names an instance that is not in the world server's list
  anymore.

If the instance does not answer `/status`, the world server waits. It never
clears a lease on a timeout. An instance that is up but cut off may still
land its `EndRun`, and clearing under it is how items get lost. Kubernetes
restarts a dead pod in seconds. Its new boot id then clears the lease.

`/status` lists every run the process still has, including runs whose
`EndRun` has not landed. So a run waiting on a database outage keeps its
lease, and the lobby shows "Saving..." until it lands.

## Back to the world

| How the run ends | Instance server | Client | World server shows |
| -- | -- | -- | -- |
| Extraction | `Extracted` with the summary, `EndRun`, close 1000 `run over` | Keeps the summary, rejoins the world | Lobby, "Saving..." until the lease clears, then the banked character. The summary on top |
| Death | `RunEnded` with the summary, `EndRun`, close 1000 `run over` | Same | Same |
| Disconnect | `leave` runs `EndRun` with the carried loadout, as now | Rejoins the world when it can | Lobby, "Saving..." until it lands. No summary, since the client never got one. A line says the run ended when the connection dropped |
| Instance server dies | Nothing | Sees the socket drop, rejoins the world | Lobby, "Saving..." until the world clears the dead lease. Same line as a disconnect |

- The client keeps the last `RunSummary` in memory until the next run
  starts. The world server does not store it. `runs` and `run_results` can
  change that later.
- The 30 s disconnect rule (FOM-217) fits on top. The instance server keeps
  the body and the lease for 30 s. A rejoin on the world server sees the
  lease and gets a new `InstanceTicket` for the same instance and run id.
  `StartRun` then needs to accept the same run id from the same instance
  boot. That is FOM-217's change, not this one.

## Instance selection

The instance manager is FOM-112. Until then there is a fixed list of
instance servers, and each one runs one instance.

### The world server's list

`INSTANCES` on the world server, one entry per instance, space separated.
Each entry is `id=internal_url=public_url`.

- The world server polls `GET {internal_url}/status` on each one every
  2 s. The answer is JSON: id, boot id, players, capacity, and the run ids
  it holds.
- An instance has room when players plus tokens issued in the last 30 s
  for it is under capacity.
- It picks the one with the most players that still has room for the
  whole party. Filling one cave before the next keeps runs social with a
  small player count.
- An instance that did not answer its last poll is never picked.
- `/status` is on the internal URL only. The IngressRoute never matches
  it.

### Local dev

Two processes, plus the world server:

```just
dev: deps
    JOIN_TOKEN_KEY=dev-only INSTANCES='instance-0=http://localhost:8081=ws://localhost:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://localhost:8082/instances/instance-1/ws' go run ./src/cmd/world-server

instance id port:
    JOIN_TOKEN_KEY=dev-only INSTANCE_ID={{id}} PORT={{port}} go run ./src/cmd/instance-server
```

`just instance instance-0 8081` and `just instance instance-1 8082` in two
more terminals. The client's `--server` points at the world server, as it
points at `cave-server` now. These go in `repo.nix`, since the justfile is
generated.

### Kubernetes

A StatefulSet, `fantasy-rpg-instance`, with a fixed number of replicas (2
to start). One instance per pod.

- A StatefulSet gives stable pod names: `fantasy-rpg-instance-0`,
  `fantasy-rpg-instance-1`. The name is the instance id, read from the pod
  name through the downward API into `INSTANCE_ID`.
- The chart loops over the replica count and makes one Service per pod,
  selecting on the `statefulset.kubernetes.io/pod-name` label that
  Kubernetes puts on every StatefulSet pod.
- The IngressRoute gets one route per pod: `PathPrefix` of
  `/instances/fantasy-rpg-instance-0/` to that pod's Service. The instance
  server serves its socket at `/instances/{INSTANCE_ID}/ws`, so no Traefik
  middleware is needed.
- `/ws` goes to the world server's Deployment and Service.
- The chart renders the world server's `INSTANCES` from the same replica
  count, so the two never disagree.
- `podManagementPolicy: Parallel`. The pods do not depend on each other,
  so one stuck pod should not hold up the others.
- No volumes. A StatefulSet is here for the names, not for storage.

Why not the others:

- **Many instances in one process.** `server.Server` is built around one
  `sim.World` and one tick loop. Making it hold many is a big change, and
  FOM-112 throws it away for a pod per instance. The StatefulSet is already
  a pod per instance.
- **One Deployment with sticky routing.** A Deployment's pods have random
  names. The world server could not put a pod in a URL, and Traefik would
  need a sticky cookie the client does not send.

### What FOM-112 changes

- The manager makes a pod per instance when one is needed, like
  `deploy/kubernetes/cave-pod.yaml` and `just cave-start` do by hand
  today, and deletes it when the instance empties.
- It makes the per-instance route, or one route sends
  `/instances/{id}/` to the right pod by header or by a small proxy. That
  is FOM-112's call.
- The world server asks the manager for an instance instead of reading
  `INSTANCES`. The token, the lease and the handoff do not change.

## Lifecycle

With a fixed list:

1. **Create.** The pod starts. The instance server builds a world with a
   new seed and a new boot id. It is ready when `/healthz` answers.
2. **Fill.** Players join with tokens up to capacity, `MAX_PLAYERS`,
   default 8. A join after the instance has players lands in the same
   world, as joining the cave does today.
3. **Run.** The tick loop runs while anyone is in the instance.
4. **Empty.** When the last player leaves and their writes are done, the
   instance server builds a new world with a new seed. The next group gets
   a fresh cave, not the last group's looted one. The boot id stays. It is
   the same process.
5. **Cleanup.** None with a fixed list. The pod lives until a deploy.
   FOM-112 deletes empty instances after a short idle time.

### Parties

Party grouping is a world server feature that does not exist yet. When it
does:

- The leader sends `EnterInstanceRequest`. The world server picks one
  instance with room for the whole party.
- It makes one run id and one token per member, all for the same instance,
  and sends each member their own `InstanceTicket`.
- A member whose join fails goes back to the lobby. The rest play on.

## Failure cases

| When | What happens | What the player sees |
| -- | -- | -- |
| World server restarts | Lobby connections drop. Runs in instances go on. Leases are in Postgres, so nothing is lost | Lobby players: the join screen, then back in. Players in a run notice nothing until they return. If the world is still down then, the client retries every 2 s |
| Instance server restarts | Its runs are lost. Characters are as they were at entry. The new boot id lets the world clear the leases | The socket drops, then the lobby with "Saving..." for a few seconds, then their pre-run character |
| Token expired | The instance closes with 1008 `token expired`. No lease was taken | Back to the lobby with "Took too long to join. Try again." |
| Instance full | The world server does not pick it. If two worlds raced, or a party filled it first, the instance closes with 1008 `instance full`. No lease was taken | Back to the lobby with "That instance filled up. Try again." The next pick goes elsewhere |
| No instance has room | `EnterInstanceRejected` with `full` | "All instances are full." Enter stays on |
| World cannot reach an instance | That instance is not picked. Its leases are not cleared | Nothing, unless every instance is down. Then `full` |
| Database down | As in Persistence. Joins fail on both servers. `StartRun` failing closes with 1013 `characters unavailable` | The same messages as today |

## Implementation plan

In order. Each one merges on its own and leaves the game playable.

1. **FOM-226: rename cave-server to instance-server.** `src/cmd/cave-server`
   to `src/cmd/instance-server`. The tick metrics label, log lines, the
   `cave`, `cave-db`, `cave-start`, `cave-stop` and `caves` recipes, the
   `cave-pod.yaml` manifest, the four proto renames above and the client's
   generated code. Docs that say `cave-server` as a process. `CaveLayout`
   content and the `caves` package stay, since a cave is still a kind of
   instance. No behavior change.
2. **FOM-227: world server.** The run lease: migration, `StartRun`,
   `ClearRun`, the lease on `Load`, `SaveLobby` and `EndRun`, on both
   stores and in the contract suite. The instance server calls `StartRun`
   on enter, on the player's writer and off the tick, so the lease is live before a second writer exists. Split
   `internal/server` into a lobby half and a run half. `world-server`
   gets join, load and create, the lobby and stash moves. One image with
   both binaries, the Deployment picking one with `command`, so one Kargo
   promotion moves both. Deploy the world server with its own Deployment
   and Service. `/ws` stays on the instance server, which keeps its own
   lobby until FOM-228. Without the handoff, a world server on `/ws` has
   no way into a run.
3. **FOM-228: enter an instance and return.** `jointoken`, the key Secret,
   `InstanceTicket` and `InstanceJoinRequest`, the instance side of the
   handoff, `/status`, `INSTANCES` and the pick, dead lease clearing, the
   reset on empty, `MAX_PLAYERS`. The StatefulSet, per-pod Services and
   routes. The client: follow the ticket, return to the world after
   `run over`, keep the summary, the new close reasons. Then remove the
   lobby from the instance server and move `/ws` to the world server.
   `net-test` starts a world server and two instance servers. It covers
   lobby, enter, extract, return, the summary and a reused token.
4. **FOM-112: instance manager.** A pod per instance on demand, routing for
   pods that come and go, idle cleanup, the world server asking the
   manager. The token, lease and handoff stay.

### What FOM-227 built

Where it differs from the plan above:

- **The split is two types in `internal/server`, not two packages.**
  `Lobby` is the lobby half and is all `world-server` runs. `Server` is the
  run half and embeds a `Lobby` for its own lobby until FOM-228. They share
  per-player state, so a package split would have exported most of it.
- **The world server builds a `sim.World` and never steps it.** It answers
  the class, gear and stat questions the lobby and character screen need.
  There is no tick loop. `Lobby.Run` handles the inbox on one goroutine.
- **The instance server clears dead leases on join.** Until FOM-228 there is
  one instance server, so a lease from another process or boot is from one
  that died. Without this, a pod restart mid-run would lock the player out.
  It goes away with the instance server's lobby.
- **A lobby held by a lease waits for it.** The world server shows `saving`,
  turns moves and Enter Cave away with `in_run`, and checks every 2 s.
- **Enter Cave on the world server is rejected with `full`**, since it has no
  instances to send anyone to yet.
- **Each write's result is handled before the next write starts.** A
  `StartRun` that lands after the player left can then still queue the
  `EndRun` that gives the lease back.
- **The Dockerfile is unmanaged in `repo.nix`.** golden-service writes one
  binary per image.

## Later

- **Second socket to the world during a run**, for chat and party
  messages.
- **Reconnect into a run** for the 30 s rule. See Back to the world.
- **Key ids in the token**, if rotating the key without a failed entry
  ever matters.
- **Run summaries on the world server**, from `runs` and `run_results`, so
  a lost client still sees how its run went.
