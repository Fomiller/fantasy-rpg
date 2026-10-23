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

deps:
    {{compose}} up -d --wait postgres nats

dev: deps
    go run ./src/cmd/world-server

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
    {{godot}} --path client/godot

client-check:
    {{godot}} --headless --path client/godot --import
    {{godot}} --headless --path client/godot --quit-after 5
