# Instance manager

The instance manager makes an instance server pod when players need one,
routes players to it, and deletes it when it empties. It replaces the fixed
`fantasy-rpg-instance` StatefulSet. It builds on [Instances](instances.md)
and the cave entrance in [Hub town](hub-town.md). FOM-112 builds it.

## Rules that do not bend

- Everything in [Instances](instances.md) still holds. One socket at a
  time, the join token, the run lease, the instance server writes run
  results. The handoff and the proto do not change.
- One instance per pod. One pod is one boot. A pod never comes back as a
  new boot under the same name.
- Postgres stays the only source of truth for player state. Kubernetes is
  the only source of truth for which instances exist. The world server
  keeps no list of its own on disk.
- A pod with a live run is never deleted on purpose. Not by the manager,
  not by a deploy.
- No new system. No operator, no CRD of our own, no Agones.

## Today

- The chart runs a StatefulSet with `instances.replicas` pods, 2 in dev.
  The pod name is the instance id.
- The chart makes one Service and one IngressRoute route per pod, at
  `/instances/fantasy-rpg-instance-N/`.
- The chart renders the world server's `INSTANCES` from the same count.
  `Directory` polls each one's `/status` every 2 s and `Reserve` picks the
  fullest instance with room.
- An empty instance resets to a new world with a new seed. The pod lives
  until a deploy.
- A PDB with `maxUnavailable: 0` stops every eviction. `kubectl drain`
  waits on the instance pods until someone deletes them by hand.
- `just instance-start` and `deploy/kubernetes/instance-pod.yaml` make a
  bare pod by hand in k3d.

## Where the manager lives

Inside the world server, as a backend behind `Directory`.

- The world server already picks the instance, counts in-flight tickets,
  mints the token and clears dead leases. All of that needs the same
  instance list. A second service would have to agree with it on every
  ticket.
- The world server is one replica with `Recreate`. One process makes and
  deletes pods, so no leader election and no lock.
- The cost: the world server needs RBAC to make pods, and a world restart
  stops new instances until it is back. Runs already in an instance go on.
  Lobby players are dropped by the restart anyway.

## How pods are made

client-go against the Kubernetes API, with a narrow Role. Not Agones.

- Agones is a controller, CRDs, a sidecar SDK and an allocator. Its model
  is UDP on host ports. We use WebSocket through Traefik. It would be one
  more operator to run and upgrade on Talos for a few hundred lines of Go.
- client-go is already how anything in the cluster does this. The fake
  clientset makes the manager testable without a cluster.

### RBAC

A ServiceAccount for the world Deployment, and a Role in the namespace:

| Resource | Verbs |
| -- | -- |
| `pods` | get, list, watch, create, delete |
| `services` | get, list, create, delete |
| `ingressroutes.traefik.io` | get, list, create, delete |

- Instance pods get no token: `automountServiceAccountToken: false`.
- No ClusterRole. The manager only works in its own namespace.

### The pod template

- The chart renders the instance pod spec into a ConfigMap,
  `fantasy-rpg-instance-template`, from the same values the StatefulSet
  uses today: image, env, `MAX_PLAYERS`, the join key and database env,
  probes and anti-affinity.
- The world server mounts it and reads it at start. It hashes the spec and
  puts the hash on each pod as a label.
- The image tag is in both the template and the world Deployment, so a
  Kargo promotion restarts the world server with the new template.
- `INSTANCE_ID` still comes from `metadata.name` through the downward API.
  The instance server does not change.

### Names and labels

- The pod name is `fantasy-rpg-instance-` plus 5 random characters, through
  `generateName`. It is the instance id, as now.
- Labels on every runtime object:
  - `app.kubernetes.io/name: fantasy-rpg-instance`
  - `app.kubernetes.io/managed-by: fantasy-rpg-world`
  - `fantasy-rpg.fomiller.com/instance: <pod name>`
  - `fantasy-rpg.fomiller.com/template: <hash>`
