# ---------------------------------------------------------------------------
# GENERATED FILE — managed by flake-hub (golden-base).
# Do not edit manually: `nix run .#generate` overwrites it.
# To change it, edit repo.nix, or the template in the pack that owns it.
# ---------------------------------------------------------------------------

import? 'just/base.just'

project := "fantasy-rpg"
compose := "docker compose -f deploy/compose/compose.yaml"
kube := "kubectl --context k3d-fantasy-rpg"
homelab := "kubectl --context admin@homelab"
godot := "/Applications/Godot.app/Contents/MacOS/Godot"
infraDir := "infra/live/dev"
databaseUrl := "postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable"
kafkaBrokers := "localhost:19092"
natsUrl := "nats://localhost:4222"
topics := "player.session instance.join instance.lifecycle run.ended inventory.applied"

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
    {{compose}} up -d --wait postgres redpanda valkey nats
    {{compose}} exec -T redpanda rpk topic create --if-not-exists -X brokers=localhost:9092 {{topics}}

dev: migrate
    LOG_FORMAT=text JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' NATS_URL='{{natsUrl}}' VALKEY_ADDR=localhost:6379 INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' go run ./src/cmd/world-server

instance id port:
    LOG_FORMAT=text JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' NATS_URL='{{natsUrl}}' INSTANCE_ID={{id}} PORT={{port}} go run ./src/cmd/instance-server

bots n:
    LOG_FORMAT=text go run ./src/cmd/bot -url ws://127.0.0.1:8080/ws -n {{n}} -metrics-addr :9091

bots-set mode count='0':
    {{homelab}} -n fantasy-rpg exec deploy/fantasy-rpg-bot-director -- /bot-director set {{mode}} {{count}}

bots-status:
    {{homelab}} -n fantasy-rpg exec deploy/fantasy-rpg-bot-director -- /bot-director status

loadtest pods bots duration='10m' ramp='2' via='http://traefik.traefik.svc.cluster.local' tag='':
    #!/usr/bin/env bash
    set -euo pipefail
    if {{homelab}} -n fantasy-rpg get job fantasy-rpg-bots >/dev/null 2>&1; then echo "A load test is still there. Run just loadtest-down first." >&2; exit 1; fi
    tag='{{tag}}'
    [ -n "$tag" ] || tag=$(awk '$1 == "tag:" {print $2}' argocd/overlays/dev/values.app.yaml | cut -d@ -f1)
    # Beside the base, since kustomize takes no absolute path to it.
    dir=$(mktemp -d deploy/kubernetes/loadtest-run.XXXXXX)
    trap 'rm -rf "$dir"' EXIT
    cat > "$dir/kustomization.yaml" <<EOF
    resources:
      - ../loadtest
    images:
      - name: 695434033664.dkr.ecr.us-east-1.amazonaws.com/fantasy-rpg
        newTag: "$tag"
    patches:
      - target: {kind: Job, name: fantasy-rpg-bots}
        patch: |
          - {op: replace, path: /spec/completions, value: {{pods}}}
          - {op: replace, path: /spec/parallelism, value: {{pods}}}
          - {op: replace, path: /spec/template/spec/containers/0/env, value: [{name: URL, value: "wss://fantasy-rpg.fomiller.com/ws"}, {name: VIA, value: "{{via}}"}, {name: BOTS, value: "{{bots}}"}, {name: RAMP, value: "{{ramp}}"}, {name: DURATION, value: "{{duration}}"}, {name: LOG_LEVEL, value: info}, {name: LOG_FORMAT, value: json}]}
    EOF
    echo "{{pods}} pods x {{bots}} bots, image $tag, via '{{via}}', for {{duration}}"
    kubectl kustomize "$dir" | {{homelab}} apply -f -

loadtest-scale pods:
    {{homelab}} -n fantasy-rpg patch job fantasy-rpg-bots --type merge -p '{"spec":{"completions":{{pods}},"parallelism":{{pods}}}}'

loadtest-status:
    {{homelab}} -n fantasy-rpg get job,pods -l app.kubernetes.io/component=bot

loadtest-down:
    {{homelab}} delete -k deploy/kubernetes/loadtest --ignore-not-found

inventory: migrate
    LOG_FORMAT=text DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' PORT=8070 go run ./src/cmd/inventory

migrate: deps
    LOG_FORMAT=text DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/instance-server migrate

up: deps
    {{compose}} --profile full up -d --build --wait

down:
    {{compose}} --profile full down

images:
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-server -t fantasy-rpg/instance-server:dev .
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-manager -t fantasy-rpg/instance-manager:dev .
    docker build -f deploy/docker/Dockerfile --build-arg BINARY=inventory -t fantasy-rpg/inventory:dev .

k8s-up:
    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false

k8s-down:
    k3d cluster delete fantasy-rpg

