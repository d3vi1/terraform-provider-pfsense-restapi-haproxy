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
  insecure_tls = true
}

variable "pfsense_endpoint" {
  type = string
}

variable "pfsense_api_key" {
  type      = string
  sensitive = true
}
