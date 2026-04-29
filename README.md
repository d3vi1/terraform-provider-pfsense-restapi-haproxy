# Terraform Provider for pfSense REST API HAProxy

Terraform provider for managing the pfSense HAProxy package through `pfSense-pkg-RESTAPI` v2 endpoints.

## Status

Bootstrap scaffold is in place. `pfsense_haproxy_settings` is implemented with
an import-first ownership model, `pfsense_haproxy_apply` provides an explicit
apply/reload lifecycle, `pfsense_haproxy_backend` manages top-level HAProxy
backends by natural name, and `pfsense_haproxy_backend_server` manages backend
server children by parent/backend name and server name. Backend ACLs and
backend actions are implemented as ordered child resources; ACLs use
`backend_name/name` as their stable ID, while anonymous actions use a
Terraform-only `key` plus exact payload matching.
`pfsense_haproxy_frontend` manages top-level HAProxy frontends by natural name,
and `pfsense_haproxy_frontend_address` manages frontend bind/listen address
children by parent/frontend name, listen address, custom address, and port.
Frontend ACLs and frontend actions are implemented as ordered child resources;
ACLs use `frontend_name/name` as their stable ID, while anonymous actions use a
Terraform-only `key` plus exact payload matching.
`pfsense_haproxy_frontend_certificate` attaches existing pfSense certificate
references to frontends without managing certificate or private key material.
Backend and backend server lookup data sources are available for read-only
references to existing HAProxy objects. Additional HAProxy resources are tracked
through GitHub issues and milestones. `pfsense_haproxy_file` is intentionally
deferred until UAT confirms the HAProxy file endpoint semantics and a security
model is selected for secret-bearing file content.

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

The REST client retries only safe reads. `GET` requests are retried once for
transient transport failures and HTTP/API-envelope `408`, `429`, `500`, `502`,
`503`, and `504` responses, honoring `Retry-After` up to a bounded delay.
Mutating `POST`, `PATCH`, `PUT`, and `DELETE` requests are never replayed
automatically; transient write failures include diagnostics telling operators
to refresh or inspect live pfSense HAProxy state before rerunning Terraform.

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

Import example:

```bash
terraform import pfsense_haproxy_apply.global apply
```

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
check, agent check, and cookie persistence controls. Backend servers, ACLs, and
actions are managed by separate child resources. Error files, stats, and
advanced pass-through text remain out of scope until their ownership and
sensitivity model is validated.

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

## HAProxy backend ACLs

`resource "pfsense_haproxy_backend_acl"` manages ordered backend ACL children:

- Create re-queries the parent backend by name, checks for an existing ACL with
  `GET /services/haproxy/backend/acls?parent_id=...&name=...`, then sends
  `POST /services/haproxy/backend/acl`.
- Read re-queries the parent backend by name, finds the ACL by exact name, and
  records `position` from the ordered plural response.
- Update re-queries the parent backend and ACL before sending
  `PATCH /services/haproxy/backend/acl` with current `parent_id`, child `id`,
  changed scalar fields, and `placement` when `position` changes.
- Delete re-queries current parent/child IDs before sending
  `DELETE /services/haproxy/backend/acl?parent_id=...&id=...`.
- Import uses `backend_name/acl_name`, for example
  `terraform import pfsense_haproxy_backend_acl.host app_backend/host_acl`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses `backend_name/name` as the stable ID. `position` is
