# fantasy-rpg

Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.

Managed by [flake-hub](https://github.com/Fomiller/flake-hub). Run `nix run .#generate`
after changing `repo.nix`.

## Layout

| Path | What lives there |
| --- | --- |
| `client/godot/` | The 2D Godot client, in GDScript. |
| `src/cmd/<binary>/` | One Go main package per server: `world-server`, `instance-server`, `instance-manager`, `inventory`, `bot` and `bot-director`. |
| `src/internal/` | Shared Go game code: world, combat, AI, inventory, loot. |
| `proto/game/v1/` | Protobuf messages shared by the client and the servers. |
| `deploy/` | Docker Compose for local runs, Kubernetes manifests for the homelab. |
| `tests/load/` | Headless bot clients and load test scenarios. |
| `docs/` | The mdbook: game design, architecture, decisions. |

The other paths get created by the ticket that needs them.

## Development

Enter the dev shell first. It has Go, buf, protoc, just, docker, kubectl, k3d, kind and helm.

```sh
nix develop
```

Godot is not in the dev shell. Install Godot 4 yourself. Docker must be running.

There are three ways to run the stack, from fastest to most realistic.

**1. Local process.** Postgres and Redpanda run in Compose. Each server runs as a normal Go process, so a restart is a few seconds.

```sh
just dev                     # migrate, then run world-server on :8080
just instance instance-0 8081 # an instance-server, in a second terminal
just instance instance-1 8082 # another, in a third
just deps                    # only postgres + redpanda, with the topics
just migrate                 # apply database migrations to the Compose postgres
just inventory               # the inventory service on :8070
```

The client connects to the world server on :8080 by default. The player walks around the hub town there, uses the stash chest, and walks into the cave entrance to get a ticket. The client then moves to that instance server for the run. When the run ends the client goes back to the town. All three processes share the Compose database and the dev join key. To go through the inventory service as the cluster does, run `just inventory` and start the others with `INVENTORY_URL=http://localhost:8070`. A server with neither `INVENTORY_URL` nor `DATABASE_URL` exits, unless `STORE=memory` is set. Then it keeps characters in memory and loses them on restart.

**2. Full Compose.** Every server runs as a container.

```sh
just up      # world-server :8080, instance-0 :8081, instance-1 :8082, postgres :5432, redpanda :19092
just down
```

**3. Local Kubernetes.** A k3d cluster called `fantasy-rpg`. Recipes always use the `k3d-fantasy-rpg` context, and creating the cluster does not change your current kubectl context. It runs the world server only. Instances need Agones, which this cluster does not install, so use the local process or Compose to play.

```sh
just deploy-local     # create the cluster, build and load images, deploy world-server
just k8s-down         # delete the cluster
```

Local Postgres login is `fantasy` / `fantasy`, database `fantasy`. It is for local use only.

Compose runs Postgres 18. A volume made by the old Postgres 17 setup will not start on it. Reset it once with `docker compose -f deploy/compose/compose.yaml down -v`.

The `internal/db` tests use `TEST_DATABASE_URL` if set, else start `postgres:18` with testcontainers. Without Docker they skip, unless `CI` is set.

Other recipes: `just build`, `just test`, `just lint`, `just fmt`.

## Godot client

The client is a 2D Godot 4.7 project in `client/godot`, written in GDScript. It uses the Compatibility renderer.

```sh
just client         # open the editor
just client-run     # run the game against a local server
just client-online  # run the game against the homelab server
just client-check   # import and run headless; prints "main scene loaded"
just client-test    # headless player tests: movement, walls, interact
just net-test       # start two worlds and three instances on a throwaway Postgres, then play the whole loop
```

Controls: WASD or arrow keys to move, Space or left click to attack in the direction the character faces, E to open the loot window on a chest or pile, then Take an item or Take all, I or Tab for the inventory, Escape to close an open window. The last mouse motion or move key sets the facing.

The server keeps your items by player name. Reconnect with the same name to get them back. Set one with `-- --name=ann`; without it the client picks a random name each launch.

To play against the server, start `just dev` and two `just instance` recipes, then run `just client-run`. The client joins the world server at `ws://127.0.0.1:8080/ws`. An instance server can run another cave with `CAVE=yard`, a small room with no enemies. The top-left label shows your player ID and the server tick. A white outline marks where the server thinks you are. Without a server the label says "offline", the game still runs, and the client retries every 2 seconds. Point at another server with `/Applications/Godot.app/Contents/MacOS/Godot --path client/godot -- --server=ws://host:port/ws`.

The recipes use `/Applications/Godot.app`. For another install, pass the path: `just godot=/path/to/godot client`.

Commit the `*.uid` files Godot writes next to scripts. Godot uses them to keep references working when a file moves. `.godot/` is a cache and is ignored.

### Editing GDScript in Neovim

The Godot editor runs a GDScript language server on port 6005 while it is open.

1. Open the editor with `just client`.
2. Start Neovim with a server socket, so Godot can open files in it:

   ```sh
   nvim --listen /tmp/godot.pipe
   ```

3. In Godot, open Editor Settings > Text Editor > External. Turn on "Use External Editor". Set the path to `nvim` and the flags to `--server /tmp/godot.pipe --remote-send "<C-\><C-N>:e {file}<CR>:call cursor({line},{col})<CR>"`.
4. In Neovim, turn on the `gdscript` LSP config from nvim-lspconfig (`vim.lsp.enable("gdscript")`), and install the `gdscript` treesitter parser.

Use Neovim for code. Use the Godot editor for scenes, nodes and the debugger.

## Hosting

The game runs in the homelab cluster. The world server is public at `wss://fantasy-rpg.fomiller.com/ws`. Instance servers are Agones GameServers in the `fantasy-rpg-instance` Fleet, each public at `/instances/<gameserver>/ws`. The world server asks `instance-manager` for one when a party walks into the cave.

- `Dockerfile` builds one image with every server. `publish-image.yml` pushes it to ECR on every merge to main.
- `helm/fantasy-rpg` is the chart. `publish-chart.yml` pushes it to ECR the same way.
- `infra/` creates both ECR repositories. `deploy-infra.yml` applies it.
- homelab's services ApplicationSet reads `argocd.yaml` and deploys `argocd/overlays/dev`.
- Kargo picks up each new image and chart and commits the new versions into `argocd/overlays/dev`.
- `argocd/agones` installs Agones into `agones-system`. `argocd/overlays/dev` includes it, so it syncs with the same Application.
- `argocd/redpanda` runs a one-broker Redpanda, its topics and the Console in `fantasy-rpg`. homelab's operator reconciles them. The Console is at redpanda-frpg-dev.fomiller.com, behind Cloudflare Access.
- The Cloudflare tunnel sends the hostname to Traefik. The chart's IngressRoute sends `/ws` to the world server. instance-manager makes a Service and an IngressRoute for each `/instances/<gameserver>/` prefix. Both are owned by the GameServer and go away with it.
- `inventory` owns characters, stashes and the run lease. It is the only workload with `DATABASE_URL`. The world and instance servers call it over HTTP at `INVENTORY_URL`. It produces `inventory.applied` from an outbox table.
- `bot-director` keeps a few bots playing in dev, in fill mode. `just bots-set` and `just bots-status` change and show it. See the Bots page in the docs.
- The world and instance servers read `JOIN_TOKEN_KEY` from the `fantasy-rpg-join-key` Secret. An ExternalSecret makes it from `dev/fomiller/fantasy-rpg-join-key` in AWS Secrets Manager, a JSON object with one key, `JOIN_TOKEN_KEY`.

- A buffer FleetAutoscaler keeps one Ready GameServer warm, from 1 to 8 in all. It owns the Fleet's replicas, so the chart never sets them.
- An instance ends itself once it is empty, and Agones replaces it. A deploy or scale-down only removes Ready GameServers. Allocated ones run until their runs end.
- On a node drain an instance gets SIGTERM and lets its runs play on for up to 600 s.

A cave lives in its pod's memory, so a crashed instance ends its runs. Players keep the gear they took in.

## Protocol

Client and servers talk in protobuf. The messages live in `proto/game/v1/game.proto`. Every WebSocket frame is one `ClientMessage` or one `ServerMessage`. Client commands (`MoveInput`, `AttackRequest`) and server results (`WorldSnapshot`, `DamageEvent`, `InventoryUpdated`) are kept apart. Never trust IDs or damage values sent by the client.

```sh
just proto            # lint, then generate Go and GDScript code
just proto-fixtures   # rewrite the test fixtures after a message change
```

- Go code goes to `gen/go/game/v1`, made by `buf` and `protoc-gen-go`.
- GDScript goes to `client/godot/gen/game_v1.gd`, made by [Godobuf](https://github.com/oniksan/godobuf), vendored in `client/godot/addons/godobuf`. Use it as `GameV1.ClientMessage` and so on.
- Both are committed. Run `just proto` after editing the `.proto` file.

Godobuf limits:

- Keep all messages in one file. Godobuf cannot resolve imports laid out the way buf requires.
- Enum values lose their prefix in GDScript: `ENTITY_KIND_PLAYER` in Go is `GameV1.EntityKind.PLAYER`.
- No default values in the `.proto` file.

`proto/testdata` holds bytes written by Go and read by GDScript, and the other way round. `just test` and `just client-test` both use them, so the two sides cannot drift apart without a test failing.

`movement.json` there is a walk through the test room recorded by the Go sim. The client's `Movement` script replays it, because the client predicts its own movement with the same math. Run `just proto-fixtures` after changing movement on either side.
