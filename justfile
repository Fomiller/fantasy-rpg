# ---------------------------------------------------------------------------
# GENERATED FILE — managed by flake-hub (golden-base).
# Do not edit manually: `nix run .#generate` overwrites it.
# To change it, edit repo.nix, or the template in the pack that owns it.
# ---------------------------------------------------------------------------

import? 'just/base.just'

project := "fantasy-rpg"

fetch:
    curl -sSfL https://raw.githubusercontent.com/Fomiller/justfiles/refs/heads/main/base.just > just/base.just

generate:
    nix run .#generate

build:
    go build -o bin/ ./src/...

test:
    go test ./src/... -race -cover

lint:
    go vet ./src/...

fmt:
    gofmt -w src

dev:
    go run ./src/cmd/world-server
