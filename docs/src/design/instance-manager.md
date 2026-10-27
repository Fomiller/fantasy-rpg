# Instance manager

Instance servers run as Agones GameServers. The game splits into four
services. Events go through Redpanda. The join path stays one synchronous
call. This replaces the fixed `fantasy-rpg-instance` StatefulSet. It builds
on [Instances](instances.md) and the cave entrance in
[Hub town](hub-town.md). FOM-112 builds it.

## Rules that do not bend

- The handoff in [Instances](instances.md) holds. One socket at a time,
  the join token, the run lease. `InstanceTicket` and
  `InstanceJoinRequest` do not change.
- One instance per pod. A GameServer is one boot. A crash makes a new
  GameServer with a new name. It never comes back under the old one.
- Postgres stays the only source of truth for what a player owns. Redpanda
  carries facts about what happened. It is never the place a player's gear
  lives.
- The join path is request and reply over HTTP. A player waiting on a
  ticket never waits on a topic.
- An Allocated GameServer is never deleted on purpose. Only its own
  `Shutdown()` ends it.
- `just dev` and `just instance` run with no Kubernetes.

## Today

- The chart runs a StatefulSet with 2 pods. Each has a Service and a route
  at `/instances/fantasy-rpg-instance-N/`.
- `world-server` reads `INSTANCES`, polls each `/status` every 2 s, and
  `Reserve` picks the fullest instance with room.
- `instance-server` takes the run lease with `StartRun` and writes the
  result with `EndRun`, both straight to Postgres.
- An empty instance resets to a new world. The pod lives until a deploy.
- A PDB with `maxUnavailable: 0` blocks every eviction. `kubectl drain`
  waits forever.
- Compose runs a NATS container that no code uses.

## Services

| Service | Owns | Talks to |
| -- | -- | -- |
| `world-server` | Town sim, player sessions, join and character create, minting join tokens | instance-manager (HTTP), inventory (HTTP), Redpanda (produce) |
| `instance-manager` | `GameServerAllocation` calls, reuse of Allocated servers, per-GameServer Service and IngressRoute, lease checks | Kubernetes API, Redpanda (produce) |
| `inventory` | Characters, stash, the run lease, loot writes. The only service with the character tables | Postgres, Redpanda (consume and produce) |
| `instance-server` | One sim and its tick loop. Emits run results as events | Agones SDK, inventory (HTTP), Redpanda (produce) |

- All four are binaries in one image, picked by `command`, as world and
  instance are today. One Kargo promotion moves all of them.
- inventory is the only one with `DATABASE_URL`. The migrate Job runs from
  its binary.
- world is one replica with `Recreate`. It keeps state per process, like
  one connection per name. inventory and instance-manager run two replicas
  each. See [Scaling](scaling.md).

### Why split inventory out

- Run results come in as events. One consumer that owns the writes makes
  idempotency one check in one place.
- World and instance servers stop sharing write access to the same rows.
  The lease rules in Instances get one enforcer.
- The `Store` interface and its contract suite move behind inventory's
  HTTP API as they are.

## Join path

```text
client walks in -> world -> POST instance-manager /v1/allocate
                         <- {instance, boot, url}
                 world mints token -> InstanceTicket -> client
client dials url -> instance-server -> POST inventory /v1/runs/start
```

`/v1/allocate` takes the run ids for the party and returns the instance
id, boot id and public URL. Errors: 409 `full`, 503 when the manager
cannot reach the API server.

### HTTP, not gRPC

- One call per walk-in. The rate is low and the payload is three strings.
- The servers already talk HTTP and JSON for `/status`. No new codegen in
  the Go build. The repo's protobuf stays the client's wire format.
- A 5 s client timeout and the request context give cancel for free.

### Why not request and reply over Redpanda

- **The last slot.** Two walk-ins can race for one seat. A
  `GameServerAllocation` is already atomic in Agones. Sending it through a
  topic adds a hop and no atomicity. The manager would still serialize on
  the API server.
