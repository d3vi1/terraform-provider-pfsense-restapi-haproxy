# Terraform Provider for pfSense REST API HAProxy

Terraform provider for managing the pfSense HAProxy package through `pfSense-pkg-RESTAPI` v2 endpoints.

## Status

Bootstrap scaffold is in place. `pfsense_haproxy_settings` is implemented with
an import-first ownership model. Additional HAProxy resources are tracked
through GitHub issues and milestones.

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

`auto_apply` is intentionally not exposed yet. Apply/reload behavior is reserved
for the planned `pfsense_haproxy_apply` implementation once HAProxy resource
semantics are in place.

## HAProxy settings ownership

`pfsense_haproxy_settings` is split into a data source and a singleton resource:

- `data "pfsense_haproxy_settings"` reads `GET /services/haproxy/settings`.
- `resource "pfsense_haproxy_settings"` must be imported with fixed ID
  `settings` before it can manage fields.
- Resource create intentionally returns a diagnostic and performs no REST API
  write.
- Resource update patches changed scalar fields with
  `PATCH /services/haproxy/settings`.
- Resource delete removes Terraform state only and performs no REST API call.
- No HAProxy apply/reload is triggered by this resource.

The current schema exposes documented scalar HAProxy settings only. Nested
`dns_resolvers` and `email_mailers` are intentionally not managed by this
resource until child-resource ownership is validated. The `advanced` field is
marked sensitive because it can contain arbitrary HAProxy global configuration.

Live UAT schema capture is still pending. The field list is based on the
upstream pfSense-pkg-RESTAPI HAProxy settings model and must be verified against
the approved UAT firewall before production use.

Import example:

```bash
terraform import pfsense_haproxy_settings.global settings
```

## Development

```bash
make lint
make test
make testacc
```

`make testacc` requires real pfSense credentials and must be run only against an approved test target unless the issue explicitly targets production.

## Resources

- `pfsense_haproxy_settings`

## Planned resources

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

Remaining resource schemas will be finalized from approved UAT pfSense REST API
endpoint responses before production use.

## Schema inventory

HAProxy endpoint inventory and provisional resource schema decisions are tracked
under `docs/schema/`. Live schema captures must be generated with:

```bash
python3 scripts/capture_haproxy_schema.py --schema both --output-dir docs/schema
```

Only redacted `haproxy-*.redacted.json` artifacts may be committed. Raw captures,
credentials, certificates, API tokens, and pfSense config backups must stay out
of Git.
