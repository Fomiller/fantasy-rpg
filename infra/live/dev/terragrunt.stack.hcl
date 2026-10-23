# Written once, then it is yours. `nix run .#generate` never touches it again.
#
# Which stacks this environment gets. Each source is a directory under
# infra/stacks/, which names the units under infra/units/.
#
# `terragrunt stack run` generates the unit directories into this one. They are
# gitignored, so nothing under infra/live/dev/ except this file, account.hcl
# and the README belongs in git.

locals {
  stacks_path  = find_in_parent_folders("stacks")
  account_vars = read_terragrunt_config("${get_terragrunt_dir()}/account.hcl")
}

stack "aws" {
  source                  = "${local.stacks_path}/aws/common"
  path                    = "aws/common"
  no_dot_terragrunt_stack = true

  values = {
    environment = local.account_vars.locals.environment
  }
}
