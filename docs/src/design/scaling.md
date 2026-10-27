# Scaling

How the game grows past one town and one of each service. Measure first,
then shard the town, then make the shared services HA, then autoscale. It
builds on [Instance manager](instance-manager.md) and
[Hub town](hub-town.md). FOM-245 is this design. Town shards were FOM-110
in Hub town.

## Rules that do not bend

- Postgres stays the only source of truth for what a player owns. A cache,
  a directory or a bus never decides who holds an item.
- One live session per name, across every shard. Two sessions for one name
  can save over each other's stash.
- The handoff in [Instances](instances.md) holds. One socket at a time.
- Caves stay on the Agones Fleet. Nothing here changes how a run starts or
  ends.
- No number in this doc is a target until the bots measure it.
- `just dev` runs with one shard and no Kubernetes.

## Today

- `world-server` is one pod, `Recreate`, one town. `TOWN_MAX_PLAYERS` is
  32. One connection per name is a map in that process, and a session
  claim in inventory across processes. Done in FOM-256.
- Caves run on the Agones Fleet with a buffer FleetAutoscaler, max 8.
- `inventory` is two pods, `RollingUpdate`, with a PDB. It owns Postgres.
  Each pod runs the outbox relay, and an advisory lock lets one send at a
  time. Done in FOM-251.
- `instance-manager` is two pods, `RollingUpdate`, with a PDB. Both
  allocate and both route, with no leader. Agones `extensions` is two pods
  too. Done in FOM-252.
- Postgres is CNPG with two instances, async, on
  `longhorn-single-replica`. Done in FOM-254. Redpanda is one broker, RF 1.
- Public traffic is Cloudflare tunnel, then Traefik. WebSocket only. UDP is
  parked in FOM-232.
- The pods export Prometheus metrics, but the chart has no PodMonitor. Only
  CNPG's own PodMonitor is set. Unverified without kubectl: nothing may
  scrape the game today.
- Longhorn storage is on two nodes, with 2 replicas per volume.
  `longhorn-single-replica` has 1, for workloads that copy their own data.

## 1. Measure

### Bot client

A headless bot in Go, `src/cmd/bot`. It speaks the same protobuf over
WebSocket as the Godot client, from `gen/go/game/v1` and
`internal/protocol`. No server backdoor. A bot gets its join token through
the world, like a player.

Each bot loops:

1. Dial `/ws`, send `JoinRequest` as `bot-<prefix>-<n>`.
2. Walk the town to random points for `-town-time`. `MoveInput` at the tick
   rate, with a sequence number, as `movement.gd` does.
3. Walk into `D`. Take the `InstanceTicket`, close, dial its URL, send
   `InstanceJoin`.
4. In the cave, path to `X` with BFS on the layout from `Joined`. With
   `-fight`, attack the nearest enemy in range on the way.
5. On `Extracted` or `RunEnded`, rejoin `/ws` with `returning` set. Time how
   long "Saving..." lasts.

- Fixed names, `bot-<prefix>-0` up. Reruns reuse the same character rows,
  so the database does not fill with bots.
- `-n` bots per process, `-ramp` bots started per second, `-duration`.
- `-via <url>` swaps the scheme and host of every URL, ticket URLs too, and
  keeps the `Host` header. In the cluster it points at Traefik, so the test
  skips Cloudflare but not routing.
- The bot serves `/metrics`:

| Metric | Type | What |
| -- | -- | -- |
| `bot_join_seconds` | histogram | Dial to `Joined` |
| `bot_ticket_seconds` | histogram | Step into `D` to `InstanceTicket` |
| `bot_instance_join_seconds` | histogram | Dial the ticket URL to `Joined` |
| `bot_snapshot_gap_seconds` | histogram, `where` | Time between snapshots, town or cave |
| `bot_saving_seconds` | histogram | Back in town to "Saving..." cleared |
| `bot_errors_total` | counter, `stage` | Closes and rejections, by stage |

### Running N bots

- **Local:** `just bots 50` against `just dev`. It finds the CPU cost of
  the sim on one machine, with no network in the way.
