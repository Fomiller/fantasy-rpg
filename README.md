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

Only `src/cmd/world-server` exists so far. The other paths get created by the ticket that needs them.

## Development

```sh
nix develop    # Go, buf, protoc, just, docker, kubectl, k3d, kind, helm
just dev       # run the world server on :8080
just build
just test
just lint
```

Godot is not in the dev shell. Install Godot 4 yourself.
