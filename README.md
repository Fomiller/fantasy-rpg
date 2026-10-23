# fantasy-rpg

Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.

Managed by [flake-hub](https://github.com/Fomiller/flake-hub). Run `nix run .#generate`
after changing `repo.nix`.

## Layout

| Path | What lives there |
| --- | --- |
| `client/godot/` | The 2D Godot client, in GDScript. |
| `src/cmd/<binary>/` | One Go main package per server: `world-server` now, `cave-server` and `instance-manager` later. |
| `src/internal/` | Shared Go game code: world, combat, AI, inventory, loot. |
| `proto/game/v1/` | Protobuf messages shared by the client and the servers. |
| `deploy/` | Docker Compose for local runs, Kubernetes manifests for the homelab. |
| `tests/load/` | Headless bot clients and load test scenarios. |
| `docs/` | The mdbook: game design, architecture, decisions. |

`src/cmd/world-server` and `src/cmd/cave-server` exist so far. The other paths get created by the ticket that needs them.

## Development

Enter the dev shell first. It has Go, buf, protoc, just, docker, kubectl, k3d, kind and helm.

```sh
nix develop
```

Godot is not in the dev shell. Install Godot 4 yourself. Docker must be running.

There are three ways to run the stack, from fastest to most realistic.

**1. Local process.** Postgres and NATS run in Compose. The world server runs as a normal Go process, so a restart is a few seconds.

```sh
just dev     # start postgres + nats, then run world-server on :8080
just deps    # only postgres + nats
```

**2. Full Compose.** Every server runs as a container.

```sh
just up      # world-server :8080, cave-server :8081, postgres :5432, nats :4222
just down
```

**3. Local Kubernetes.** A k3d cluster called `fantasy-rpg`. Recipes always use the `k3d-fantasy-rpg` context, and creating the cluster does not change your current kubectl context.

```sh
just deploy-local   # create the cluster, build and load images, deploy world-server
just cave-start 1   # start a cave pod called cave-1
just caves          # list cave pods
just cave-stop 1
just k8s-down       # delete the cluster
```

Local Postgres login is `fantasy` / `fantasy`, database `fantasy`. It is for local use only.

Other recipes: `just build`, `just test`, `just lint`, `just fmt`.