- **In the cluster:** an Indexed Job, `deploy/kubernetes/loadtest/bots.yaml`.
  `parallelism` pods, each with `-n` bots and `JOB_COMPLETION_INDEX` as the
  name prefix. Same image, `command: [/bot]`. `just loadtest <pods> <bots>`
  applies it. [Load test](load-test.md) is the runbook.
- The Job is not in Argo CD. A load test runs on demand. An Argo CD Job
  needs hooks, and a sync could start one by surprise.
- A PodMonitor on `app.kubernetes.io/component: bot` picks up the bots'
  metrics.
- One short run through the public host, with no `-via`, gives the
  Cloudflare tunnel's share of latency.

### Metrics

Today's metrics, and the few to add first.

| Metric | Where | State |
| -- | -- | -- |
| `game_tick_duration_seconds`, `game_ticks_missed_total` | world, instance | Exists |
| `store_duration_seconds`, `store_errors_total`, by `call` | world, instance, inventory | Exists |
| `store_pending_saves` | world, instance | Exists |
| `outbox_pending` | inventory | Exists |
| `events_produce_errors_total` | all | Exists |
| `instance_allocation_seconds`, `instance_startup_seconds`, `instance_routing_errors_total` | manager | Exists |
| Go and process collectors | all | Exists |
| CNPG, Agones controller, Redpanda | operators | Need scraping turned on |
| `town_players` | world | Exists. Gauge, players with a body in the town |
| `instance_players` | instance | Exists. Gauge, players in the cave |
| `ws_sent_bytes_total`, `ws_received_bytes_total` | world, instance | Exists. Counters |
| `ws_slow_kicks_total` | world, instance | Exists. Clients closed for a full outbox |
| `inventory_consumer_lag` | inventory | Add with the `run.ended` consumer, as Instance manager says |

CPU and memory per pod come from cAdvisor through kube-prometheus-stack.

The chart has a PodMonitor for each server, behind `metrics.podMonitors`.
Each scrapes `/metrics` on the container port named `http`. dev turns
them on with the `release: prometheus` label.

### Grafana

Dashboards as JSON in the chart, rendered as ConfigMaps with
`grafana_dashboard: "1"`. The kube-prometheus-stack Grafana sidecar loads
them.

- **Town:** tick p50 and p99 by pod, missed ticks, `town_players`, bytes
  out, slow kicks, CPU and memory.
- **Caves:** tick p99 by GameServer, `instance_players`, GameServers by
  state, allocation p99 by `result`, startup time, routing errors.
- **Persistence:** store p99 by `call`, store errors, `outbox_pending`,
  consumer lag, CNPG connections and commits, Redpanda produce latency.
- **Bots:** every `bot_` metric, so one screen shows a run.

Check in homelab before the first one: the sidecar must search all
namespaces, and Prometheus must select monitors without a `release` label.
Both are kube-prometheus-stack values.

### Numbers to find

| Question | Read | Line it must hold |
| -- | -- | -- |
| Players per town shard | world tick p99, missed ticks, bytes out per pod | p99 under 10 ms, no missed ticks |
| Caves per node | instance tick p99, CPU and memory per GameServer | p99 under 20 ms with 8 players. Sets requests and Fleet `maxReplicas` |
| Walk-in to cave | `bot_ticket_seconds`, allocation p99 by `result` | Warm under 250 ms |
| Run end to stash | `bot_saving_seconds`, `outbox_pending`, consumer lag | p99 under 2 s |
| inventory headroom | store p99 by `call`, calls per second, CNPG CPU | p99 under 50 ms. Gives calls per second per replica |
| Join storm | `bot_join_seconds` at a ramp of 20 per second | p99 under 1 s |
| Tunnel ceiling | public run against `-via` run, `cloudflared` CPU | Where it bends |

- A town snapshot goes to every player. Bytes out grow with the square of
  the player count. At 64 players that is about 5 MB/s out of one pod. The
  tunnel may bend before the CPU does.
- The first run's numbers go into [Load test](load-test.md). They set
  `TOWN_MAX_PLAYERS`, the shard soft cap, and every autoscaler threshold
  below.

## 2. Town shards

Several world pods. Each pod is one town shard. A player is in one shard at
a time.