deploy-local: k8s-up images
    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev
    {{kube}} apply -f deploy/kubernetes/world-server.yaml
    {{kube}} -n fantasy-rpg rollout restart deployment/world-server
    {{kube}} -n fantasy-rpg rollout status deployment/world-server

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
    {{godot}} --headless --path client/godot --script res://tests/chat_test.gd
    {{godot}} --headless --path client/godot --script res://tests/settings_test.gd

net-test: build
    #!/usr/bin/env bash
    set -euo pipefail
    ports="8070 8081 8082 8083 8080 8090"
    for port in $ports; do if curl -sf localhost:$port/healthz >/dev/null; then echo "port $port is in use. Stop the other server first." >&2; exit 1; fi; done
    {{godot}} --headless --path client/godot --import
    # Picked here, not by the server, so a failure can say how to replay it.
    export LOOT_SEED="${LOOT_SEED:-$(od -An -N8 -tu8 /dev/urandom | tr -d ' ')}"
    # A throwaway Postgres, so the servers share a store and nothing from an earlier run or `just dev` leaks in.
    pg=$(docker run -d --rm -e POSTGRES_USER=fantasy -e POSTGRES_PASSWORD=fantasy -e POSTGRES_DB=fantasy -p 127.0.0.1::5432 postgres:18)
    pids=() rp="" nt=""
    trap 'status=$?; kill "${pids[@]}" 2>/dev/null || true; wait 2>/dev/null || true; docker rm -f "$pg" $rp $nt >/dev/null 2>&1 || true; if [ $status -ne 0 ]; then echo "net-test failed. Replay with: LOOT_SEED=$LOOT_SEED just net-test" >&2; fi' EXIT
    export DATABASE_URL="postgres://fantasy:fantasy@127.0.0.1:$(docker port "$pg" 5432/tcp | head -1 | cut -d: -f2)/fantasy?sslmode=disable" JOIN_TOKEN_KEY=net-test LOG_FORMAT=text
    # And a throwaway Redpanda. Its port is fixed, since the broker hands its address to clients.
    rp=$(docker run -d --rm -p 127.0.0.1:29092:29092 redpandadata/redpanda:v26.2.6 redpanda start --mode dev-container --smp 1 --kafka-addr 0.0.0.0:29092 --advertise-kafka-addr 127.0.0.1:29092)
    export KAFKA_BROKERS=127.0.0.1:29092
    # And a throwaway NATS, so chat crosses the servers as it does in the cluster.
    nt=$(docker run -d --rm -p 127.0.0.1:24222:4222 nats:2.15.2-alpine)
    export NATS_URL=nats://127.0.0.1:24222
    for i in $(seq 50); do docker exec "$rp" rpk topic create --if-not-exists -X brokers=$KAFKA_BROKERS {{topics}} >/dev/null 2>&1 && break; if [ "$i" = 50 ]; then echo "Redpanda did not start." >&2; exit 1; fi; sleep 0.2; done
    ./bin/instance-server migrate
    PORT=8070 ./bin/inventory & pids+=($!)
    for i in $(seq 50); do curl -sf localhost:8070/healthz >/dev/null && break; sleep 0.1; done
    # Only inventory opens the database, as in the cluster.
    export INVENTORY_URL=http://localhost:8070
    INSTANCE_ID=instance-0 PORT=8081 ./bin/instance-server & pids+=($!)
    INSTANCE_ID=instance-1 PORT=8082 ./bin/instance-server & pids+=($!)
    INSTANCE_ID=instance-2 PORT=8083 CAVE=yard ./bin/instance-server & pids+=($!)
    # Instances first, so each world's first poll finds them up.
    for port in 8081 8082 8083; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done
    WORLD_ID=world-0 INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' ./bin/world-server & pids+=($!)
    # A second world whose one instance is the yard, so world_test can loot and extract without a fight.
    WORLD_ID=world-1 PORT=8090 INSTANCES='instance-2=http://localhost:8083=ws://127.0.0.1:8083/instances/instance-2/ws' ./bin/world-server & pids+=($!)
    for port in 8080 8090; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done
    {{godot}} --headless --path client/godot --script res://tests/net_test.gd
    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd
    {{godot}} --headless --path client/godot --script res://tests/world_test.gd -- --server=ws://127.0.0.1:8090/ws
    # Every test above walks from the town into a cave, and world_test ends a run.
    for topic in player.session instance.join run.ended inventory.applied; do n=$(docker exec "$rp" rpk topic describe -p "$topic" -X brokers=$KAFKA_BROKERS | awk 'NR>1 {s+=$NF} END {print s+0}'); if [ "$n" = 0 ]; then echo "No $topic events." >&2; exit 1; fi; done
    # The run's loot reached the stash through the inventory consumer, not a direct write.
    curl -sf localhost:8070/metrics | grep -q '^inventory_consumer_lag{partition="0",topic="run.ended"} 0$' || { echo "inventory never consumed run.ended." >&2; exit 1; }

proto:
    buf lint
    buf generate
    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1

proto-fixtures:
    go test ./src/internal/protocol -run TestServerSnapshotFixture -update
    go test ./src/internal/sim -run TestMovementFixture -update
    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd
