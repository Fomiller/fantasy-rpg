# Written once, then it is yours. `nix run .#generate` never touches it again.

output "image_repository_url" {
  value = aws_ecr_repository.image.repository_url
}

output "chart_repository_url" {
  value = aws_ecr_repository.chart.repository_url
}