### Shard lifecycle: a StatefulSet

`fantasy-rpg-world` becomes a StatefulSet. Pods are `world-0`, `world-1`
and up.

- **Why not an Agones Fleet:** Agones is built for sessions that end. An
  Allocated server is never deleted on purpose, and a rollout leaves it
  until it shuts down. A busy town never empties, so it would never take a
  new image.
- **Why not a Deployment:** pod names change on every rollout. A
  StatefulSet gives stable shard ids, `world-N`, so routes can be static.
- Scale-down removes the highest ordinal. A rollout goes highest ordinal
  first, one pod at a time. Both end in the same drain.
- `podManagementPolicy: Parallel`, so a scale-up starts every new pod at
  once.
- No PVC. A shard holds no data. The lobby is saved through inventory, as
  now.

### Routing

| Path | Goes to | Used for |
| -- | -- | -- |
| `/ws` | Service over every Ready world pod | The front door |
| `/towns/world-N/ws` | Service per pod, on `statefulset.kubernetes.io/pod-name` | One shard |

- The chart renders a Service and an IngressRoute for each ordinal up to
  `world.maxShards`. A route to a pod that is not running returns 503 from
  Traefik, and the client retries through `/ws`.
- No Traefik sticky cookie. Godot's `WebSocketPeer` keeps no cookies. Once
  the socket is open it stays on one pod anyway.
- Static routes, not routes made by instance-manager. Shards are few and
  known ahead. The manager stays about caves.

### Picking a shard: a directory, fill-first

Every world pod is also the front door. On a `JoinRequest` at `/ws` the pod
asks the directory where the name goes.

- If the answer is this pod, it takes the player.
- If not, it sends `TownTicket{url}` and closes 1000 `changing town`. The
  client dials the URL and sends the same `JoinRequest`.
- A shard path never forwards again. A full or draining shard closes 1013
  `town full`, and the client goes back to `/ws`.
- After a run, or any drop, the client goes back to `/ws`. The front door
  sends it on.

The directory picks, in order:

1. The party's shard, if the player is in a party and it is under the hard
   cap.
2. The player's last shard, if it is under the hard cap and not draining.
   Players come back from a cave to the same town.
3. The fullest shard under the soft cap, on the newest revision. Ties go to
   the lowest ordinal.
4. The emptiest shard under the hard cap. A scale-up is on its way.
5. None: `town full`, as today.

- The hard cap is `TOWN_MAX_PLAYERS`. The soft cap leaves room for parties
  and returning players. Start at 75%, then set both from measurement.
- **Why fill-first:** the town is social. Spread thin, every shard is
  empty. Fill-first also lets the highest ordinal empty out, which is the
  one a scale-down removes.
- **Why not a consistent hash:** it spreads players evenly, splits parties,
  and moves players on every resize.
- **Why not an assignment service:** the logic is small. Every world pod
  can run it against the directory. A service would add a deploy and a
  hop for no new guarantee.
- Two front doors can pick the same shard for its last seat. The shard's
  hard cap is the real check. The loser goes back to `/ws` once.

### One session per name: in Postgres

The global claim lives in inventory, beside the run lease. It cannot live
in a store that can lose a key. Done in FOM-256.

```sql
CREATE TABLE sessions (
    name       text PRIMARY KEY,
    session    text NOT NULL,
    shard      text NOT NULL,
    boot       text NOT NULL,
    claimed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE shards (
    id         text PRIMARY KEY,
    boot       text NOT NULL,
    seen_at    timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
```

- `sessions` is keyed by name, not account id. A first join claims the
  name before its account row exists.
- `session` is new on every join. `shard` is `WorldID`, the pod name.
  `WORLD_ID` overrides it, so two world servers on one host, as in
  `just net-test`, do not share a shard. `boot` is new on every start of
  the world process.
- A shard writes its `shards` row every 5 s, with a 15 s TTL in
  `expires_at`. One write per shard, not per player.
  `POST /v1/shards/heartbeat`.
- `POST /v1/sessions/claim` wins if there is no row, if the row is this
  session's, if the row is this shard's boot, or if the row's shard and
  boot have no live `shards` row. A restart with a new boot ends the old
  boot's claims at once.
