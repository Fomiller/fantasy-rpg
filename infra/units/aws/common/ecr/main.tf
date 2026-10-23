# Written once, then it is yours. `nix run .#generate` never touches it again.
#
# Two repositories: one for the image, one for the chart. The chart's name has
# to match Chart.yaml, which is a single value for the whole repo, so these are
# account-level and not per-environment.

resource "aws_ecr_repository" "image" {
  name                 = var.repository_name
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_repository" "chart" {
  name = "${var.repository_name}-chart"
  # Charts are pushed at the version in Chart.yaml, and re-pushing the same
  # version during development is normal. The image is immutable; this is not.
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = false
  }
}

resource "aws_ecr_lifecycle_policy" "image" {
  repository = aws_ecr_repository.image.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Delete untagged images after 1 day."
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
    ]
  })
}
