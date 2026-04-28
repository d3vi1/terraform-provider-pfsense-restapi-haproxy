# Terraform Provider for pfSense REST API HAProxy

Terraform provider for managing the pfSense HAProxy package through `pfSense-pkg-RESTAPI` v2 endpoints.

## Status

Bootstrap scaffold is in place. Resource implementation is tracked through GitHub issues and milestones.

## Requirements

- Go 1.22+
- Terraform 1.5+
- pfSense with:
  - `pfSense-pkg-RESTAPI`
  - HAProxy or HAProxy-devel package

## Provider configuration

```hcl
terraform {
  required_providers {
    pfsense-haproxy = {
      source  = "d3vi1/pfsense-restapi-haproxy"
      version = ">= 0.1.0"
    }
  }
}

provider "pfsense-haproxy" {
  endpoint     = "https://pfsense.example.com"
  api_key      = var.pfsense_api_key
  insecure_tls = true
  timeout      = "30s"
}
```

Provider arguments can also be supplied with environment variables:

- `PFSENSE_ENDPOINT`
- `PFSENSE_API_KEY`
- `PFSENSE_USERNAME`
- `PFSENSE_PASSWORD`
- `PFSENSE_INSECURE_TLS`
- `PFSENSE_TIMEOUT`

Prefer API key authentication for automation.

## Development

```bash
make lint
make test
make testacc
```

`make testacc` requires real pfSense credentials and must be run only against an approved test target unless the issue explicitly targets production.

## Planned resources

- `pfsense_haproxy_settings`
- `pfsense_haproxy_backend`
- `pfsense_haproxy_backend_server`
- `pfsense_haproxy_frontend`
- `pfsense_haproxy_frontend_address`
- `pfsense_haproxy_frontend_acl`
- `pfsense_haproxy_frontend_action`
- `pfsense_haproxy_frontend_certificate`
- `pfsense_haproxy_file`
- `pfsense_haproxy_apply`

## Example ingress contract

```hcl
resource "pfsense_haproxy_backend" "addressvalidator" {
  name = "addressvalidator_uat"
  mode = "http"
}

resource "pfsense_haproxy_backend_server" "addressvalidator_01" {
  backend = pfsense_haproxy_backend.addressvalidator.name
  name    = "01-gts-u-addressvalidator"
  address = "192.168.31.101"
  port    = 8080
  check   = true
}
```

The exact schemas will be finalized from pfSense REST API endpoint responses during Milestone 1.