- The claim is one `INSERT ... ON CONFLICT DO UPDATE ... WHERE`, so two
  inventory replicas cannot both hand out a name.
- The world claims after its local `conns` check and before `Load`, so the
  load sees every write the last session made.
- `SaveLobby` takes the session id. It locks the session row `FOR SHARE`
  and fails with `session_lost` unless that session holds the name. A
  claim that takes the name waits for a save in flight.
- `POST /v1/sessions/release` is the last write on the player's writer, so
  it lands after their last lobby write. That is when today's `conns`
  entry goes. It only drops the row if it is still this session's.
- A lost claim at join closes 1013 `already playing`. The client already
  retries every 2 s. After a shard crash, its players are back within 15 s.
- A lobby save that gets `session_lost` closes the player 1013
  `already playing` too. Another shard took the name, so this lobby is
  stale.
- A release that fails holds the name on other shards until this shard's
  boot stops beating. The same shard can still take it over.
- The local `conns` map stays. It is the fast check inside one shard.
- `EndRun` and `StartRun` do not check the claim. The run lease covers
  them, and a player in a cave holds no claim.
- The memory store has the same rules, so `STORE=memory` and `just dev`
  work as before.
- Rollout: an old world server sends no session id, so the new inventory
  turns its lobby saves away until the world rolls too.

### Presence and the directory: Valkey

The directory is a routing hint. It lives in Valkey. Losing it costs a
worse pick, not an item.

- Each shard writes `town:{id}` every 2 s, with a 10 s TTL: URL, players,
  revision, draining. A dead shard drops out in 10 s.
- `player:{name}` is the last shard, with a 10 minute TTL. `party:{id}` is
  the party's shard, once parties exist.
- If Valkey is down, the front door takes the player itself if it has room.
  The Postgres claim still blocks a second session.
- One Valkey pod, no persistence, `maxmemory 64mb`. Every key is rebuilt
  within one heartbeat. Client is `valkey-go`.

**Why not NATS for presence:** the homelab NATS is core NATS. It holds no
state. A front door that just started must read every shard's count at
once, not wait for the next heartbeat. NATS KV would work, but needs
JetStream and storage on a NATS that other apps share.

**Where NATS fits later:** messages, not state. Chat, party invites to a
player on another shard, and a message to a player in a cave. Core NATS
subjects fit that: `frpg.chat.global`, `frpg.player.<name>`. The homelab
already runs it.

### Parties landing together

Parties do not exist yet. When they do:

- A party forms in one town, so its members start on one shard.
- An invite to someone on another shard moves them to the leader's shard
  with a `TownTicket`.
- The directory writes `party:{id}`. Members coming back from a cave land
  there first, up to the hard cap.
- `/v1/allocate` already seats the whole party in one cave.

### Moving between shards

One mechanism, `TownTicket`, for three causes: drain, party, and later a
"change town" choice.

1. The shard waits for the player's queued lobby saves, as `enterInstance`
   does.
2. It releases the claim and sends `TownTicket` for the target.
3. The client dials it. The target claims and loads the character.

The town has no state that a save does not cover. A move costs the player
a reconnect, about one round trip and a character load.

### Drain

On SIGTERM a shard:

1. Fails readiness, so `/ws` stops sending it players.
2. Sets `draining` in Valkey.
3. Moves its players a few per tick with `TownTicket`, to shards the
   directory picks.
4. Exits when empty, or at 30 s. Players still there are closed and
   reconnect through `/ws`.

- `terminationGracePeriodSeconds: 45`.
- The directory prefers the newest revision, from the
  `controller-revision-hash` label through the downward API. In a rollout a
  player moves once or twice, not once per shard.
- Players in a cave are not on any shard. A drain does not touch them.

### What else changes on the world server

- `WorldID` is the pod name, so `player.session` already names the shard.
- When the world consumes `inventory.applied`, each shard reads every
  partition with no consumer group. Every shard needs every message for its
  own players.
- `just dev` is one shard. The front door's pick is always itself.

### Cross-shard chat (stretch)

- Each shard subscribes to `frpg.chat.global` on NATS, and to
  `frpg.chat.party.<id>` for its parties.
- A chat message goes to NATS, and every shard sends it to its players.
  At most once. No history.
- A rate limit per player, on the shard that receives it.

## 3. HA

### inventory: 2 replicas

- `RollingUpdate` with `maxUnavailable: 0`, `maxSurge: 1`. A PDB with
  `minAvailable: 1`.
- The HTTP API is already stateless. Each call is a Postgres transaction.
- **Outbox relay: an advisory lock.** `flush` starts with
  `pg_try_advisory_xact_lock(<relay key>)`. If another pod holds it, skip
  this round. Keep `FOR UPDATE` as it is.
- **Why not `SKIP LOCKED`:** a second relay would send rows past a row the
  first is stuck on. Per-account order breaks, and `stash_version` can
  arrive out of order.
- **Why not a Kubernetes Lease:** it adds RBAC and client-go to inventory
  for what one SQL call does. The lock ends with the transaction, so a dead
  pod frees it at once. It also works through PgBouncer in transaction mode.
- The `run.ended` consumer group splits partitions across replicas. 3
  partitions cap it at 3 useful consumers.
- A rebalance can hand a record to the other replica before its offset is
  committed. `EndRun` locks the account row and skips a run id it already
  wrote, so the second apply is a no-op.

### instance-manager: active-active

2 replicas, no leader election.

- `GameServerAllocation` is atomic in Agones. Two managers cannot both give
  out one seat.
- Route creation already treats `AlreadyExists` as done. Two informers
  making the same Service is safe. A test races a walk-in on one manager
  against the informer on the other.
- The manager keeps no state of its own. Every answer reads Agones or the
  API server, so either replica can answer any call.
- The manager never updates or deletes a route. Owner references on the
  GameServer let Kubernetes delete them.
- Each pod observes `instance_startup_seconds` for every GameServer, so the
  count is doubled. The dashboards only take quantiles of it, which do not
  change. A failed informer route is counted by each pod that tried.
- If the manager starts producing `instance.lifecycle` from the informer,
  both would send it. Produce it from the allocate path, or dedupe by name
  and state.
- `RollingUpdate` with `maxUnavailable: 0`, `maxSurge: 1`. A PDB with
  `minAvailable: 1`. On SIGTERM a pod keeps answering for 5 s with
  keep-alives off, so the Service drops it before it stops.
- Agones `extensions` serves `GameServerAllocation`. It runs 2 replicas in
  `argocd/agones`, `RollingUpdate`, spread across nodes, with the chart's
  PDB at `minAvailable: 1`. The chart's default strategy is `Recreate`,
  which would stop both at once. The controller stays at 1. A restart
  there does not block a walk-in.

### Postgres: a second CNPG instance

Done in FOM-254. The details and the move off the old volume are in
[Persistence](persistence.md), under Two instances.

- `postgres.instances: 2`. One per storage node, with CNPG's pod
  anti-affinity `required` on `kubernetes.io/hostname`.
- `longhorn-single-replica` for new volumes. CNPG already copies the data.
  Two Longhorn replicas each would be four copies on two nodes. CNPG does
  not move an existing PVC to a new class, so moving the first instance is
  a person's job.
- Async replication. One sync replica would block every commit while it is
  down, which is the outage this removes.
- `primaryUpdateMethod: switchover`, so a rollout promotes the replica
  instead of restarting the primary.
- Failover takes tens of seconds. inventory calls fail meanwhile. Players
  see "characters unavailable" or "Saving...", as in any database outage.
- Apps use the `-rw` Service from the CNPG secret, so failover needs no app
  change.
- **Pooler: deferred.** pgxpool's default is `max(4, CPUs)` per pool.
  inventory has two pools. Three replicas stay well under the default 100
  connections. Add a CNPG Pooler in transaction mode when connections pass
  half of `max_connections`. Persistence, under Connections, has the query.
- HA is not a backup. The nightly ScheduledBackup and WAL archiving are
  FOM-255.

### Redpanda: stays at one broker for now

- 3 brokers with RF 3 need one broker per node and three storage nodes.
  Longhorn has two.
