locals {
  validator_regions = [
    "nyc",
    "nyc",
    "london",
    "london",
    "singapore",
    "singapore",
    "singapore",
  ]
  relay_regions = [
    "nyc",
    "nyc",
    "london",
    "london",
    "singapore",
    "singapore",
    "singapore",
  ]
  validators = {
    for index in range(var.validator_count) :
    "validator-${index + 1}" => {
      ordinal = index + 1
      region  = local.validator_regions[index]
    }
  }
  relays = {
    for index in range(var.relay_count) :
    "relay-${index + 1}" => {
      ordinal = index + 1
      region  = local.relay_regions[index]
    }
  }
}

module "region" {
  for_each = var.regions
  source   = "./modules/region"

  project_name    = var.project_name
  environment     = var.environment
  region_key      = each.key
  region_label    = each.value.label
  cidr_block      = each.value.cidr_block
  validator_ids   = [for id, spec in local.validators : id if spec.region == each.key]
  relay_ids       = [for id, spec in local.relays : id if spec.region == each.key]
  container_image = var.container_image
}

check "private_testbed_boundary" {
  assert {
    condition     = var.enable_public_exits == false
    error_message = "Phase 9 scaffolding must not enable public exits."
  }
}

check "production_pilot_shape" {
  assert {
    condition = var.deployment_profile != "production-pilot" || (
      var.validator_count == 7 &&
      var.quorum_threshold == 5 &&
      length(var.secret_reference_names) >= 5
    )
    error_message = "production-pilot must model seven validators, a 5-of-7 quorum, and required secret references."
  }
}
