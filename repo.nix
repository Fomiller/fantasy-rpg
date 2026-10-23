{
  name = "fantasy-rpg";
  description = "Multiplayer 2D fantasy cave-crawl loot extractor. Godot client, Go authoritative servers, homelab Kubernetes.";

  language = "go";

  github.codeowners = [ "@Fomiller" ];

  service = {
    binary = "world-server";
    port = 8080;
  };

  docs = {
    authors = [ "Forrest Miller" ];
    repoUrl = "https://github.com/Fomiller/fantasy-rpg";
  };

  just.variables = [
    { name = "compose"; value = "docker compose -f deploy/compose/compose.yaml"; }
    # Pinned so a recipe can never reach the homelab cluster by accident.
    { name = "kube"; value = "kubectl --context k3d-fantasy-rpg"; }
  ];

  # Setting this list here replaces the pack recipes, so build and test are restated.
  just.recipes = [
    { name = "build"; cmdFrom = "buildCmd"; }
    { name = "test"; cmdFrom = "testCmd"; }
    { name = "lint"; cmdFrom = "lintCmd"; }
    "fmt:\n    gofmt -w src"
    "deps:\n    {{compose}} up -d --wait postgres nats"
    "dev: deps\n    go run ./src/cmd/world-server"
    "up:\n    {{compose}} --profile full up -d --build --wait"
    "down:\n    {{compose}} --profile full down"
    "images:\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=world-server -t fantasy-rpg/world-server:dev .\n    docker build -f deploy/docker/Dockerfile --build-arg BINARY=cave-server -t fantasy-rpg/cave-server:dev ."
    "k8s-up:\n    k3d cluster list fantasy-rpg >/dev/null 2>&1 || k3d cluster create fantasy-rpg --wait --kubeconfig-switch-context=false"
    "k8s-down:\n    k3d cluster delete fantasy-rpg"
    "deploy-local: k8s-up images\n    k3d image import -c fantasy-rpg fantasy-rpg/world-server:dev fantasy-rpg/cave-server:dev\n    {{kube}} apply -f deploy/kubernetes/world-server.yaml\n    {{kube}} -n fantasy-rpg rollout restart deployment/world-server\n    {{kube}} -n fantasy-rpg rollout status deployment/world-server"
    "cave-start id:\n    sed 's/CAVE_ID/{{id}}/g' deploy/kubernetes/cave-pod.yaml | {{kube}} apply -f -\n    {{kube}} -n fantasy-rpg wait --for=condition=Ready pod/cave-{{id}} --timeout=60s"
    "cave-stop id:\n    {{kube}} -n fantasy-rpg delete pod cave-{{id}}"
    "caves:\n    {{kube}} -n fantasy-rpg get pods -l app=cave-server"
  ];
}
