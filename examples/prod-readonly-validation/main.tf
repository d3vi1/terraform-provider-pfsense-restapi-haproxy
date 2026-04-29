terraform {
  required_providers {
    pfsense = {
      source = "d3vi1/pfsense-restapi-haproxy"
    }
  }
}

provider "pfsense" {
  endpoint     = var.pfsense_endpoint
  api_key      = var.pfsense_api_key
  username     = var.pfsense_username
  password     = var.pfsense_password
  insecure_tls = var.pfsense_insecure_tls
  timeout      = var.pfsense_timeout
}

variable "pfsense_endpoint" {
  type = string
}

variable "pfsense_api_key" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_username" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_password" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_insecure_tls" {
  type    = bool
  default = false
}

variable "pfsense_timeout" {
  type    = string
  default = "30s"
}

variable "existing_backend_name" {
  type        = string
  description = "Existing production backend name to read. Leave null to skip backend data source validation."
  default     = null
}

variable "existing_backend_server_name" {
  type        = string
  description = "Existing server name under existing_backend_name to read. Leave null to skip server validation."
  default     = null
}

data "pfsense_haproxy_settings" "current" {}

data "pfsense_haproxy_apply" "status" {}

data "pfsense_haproxy_backend" "existing" {
  count = var.existing_backend_name == null ? 0 : 1
  name  = var.existing_backend_name
}

data "pfsense_haproxy_backend_server" "existing" {
  count = var.existing_backend_name == null || var.existing_backend_server_name == null ? 0 : 1

  backend_name = var.existing_backend_name
  name         = var.existing_backend_server_name
}

output "readonly_haproxy_status" {
  value = {
    apply_status         = data.pfsense_haproxy_apply.status.status
    settings_enabled     = data.pfsense_haproxy_settings.current.enable
    backend_found        = length(data.pfsense_haproxy_backend.existing) > 0
    backend_server_found = length(data.pfsense_haproxy_backend_server.existing) > 0
  }
}
