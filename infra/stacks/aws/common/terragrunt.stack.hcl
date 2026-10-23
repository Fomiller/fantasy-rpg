# Written once, then it is yours. `nix run .#generate` never touches it again.
#
# The aws units that are shared across environments. "common" means deployed
# once per account, not once per environment.

locals {
  units_path = find_in_parent_folders("units")
}

unit "ecr" {
  source                  = "${local.units_path}/aws/common/ecr"
  path                    = "ecr"
  no_dot_terragrunt_stack = true
}
