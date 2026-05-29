locals {
  summary = {
    project_name    = var.project_name
    environment     = var.environment
    region_key      = var.region_key
    region_label    = var.region_label
    cidr_block      = var.cidr_block
    validator_ids   = var.validator_ids
    relay_ids       = var.relay_ids
    container_image = var.container_image
    resource_mode   = "scaffold-only"
  }
}
