variable "project_name" {
  description = "Name prefix for future pq-fabric deployment resources."
  type        = string
  default     = "pq-fabric"
}

variable "environment" {
  description = "Deployment environment label. This scaffold defaults to local planning."
  type        = string
  default     = "local-simulation"
}

variable "deployment_profile" {
  description = "Controlled deployment profile. This scaffold supports validation for local, staging, and production-pilot only."
  type        = string
  default     = "local"

  validation {
    condition     = contains(["local", "staging", "production-pilot"], var.deployment_profile)
    error_message = "deployment_profile must be local, staging, or production-pilot."
  }
}

variable "regions" {
  description = "Conceptual regions for future controlled deployment."
  type = map(object({
    label      = string
    cidr_block = string
  }))
  default = {
    nyc = {
      label      = "nyc"
      cidr_block = "10.91.0.0/20"
    }
    london = {
      label      = "london"
      cidr_block = "10.92.0.0/20"
    }
    singapore = {
      label      = "singapore"
      cidr_block = "10.93.0.0/20"
    }
  }
}

variable "validator_count" {
  description = "Number of validators modeled by the future deployment plan."
  type        = number
  default     = 7
}

variable "relay_count" {
  description = "Number of private-testbed relays modeled by the future deployment plan."
  type        = number
  default     = 7
}

variable "quorum_threshold" {
  description = "Validator quorum threshold. The local prototype remains 5-of-7."
  type        = number
  default     = 5
}

variable "container_image" {
  description = "Container image reference for future runtime wiring."
  type        = string
  default     = "pq-fabric:local"
}

variable "enable_public_exits" {
  description = "Must remain false for the Phase 9 private-testbed boundary."
  type        = bool
  default     = false
}

variable "remote_state_configured" {
  description = "Boolean evidence marker only. No backend is configured in this scaffold."
  type        = bool
  default     = false
}

variable "secret_reference_names" {
  description = "Expected Kubernetes Secret names for the controlled pilot contract. These are references only, not secret values."
  type        = set(string)
  default = [
    "pq-fabric-api-keys",
    "pq-fabric-consortium-manifest",
    "pq-fabric-peer-tls",
    "pq-fabric-kms",
    "pq-fabric-kms-ca",
  ]
}
