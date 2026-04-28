# Terraform Provider for pfSense REST API HAProxy

Terraform provider for managing the pfSense HAProxy package through `pfSense-pkg-RESTAPI` v2 endpoints.

## Status

Bootstrap scaffold is in place. `pfsense_haproxy_settings` is implemented with
an import-first ownership model, `pfsense_haproxy_apply` provides an explicit
apply/reload lifecycle, `pfsense_haproxy_backend` manages top-level HAProxy
backends by natural name, and `pfsense_haproxy_backend_server` manages backend
server children by parent/backend name and server name.
`pfsense_haproxy_frontend` manages top-level HAProxy frontends by natural name.
Backend and backend server lookup data sources are available for read-only
references to existing HAProxy objects. Additional HAProxy resources are tracked
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
    pfsense = {
      source  = "d3vi1/pfsense-restapi-haproxy"
      version = ">= 0.1.0"
    }
  }
}

provider "pfsense" {
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

`auto_apply` is intentionally not exposed. HAProxy apply/reload behavior is
modeled explicitly by `pfsense_haproxy_apply`; settings and other durable
resources do not trigger hidden reloads.

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

## HAProxy apply lifecycle

`data "pfsense_haproxy_apply"` reads `GET /services/haproxy/apply` and exposes:

- `applied`: true when pfSense reports all HAProxy changes are applied.
- `pending`: true when pfSense reports pending HAProxy changes.
- `status`: normalized as `done` or `pending`.
- `status_detail`: human-readable next-step guidance.

`resource "pfsense_haproxy_apply"` is an explicit action resource:

- Create runs `POST /services/haproxy/apply`.
- Update runs `POST /services/haproxy/apply` only when `triggers` changes.
- Create/update poll `GET /services/haproxy/apply` until `applied=true`.
- Polling is bounded by `timeout` and `poll_interval`.
- Delete removes Terraform state only.
- Import uses the fixed ID `apply`.

Example:

```hcl
resource "pfsense_haproxy_apply" "global" {
  depends_on = [pfsense_haproxy_settings.global]

  triggers = {
    settings = sha1(jsonencode({
      enable               = pfsense_haproxy_settings.global.enable
      maxconn              = pfsense_haproxy_settings.global.maxconn
      nbthread             = pfsense_haproxy_settings.global.nbthread
      hard_stop_after      = pfsense_haproxy_settings.global.hard_stop_after
      sslcompatibilitymode = pfsense_haproxy_settings.global.sslcompatibilitymode
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
```

UAT validation is still pending. The implementation assumes the pfREST
`HAProxyApply` model shape where `GET /services/haproxy/apply` returns an
`applied` boolean, and `POST /services/haproxy/apply` starts HAProxy
configuration application.

## HAProxy backends

`resource "pfsense_haproxy_backend"` manages top-level HAProxy backends:

- Create sends `POST /services/haproxy/backend`.
- Read resolves by backend name with `GET /services/haproxy/backends?name=...`.
- Update resolves the current pfSense object ID by name, then sends
  `PATCH /services/haproxy/backend` with that ID and changed scalar fields.
- Delete resolves the current pfSense object ID by name, then sends
  `DELETE /services/haproxy/backend?id=...`.
- Import uses the backend name, for example
  `terraform import pfsense_haproxy_backend.app app_backend`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses the backend name as the stable ID because pfSense object
IDs are implementation details and may not be durable across config rewrites.
The resource schema is intentionally conservative: it exposes the backend name
and selected scalar fields needed for common backend creation, including health
check, agent check, and cookie persistence controls. Nested servers, ACLs,
actions, error files, stats, and advanced pass-through text remain out of scope
until their ownership and sensitivity model is validated.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/backends?name=...` returns backend objects with `id` and
`name`, the singular backend write endpoints accept the documented pfREST
`HAProxyBackend` scalar field names, and backend writes do not apply pending
HAProxy changes unless a separate `pfsense_haproxy_apply` resource is used.

`data "pfsense_haproxy_backend"` looks up an existing backend by exact name. It
returns the same conservative scalar fields as the resource and uses the backend
name as `id`; pfSense REST object IDs are not exposed. Missing or duplicate
matches return diagnostics. Data sources are lookup-only and cannot be imported.

## HAProxy backend servers

`resource "pfsense_haproxy_backend_server"` manages backend server children:

- Create re-queries the parent backend by name, checks for an existing server
  with `GET /services/haproxy/backend/servers?parent_id=...&name=...`, then
  sends `POST /services/haproxy/backend/server`.
- Read re-queries the parent backend by name and removes Terraform state if the
  parent backend or child server no longer exists.
- Update re-queries the parent backend and child server before sending
  `PATCH /services/haproxy/backend/server` with the current `parent_id`, child
  `id`, and changed scalar fields.
- Delete re-queries the parent backend and child server before sending
  `DELETE /services/haproxy/backend/server?parent_id=...&id=...`; if either is
  already gone, Terraform state is removed.
- Import uses `backend_name/server_name`, for example
  `terraform import pfsense_haproxy_backend_server.app app_backend/app01`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses `backend_name/server_name` as the stable ID because pfSense
object IDs are implementation details and may change across config rewrites. The
resource schema is intentionally conservative: it exposes `backend_name`,
`name`, `address`, `port`, `status`, `weight`, `ssl`, `sslserververify`, and the
read-only `serverid`. The `advanced` server field is deferred until the
sensitivity and ownership model is validated on UAT.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/backend/servers?parent_id=...&name=...` returns backend
server objects with `id`, `name`, `address`, and `port`; create/update/delete
use the documented child `parent_id` contract; and backend server writes only
mark HAProxy configuration pending until a separate `pfsense_haproxy_apply`
resource is used.

`data "pfsense_haproxy_backend_server"` looks up an existing backend server by
`backend_name` and server `name`. It first resolves the parent backend by name,
then queries child servers under the current parent ID. The returned `id` is the
stable `backend_name/server_name` natural key; transient pfSense parent/child
REST IDs are not exposed. Missing parent, missing child, or duplicate child
matches return diagnostics. Data sources are lookup-only and cannot be imported.

## HAProxy frontends

`resource "pfsense_haproxy_frontend"` manages top-level HAProxy frontends:

- Create sends `POST /services/haproxy/frontend`.
- Read resolves by frontend name with
  `GET /services/haproxy/frontends?name=...`.
- Update resolves the current pfSense object ID by name, then sends
  `PATCH /services/haproxy/frontend` with that ID and changed scalar fields.
- Delete resolves the current pfSense object ID by name, then sends
  `DELETE /services/haproxy/frontend?id=...`.
- Import uses the frontend name, for example
  `terraform import pfsense_haproxy_frontend.app app_frontend`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses the frontend name as the stable ID because pfSense object
IDs are implementation details and may not be durable across config rewrites.
The resource schema is intentionally conservative: it exposes required `name`
and `type` plus selected scalar fields for basic HTTP/TCP frontend lifecycle.
Only `http` and `tcp` are supported for `type`; `https` is deferred until
certificate ownership is modeled. Addresses, ACLs, actions, certificates, error
files, `advanced`, `advanced_bind`, and default certificate ownership remain out
of scope.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontends?name=...` returns frontend objects with `id`,
`name`, and `type`; create/update/delete use singular frontend endpoints; and
frontend writes only mark HAProxy configuration pending until a separate
`pfsense_haproxy_apply` resource is used.

## Development

```bash
make lint
make test
make testacc
```

`make testacc` requires real pfSense credentials and must be run only against an approved test target unless the issue explicitly targets production.

## Resources

- `pfsense_haproxy_apply`
- `pfsense_haproxy_backend`
- `pfsense_haproxy_backend_server`
- `pfsense_haproxy_frontend`
- `pfsense_haproxy_settings`

## Data Sources

- `pfsense_haproxy_apply`
- `pfsense_haproxy_backend`
- `pfsense_haproxy_backend_server`
- `pfsense_haproxy_settings`

## Planned resources

- `pfsense_haproxy_frontend_address`
- `pfsense_haproxy_frontend_acl`
- `pfsense_haproxy_frontend_action`
- `pfsense_haproxy_frontend_certificate`
- `pfsense_haproxy_file`

## Example ingress contract

```hcl
resource "pfsense_haproxy_backend" "addressvalidator" {
  name                = "addressvalidator_uat"
  balance             = "roundrobin"
  connection_timeout  = 30000
  server_timeout      = 30000
  check_type          = "HTTP"
  checkinter          = 2000
  httpcheck_method    = "GET"
  monitor_uri         = "/health"
  monitor_httpversion = "HTTP/1.1"
}

resource "pfsense_haproxy_backend_server" "addressvalidator_01" {
  backend_name = pfsense_haproxy_backend.addressvalidator.name
  name         = "addressvalidator_01"
  address      = "10.30.10.21"
  port         = 8080
  status       = "active"
  weight       = 10
}

resource "pfsense_haproxy_frontend" "addressvalidator" {
  name               = "addressvalidator_uat_http"
  type               = "http"
  descr              = "Address Validator UAT HTTP frontend"
  status             = "active"
  backend_serverpool = pfsense_haproxy_backend.addressvalidator.name
  max_connections    = 2000
  client_timeout     = 30000
  forwardfor         = true
  httpclose          = "http-server-close"
}

resource "pfsense_haproxy_apply" "addressvalidator" {
  depends_on = [
    pfsense_haproxy_backend.addressvalidator,
    pfsense_haproxy_backend_server.addressvalidator_01,
    pfsense_haproxy_frontend.addressvalidator,
  ]

  triggers = {
    backend = sha1(jsonencode({
      name           = pfsense_haproxy_backend.addressvalidator.name
      balance        = pfsense_haproxy_backend.addressvalidator.balance
      check_type     = pfsense_haproxy_backend.addressvalidator.check_type
      monitor_uri    = pfsense_haproxy_backend.addressvalidator.monitor_uri
      server_timeout = pfsense_haproxy_backend.addressvalidator.server_timeout
    }))
    backend_servers = sha1(jsonencode({
      app01 = {
        name            = pfsense_haproxy_backend_server.addressvalidator_01.name
        address         = pfsense_haproxy_backend_server.addressvalidator_01.address
        port            = pfsense_haproxy_backend_server.addressvalidator_01.port
        status          = pfsense_haproxy_backend_server.addressvalidator_01.status
        weight          = pfsense_haproxy_backend_server.addressvalidator_01.weight
        ssl             = pfsense_haproxy_backend_server.addressvalidator_01.ssl
        sslserververify = pfsense_haproxy_backend_server.addressvalidator_01.sslserververify
      }
    }))
    frontend = sha1(jsonencode({
      name               = pfsense_haproxy_frontend.addressvalidator.name
      type               = pfsense_haproxy_frontend.addressvalidator.type
      backend_serverpool = pfsense_haproxy_frontend.addressvalidator.backend_serverpool
      max_connections    = pfsense_haproxy_frontend.addressvalidator.max_connections
      client_timeout     = pfsense_haproxy_frontend.addressvalidator.client_timeout
      forwardfor         = pfsense_haproxy_frontend.addressvalidator.forwardfor
      httpclose          = pfsense_haproxy_frontend.addressvalidator.httpclose
    }))
  }
}
```

Remaining schemas will be finalized from approved UAT pfSense REST API endpoint
responses before production use.

## Schema inventory

HAProxy endpoint inventory and provisional resource schema decisions are tracked
under `docs/schema/`. Live schema captures must be generated with:

```bash
python3 scripts/capture_haproxy_schema.py --schema both --output-dir docs/schema
```

Only redacted `haproxy-*.redacted.json` artifacts may be committed. Raw captures,
credentials, certificates, API tokens, and pfSense config backups must stay out
of Git.
