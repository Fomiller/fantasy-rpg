{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    golden-engine.url = "github:Fomiller/flake-hub?dir=golden-engine&ref=refs/tags/golden-engine-0.5.0";
    golden-base.url = "github:Fomiller/flake-hub?dir=golden-base&ref=refs/tags/golden-base-0.7.1";
    golden-github.url = "github:Fomiller/flake-hub?dir=golden-github&ref=refs/tags/golden-github-0.11.0";
    golden-service.url = "github:Fomiller/flake-hub?dir=golden-service&ref=refs/tags/golden-service-0.7.0";
    golden-infra.url = "github:Fomiller/flake-hub?dir=golden-infra&ref=refs/tags/golden-infra-0.7.0";
    golden-argocd.url = "github:Fomiller/flake-hub?dir=golden-argocd&ref=refs/tags/golden-argocd-0.20.0";
    golden-docs.url = "github:Fomiller/flake-hub?dir=golden-docs&ref=refs/tags/golden-docs-0.2.0";
  };

  outputs = { self, nixpkgs, flake-utils, golden-engine, golden-base, golden-github, golden-service, golden-infra, golden-argocd, golden-docs }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        golden = golden-engine.lib.mkGolden {
          packs = [ golden-base.pack golden-github.pack golden-service.pack golden-infra.pack golden-argocd.pack golden-docs.pack ];
        } pkgs (import ./repo.nix);
      in
      {
        apps.generate = golden.generateApp;
        packages.golden-files = golden.filesDrv;

        # Godot is left out: nixpkgs has no cached darwin build, so use the installed Godot.app.
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_27
            gopls
            buf
            protobuf
            protoc-gen-go
            just
            docker-client
            kubectl
            k3d
            kind
            kubernetes-helm
          ];
        };
      });
}