- Cost at 3 brokers: 4.5 Gi of memory, up from 1.5 Gi. 15 Gi of disk on a
  1-replica class, or 30 Gi on today's.
- What a broker outage costs today: results wait in the instance server's
  memory, and players see "Saving..." longer. The broker's volume has 2
  Longhorn replicas, so a node loss does not lose acked data.
- Move when a third storage node exists. Then 3 brokers on the 1-replica
  class, and raise each Topic's `replicationFactor` to 3.

### PDBs

| Workload | PDB | Why |
| -- | -- | -- |
| world shards | `maxUnavailable: 1` | A drain moves players. One shard at a time keeps room for them |
| inventory | `minAvailable: 1` | The join path needs one |
| instance-manager | `minAvailable: 1` | Same |
| Agones extensions | `minAvailable: 1` | It serves `GameServerAllocation` |
| Postgres | CNPG's own. `-primary` at `minAvailable: 1`, none for one replica | CNPG switches over before it lets the primary's node drain |
| Redpanda | The operator makes its own | |
| Caves | None | `eviction.safe: Always`. The drain is `DRAIN_TIMEOUT` |
| Valkey | None | One pod. Losing it is safe |

## 4. Autoscaling

### KEDA

Install the KEDA operator in homelab, like the other shared operators.
ScaledObjects live in the fantasy-rpg chart. The trigger is the in-cluster
Prometheus.

- **Town shards:** a ScaledObject on the world StatefulSet. Query
  `sum(town_players)`. Threshold is the soft cap, so KEDA wants one shard
  per soft cap of players. `minReplicaCount: 1`. `cooldownPeriod: 600`, so
  a quiet minute does not drain a shard.
- **inventory:** two triggers. Store calls per second from
  `store_duration_seconds_count`, at a threshold from measurement. And
  `run.ended` lag with KEDA's Kafka scaler. `minReplicaCount: 2`,
  `maxReplicaCount: 3`, the partition count.
- instance-manager stays at 2. Its load is one call per walk-in.
- **Why KEDA, not a plain HPA:** an HPA on a custom metric needs
  prometheus-adapter and its rules. KEDA reads a PromQL query and Kafka lag
  directly, and one operator covers both.

### Caves: FleetAutoscaler stays

- The buffer FleetAutoscaler knows which GameServers are Ready and only
  removes those. KEDA on the Fleet would not.
- Raise `maxReplicas` from the caves-per-node number.
- Town shards are not a Fleet, so the FleetAutoscaler does not apply to
  them.

## 5. Caching

Deferred. Nothing reads hot enough to need one.

- Character load is one primary key read per join. Measured p99 decides,
  but it should be a few ms.
- Item, class and cave data are in the binary.
- In the town, the lobby is in the shard's memory. It is already the cache.
- The directory in Valkey is the one shared cache, and it holds routing
  only.
- A cache on stash reads would need invalidation on the one path that must
  never be wrong.
- **Trigger:** `Load` p99 over 20 ms with CNPG CPU over 70%. Then a cache
  inside inventory, keyed by account and `stash_version`. Not a shared
  one.

## 6. Deferred

| Item | Why not now | Trigger to revisit |
| -- | -- | -- |
| UDP or WebTransport, FOM-232 | Cloudflare carries WebSocket only. The transport interface is in place | Snapshot gap p99 through the tunnel far over the `-via` run, or movement that feels bad at measured RTT |
| Predictive scaler, FOM-242 | The buffer may be enough | Walk-ins hit `full`, or allocation p99 shows cold starts, with the buffer at 1 |
| Redpanda 3 brokers | Two storage nodes | A third storage node |
| CNPG Pooler | Connections are low | Over half of `max_connections` |
| Stash cache | No hot reads | See Caching |
| Towns on Agones | Towns do not end | If towns become timed sessions |
| Parties, cross-shard chat | Features that do not exist | When parties ship |

## 7. Operators and infra