zero-based and maps to pfREST `placement` on create/update; when omitted,
Terraform reads the live order from the plural ACL list. Duplicate ACL names
under one backend are rejected because they cannot be managed safely by natural
key.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/backend/acls?parent_id=...&name=...` returns ACL objects
with `id`, `name`, `expression`, and `value`; full plural reads preserve backend
ACL order; and backend ACL writes only mark HAProxy configuration pending until
a separate `pfsense_haproxy_apply` resource is used.

## HAProxy backend actions

`resource "pfsense_haproxy_backend_action"` manages ordered backend action
children:

- `key` is a Terraform-only identity component. It is never sent to pfSense.
- Create/read/update/delete always re-query the parent backend by name.
- Live actions are matched by normalized action payload fields: `action`, `acl`,
  and the conditional field(s) required by the selected action. Transient `id`,
  `parent_id`, `position`, and `key` are excluded from matching.
- Duplicate live payload matches return a diagnostic requiring cleanup or a more
  unique action payload before Terraform can manage the action.
- `position` is zero-based and maps to pfREST `placement` on create/update.
- Import uses `backend_name/key`, for example
  `terraform import pfsense_haproxy_backend_action.route app_backend/route_app01`.
- No HAProxy apply/reload is triggered by this resource.

Supported backend action choices follow the pfREST `HAProxyBackendAction`
model, including `use_server`, `custom`, HTTP request/response mutations, HTTP
after-response mutations, and TCP request/response actions. Conditional fields
are validated conservatively: Terraform requires the field(s) pfREST marks
required for the selected action and rejects non-null fields that are not
applicable to that action.

Because pfSense backend actions do not expose a stable name, import initializes
only `backend_name/key`; the resource configuration must include the exact
unique payload so the next refresh/apply can match the live action. If multiple
existing actions have the same normalized payload, clean up duplicates before
importing.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/backend/actions?parent_id=...` returns ordered backend
action objects with transient `id` and action payload fields; dynamic pfREST
internal names are action-prefixed, with `lua_function` mapped to
`lua-function`; and backend action writes only mark HAProxy configuration
pending until a separate `pfsense_haproxy_apply` resource is used.

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
Only `http` and `tcp` are supported for `type`; any separate `https` frontend
mode remains deferred until UAT confirms how it interacts with frontend address
SSL offload and certificate attachments. Addresses, ACLs, actions, and
certificates are separate child resources. Error files, `advanced`,
`advanced_bind`, and default certificate ownership remain out of scope for this
top-level frontend resource.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontends?name=...` returns frontend objects with `id`,
`name`, and `type`; create/update/delete use singular frontend endpoints; and
frontend writes only mark HAProxy configuration pending until a separate
`pfsense_haproxy_apply` resource is used.

## HAProxy frontend addresses

`resource "pfsense_haproxy_frontend_address"` manages frontend bind/listen
address children:

- Create re-queries the parent frontend by name, checks for an existing address
  with `GET /services/haproxy/frontend/addresses?parent_id=...&extaddr=...&extaddr_custom=...&extaddr_port=...`,
  then sends `POST /services/haproxy/frontend/address`.
- Read re-queries the parent frontend by name and removes Terraform state if
  the parent frontend or child address no longer exists.
- Update re-queries the parent frontend and child address before sending
  `PATCH /services/haproxy/frontend/address` with the current `parent_id`,
  child `id`, and changed scalar fields.
- Delete re-queries the parent frontend and child address before sending
  `DELETE /services/haproxy/frontend/address?parent_id=...&id=...`; if either
  is already gone, Terraform state is removed.
- Import uses `frontend_name/extaddr/extaddr_custom_or_-/extaddr_port`, for
  example `terraform import pfsense_haproxy_frontend_address.app app_frontend/any_ipv4/-/443`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses the frontend name, external address selector, optional
custom IP address, and port as the stable ID because pfSense object IDs are
implementation details and may change across config rewrites. The resource
schema is intentionally conservative: it exposes `frontend_name`, `extaddr`,
`extaddr_custom`, `extaddr_port`, and `extaddr_ssl` only. `exaddr_advanced`,
placement, nested arrays, and transient REST IDs remain out of scope pending
UAT confirmation.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontend/addresses?parent_id=...&extaddr=...&extaddr_custom=...&extaddr_port=...`
returns frontend address objects with `id`, `extaddr`, `extaddr_custom`, and
`extaddr_port`; create/update/delete use the documented child `parent_id`
contract; and frontend address writes only mark HAProxy configuration pending
until a separate `pfsense_haproxy_apply` resource is used.

## HAProxy frontend ACLs

`resource "pfsense_haproxy_frontend_acl"` manages ordered frontend ACL children:

- Create re-queries the parent frontend by name, checks for an existing ACL with
  `GET /services/haproxy/frontend/acls?parent_id=...&name=...`, then sends
  `POST /services/haproxy/frontend/acl`.
