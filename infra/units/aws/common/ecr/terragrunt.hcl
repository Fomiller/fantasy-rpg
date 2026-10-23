# Written once, then it is yours. `nix run .#generate` never touches it again.

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  asset_name      = "ecr"
  repository_name = "fantasy-rpg"
}
