terraform {
  required_providers {
    pfsense-haproxy = {
      source = "d3vi1/pfsense-restapi-haproxy"
    }
  }
}

provider "pfsense-haproxy" {
  endpoint     = var.pfsense_endpoint
  api_key      = var.pfsense_api_key
  insecure_tls = true
}

variable "pfsense_endpoint" {
  type = string
}

variable "pfsense_api_key" {
  type      = string
  sensitive = true
}