- Read re-queries the parent frontend by name, finds the ACL by exact name, and
  records `position` from the ordered plural response.
- Update re-queries the parent frontend and ACL before sending
  `PATCH /services/haproxy/frontend/acl` with current `parent_id`, child `id`,
  changed scalar fields, and `placement` when `position` changes.
- Delete re-queries current parent/child IDs before sending
  `DELETE /services/haproxy/frontend/acl?parent_id=...&id=...`.
- Import uses `frontend_name/acl_name`, for example
  `terraform import pfsense_haproxy_frontend_acl.host app_frontend/host_acl`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses `frontend_name/name` as the stable ID. `position` is
zero-based and maps to pfREST `placement` on create/update; when omitted,
Terraform reads the live order from the plural ACL list. Duplicate ACL names
under one frontend are rejected because they cannot be managed safely by
natural key.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontend/acls?parent_id=...&name=...` returns ACL objects
with `id`, `name`, `expression`, and `value`; full plural reads preserve
frontend ACL order; and frontend ACL writes only mark HAProxy configuration
pending until a separate `pfsense_haproxy_apply` resource is used.

## HAProxy frontend actions

`resource "pfsense_haproxy_frontend_action"` manages ordered frontend action
children:

- `key` is a Terraform-only identity component. It is never sent to pfSense.
- Create/read/update/delete always re-query the parent frontend by name.
- Live actions are matched by normalized action payload fields: `action`, `acl`,
  and the conditional field(s) required by the selected action. Transient `id`,
  `parent_id`, `position`, and `key` are excluded from matching.
- Duplicate live payload matches return a diagnostic requiring cleanup or a more
  unique action payload before Terraform can manage the action.
- `position` is zero-based and maps to pfREST `placement` on create/update.
- Import uses `frontend_name/key`, for example
  `terraform import pfsense_haproxy_frontend_action.route app_frontend/route_app`.
- No HAProxy apply/reload is triggered by this resource.

Supported frontend action choices follow the pfREST `HAProxyFrontendAction`
model, including `use_backend`, `custom`, HTTP request/response mutations, HTTP
after-response mutations, and TCP request/response actions. The route action is
`use_backend` and its conditional target field is `backend`. Conditional fields
are validated conservatively: Terraform requires the field(s) pfREST marks
required for the selected action and rejects non-null fields that are not
applicable to that action.

Because pfSense frontend actions do not expose a stable name, import
initializes only `frontend_name/key`; the resource configuration must include
the exact unique payload so the next refresh/apply can match the live action. If
multiple existing actions have the same normalized payload, clean up duplicates
before importing.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontend/actions?parent_id=...` returns ordered frontend
action objects with transient `id` and action payload fields; dynamic pfREST
internal names are action-prefixed, with `lua_function` mapped to
`lua-function`; and frontend action writes only mark HAProxy configuration
pending until a separate `pfsense_haproxy_apply` resource is used.

## HAProxy frontend certificates

`resource "pfsense_haproxy_frontend_certificate"` attaches existing pfSense
certificate references to frontend objects:

- Create re-queries the parent frontend by name, checks for an existing
  certificate attachment with
  `GET /services/haproxy/frontend/certificates?parent_id=...&ssl_certificate=...`,
  then sends `POST /services/haproxy/frontend/certificate`.
- Read re-queries the parent frontend by name and removes Terraform state if
  the parent frontend or child certificate attachment no longer exists.
- Update is intentionally unsupported. `frontend_name` and `ssl_certificate`
  both require replacement.
- Delete re-queries the parent frontend and child certificate before sending
  `DELETE /services/haproxy/frontend/certificate?parent_id=...&id=...`; if
  either is already gone, Terraform state is removed.
- Import uses `frontend_name/ssl_certificate`, for example
  `terraform import pfsense_haproxy_frontend_certificate.app app_frontend/existing_cert_ref`.
- No HAProxy apply/reload is triggered by this resource.

Terraform state uses `frontend_name/ssl_certificate` as the stable ID because
pfSense object IDs are implementation details and may change across config
rewrites. The schema accepts only an existing certificate reference/name. It
does not expose certificate bodies, private keys, PEM content, advanced fields,
placement controls, or transient REST IDs.

