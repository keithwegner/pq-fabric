output "deployment_summary" {
  description = "Local planning summary. These are labels and intended topology only."
  value = {
    project_name       = var.project_name
    environment        = var.environment
    deployment_profile = var.deployment_profile
    validator_count    = var.validator_count
    relay_count        = var.relay_count
    quorum_threshold   = var.quorum_threshold
    container_image    = var.container_image
    public_exits       = var.enable_public_exits
    remote_state       = var.remote_state_configured ? "operator-provided" : "not configured by scaffold"
    secret_references  = sort(tolist(var.secret_reference_names))
    region_summaries   = { for key, region in module.region : key => region.summary }
    non_production     = true
    terraform_apply    = "out of scope for controlled deployment readiness"
  }
}
