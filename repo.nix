{
  name = "fantasy-rpg";
  description = "Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.";

  language = "go";

  github = {
    codeowners = [ "@Fomiller" ];
    publishImage = true;
    publishChart = true;
  };

  # The instance-server is the image's entrypoint, which the migrate Job runs.
  # The image holds world-server, instance-manager and inventory too, picked
  # with command.
  service = {
    binary = "instance-server";
    port = 8080;
  };

  # golden-service builds one binary per image, and the image holds every
  # service, so the Dockerfile is written by hand. The pack's Deployment and
  # Service are listed so generate does not bring them back: instance servers
  # are an Agones Fleet, and instance-manager routes each GameServer.
  unmanaged = [
    "Dockerfile"
    "helm/fantasy-rpg/templates/deployment.yaml"
    "helm/fantasy-rpg/templates/service.yaml"
  ];

  infra = {
    dopplerProject = "fantasy-rpg";
    ownerEmail = "forrestmillerj@gmail.com";
  };

  argocd = {
    environment = "dev";
  };

  docs = {
    authors = [ "Forrest Miller" ];
    repoUrl = "https://github.com/Fomiller/fantasy-rpg";
  };

  just.variables = [
    { name = "compose"; value = "docker compose -f deploy/compose/compose.yaml"; }
    # Pinned so a recipe can never reach the homelab cluster by accident.
    { name = "kube"; value = "kubectl --context k3d-fantasy-rpg"; }
    # The real cluster, for the load test and the bot director only. Named so it is never the default.
    { name = "homelab"; value = "kubectl --context admin@homelab"; }
    # Godot is not in the dev shell, so point at the installed app. Override with `just godot=<path> ...`.
    { name = "godot"; value = "/Applications/Godot.app/Contents/MacOS/Godot"; }
    # deploy-infra.yml runs `just infraDir=<dir> plan-all`, and setting this list
    # here replaces golden-infra's, so it is restated.
    { name = "infraDir"; value = "infra/live/dev"; }
    # The Compose Postgres from `just deps`.
    { name = "databaseUrl"; value = "postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable"; }
    # The Compose Redpanda's listener for the host.
    { name = "kafkaBrokers"; value = "localhost:19092"; }
    # The topics in argocd/redpanda/topics.yaml. Nothing auto-creates them, here or in the cluster.
    { name = "topics"; value = "player.session instance.join instance.lifecycle run.ended inventory.applied"; }
  ];

  # Setting this list here replaces the pack recipes, so build and test are restated.
  just.recipes = [
    { name = "build"; cmdFrom = "buildCmd"; }
    { name = "test"; cmdFrom = "testCmd"; }
    { name = "lint"; cmdFrom = "lintCmd"; }
    "plan-all:\n    doppler run --name-transformer tf-var -- \\\n    terragrunt stack run --tf-path terraform --working-dir {{infraDir}} plan"
    "apply-all:\n    doppler run --name-transformer tf-var -- \\\n    terragrunt --non-interactive stack run --tf-path terraform --working-dir {{infraDir}} apply"
    "fmt:\n    gofmt -w src"
    "deps:\n    {{compose}} up -d --wait postgres redpanda\n    {{compose}} exec -T redpanda rpk topic create --if-not-exists -X brokers=localhost:9092 {{topics}}"
    # The world server on 8080, where the client connects. Run `just instance instance-0 8081`
    # and `just instance instance-1 8082` in two more terminals. All three share the Compose database.
    # To go through the inventory service instead, run `just inventory` and start the others with
    # INVENTORY_URL=http://localhost:8070 in their environment.
    "dev: migrate\n    LOG_FORMAT=text JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' go run ./src/cmd/world-server"
    "instance id port:\n    LOG_FORMAT=text JOIN_TOKEN_KEY=dev-only DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' INSTANCE_ID={{id}} PORT={{port}} go run ./src/cmd/instance-server"
    "bots n:\n    LOG_FORMAT=text go run ./src/cmd/bot -url ws://127.0.0.1:8080/ws -n {{n}} -metrics-addr :9091"
    # The bot director in the homelab cluster. Its API is ClusterIP only, so these exec its own
    # client, which calls localhost. mode is off, fill or fixed. fill keeps humans plus bots at
    # count, fixed runs count bots. The setting lives in a ConfigMap and survives a restart.
    "bots-set mode count='0':\n    {{homelab}} -n fantasy-rpg exec deploy/fantasy-rpg-bot-director -- /bot-director set {{mode}} {{count}}"
    "bots-status:\n    {{homelab}} -n fantasy-rpg exec deploy/fantasy-rpg-bot-director -- /bot-director status"
    # The bot Job in the homelab cluster, `pods` pods of `bots` bots each. The tag defaults to the one
    # Kargo last promoted to dev. An empty via goes through the public host and the tunnel.
    "loadtest pods bots duration='10m' ramp='2' via='http://traefik.traefik.svc.cluster.local' tag='':\n    #!/usr/bin/env bash\n    set -euo pipefail\n    if {{homelab}} -n fantasy-rpg get job fantasy-rpg-bots >/dev/null 2>&1; then echo \"A load test is still there. Run just loadtest-down first.\" >&2; exit 1; fi\n    tag='{{tag}}'\n    [ -n \"$tag\" ] || tag=$(awk '$1 == \"tag:\" {print $2}' argocd/overlays/dev/values.app.yaml | cut -d@ -f1)\n    # Beside the base, since kustomize takes no absolute path to it.\n    dir=$(mktemp -d deploy/kubernetes/loadtest-run.XXXXXX)\n    trap 'rm -rf \"$dir\"' EXIT\n    cat > \"$dir/kustomization.yaml\" <<EOF\n    resources:\n      - ../loadtest\n    images:\n      - name: 695434033664.dkr.ecr.us-east-1.amazonaws.com/fantasy-rpg\n        newTag: \"$tag\"\n    patches:\n      - target: {kind: Job, name: fantasy-rpg-bots}\n        patch: |\n          - {op: replace, path: /spec/completions, value: {{pods}}}\n          - {op: replace, path: /spec/parallelism, value: {{pods}}}\n          - {op: replace, path: /spec/template/spec/containers/0/env, value: [{name: URL, value: \"wss://fantasy-rpg.fomiller.com/ws\"}, {name: VIA, value: \"{{via}}\"}, {name: BOTS, value: \"{{bots}}\"}, {name: RAMP, value: \"{{ramp}}\"}, {name: DURATION, value: \"{{duration}}\"}, {name: LOG_LEVEL, value: info}, {name: LOG_FORMAT, value: json}]}\n    EOF\n    echo \"{{pods}} pods x {{bots}} bots, image $tag, via '{{via}}', for {{duration}}\"\n    kubectl kustomize \"$dir\" | {{homelab}} apply -f -"
    # Adds or removes pods in a running test. New pods start their own -duration when they start.
    "loadtest-scale pods:\n    {{homelab}} -n fantasy-rpg patch job fantasy-rpg-bots --type merge -p '{\"spec\":{\"completions\":{{pods}},\"parallelism\":{{pods}}}}'"
    "loadtest-status:\n    {{homelab}} -n fantasy-rpg get job,pods -l app.kubernetes.io/component=bot"
    "loadtest-down:\n    {{homelab}} delete -k deploy/kubernetes/loadtest --ignore-not-found"
    "inventory: migrate\n    LOG_FORMAT=text DATABASE_URL='{{databaseUrl}}' KAFKA_BROKERS='{{kafkaBrokers}}' PORT=8070 go run ./src/cmd/inventory"
    "migrate: deps\n    LOG_FORMAT=text DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/instance-server migrate"
    "up: deps\n    {{compose}} --profile full up -d --build --wait"
    "down:\n    {{compose}} --profile full down"
    "images:\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-server -t fantasy-rpg/instance-server:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-manager -t fantasy-rpg/instance-manager:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=inventory -t fantasy-rpg/inventory:dev ."
    "k8s-up:\n    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false"
    "k8s-down:\n    k3d cluster delete fantasy-rpg"
    # Only the world server. Instances need Agones, which the local cluster does not run.
    "deploy-local: k8s-up images\n    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev\n    {{kube}} apply -f deploy/kubernetes/world-server.yaml\n    {{kube}} -n fantasy-rpg rollout restart deployment/world-server\n    {{kube}} -n fantasy-rpg rollout status deployment/world-server"
    "client:\n    {{godot}} --path client/godot --editor"
    # Import first. A fresh checkout has no .godot/ class cache, and without it
    # every class_name in the scripts fails to resolve.
    "client-run:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot"
    "client-online:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot -- --server=wss://fantasy-rpg.fomiller.com/ws"
    "client-check:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --quit-after 5"
    "client-test:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --script res://tests/player_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/proto_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/movement_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/enemy_view_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/sprites_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/abilities_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/facing_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/windows_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/loot_window_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/window_attack_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/character_screen_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/debug_overlay_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/progression_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/town_test.gd"
    "net-test: build\n    #!/usr/bin/env bash\n    set -euo pipefail\n    ports=\"8070 8081 8082 8083 8080 8090\"\n    for port in $ports; do if curl -sf localhost:$port/healthz >/dev/null; then echo \"port $port is in use. Stop the other server first.\" >&2; exit 1; fi; done\n    {{godot}} --headless --path client/godot --import\n    # Picked here, not by the server, so a failure can say how to replay it.\n    export LOOT_SEED=\"\${LOOT_SEED:-$(od -An -N8 -tu8 /dev/urandom | tr -d ' ')}\"\n    # A throwaway Postgres, so the servers share a store and nothing from an earlier run or `just dev` leaks in.\n    pg=$(docker run -d --rm -e POSTGRES_USER=fantasy -e POSTGRES_PASSWORD=fantasy -e POSTGRES_DB=fantasy -p 127.0.0.1::5432 postgres:18)\n    pids=() rp=\"\"\n    trap 'status=$?; kill \"\${pids[@]}\" 2>/dev/null || true; wait 2>/dev/null || true; docker rm -f \"$pg\" $rp >/dev/null 2>&1 || true; if [ $status -ne 0 ]; then echo \"net-test failed. Replay with: LOOT_SEED=$LOOT_SEED just net-test\" >&2; fi' EXIT\n    export DATABASE_URL=\"postgres://fantasy:fantasy@127.0.0.1:$(docker port \"$pg\" 5432/tcp | head -1 | cut -d: -f2)/fantasy?sslmode=disable\" JOIN_TOKEN_KEY=net-test LOG_FORMAT=text\n    # And a throwaway Redpanda. Its port is fixed, since the broker hands its address to clients.\n    rp=$(docker run -d --rm -p 127.0.0.1:29092:29092 redpandadata/redpanda:v26.2.6 redpanda start --mode dev-container --smp 1 --kafka-addr 0.0.0.0:29092 --advertise-kafka-addr 127.0.0.1:29092)\n    export KAFKA_BROKERS=127.0.0.1:29092\n    for i in $(seq 50); do docker exec \"$rp\" rpk topic create --if-not-exists -X brokers=$KAFKA_BROKERS {{topics}} >/dev/null 2>&1 && break; if [ \"$i\" = 50 ]; then echo \"Redpanda did not start.\" >&2; exit 1; fi; sleep 0.2; done\n    ./bin/instance-server migrate\n    PORT=8070 ./bin/inventory & pids+=($!)\n    for i in $(seq 50); do curl -sf localhost:8070/healthz >/dev/null && break; sleep 0.1; done\n    # Only inventory opens the database, as in the cluster.\n    export INVENTORY_URL=http://localhost:8070\n    INSTANCE_ID=instance-0 PORT=8081 ./bin/instance-server & pids+=($!)\n    INSTANCE_ID=instance-1 PORT=8082 ./bin/instance-server & pids+=($!)\n    INSTANCE_ID=instance-2 PORT=8083 CAVE=yard ./bin/instance-server & pids+=($!)\n    # Instances first, so each world's first poll finds them up.\n    for port in 8081 8082 8083; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done\n    WORLD_ID=world-0 INSTANCES='instance-0=http://localhost:8081=ws://127.0.0.1:8081/instances/instance-0/ws instance-1=http://localhost:8082=ws://127.0.0.1:8082/instances/instance-1/ws' ./bin/world-server & pids+=($!)\n    # A second world whose one instance is the yard, so world_test can loot and extract without a fight.\n    WORLD_ID=world-1 PORT=8090 INSTANCES='instance-2=http://localhost:8083=ws://127.0.0.1:8083/instances/instance-2/ws' ./bin/world-server & pids+=($!)\n    for port in 8080 8090; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done\n    {{godot}} --headless --path client/godot --script res://tests/net_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/world_test.gd -- --server=ws://127.0.0.1:8090/ws\n    # Every test above walks from the town into a cave, and world_test ends a run.\n    for topic in player.session instance.join run.ended inventory.applied; do n=$(docker exec \"$rp\" rpk topic describe -p \"$topic\" -X brokers=$KAFKA_BROKERS | awk 'NR>1 {s+=$NF} END {print s+0}'); if [ \"$n\" = 0 ]; then echo \"No $topic events.\" >&2; exit 1; fi; done\n    # The run's loot reached the stash through the inventory consumer, not a direct write.\n    curl -sf localhost:8070/metrics | grep -q '^inventory_consumer_lag{partition=\"0\",topic=\"run.ended\"} 0$' || { echo \"inventory never consumed run.ended.\" >&2; exit 1; }"
    "proto:\n    buf lint\n    buf generate\n    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1"
    "proto-fixtures:\n    go test ./src/internal/protocol -run TestServerSnapshotFixture -update\n    go test ./src/internal/sim -run TestMovementFixture -update\n    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd"
  ];
}
