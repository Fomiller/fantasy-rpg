# ---------------------------------------------------------------------------
# GENERATED FILE — managed by flake-hub (golden-base).
# Do not edit manually: `nix run .#generate` overwrites it.
# To change it, edit repo.nix, or the template in the pack that owns it.
# ---------------------------------------------------------------------------

import? 'just/base.just'

project := "fantasy-rpg"
compose := "docker compose -f deploy/compose/compose.yaml"
kube := "kubectl --context k3d-fantasy-rpg"
godot := "/Applications/Godot.app/Contents/MacOS/Godot"
infraDir := "infra/live/dev"

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

plan-all:
    doppler run --name-transformer tf-var -- \
    terragrunt stack run --tf-path terraform --working-dir {{infraDir}} plan

apply-all:
    doppler run --name-transformer tf-var -- \
    terragrunt --non-interactive stack run --tf-path terraform --working-dir {{infraDir}} apply

fmt:
    gofmt -w src

deps:
    {{compose}} up -d --wait postgres nats

dev: deps
    go run ./src/cmd/world-server

cave:
    go run ./src/cmd/cave-server

up:
    {{compose}} --profile full up -d --build --wait

down:
    {{compose}} --profile full down

images:
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=cave-server -t fantasy-rpg/cave-server:dev .

k8s-up:
    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false

k8s-down:
    k3d cluster delete fantasy-rpg

deploy-local: k8s-up images
    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev fantasy-rpg/cave-server:dev
    {{kube}} apply -f deploy/kubernetes/world-server.yaml
    {{kube}} -n fantasy-rpg rollout restart deployment/world-server
    {{kube}} -n fantasy-rpg rollout status deployment/world-server

cave-start id:
    sed 's/CAVE_ID/{{id}}/g' deploy/kubernetes/cave-pod.yaml | {{kube}} apply -f -
    {{kube}} -n fantasy-rpg wait --for=condition=Ready pod/cave-{{id}} --timeout=60s

cave-stop id:
    {{kube}} -n fantasy-rpg delete pod cave-{{id}}

caves:
    {{kube}} -n fantasy-rpg get pods -l app=cave-server

client:
    {{godot}} --path client/godot --editor

client-run:
    {{godot}} --headless --path client/godot --import
    {{godot}} --path client/godot

client-online:
    {{godot}} --headless --path client/godot --import
    {{godot}} --path client/godot -- --server=wss://fantasy-rpg.fomiller.com/ws

client-check:
    {{godot}} --headless --path client/godot --import
    {{godot}} --headless --path client/godot --quit-after 5

client-test:
    {{godot}} --headless --path client/godot --import
    {{godot}} --headless --path client/godot --script res://tests/player_test.gd
    {{godot}} --headless --path client/godot --script res://tests/proto_test.gd
    {{godot}} --headless --path client/godot --script res://tests/movement_test.gd
    {{godot}} --headless --path client/godot --script res://tests/enemy_view_test.gd
    {{godot}} --headless --path client/godot --script res://tests/sprites_test.gd

net-test: build
    #!/usr/bin/env bash
    set -euo pipefail
    if curl -sf localhost:8080/healthz >/dev/null; then echo "port 8080 is in use. Stop the other cave-server first." >&2; exit 1; fi
    ./bin/cave-server & pid=$!
    trap 'kill $pid' EXIT
    for i in $(seq 50); do curl -sf localhost:8080/healthz >/dev/null && break; sleep 0.1; done
    {{godot}} --headless --path client/godot --script res://tests/net_test.gd
    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd

proto:
    buf lint
    buf generate
    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1

proto-fixtures:
    go test ./src/internal/protocol -run TestServerSnapshotFixture -update
    go test ./src/internal/sim -run TestMovementFixture -update
    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd
