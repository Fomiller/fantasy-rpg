{
  name = "fantasy-rpg";
  description = "Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.";

  language = "go";

  github = {
    codeowners = [ "@Fomiller" ];
    publishImage = true;
    publishChart = true;
  };

  # The deployed image is the cave-server, since that is what players connect
  # to. world-server is still only a health check.
  service = {
    binary = "cave-server";
    port = 8080;
  };

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
    "dev: deps\n    go run ./src/cmd/world-server"
    "cave:\n    go run ./src/cmd/cave-server"
    "up:\n    {{compose}} --profile full up -d --build --wait"
    "down:\n    {{compose}} --profile full down"
    "images:\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=cave-server -t fantasy-rpg/cave-server:dev ."
    "k8s-up:\n    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false"
    "k8s-down:\n    k3d cluster delete fantasy-rpg"
    "deploy-local: k8s-up images\n    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev fantasy-rpg/cave-server:dev\n    {{kube}} apply -f deploy/kubernetes/world-server.yaml\n    {{kube}} -n fantasy-rpg rollout restart deployment/world-server\n    {{kube}} -n fantasy-rpg rollout status deployment/world-server"
    "cave-start id:\n    sed 's/CAVE_ID/{{id}}/g' deploy/kubernetes/cave-pod.yaml | {{kube}} apply -f -\n    {{kube}} -n fantasy-rpg wait --for=condition=Ready pod/cave-{{id}} --timeout=60s"
    "cave-stop id:\n    {{kube}} -n fantasy-rpg delete pod cave-{{id}}"
    "caves:\n    {{kube}} -n fantasy-rpg get pods -l app=cave-server"
    "client:\n    {{godot}} --path client/godot --editor"
    # Import first. A fresh checkout has no .godot/ class cache, and without it
    # every class_name in the scripts fails to resolve.
    "client-run:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot"
    "client-online:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --path client/godot -- --server=wss://fantasy-rpg.fomiller.com/ws"
    "client-check:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --quit-after 5"
    "client-test:\n    {{godot}} --headless --path client/godot --import\n    {{godot}} --headless --path client/godot --script res://tests/player_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/proto_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/movement_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/enemy_view_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/sprites_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/abilities_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/facing_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/windows_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/loot_window_test.gd"
    "net-test: build\n    #!/usr/bin/env bash\n    set -euo pipefail\n    if curl -sf localhost:8080/healthz >/dev/null; then echo \"port 8080 is in use. Stop the other cave-server first.\" >&2; exit 1; fi\n    ./bin/cave-server & pid=$!\n    trap 'kill $pid' EXIT\n    for i in $(seq 50); do curl -sf localhost:8080/healthz >/dev/null && break; sleep 0.1; done\n    {{godot}} --headless --path client/godot --script res://tests/net_test.gd\n    {{godot}} --headless --path client/godot --script res://tests/two_clients_test.gd"
    "proto:\n    buf lint\n    buf generate\n    {{godot}} --headless --path client/godot -s res://addons/godobuf/godobuf_cmdln.gd --input={{justfile_directory()}}/proto/game/v1/game.proto --output={{justfile_directory()}}/client/godot/gen/game_v1.gd --class_name=GameV1"
    "proto-fixtures:\n    go test ./src/internal/protocol -run TestServerSnapshotFixture -update\n    go test ./src/internal/sim -run TestMovementFixture -update\n    UPDATE=1 {{godot}} --headless --path client/godot --script res://tests/proto_test.gd"
  ];
}