- **Correlation and timeouts.** The world server would need a reply topic,
  correlation ids and a timer per request. A reply that lands after the
  world gave up holds a seat that no ticket points at.
- **Ordering hides failure.** A slow consumer turns a failed allocation
  into a long wait on the entrance, not a clear `full`.

## Agones

### Version and port policy

The public host is proxied by Cloudflare through `cloudflared` into
Traefik. Only HTTP and WebSocket reach the cluster. Raw UDP to a node port
does not. So GameServers must not use a `hostPort`.

- `portPolicy: None` on the GameServer port. Agones then ignores
  `hostPort` and uses `containerPort` as is.
- `PortPolicyNone` is stable and on by default since Agones 1.60.0, per
  the feature stages page. Install 1.60.0 or later. Older releases have it
  behind a feature gate.
- Counters and lists need `CountsAndLists`. It is beta and on by default
  since 1.41.0.

### Fleet

The chart makes one `Fleet`, `fantasy-rpg-instance`, and one
`FleetAutoscaler` with a buffer policy.

```yaml
spec:
  template:
    spec:
      ports:
        - name: ws
          portPolicy: None
          containerPort: 8080
      lists:
        runs:
          capacity: 8   # MAX_PLAYERS
      eviction:
        safe: Always
```

- Buffer `bufferSize: 1`, `minReplicas: 1`, `maxReplicas: 8`. One warm
  Ready server keeps a walk-in from waiting on a pod.
- A Fleet scale-down only removes Ready servers. A Fleet rollout on a new
  image does the same, and leaves Allocated servers until they shut down.
  That is the drain on deploys.
- Argo CD v3.5.6 tracks by annotation. It tracks the Fleet and the
  FleetAutoscaler. Agones makes GameServerSets and GameServers with owner
  references and no tracking annotation, so Argo CD never prunes them.
- The FleetAutoscaler writes `spec.replicas`. The chart leaves `replicas`
  off the Fleet. Argo CD only diffs fields the chart sets, so it does not
  fight the autoscaler. No `ignoreDifferences` is needed.

### The runs list

A `runs` list on each GameServer is its seat count. One value per run id.

- `/v1/allocate` adds the party's run ids with the allocation's
  `lists.runs.addValues`. Seats are taken in the same atomic update that
  allocates.
- The instance server removes a run id when that run's result is acked by
  Redpanda. It also removes one whose token expired with no join.
- Capacity is `MAX_PLAYERS`. The list replaces `/status` players and runs.
  The world server stops polling.

### Instance server and the SDK

Behind a small interface, so local dev needs no sidecar:

```go
type GameServer interface {
	Ready() error
	Health() error
	// Shutdown ends this instance. On Agones the pod goes away. Locally it
	// resets the world, as an empty instance does today.
	Shutdown() error
	SetAnnotation(key, value string) error
	AddRun(runID string) error
	RemoveRun(runID string) error
	Runs() ([]string, error)
	SetRunCapacity(n int64) error
}
```

- The Agones one is used when the sidecar's `AGONES_SDK_GRPC_PORT` is
  set. Otherwise a local one.
- On start: build the world, set the `boot` annotation, call `Ready()`
  once the server listens. The SDK prefixes the key, so it lands as
  `agones.dev/sdk-boot`. `Health()` every 2 s from the tick loop, so a
  stuck tick fails health.
- On empty: set run capacity to 0, so no allocation can add a seat. Read
  the list again. If a run id landed first, restore capacity and stay.
  Otherwise `Shutdown()`. Agones deletes the pod, and the buffer makes a
  fresh one.

## Allocation and reuse

`/v1/allocate` sends one `GameServerAllocation` with two selectors, in
order:

1. `gameServerState: Allocated`, `lists.runs.minAvailable: <party size>`.
   Join a cave that is already running and has room for the whole party.
2. `gameServerState: Ready`. Take a warm one.

- `priorities` on the `runs` list, `Ascending`. Agones sorts by available
  capacity, so the fullest Allocated server wins. One cave fills before the
  next, as now.
- No match is `full`. With the buffer, that only happens at `maxReplicas`.
- The answer's GameServer name is the instance id. The boot id comes from
  the `agones.dev/sdk-boot` annotation in the allocation status. The token's `inst` keeps its
  `id@boot` form.

## Routing

A Service and an IngressRoute per GameServer, made by instance-manager.

- The manager watches GameServers. When one turns Ready, it makes a
  Service on the `agones.dev/gameserver` pod label and an IngressRoute
  with `PathPrefix(/instances/{name}/)`. A warm server is routed before
  anyone needs it.
- `/v1/allocate` checks both exist and makes any that are missing. That
  covers a manager restart or a missed event.
- Both have an owner reference to the GameServer. Kubernetes deletes them
  when it goes. The manager never deletes a route itself.
- Neither carries an Argo CD tracking annotation, so Argo CD never prunes
  them.
- The instance server keeps serving its socket at `/instances/{id}/ws`. No
  middleware.

### RBAC for instance-manager

| Resource | Verbs |
| -- | -- |
| `gameserverallocations.allocation.agones.dev` | create |
| `gameservers.agones.dev` | get, list, watch |
| `services` | get, list, create |
| `ingressroutes.traefik.io` | get, list, create |

The manager creates allocations through the Kubernetes API. The Agones
allocator service is for clients outside the cluster, so it is not needed.

## Lifecycle

| Agones state | Meaning | Picked by |
| -- | -- | -- |
| `Scheduled`, `RequestReady` | Pod starting | Nothing |
| `Ready` | Warm, routed, no players | Selector 2 |
| `Allocated` | Has runs | Selector 1, while `runs` has room |
| `Shutdown` | Empty, going away | Nothing |
| `Unhealthy` | Missed health or the container restarted | Nothing. Agones deletes it |

### Leases when a server dies

- inventory still holds the lease, with `id@boot`, as now.
- The world server asks `GET instance-manager /v1/instances/{id}`. 404
  means the GameServer is gone. The lease is dead and the world calls
  inventory's `ClearRun`.
- The run id missing from `runs`, with a lease older than 60 s, is dead
  too. Same rule as Dead leases today.
- If instance-manager does not answer, the lease waits. Never clear on a
  timeout.

### World or manager restart

- world-server has no instance state anymore. A restart drops town players
  as now. Runs go on.
- instance-manager rebuilds from its GameServer informer. Agones holds
  every count. In-flight allocations hold their seats in `runs`, so a
  restart cannot overfill.

## Graceful drain

- Drop the PDB.
- `eviction.safe: Always`. With `Never`, the default, Agones makes its
  own PDB for the pod. A node drain then waits forever, as today. Unverified
  against 1.60: check the PDB name on install.
- On SIGTERM the instance server stops taking joins and lets runs play on
  up to `DRAIN_TIMEOUT`, 600 s. Then it ends each run as a disconnect does
  and produces `run.ended` for it.
- `terminationGracePeriodSeconds: 630`, for the last produce.
- The descheduler must skip GameServer pods. They have an owner, so it no
  longer skips them by default. Exclude `agones.dev/role: gameserver` in
  the homelab descheduler config.

## Events

Redpanda, one broker in the homelab. franz-go in Go.

| Topic | Key | Producer | Payload |
| -- | -- | -- | -- |
| `player.session` | account | world | `enter` or `leave` town, time, world id |
| `instance.join` | run id | instance | `accepted` or `rejected` with reason, instance, time |
| `instance.lifecycle` | GameServer name | manager | `ready`, `allocated`, `shutdown`, `gone`, time |
| `run.ended` | run id | instance | account, class, outcome (`extracted`, `died`, `disconnect`), XP, carried loadout, loot, deaths, instance |
| `inventory.applied` | account | inventory | run id, new stash version |

- Payloads are protobuf, from `proto/events.proto`, with a version field.
- `run.ended` is the only topic that must not lose a message. It has
  7 days of retention and `acks=all`. The others are best effort.

