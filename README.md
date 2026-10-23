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

## Godot client

The client is a 2D Godot 4.7 project in `client/godot`, written in GDScript. It uses the Compatibility renderer.

```sh
just client         # open the editor
just client-run     # run the game
just client-check   # import and run headless; prints "main scene loaded"
just client-test    # headless player tests: movement, walls, interact
just net-test       # start cave-server and check a real client can join and move
```

Controls: WASD or arrow keys to move, E to interact.

To play against the server, start it with `just cave`, then run `just client-run`. The client joins `ws://127.0.0.1:8080/ws`. The top-left label shows your player ID and the server tick. A white outline marks where the server thinks you are. Without a server the label says "offline", the game still runs, and the client retries every 2 seconds. Point at another server with `/Applications/Godot.app/Contents/MacOS/Godot --path client/godot -- --server=ws://host:port/ws`.

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
