{
  name = "fantasy-rpg";
  description = "Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.";

  language = "go";

  github = {
    codeowners = [ "@Fomiller" ];
    publishImage = true;
    publishChart = true;
  };

  # The instance-server is the image's entrypoint, since players connect to it
  # until FOM-228. The image holds world-server too.
  service = {
    binary = "instance-server";
    port = 8080;
  };

  # golden-service builds one binary per image, and the image holds both
  # servers, so the Dockerfile is written by hand.
  unmanaged = [ "Dockerfile" ];

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
    # Godot is not in the dev shell, so point at the installed app. Override with `just godot=<path> ...`.
    { name = "godot"; value = "/Applications/Godot.app/Contents/MacOS/Godot"; }
    # deploy-infra.yml runs `just infraDir=<dir> plan-all`, and setting this list
    # here replaces golden-infra's, so it is restated.
    { name = "infraDir"; value = "infra/live/dev"; }
    # The Compose Postgres from `just deps`.
    { name = "databaseUrl"; value = "postgres://fantasy:fantasy@localhost:5432/fantasy?sslmode=disable"; }
  ];

  # Setting this list here replaces the pack recipes, so build and test are restated.
  just.recipes = [
    { name = "build"; cmdFrom = "buildCmd"; }
    { name = "test"; cmdFrom = "testCmd"; }
    { name = "lint"; cmdFrom = "lintCmd"; }
    "plan-all:\n    doppler run --name-transformer tf-var -- \\\n    terragrunt stack run --tf-path terraform --working-dir {{infraDir}} plan"
    "apply-all:\n    doppler run --name-transformer tf-var -- \\\n    terragrunt --non-interactive stack run --tf-path terraform --working-dir {{infraDir}} apply"
    "fmt:\n    gofmt -w src"
    "deps:\n    {{compose}} up -d --wait postgres nats"
    # The world server on its own port, beside `just instance-db` on 8080 and the same database.
    "dev: migrate\n    PORT=8090 DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/world-server"
    "instance:\n    go run ./src/cmd/instance-server"
    "migrate: deps\n    DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/instance-server migrate"
    "instance-db: migrate\n    DATABASE_URL='{{databaseUrl}}' go run ./src/cmd/instance-server"
    "up:\n    {{compose}} --profile full up -d --build --wait"
    "down:\n    {{compose}} --profile full down"
    "images:\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=instance-server -t fantasy-rpg/instance-server:dev ."
    "k8s-up:\n    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false"
    "k8s-down:\n    k3d cluster delete fantasy-rpg"
    "deploy-local: k8s-up images\n    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev fantasy-rpg/instance-server:dev\n    {{kube}} apply -f deploy/kubernetes/world-server.yaml\n    {{kube}} -n fantasy-rpg rollout restart deployment/world-server\n    {{kube}} -n fantasy-rpg rollout status deployment/world-server"
    "instance-start id:\n    sed 's/INSTANCE_ID/{{id}}/g' deploy/kubernetes/instance-pod.yaml | {{kube}} apply -f -\n    {{kube}} -n fantasy-rpg wait --for=condition=Ready pod/instance-{{id}} --timeout=60s"
    "instance-stop id:\n    {{kube}} -n fantasy-rpg delete pod instance-{{id}}"
    "instances:\n    {{kube}} -n fantasy-rpg get pods -l app=instance-server"
    "client:\n    {{godot}} --path client/godot --editor"
    # Import first. A fresh checkout has no .godot/ class cache, and without it
    # every class_name in the scripts fails to resolve.
    "client-run:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot"
    "client-world:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot -- --server=ws://127.0.0.1:8090/ws"
    "client-online:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot -- --server=wss://fantasy-rpg.fomiller.com/ws"
    "client-check:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --quit-after 5"
    "client-test:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --script res://tests/player_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/proto_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/movement_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/enemy_view_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/sprites_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/abilities_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/facing_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/windows_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/loot_window_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/window_attack_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/character_screen_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/debug_overlay_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/progression_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/lobby_test.gd"
    "net-test: build\n    #!/usr/bin/env bash\n    set -euo pipefail\n    for port in 8080 8090; do if curl -sf localhost:$port/healthz >/dev/null; then echo \"port $port is in use. Stop the other server first.\" >&2; exit 1; fi; done\n    {{godot}} --headless --path client/godot --import\n    # Picked here, not by the server, so a failure can say how to replay it.\n    export LOOT_SEED=\"\${LOOT_SEED:-$(od -An -N8 -tu8 /dev/urandom | tr -d ' ')}\"\n    ./bin/instance-server & pid=$!\n    PORT=8090 ./bin/world-server & world=$!\n    trap 'status=$?; kill $pid $world 2>/dev/null || true; if [ $status -ne 0 ]; then echo \"net-test failed. Replay with: LOOT_SEED=$LOOT_SEED just net-test\" >&2; fi' EXIT\n    for port in 8080 8090; do for i in $(seq 50); do curl -sf localhost:$port/healthz >/dev/null && break; sleep 0.1; done; done\n    {{godot}} --headless --path client/godot --script res://tests/net_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/world_test.gd -- --server=ws://127.0.0.1:8090/ws"
    "proto:\n    buf lint\n    buf generate\n    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1"
    "proto-fixtures:\n    go test ./src/internal/protocol -run TestServerSnapshotFixture -update\n    go test ./src/internal/sim -run TestMovementFixture -update\n    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd"
  ];
}
