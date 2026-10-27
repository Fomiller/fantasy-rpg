# Load test

How to run the bots in the homelab cluster and write down what they show.
The plan behind it is in [Scaling](scaling.md). FOM-250 is the Job and this
page. The bot is `src/cmd/bot`, from FOM-249.

## What runs

- `deploy/kubernetes/loadtest` is a kustomize base with two objects.
  - `fantasy-rpg-bots`, an Indexed Job. One pod per index, each running
    `-n` bots named `bot-<index>-<n>`.
  - A PodMonitor with `release: prometheus` on
    `app.kubernetes.io/component: bot`. The Bots dashboard reads it.
- Same image as the game, `command: [/bot]`. `just loadtest` uses the tag
  Kargo last wrote to `argocd/overlays/dev/values.app.yaml`.
- The bots dial `wss://fantasy-rpg.fomiller.com/ws` through
  `http://traefik.traefik.svc.cluster.local`. That is the Traefik Service
  the Cloudflare tunnel sends to, so routing is the same as a player's.
  Ticket URLs go the same way.
- Argo CD never applies it. It only reads `argocd/overlays/dev`, and nothing
  there points at `deploy/`. A sync cannot start a test.
- The recipes use `kubectl --context admin@homelab`. Change it with
  `just homelab='kubectl --context <ctx>' ...`.

## Commands

```sh
# pods, bots per pod, then optional duration, ramp per pod, via, tag
just loadtest 1 10
just loadtest 2 25 15m 2
just loadtest 1 10 10m 2 ''   # through the public host and the tunnel

just loadtest-status          # Job and pods
just loadtest-scale 4         # more or fewer pods in a running test
just loadtest-down            # delete the Job, its pods and the PodMonitor
```

- `ramp` is per pod. The Job starts `pods` times `ramp` bots per second.
- A pod stops on its own after `duration`. The Job then shows Complete.
- `just loadtest` refuses to start while a Job is there. Run
  `just loadtest-down` between stages.
- `just loadtest-scale` starts new pods at once. Their `duration` starts
  then, so they end later than the first pods.
- Without `just`:

```sh
kubectl --context admin@homelab apply -k deploy/kubernetes/loadtest
kubectl --context admin@homelab -n fantasy-rpg patch job fantasy-rpg-bots \
  --type merge -p '{"spec":{"completions":2,"parallelism":2}}'
kubectl --context admin@homelab delete -k deploy/kubernetes/loadtest
```

  The base is 1 pod of 10 bots for 10 minutes, at the tag in
  `kustomization.yaml`.

## Before the first run

- The Bots dashboard shows data. If it is empty, check Prometheus picks up
  the PodMonitor. This is the homelab check in Scaling, ticket 2.
- The town holds `TOWN_MAX_PLAYERS` players, 32 today. The Fleet holds at
  most 8 caves of 8. Past those, the bots count `world_join` errors (a
  `town full` close) or `enter_full` errors. That is a finding, not a broken test. To measure
  past the cap, raise `TOWN_MAX_PLAYERS` in `world.env` and
  `instances.autoscaler.maxReplicas` in
  `argocd/overlays/dev/values.app.yaml` first.
- Nobody else is playing. A real player in the town changes the numbers.

## Stages

Run each stage for 10 minutes. Wait for the graphs to settle before you
read them. Read the last 5 minutes, not the ramp.

| Stage | Command |
| -- | -- |
| 10 bots | `just loadtest 1 10` |
| 25 bots | `just loadtest 1 25` |
| 50 bots | `just loadtest 2 25` |
| 100 bots | `just loadtest 4 25` |
| 100 bots, public | `just loadtest 4 25 10m 2 ''` |
| Join storm | `just loadtest 4 25 10m 5`, 20 per second |

Stop at the first stage that breaks a line in Scaling's "Numbers to find".
Then the stage before it is the number.

## What to watch

- **Town:** Tick p99 and Tick duration by pod. Missed ticks per second.
  Players in town. WebSocket bytes. Slow kicks. CPU and memory by pod.
- **Caves:** Tick p99 by instance. Players by instance. Instances by state.
  Allocation p99 and latency by result. Startup time. Routing errors. CPU
  and memory by pod.
- **Persistence:** Inventory p99 by call. Store errors by call. Outbox
  pending. Consumer lag. Pending saves.
- **Bots:** Join, Ticket, Instance join and Saving, p50 and p99. Snapshot
  gap p99 by where. Errors by stage.

## Results

Fill one row per stage, from the last 5 minutes of the run. Errors is the
sum of Errors by stage. Name the stages that had any.

| Players | Town tick p99 | Cave tick p99 | World CPU | World memory | Cave CPU | Cave memory | Allocation p99 | Errors | Date, tag |
| -- | -- | -- | -- | -- | -- | -- | -- | -- | -- |
| 10 | | | | | | | | | |
| 25 | | | | | | | | | |
| 50 | | | | | | | | | |
| 100 | | | | | | | | | |
| 100, public | | | | | | | | | |

## What the results set

- **`TOWN_MAX_PLAYERS`:** the most players in town where Town tick p99
  stays under 10 ms with no missed ticks. Round down. It goes in `world.env`
  in `argocd/overlays/dev/values.app.yaml`. Read Players in town, not the
  stage size. Part of the bots are in caves.
- **Shard soft cap:** 75% of `TOWN_MAX_PLAYERS` to start, as Scaling says.
  Lower it if the town bends sharply near the cap. It is the KEDA threshold
  in ticket 15.
- **Pod requests:** the chart sets no requests on the world, instance or
  inventory pods today. Set the CPU request to the measured CPU at the
  chosen cap, and the memory request to the measured memory plus room for
  a spike. The cave numbers also set Fleet `maxReplicas`, from caves per
  node.
- Write the numbers that came out of it here, under the table, with the
  date and tag. Change Scaling's "Today" section to match.
