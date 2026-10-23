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
```

Controls: WASD or arrow keys to move, E to interact.

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