### run.ended

- The instance server produces it with an idempotent producer and
  `acks=all`. It retries in memory until acked, as pending saves do now.
  It does not call `Shutdown()` with an unacked result.
- inventory consumes it in a consumer group. One transaction applies XP,
  loadout and loot, clears the lease, sets `last_run_id`, and writes the
  `inventory.applied` outbox row. Then it commits the offset.
- A run id that equals `last_run_id` is the retry no-op, as `EndRun` is
  today. Replay and redelivery are safe.
- Partition by run id. One account has one live run, so no two results
  for one account race.

### Transactional outbox

Any service that writes Postgres and emits an event uses an outbox. Today
that is inventory only.

```sql
CREATE TABLE outbox (
    id         bigserial PRIMARY KEY,
    topic      text NOT NULL,
    key        text NOT NULL,
    payload    bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
```

- The row goes in the same transaction as the write it describes.
- A relay goroutine in inventory reads rows in `id` order, produces them,
  and deletes them once acked. At least once. Consumers dedupe by key and
  run id.
- Services with no database write, like world and instance, produce
  straight to Redpanda.

### Eventual consistency

- The stash updates a moment after the run ends. Usually well under a
  second, longer if inventory is behind.
- The client already handles this. The lease is held until inventory
  applies the result, so the world shows "Saving..." until it clears.
- The world server learns it from `inventory.applied`, not the 2 s poll.
  It keeps the poll as a fallback when Redpanda is down.
- Each world reads `inventory.applied` from the end with no consumer
  group. Every pod sees every record and leaves no group behind.
- If inventory is down, runs still end. Results wait in the topic. Players
  see "Saving..." until it is back.

## Transport

WebSocket through Cloudflare, `cloudflared` and Traefik. No change now.

- Keep the transport behind an interface on both ends: a listener that
  yields connections, and a connection with send, receive and close with
  a code and reason.
- `InstanceTicket.url`'s scheme picks the transport. `wss://` today.
- A later move to UDP or WebTransport then means a new implementation, a
  new Agones port with a real port policy, and a path past Cloudflare.
  Not a rewrite of the server.

## Local dev

- `just deps` starts Postgres and Redpanda in Compose. Redpanda replaces
  the unused NATS container. `rpk` dev mode, one node, no auth.
- `just dev` runs inventory and world. World uses a static pool from
  `INSTANCES` when `INSTANCE_MANAGER_URL` is empty. That is today's
  `Directory`, with polling.
- `just instance <id> <port>` runs as now. The local SDK resets the world
  on `Shutdown()`.
- `net-test` adds a throwaway Redpanda container next to its throwaway
  Postgres, and starts inventory. Run results go through `run.ended`.
- With `KAFKA_BROKERS` or `INVENTORY_URL` empty, the instance calls
  `EndRun` on its store. Nothing would consume `run.ended`, so `just dev`
  and `just instance` keep working without the inventory service.
- Why Redpanda in Compose and not a no-op bus: `run.ended` is how a run's
  loot reaches the stash. A no-op bus would need a second code path for
  the one write that matters most.

## Metrics

| Metric | Where | Type | What |
| -- | -- | -- | -- |
| `instance_startup_seconds` | manager | histogram | GameServer create to Ready |
| `instance_allocation_seconds` | manager | histogram, `result` | `/v1/allocate` time, by `reused`, `ready`, `full`, `error` |
| `instance_routing_errors_total` | manager, instance | counter, `reason` | See below |
| `inventory_consumer_lag` | inventory | gauge, `topic`, `partition` | Records behind the high watermark |
| `outbox_pending` | inventory | gauge | Rows not yet produced |

`reason` values:

- `full`. No GameServer matched.
- `allocate`. The API refused the allocation.
- `route`. Making the Service or IngressRoute failed.
- `ticket_unused`. The instance server dropped a run id whose token
  expired with no join.
- `draining`. A join landed on a server that was shutting down.

## Failure cases

