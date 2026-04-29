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
  timeout      = var.pfsense_timeout
}

provider "pfsense" {
  alias        = "uat"
  endpoint     = var.pfsense_endpoint
  api_key      = var.pfsense_api_key
  insecure_tls = true
  timeout      = var.pfsense_timeout
}

provider "pfsense" {
  alias        = "prod"
  endpoint     = var.pfsense_prod_endpoint
  api_key      = var.pfsense_prod_api_key
  username     = var.pfsense_prod_username
  password     = var.pfsense_prod_password
  insecure_tls = var.pfsense_prod_insecure_tls
  timeout      = var.pfsense_timeout
}

variable "pfsense_endpoint" {
  type = string
}

variable "pfsense_api_key" {
  type      = string
  sensitive = true
}

variable "pfsense_timeout" {
  type    = string
  default = "30s"
}

variable "pfsense_prod_endpoint" {
  type = string
}

variable "pfsense_prod_api_key" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_prod_username" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_prod_password" {
  type      = string
  sensitive = true
  default   = null
}

variable "pfsense_prod_insecure_tls" {
  type    = bool
  default = false
}