UAT validation is still pending. The implementation assumes
`GET /services/haproxy/frontend/certificates?parent_id=...&ssl_certificate=...`
returns certificate attachment objects with `id` and `ssl_certificate`;
create/delete use the documented child `parent_id` contract; and certificate
attachment writes only mark HAProxy configuration pending until a separate
`pfsense_haproxy_apply` resource is used.

## Deferred HAProxy files

`pfsense_haproxy_file` is intentionally deferred out of M4. The current
frontend resources do not require `HAProxyFile.content` for their lifecycle.

The REST API's `HAProxyFile.content` field can carry custom error pages,
snippets, or other HAProxy text that may contain private material. Before this
provider manages it, a later issue must pass these gates:

- A managed error-file or snippet use case requires first-class HAProxy file
  ownership.
- UAT confirms `/services/haproxy/file` and `/services/haproxy/files` response
  shape, import identity, read-after-write behavior, and whether reads return
  file content.
- A security model is selected for either sensitive Terraform state content or
  write-only content with a caller-supplied content hash for drift detection.

## Development

```bash
make lint
make test
make testacc
```

`make testacc` requires real pfSense credentials and must be run only against an
approved UAT target unless the issue explicitly targets production. Set
`PFSENSE_TEST_ENVIRONMENT=uat` and `PFSENSE_TEST_PREFIX` before running it. The
acceptance runner is serialized because HAProxy package writes mutate shared
pfSense configuration.

Import coverage is intentionally all-or-nothing for implemented resources:
every registered resource implements Terraform import state, has a documented
`terraform import` command, and has unit coverage for its import ID parser.
Registered data sources are lookup-only and cannot be imported.

## Resources

- `pfsense_haproxy_apply`
- `pfsense_haproxy_backend`
- `pfsense_haproxy_backend_acl`
- `pfsense_haproxy_backend_action`
- `pfsense_haproxy_backend_server`
- `pfsense_haproxy_frontend`
- `pfsense_haproxy_frontend_acl`
- `pfsense_haproxy_frontend_action`
- `pfsense_haproxy_frontend_address`
- `pfsense_haproxy_frontend_certificate`
- `pfsense_haproxy_settings`

## Data Sources

- `pfsense_haproxy_apply`
- `pfsense_haproxy_backend`
- `pfsense_haproxy_backend_server`
- `pfsense_haproxy_settings`

## Planned and deferred resources

Deferred pending UAT and security-model gates:

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

resource "pfsense_haproxy_backend_acl" "addressvalidator_host" {
  backend_name = pfsense_haproxy_backend.addressvalidator.name
  name         = "addressvalidator_host"
  expression   = "host_matches"
  value        = "addressvalidator-uat.example.com"
  position     = 0
}

