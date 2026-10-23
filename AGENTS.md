# AGENTS.md

Notes for anyone, human or agent, working in fantasy-rpg.

Written once by flake-hub and never touched again, so edit it freely.

## Generated files

Files with a `GENERATED FILE` header are written by `nix run .#generate` from
`repo.nix`. Editing one is pointless — the next run overwrites it. Change
`repo.nix` instead, or the template in the pack that owns the file.

## Before you push

```sh
nix run .#generate   # no output other than "0 change(s)" means no drift
```
