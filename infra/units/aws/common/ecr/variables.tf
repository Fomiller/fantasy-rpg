# Written once, then it is yours. `nix run .#generate` never touches it again.
#
# `environment`, `app_prefix`, `namespace` and `asset_name` are not declared
# here. root.hcl generates them into every unit.
variable "repository_name" {
  type = string
}