resource "pfsense_haproxy_backend_action" "addressvalidator_route" {
  backend_name = pfsense_haproxy_backend.addressvalidator.name
  key          = "route_addressvalidator_01"
  action       = "use_server"
  acl          = pfsense_haproxy_backend_acl.addressvalidator_host.name
  server       = pfsense_haproxy_backend_server.addressvalidator_01.name
  position     = 0
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

resource "pfsense_haproxy_frontend_address" "addressvalidator_https" {
  frontend_name  = pfsense_haproxy_frontend.addressvalidator.name
  extaddr        = "any_ipv4"
  extaddr_port   = 443
  extaddr_ssl    = true
}

resource "pfsense_haproxy_frontend_certificate" "addressvalidator_https" {
  frontend_name   = pfsense_haproxy_frontend.addressvalidator.name
  ssl_certificate = "existing_uat_certificate_ref"
}

resource "pfsense_haproxy_frontend_acl" "addressvalidator_sni" {
  frontend_name = pfsense_haproxy_frontend.addressvalidator.name
  name          = "addressvalidator_sni"
  expression    = "ssl_sni_matches"
  value         = "addressvalidator-uat.example.com"
  position      = 0
}

resource "pfsense_haproxy_frontend_action" "addressvalidator_route" {
  frontend_name = pfsense_haproxy_frontend.addressvalidator.name
  key           = "route_addressvalidator"
  action        = "use_backend"
  acl           = pfsense_haproxy_frontend_acl.addressvalidator_sni.name
  backend       = pfsense_haproxy_backend.addressvalidator.name
  position      = 0
}

resource "pfsense_haproxy_apply" "addressvalidator" {
  depends_on = [
    pfsense_haproxy_backend.addressvalidator,
    pfsense_haproxy_backend_server.addressvalidator_01,
    pfsense_haproxy_backend_acl.addressvalidator_host,
    pfsense_haproxy_backend_action.addressvalidator_route,
    pfsense_haproxy_frontend.addressvalidator,
    pfsense_haproxy_frontend_address.addressvalidator_https,
    pfsense_haproxy_frontend_certificate.addressvalidator_https,
    pfsense_haproxy_frontend_acl.addressvalidator_sni,
    pfsense_haproxy_frontend_action.addressvalidator_route,
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
    backend_acls = sha1(jsonencode({
      host = {
        name       = pfsense_haproxy_backend_acl.addressvalidator_host.name
        expression = pfsense_haproxy_backend_acl.addressvalidator_host.expression
        value      = pfsense_haproxy_backend_acl.addressvalidator_host.value
        position   = pfsense_haproxy_backend_acl.addressvalidator_host.position
      }
    }))
    backend_actions = sha1(jsonencode({
      route = {
        key      = pfsense_haproxy_backend_action.addressvalidator_route.key
        action   = pfsense_haproxy_backend_action.addressvalidator_route.action
        acl      = pfsense_haproxy_backend_action.addressvalidator_route.acl
        server   = pfsense_haproxy_backend_action.addressvalidator_route.server
        position = pfsense_haproxy_backend_action.addressvalidator_route.position
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
    frontend_addresses = sha1(jsonencode({
      https = {
        extaddr        = pfsense_haproxy_frontend_address.addressvalidator_https.extaddr
        extaddr_custom = pfsense_haproxy_frontend_address.addressvalidator_https.extaddr_custom
        extaddr_port   = pfsense_haproxy_frontend_address.addressvalidator_https.extaddr_port
        extaddr_ssl    = pfsense_haproxy_frontend_address.addressvalidator_https.extaddr_ssl
      }
    }))
    frontend_certificates = sha1(jsonencode({
      https = {
        ssl_certificate = pfsense_haproxy_frontend_certificate.addressvalidator_https.ssl_certificate
      }
    }))
    frontend_acls = sha1(jsonencode({
      sni = {
        name       = pfsense_haproxy_frontend_acl.addressvalidator_sni.name
        expression = pfsense_haproxy_frontend_acl.addressvalidator_sni.expression
        value      = pfsense_haproxy_frontend_acl.addressvalidator_sni.value
        position   = pfsense_haproxy_frontend_acl.addressvalidator_sni.position
      }
    }))
    frontend_actions = sha1(jsonencode({
      route = {
        key      = pfsense_haproxy_frontend_action.addressvalidator_route.key
        action   = pfsense_haproxy_frontend_action.addressvalidator_route.action
        acl      = pfsense_haproxy_frontend_action.addressvalidator_route.acl
        backend  = pfsense_haproxy_frontend_action.addressvalidator_route.backend
        position = pfsense_haproxy_frontend_action.addressvalidator_route.position
      }
    }))
  }
}
```

Remaining schemas will be finalized from approved UAT pfSense REST API endpoint
responses before production use.

## Schema inventory

HAProxy endpoint inventory and provisional resource schema decisions are tracked
under `docs/schema/`. M6 issue #18 UAT closeout evidence, including the
acceptance runbook and endpoint response shape ledger, is tracked in
[`docs/schema/m6-uat-acceptance-evidence.md`](docs/schema/m6-uat-acceptance-evidence.md).
M6 issue #19 production read-only validation workflow and evidence format are
tracked in
[`docs/schema/m6-prod-readonly-validation.md`](docs/schema/m6-prod-readonly-validation.md).
Live schema captures must be generated with:

```bash
python3 scripts/capture_haproxy_schema.py --schema both --output-dir docs/schema
```

Only redacted `haproxy-*.redacted.json` artifacts may be committed. Raw captures,
credentials, certificates, API tokens, and pfSense config backups must stay out
of Git.
