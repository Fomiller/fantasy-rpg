# Bots

Always-on bots in the cluster. A `bot-director` decides how many bots run,
and bot worker pods run them. FOM-263 is this page. The bot itself is
`src/internal/bot`, from FOM-249. The plan it fits in is
[Scaling](scaling.md).

## Modes

| Mode | What runs |
| -- | -- |
| `fill` | Bots so that humans plus bots reach `count`. Bots leave as humans join |
| `fixed` | Exactly `count` bots. For load tests |
| `off` | No bots |

```sh
just bots-set fill 6
just bots-set fixed 100
just bots-set off
just bots-status    # target, desired, replicas, workers, humans, bots
```

- The setting is saved in the ConfigMap `fantasy-rpg-bot-director-target`
  before it applies. A restart keeps it.
- Until the first `bots-set`, the target is `bots.mode` and `bots.count`
  from the chart.
- The recipes `kubectl exec` the director's own client, which calls
  `localhost`. The API has no public route.

## Shape

```text
world /v1/players ----------+
instance-manager /v1/players +--> bot-director --scale--> worker Deployment
  (asks each Allocated cave)          ^
                                      | GET /v1/assignment?pod=&have=
                              worker pods, 25 bots each at most
```

- One image. `src/cmd/bot-director` is the director. The worker is
  `/bot -director <url>`. Without `-director`, `/bot -n` runs as before,
  so `just bots` and the load test Job are unchanged.
- Every 5 s the director counts players, picks the bot count, and sets the
  worker Deployment's replicas to `ceil(bots / perPod)` through the scale
  subresource.
- The worker template has no `replicas`, so Argo CD leaves them to the
  director, as it leaves the Fleet's to the FleetAutoscaler.
- The director is one pod, `Recreate`. The slot split lives in its memory.

### Slots

- The director hands out slots `0` to `bots - 1`. Slot `n` is the bot named
  `bot-fill-<n>`. Names are stable, so bots reuse a few characters, and
  never clash with the load test's `bot-<index>-<n>`.
- Each live pod gets an even share, at most `perPod`. A new pod takes slots
  from the busiest, and the rest stay put.
- A worker polls every 2 s with the slots it still runs in `have`. A slot
  taken from a pod goes to another only once the first pod no longer has
  it. One name never has two sockets, so no bot hits `already playing`.
- A pod that stops polling for 15 s loses its slots.
- After a director restart, each worker's `have` gives it back the slots it
  already runs.

### Workers

- A worker starts at most `ramp` bots per second, and stops removed bots at
  once.
- A removed bot closes its socket. In a cave that ends the run as a
  disconnect does, so the seat frees and nothing is left stuck.
- With the director unreachable, a worker keeps its last share for 60 s,
  then stops every bot.
- SIGTERM stops every bot and waits for its socket to close. The grace
  period is 30 s.

## Humans and bots

- The bot sets `bot` in `JoinRequest`. The world keeps it on the player and
  signs it into the join token, so the instance counts the bot the same way
  without trusting the client again.
- The flag only takes a player out of the human count. A client that sets
  it gains nothing, and at worst gets more bots for company.
- Why a flag, not a name prefix: real players can pick any name. A flag is
  explicit and costs one proto field.
- Metrics: `town_players` and `instance_players` stay totals, so the
  dashboards keep working. `town_bots` and `instance_bots` are the bots
  among them.

### Counting

- `GET /v1/players` on the world server answers `{humans, bots}` for the
  town.
- `GET /v1/players` on instance-manager asks every Allocated GameServer's
  own `/v1/players` at once and adds them up. A Ready one is empty.
- Both are on the internal port. The world's public route is `/ws` only,
  and a cave's is `/instances/<name>/`.
- The director adds the two. If either fails, fill mode holds the bot count
  it has. A short count would add bots for humans who are there.
- Why not Prometheus: a scrape is 15 s old, and the director would depend on
  the monitoring stack to run the game.
- A player between the town and a cave is in neither count for a moment.
  The up delay covers it.
- Once the world is several shards behind one Service, the world URL has to
  be each shard. [Scaling](scaling.md) puts that in the world StatefulSet
  ticket.

## Fill rule

Every round:

1. `want = clamp(count - humans, 0, maxBots)`.
2. Bots over `want + slack` leave at once.
3. Otherwise a change waits until `want` has been on the same side for
   `upDelay` (10 s) to add, or `downDelay` (60 s) to remove.
4. Then it moves at most `step` (2) bots per round.

- Humans flickering across the line reset the wait, so bots do not flap.
- `slack` (2) is how far over the target the game may run while a removal
  waits. Past it, bots leave at once, so a crowd is never kept out.
- `maxBots` (16) caps fill mode. Keep it under `TOWN_MAX_PLAYERS` (32) and
  the Fleet's seats, `maxReplicas * maxPlayers` (64). Bots alone then never
  fill the town or the caves.
- `maxWorkers * perPod` (100) caps every mode, `fixed` too.
- After a restart, fill mode starts from the bots already playing, not 0.

## Metrics

| Metric | What |
| -- | -- |
| `bot_director_target` | The target's count. 0 when off |
| `bot_director_mode` | 1 for the current mode, by `mode` |
| `bot_director_desired` | Bots the director wants |
| `bot_director_humans`, `bot_director_bots` | The last count |
| `bot_director_workers` | Pods polling |
| `bot_director_worker_replicas` | Replicas set on the worker Deployment |
| `bot_director_errors_total` | By `what`: `players`, `scale`, `store` |
| `bot_assigned` | A worker's share |
| `bot_assignment_errors_total` | A worker's failed polls |

The Bots dashboard has two panels for these: target, humans and bots, and
workers.

## Chart

- `bots.enabled` is off by default and needs `world.enabled`. dev turns it
  on in `fill` mode with a target of 6.
- `templates/bots.yaml`: the director's ServiceAccount, Role, RoleBinding,
  Deployment and ClusterIP Service, and the worker Deployment. PodMonitors
  for both are in `podmonitors.yaml`.
- The Role: `get` and `update` on the target ConfigMap and on the worker
  Deployment's `deployments/scale`, by name. `create` on ConfigMaps, which
  Kubernetes cannot limit by name.
- The director makes the ConfigMap on its first save. The chart does not
  render it, so Argo CD never puts an old target back. Deleting the release
  leaves it.
- Both pods run non-root with `RuntimeDefault` seccomp and a read-only root.

## The load test Job

`deploy/kubernetes/loadtest` stays for now. `just bots-set fixed N` does
the same job from inside the chart. Once a run in fixed mode matches a Job
run, the Job and its recipes go.
