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

  # Setting this list here replaces the pack recipes, so build and test are restated.
  just.recipes = [
    { name = "build"; cmdFrom = "buildCmd"; }
    { name = "test"; cmdFrom = "testCmd"; }
    { name = "lint"; cmdFrom = "lintCmd"; }
    "fmt:\n    gofmt -w src"
    "dev:\n    go run ./src/cmd/world-server"
  ];
}
