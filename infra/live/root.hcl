# ---------------------------------------------------------------------------
# GENERATED FILE — managed by flake-hub (golden-infra).
# Do not edit manually: `nix run .#generate` overwrites it.
# To change it, edit repo.nix, or the template in the pack that owns it.
# ---------------------------------------------------------------------------
#
# Root terragrunt config. Every unit includes this, so a unit's own
# terragrunt.hcl needs only `include "root"` and its inputs.
#
# Which provider to configure is derived from the unit's own path rather than
# declared per unit. Terragrunt treats a child `generate` block that shadows an
# inherited one as an error, so per-unit overrides are not an option — it has
# to be one conditional generator here.

locals {
  service_vars = read_terragrunt_config(find_in_parent_folders("service.hcl"))
  tag_vars     = read_terragrunt_config(find_in_parent_folders("tags.hcl"))
  version_vars = read_terragrunt_config(find_in_parent_folders("version.hcl"))
  account_vars = read_terragrunt_config(find_in_parent_folders("account.hcl"))

  # variables.hcl is optional and hand-written. try() covers both the missing
  # file and a file that does not set the key.
  variable_vars = try(read_terragrunt_config(find_in_parent_folders("variables.hcl")), { locals = {} })

  environment = local.account_vars.locals.environment
  region      = local.account_vars.locals.region

  state_bucket = coalesce(
    try(local.variable_vars.locals.bucket, null),
    try(local.account_vars.locals.state_bucket, null),
    "${local.service_vars.locals.namespace}-terraform-state-${local.environment}"
  )

  # The override has to happen here, not in tags.hcl: find_in_parent_folders()
  # inside a nested read starts above that file's own directory, so a
  # variables.hcl sitting beside tags.hcl would never be seen.
  default_tags = merge(
    local.tag_vars.locals.default_tags,
    try({ email = local.variable_vars.locals.owner_email }, {})
  )

  # path_relative_to_include() is "<env>/<provider>/<scope>/<unit...>";
  # segment [1] is the provider.
  path_parts = split("/", path_relative_to_include())
  provider   = local.path_parts[1]

  # tobool() on anything other than "true"/"false" raises an error carrying the
  # string, which is the only way to fail here with a readable message.
  _assert_provider = contains(keys(local.provider_versions), local.provider) ? true : tobool(
    "unknown provider '${local.provider}' from path '${path_relative_to_include()}'; expected one of ${join(", ", keys(local.provider_versions))}"
  )

  provider_versions = {
    aws = {
      source  = "hashicorp/aws"
      version = local.version_vars.locals.aws_provider_version
    }
  }

  provider_blocks = {
    aws = <<-EOT
      provider "aws" {
        region = "${local.region}"
        default_tags {
          tags = ${jsonencode(local.default_tags)}
        }
      }
    EOT
  }
}

remote_state {
  backend = "s3"
  config = {
    encrypt               = true
    disable_bucket_update = true
    bucket                = local.state_bucket
    # <repo>/<env>/<provider>/<scope>/<unit>/terraform.tfstate. repo_name is
    # what keeps two repos sharing this bucket from colliding.
    key          = "${local.service_vars.locals.repo_name}/${path_relative_to_include()}/terraform.tfstate"
    region       = local.region
    use_lockfile = true
  }
  generate = {
    path      = "_.backend.gen.tf"
    if_exists = "overwrite_terragrunt"
  }
}

generate "provider" {
  path      = "_.provider.gen.tf"
  if_exists = "overwrite_terragrunt"
  contents  = local.provider_blocks[local.provider]
}

generate "versions" {
  path      = "_.versions.gen.tf"
  if_exists = "overwrite_terragrunt"
  contents  = <<-EOF
    terraform {
      required_version = "${local.version_vars.locals.terraform_version}"
      required_providers {
        ${local.provider} = {
          source  = "${local.provider_versions[local.provider].source}"
          version = "${local.provider_versions[local.provider].version}"
        }
      }
    }
  EOF
}

generate "variables" {
  path      = "_.variables.gen.tf"
  if_exists = "overwrite_terragrunt"
  contents  = <<-EOF
    variable "environment" {
      type    = string
      default = "${local.environment}"
    }

    variable "app_prefix" {
      type    = string
      default = "${local.service_vars.locals.app_prefix}"
    }

    variable "namespace" {
      type    = string
      default = "${local.service_vars.locals.namespace}"
    }

    variable "asset_name" {
      type = string
    }
  EOF
}