| What | Repo | Notes |
| -- | -- | -- |
| KEDA operator | homelab | Shared operator, like CNPG and the Redpanda operator |
| Longhorn 1-replica StorageClass | homelab | For CNPG now, Redpanda later |
| Prometheus and Grafana values | homelab | Monitor selector and sidecar namespaces, only if the defaults block them |
| NATS | homelab | Already installed. Used for chat later |
| Valkey | fantasy-rpg | A Deployment and Service in the chart. One pod needs no operator |
| ScaledObjects | fantasy-rpg | In the chart |
| PodMonitors, dashboards, PDBs | fantasy-rpg | In the chart |
| Agones `extensions` replicas | fantasy-rpg | `argocd/agones`, where Agones is installed |
| CNPG `instances: 2` | fantasy-rpg | The Cluster is in the chart |
| Bot Job | fantasy-rpg | `deploy/kubernetes/loadtest`. Not in Argo CD |

## Tickets

In build order. Each one merges on its own and leaves the game playable.

1. **fantasy-rpg: scrape the game.**
   - PodMonitors for world, inventory, manager and cave GameServers. Agones
     and Redpanda metrics on through their values.
   - Add `town_players`, `instance_players`, `ws_sent_bytes_total`,
     `ws_slow_kicks_total`.
2. **homelab: let Prometheus and Grafana find app monitors.**
   - Check the kube-prometheus-stack monitor selectors and the dashboard
     sidecar's namespaces.
   - Change the values only if the defaults block fantasy-rpg.
3. **fantasy-rpg: Grafana dashboards.**
   - Town, Caves, Persistence and Bots as JSON in the chart.
   - ConfigMaps with `grafana_dashboard: "1"`.
4. **fantasy-rpg: bot client.**
   - `src/cmd/bot`: join, walk, cave, extract, rejoin. `-n`, `-ramp`,
     `-duration`, `-via`, `-fight`.
   - `bot_` metrics on `/metrics`. `just bots N` against `just dev`.
5. **fantasy-rpg: load test Job and first numbers.**
   - Indexed Job in `deploy/kubernetes/loadtest`, PodMonitor for bots,
     `just loadtest`.
   - Run it, in-cluster and public. Write the numbers into this doc. Set
     `TOWN_MAX_PLAYERS`, the soft cap and resource requests.
6. **fantasy-rpg: inventory with 2 replicas.** Done, FOM-251.
   - Advisory lock in the relay, with a test of two relays on one table.
   - `RollingUpdate`, 2 replicas, PDB.
7. **fantasy-rpg: instance-manager with 2 replicas.** Done, FOM-252.
   - `RollingUpdate`, 2 replicas, PDB. A test of two managers routing one
     GameServer.
   - Agones `extensions` at 2 replicas.
8. **homelab: Longhorn 1-replica StorageClass.** Done, FOM-253.
   - For workloads that copy their own data.
9. **fantasy-rpg: second Postgres instance.** Done, FOM-254.
   - `instances: 2` on the new class. Fail over once by hand and watch the
     game.
   - Moving the existing volume to the new class is a person's job. Write
     the steps in the PR.
10. **fantasy-rpg: session claim in inventory.** Done, FOM-256.
    - `sessions` and `shards` tables, claim, release, heartbeat.
    - World claims on join and releases after the last write. Still one
      shard.
11. **fantasy-rpg: Valkey and the directory.**
    - Valkey in the chart and in Compose.
    - `internal/towns`: shard heartbeat and the pick order. Fail open when
      Valkey is down.
12. **fantasy-rpg: front door and TownTicket.**
    - `TownTicket` in the proto. `/towns/{id}/ws` on the world server. The
      `/ws` redirect.
    - Godot follows `TownTicket` and falls back to `/ws`. The bot does too.
13. **fantasy-rpg: world as a StatefulSet.**
    - StatefulSet, per-shard Services and routes up to `world.maxShards`,
      PDB.
    - Drain on SIGTERM with `TownTicket`. Newest revision first. 2 replicas.
14. **homelab: install KEDA.**
    - Operator through Argo CD, like the other shared operators.
15. **fantasy-rpg: ScaledObjects.**
    - World shards on `sum(town_players)`. inventory on store calls and
      `run.ended` lag.
    - Bot run that scales shards up and back down.
16. **fantasy-rpg: cross-shard chat on NATS (stretch).**
    - Global and party subjects, rate limit per player.
    - Only once chat exists in one town.