| When | What happens | What the player sees |
| -- | -- | -- |
| No warm server | Allocation fails with `full` until the buffer catches up | "All instances are full." Step out and back in |
| Manager down | World gets 503, rejects with `full`. Leases wait | Same. Running caves are fine |
| inventory down | Joins fail at `runs/start`. Results wait in the topic | "Characters unavailable", as a database outage today. Returning players see "Saving..." |
| Redpanda down | Results retry in the instance server's memory. Shutdown waits | "Saving..." on return, longer |
| Instance crashes | GameServer `Unhealthy`, deleted. Run lost. Lease cleared on 404 | Back to town, then the pre-run character |
| Node drain | Runs end by `DRAIN_TIMEOUT`, as a disconnect | Run ends early with what they carry |

## Implementation plan

In build order. Each one merges on its own and leaves the game playable.

1. **homelab: install Agones.**
   - Agones 1.60.0 or later through Argo CD. Game server namespace
     `fantasy-rpg`.
   - Exclude `agones.dev/role: gameserver` pods from the descheduler.
   - Check the PDB Agones makes for `eviction.safe: Never`, and that
     `Always` has none.
2. **homelab: install Redpanda.**
   - Redpanda operator and one broker on Longhorn, through Argo CD.
   - In-cluster listener only. No public route.
   - Topic CRDs available for app charts.
3. **Event bus and local Redpanda.**
   - `internal/events` on franz-go, `proto/events.proto`, Topic resources
     in the chart.
   - Redpanda in Compose in place of NATS. `net-test` gets a throwaway one.
   - world produces `player.session`. instance produces `instance.join`.
4. **inventory service.**
   - New binary. `Store` behind HTTP: load, create, lobby save, `StartRun`,
     `ClearRun`. Contract suite runs against it.
   - world and instance call it. Only inventory gets `DATABASE_URL`.
   - Outbox table and relay. Deployment and Service in the chart.
5. **run.ended through Redpanda.**
   - instance produces `run.ended` with retry until acked. `EndRun` leaves
     the instance server.
   - inventory consumes it idempotently by run id and emits
     `inventory.applied` through the outbox. World clears "Saving..." on it.
   - `inventory_consumer_lag` and `outbox_pending`.
6. **Agones SDK in instance-server.**
   - `GameServer` interface, Agones and local implementations. Ready,
     Health, boot annotation, runs list, Shutdown on empty.
   - SIGTERM drain with `DRAIN_TIMEOUT`.
   - Still runs under the StatefulSet with the local SDK.
7. **instance-manager service.**
   - New binary. `/v1/allocate` with the two selectors and `addValues`,
     `/v1/instances/{id}`.
   - Service and IngressRoute on Ready and on allocate, owned by the
     GameServer.
   - `instance_startup_seconds`, `instance_allocation_seconds`, routing
     errors. Tests on fake clientsets.
8. **Chart switch to Agones.**
   - Fleet with `portPolicy: None`, `runs` list, `eviction.safe: Always`.
     FleetAutoscaler buffer. RBAC and Deployment for the manager.
   - World sets `INSTANCE_MANAGER_URL`. Remove the StatefulSet, its
     Services and routes, the PDB and the `INSTANCES` helper.
   - No `replicas` on the Fleet, so homelab needs no `ignoreDifferences`.
9. **Transport interface.**
   - Listener and connection interfaces in the Go servers. WebSocket is
     the one implementation.
   - The client picks its connection by the ticket URL's scheme.
10. **Optional: predictive scaler.**
    - FleetAutoscaler webhook policy, served by instance-manager, reading
      `player.session` and `instance.lifecycle`.
    - Chained after the buffer policy, so a webhook failure falls back to
      the buffer.
    - Only if the buffer leaves walk-ins waiting.

## Later

- More than one world replica. See [Scaling](scaling.md).
- UDP or WebTransport, past Cloudflare. See Transport.
- Reconnect into a run (FOM-217). An Allocated server with a held body
  keeps its run id in `runs`, so it stays up.