- Not `app.kubernetes.io/instance`. Argo CD's legacy tracking reads that
  label. A pod that carries it can be shown as out of sync and pruned.

### Keeping Argo CD out of it

- Argo CD only prunes what it tracks. Runtime objects carry no tracking
  label or annotation, so Argo CD never sees them as its own.
- The pod has no ownerReference. A world server restart or rollout must not
  delete live runs through garbage collection. A pod with no controller is
  also one the descheduler leaves alone by default.
- The Service and the IngressRoute have an ownerReference to their pod.
  When the pod goes, Kubernetes deletes both. The manager never cleans up
  routes by itself.
- Deleting the Argo CD Application leaves instance pods behind. They are
  found by `app.kubernetes.io/managed-by=fantasy-rpg-world`. That is fine
  for a homelab.

## Routing

A Service and an IngressRoute per pod, made at runtime. Same paths as now.

- The pod serves its socket at `/instances/{id}/ws`, as now. The route is
  `PathPrefix(/instances/{id}/)` to the pod's Service. No middleware.
- The Service selects on `fantasy-rpg.fomiller.com/instance`.
- The world server's internal URL is the pod IP, `http://{podIP}:8080`. It
  does not need the Service for `/status`.
- Traefik watches IngressRoutes and picks a new one up in about a second.
  That is part of startup time.

Why not the others:

- **One route to a proxy in the world server.** Every run's traffic would
  go through the world server. A world restart would drop every run.
- **One route and a header or cookie.** The client does not send one, and
  Traefik cannot pick a pod by path without a Service per pod anyway.

## Lifecycle

| State | Meaning | Picked? |
| -- | -- | -- |
| `starting` | Created. Not yet ready, or its route does not answer yet | Only by a wait (see Reuse or create) |
| `ready` | Pod ready, route answers, `/status` answers | Yes, if it has room |
| `full` | Players plus in-flight tickets reach capacity | No |
| `draining` | Being deleted, or old template, or reports `draining` | No |
| `gone` | Pod deleted or failed | No. Dropped from the list |

`full` is worked out on each pick. The others come from Kubernetes and
`/status`.

### How the manager learns state

- A pod informer on `app.kubernetes.io/managed-by=fantasy-rpg-world`. It
  gives create, the `Ready` condition, phase, pod IP and
  `deletionTimestamp`.
- `Directory` keeps polling `/status` every 2 s, as now. Its list is
  whatever pods the informer holds, not `INSTANCES`.
- Ready means all three: the pod is `Ready`, `/status` answers with a boot
  id, and a probe through Traefik answers.
- The route probe is `GET /instances/{id}/healthz` on
  `ROUTE_PROBE_URL`, Traefik's in-cluster Service, with the public `Host`
  header. The instance server adds that handler. Empty `ROUTE_PROBE_URL`
  skips the probe.
- A pod that is not ready 60 s after create is deleted and counted as a
  startup failure.

### Idle, spare and cap

| Env | Default | What |
| -- | -- | -- |
| `INSTANCE_MAX` | 8 | Pods at once, any state. At the cap, an entry with no room is `full` |
| `INSTANCE_SPARE` | 1 | Empty `ready` instances to keep warm |
| `INSTANCE_IDLE` | 60s | How long an extra empty instance lives |

- Empty means no players, no in-flight tickets, and no runs in `/status`.
  `/status` lists runs whose `EndRun` has not landed, so a pod waiting on a
  database outage is never empty.
- An instance already resets to a new world when it empties. So a spare
  is a fresh cave. Reuse is safe.
- An empty instance past `INSTANCE_IDLE`, with more than `INSTANCE_SPARE`
  empty instances left, is deleted. The newest goes first, so older pods
  fill.

### Crashes

- Instance pods run with `restartPolicy: Never`. A crash makes the pod
  `Failed`. The manager deletes it. Its Service and route go with it.
- The pod is then gone from the list. `DeadLease` already treats a lease
  on an instance that is not in the list as dead. The player's next join
  clears it, as now.
- So a crash costs what it costs today: the run is lost, and the character
  is as it was at `StartRun`.
- A pod that crashes in `starting` counts as a startup failure. The
  manager makes a new one on the next need, not in a loop.

## Reuse or create

On a walk-in, `Reserve` runs as now, over `ready` instances:

1. Pick the `ready` instance with the most players that has room for the
   whole party. One cave fills before the next.
2. If none, and a `starting` instance will have room, wait for it.
3. If none, and the cap allows, create one and wait for it.
4. If the cap is reached, reject with `full`.

- The wait is up to 20 s. The client shows "Entering the cave..." as now.
  It runs off the tick, like the `Load` before it. Past 20 s, reject with
  `full`.
- A `starting` instance counts the runs waiting on it against its
  capacity, so two walk-ins at once do not make two pods.
- After every pick, if fewer than `INSTANCE_SPARE` empty instances are
  left, create one in the background. With one spare, a walk-in almost
  never waits on a pod.
- No new proto. The ticket just arrives later.

## World server restart

- The informer's first list is the state. Every pod with the label is
  adopted, whatever made it.
- The first `/status` poll fills players, runs and boot ids. Until a pod
  answers, it is not picked, and its leases are not cleared. Same rule as
  now.
- In-flight tickets are in memory and lost. A pick right after a restart
  can overfill by a few. The instance still closes extra joins with
  `instance full`, so the player goes back to the town and tries again.
- Idle timers restart from zero. An idle pod lives one `INSTANCE_IDLE`
  longer. Nothing worse.
- Pods with an old template hash are `draining` from the start.

## Deploys

- A new template hash means every pod with the old hash is `draining`.
  New walk-ins go to new pods. Old pods are deleted when empty, with no
  idle wait.
- So a deploy never kills a run. It does mean old and new instance
  servers run side by side against one schema for as long as a run lasts.
  Migrations must keep working for the version before. They already must,
  since the migrate Job runs before the rollout.

## Graceful drain

Drop the PDB. Let a pod be deleted, and make the instance server finish its
runs before it exits.

On SIGTERM the instance server:

1. Reports `draining: true` in `/status`. The manager stops picking it.
   Readiness stays green, so Traefik keeps the route for players already
   in.
2. Closes new joins with 1013 `instance draining`. No lease is taken.
3. Lets current runs play on until they end, up to `DRAIN_TIMEOUT`.
4. At the deadline, ends each run as a disconnect does: `EndRun` with the
   carried loadout, then close 1001 `instance draining`.
5. Waits for the writer to land pending saves, then exits.

- `terminationGracePeriodSeconds` is `DRAIN_TIMEOUT` plus 30 s for the
  writes. Start at 600 s and 630 s.
- Today the signal cancels the server context and closes every socket at
  once. Draining changes that order. The tick loop must keep running until
  step 5.
- A pod killed past its grace period is a crash. Its leases clear as
  above.

Who sends SIGTERM:

- The manager, when it deletes an empty or old pod. It only deletes empty
  ones, so the drain is instant.
- A node drain. Bare pods need `kubectl drain --force`. The force only
  skips the "no controller" check. The pod still gets SIGTERM and its
  grace period.
- The descheduler skips pods with no controller by default, so it should
  not evict them at all. If that changes, the drain covers it.

## Local dev

`Directory` takes a pool behind an interface:

```go
// Pool says which instances exist and makes or drops them.
type Pool interface {
	// List is every instance that exists now, in any state.
	List() []Instance
	// Grow asks for one more instance. A static pool never grows.
	Grow(ctx context.Context) error
	// Drop deletes an empty instance. A static pool never drops.
	Drop(ctx context.Context, id string) error
}
```

- `INSTANCE_POOL=static` is the default. It reads `INSTANCES`, as now.
  `just dev`, `just instance <id> <port>` and `net-test` do not change.
- `INSTANCE_POOL=kube` is the client-go pool. The chart sets it.
- `Reserve` and `DeadLease` work the same on both. Only the list's source
  differs.
- `just deploy-local` in k3d can run the kube pool. Then
  `instance-start`, `instance-stop` and `instance-pod.yaml` go away.

## Metrics

On the world server:

| Metric | Type | Labels | What |
| -- | -- | -- | -- |
| `instance_startup_seconds` | histogram | `result` (`ready`, `failed`) | From create to ready, including the route probe. Buckets 0.5 s to 60 s |
| `instance_routing_errors_total` | counter | `reason` | See below |
| `instances` | gauge | `state` | Pods per state |
| `instance_reserve_wait_seconds` | histogram | | How long a walk-in waited for a pick |

`reason` values:

- `ticket_unused`. A ticket's run never showed in that instance's
  `/status` before the token expired. The client could not reach it, or
  was turned away.
- `route_probe`. The route probe failed for a pod that was otherwise
  ready.
- `full`. A walk-in was rejected because the cap was reached.
- `create`. The API refused a pod, Service or IngressRoute.

`ticket_unused` uses the in-flight list `Directory` already keeps. When an
entry expires without its run in `/status`, it counts one.

## Failure cases

| When | What happens | What the player sees |
| -- | -- | -- |
| Pod is slow to start | The walk-in waits up to 20 s | "Entering the cave..." for longer. Then `full` if it never comes |
| Pod never gets ready | Deleted at 60 s, counted as `failed` | `full` after 20 s. The next walk-in makes a new one |
| Route not picked up yet | Not ready until the probe answers | A longer wait, not a failed join |
| Cap reached | `full` | "All instances are full." |
| API server down | No create, no delete. Existing pods keep working. The informer serves its cache | Walk-ins work while there is room, then `full` |
| World server restarts | Adopts pods by label. Runs go on | Town players reconnect, as now |
| Instance crashes | Pod `Failed`, deleted. Lease clears on the next join | As in Instances: back to the town with "Saving...", then the pre-run character |
| Node drain | Pods drain, then go | Runs end at the latest at `DRAIN_TIMEOUT`, as a disconnect |

## Implementation plan

In order. Each one merges on its own and leaves the game playable.

1. **Pool interface and routing metrics.**
   - `Pool` with the static backend from `INSTANCES`. `Directory` reads its
     list from the pool. No behavior change.
   - `instance_routing_errors_total` with `ticket_unused`, and the
     `instances` gauge.
   - `just dev`, `just instance` and `net-test` still pass as they are.
2. **Graceful drain on the instance server.**
   - SIGTERM handling as above, `DRAIN_TIMEOUT`, `draining` in `/status`,
     the world server never picks a draining instance.
   - `terminationGracePeriodSeconds` on the StatefulSet. Drop the PDB.
   - A Go test that a run started before SIGTERM still lands its
     `EndRun`.
3. **Kube pool.**
   - client-go pool: create the pod, Service and IngressRoute with the
     labels and ownerReferences above. Informer, states, the route probe,
     `/instances/{id}/healthz`.
   - Reuse or create, the wait, `INSTANCE_MAX`, `INSTANCE_SPARE`,
     `INSTANCE_IDLE`, crash cleanup, rebuild on restart.
   - `instance_startup_seconds` and `instance_reserve_wait_seconds`. Tests
     against the fake clientset.
4. **Chart switch.**
   - The template ConfigMap, the ServiceAccount and Role,
     `INSTANCE_POOL=kube` and `ROUTE_PROBE_URL` on the world server.
   - Remove the StatefulSet, its Services, its routes and the `INSTANCES`
     helper. Template hash labels, and draining old pods on deploy.
   - `deploy-local` on the kube pool in k3d. Remove `instance-start`,
     `instance-stop` and `instance-pod.yaml`. Update the README.

## Later

- **Instances of more than one kind.** `CAVE` is per pod today. The pool
  could take the cave as an argument when caves differ.
- **A pod per node pool or region**, if the cluster ever has more than one.
- **Reconnect into a run** (FOM-217) needs the pod to still exist when the
  player comes back. An instance with a held body is not empty, so the
  rules above already keep it.
