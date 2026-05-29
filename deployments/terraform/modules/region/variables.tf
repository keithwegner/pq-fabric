variable "project_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "region_key" {
  type = string
}

variable "region_label" {
  type = string
}

variable "cidr_block" {
  type = string
}

variable "validator_ids" {
  type = list(string)
}

variable "relay_ids" {
  type = list(string)
}

variable "container_image" {
  type = string
}
