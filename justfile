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
databaseUrl := "postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable"

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

dev: migrate
    JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' go run ./src/cmd/world-server

instance id port:
    JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' INSTANCE_ID={{id}} PORT={{port}} go run ./src/cmd/instance-server

migrate: deps
    DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/instance-server migrate

up:
    {{compose}} --profile full up -d --build --wait

down:
    {{compose}} --profile full down

images:
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-server -t fantasy-rpg/instance-server:dev .

k8s-up:
    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false

k8s-down:
    k3d cluster delete fantasy-rpg

deploy-local: k8s-up images
    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev fantasy-rpg/instance-server:dev
    {{kube}} apply -f deploy/kubernetes/world-server.yaml
    {{kube}} -n fantasy-rpg rollout restart deployment/world-server
    {{kube}} -n fantasy-rpg rollout status deployment/world-server

instance-start id:
    sed 's/INSTANCE_ID/{{id}}/g' deploy/kubernetes/instance-pod.yaml | {{kube}} apply -f -
    {{kube}} -n fantasy-rpg wait --for=condition=Ready pod/instance-{{id}} --timeout=60s

instance-stop id:
    {{kube}} -n fantasy-rpg delete pod instance-{{id}}

instances:
    {{kube}} -n fantasy-rpg get pods -l app=instance-server

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
    {{godot}} --headless --path client/godot --script res://tests/abilities_test.gd
    {{godot}} --headless --path client/godot --script res://tests/facing_test.gd
    {{godot}} --headless --path client/godot --script res://tests/windows_test.gd
    {{godot}} --headless --path client/godot --script res://tests/loot_window_test.gd
    {{godot}} --headless --path client/godot --script res://tests/window_attack_test.gd
    {{godot}} --headless --path client/godot --script res://tests/character_screen_test.gd
    {{godot}} --headless --path client/godot --script res://tests/debug_overlay_test.gd
    {{godot}} --headless --path client/godot --script res://tests/progression_test.gd
    {{godot}} --headless --path client/godot --script res://tests/town_test.gd

net-test: build
    #!/usr/bin/env bash
    set -euo pipefail
    ports="8081 8082 8083 8080 8090"
    for port in $ports; do if curl -sf localhost:$port/healthz >/dev/null; then echo "port $port is in use. Stop the other server first." >&2; exit 1; fi; done
    {{godot}} --headless --path client/godot --import
    # Picked here, not by the server, so a failure can say how to replay it.
    export LOOT_SEED="${LOOT_SEED:-$(od -An -N8 -tu8 /dev/urandom | tr -d ' ')}"
    # A throwaway Postgres, so the servers share a store and nothing from an earlier run or `just dev` leaks in.
    pg=$(docker run -d --rm -e POSTGRES_USER=fantasy -e POSTGRES_PASSWORD=fantasy -e POSTGRES_DB=fantasy -p 127.0.0.1::5432 postgres:18)
    pids=()
    trap 'status=$?; kill "${pids[@]}" 2>/dev/null || true; wait 2>/dev/null || true; docker rm -f "$pg" >/dev/null 2>&1 || true; if [ $status -ne 0 ]; then echo "net-test failed. Replay with: LOOT_SEED=$LOOT_SEED just net-test" >&2; fi' EXIT
    export DATABASE_URL="postgres://fantasy:fantasy@127.0.0.1:$(docker port "$pg" 5432/tcp | head -1 | cut -d: -f2)/fantasy?sslmode=disable" JOIN_TOKEN_KEY=net-test
    ./bin/instance-server migrate
    INSTANCE_ID=instance-0 PORT=8081 ./bin/instance-server & pids+=($!)
    INSTANCE_ID=instance-1 PORT=8082 ./bin/instance-server & pids+=($!)
    INSTANCE_ID=instance-2 PORT=8083 CAVE=yard ./bin/instance-server & pids+=($!)
    # Instances first, so each world's first poll finds them up.
    for port in 8081 8082 8083; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done
    INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' ./bin/world-server & pids+=($!)
    # A second world whose one instance is the yard, so world_test can loot and extract without a fight.
    PORT=8090 INSTANCES='instance-2=http://localhost:8083=ws://127.0.0.1:8083/instances/instance-2/ws' ./bin/world-server & pids+=($!)
    for port in 8080 8090; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done
    {{godot}} --headless --path client/godot --script res://tests/net_test.gd
    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd
    {{godot}} --headless --path client/godot --script res://tests/world_test.gd -- --server=ws://127.0.0.1:8090/ws

proto:
    buf lint
    buf generate
    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1

proto-fixtures:
    go test ./src/internal/protocol -run TestServerSnapshotFixture -update
    go test ./src/internal/sim -run TestMovementFixture -update
    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd
