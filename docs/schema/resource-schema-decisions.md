# Resource Schema Decisions

Status: provisional from primary pfREST documentation. UAT schema capture is
pending because both live UAT OpenAPI probes timed out from this environment on
2026-04-28.

Primary references:

- https://pfrest.org/SWAGGER_AND_OPENAPI/
- https://pfrest.org/api-docs/

## Cross-Cutting Decisions

- Use `KeyAuth`/`PFSENSE_API_KEY` for automation. Basic auth remains a fallback
  for capture and acceptance work.
- Treat singular object operations as ID-addressed API calls. Published OpenAPI
  documents an `id` query parameter for top-level singular reads, patches, and
  deletes.
- Treat child operations as parent-addressed API calls. Published OpenAPI
  documents `parent_id` plus child `id` for child reads, patches, and deletes.
- Model Terraform resource IDs from stable natural keys. Resolve transient
  parent and child API IDs from plural lookup endpoints immediately before
  writes and deletes.
- Do not use plural `PUT` replace-all endpoints or plural query `DELETE`
  endpoints for normal single-resource lifecycle operations.
- HAProxy write endpoints do not apply pending service changes immediately in
  the public OpenAPI metadata. Use an explicit apply workflow instead of hidden
  automatic reloads.
- Model HAProxy apply as an explicit action resource and read-only status data
  source. Do not add auto-apply flags to settings or durable resources unless a
  later issue intentionally changes that contract.
- Mark actual secret-bearing attributes as sensitive when implemented. Public
  schema names include `stats_password`, `HAProxyFile.content`, certificate or
  private-key bodies, and custom/advanced HAProxy text fields that may carry
  private material.

## Resource Mapping

| Terraform resource | API model | Primary endpoints | Documented create/update shape | Decision |
|--------------------|-----------|-------------------|--------------------------------|----------|
| `pfsense_haproxy_settings` | HAProxySettings | `GET/PATCH /api/v2/services/haproxy/settings` | Settings patch body is partial; no create/delete. | Implemented as split model in M2-01: data source reads settings; resource is singleton import-first with fixed ID `settings`, create blocked, update PATCH only, delete state-only, no apply/reload. |
| `pfsense_haproxy_backend` | HAProxyBackend | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend`; `GET /backends` | Create requires `name`; public OpenAPI also marks conditional `agent_port` and `persist_cookie_name` as required when agent checks or cookie persistence are enabled. Patch requires `id`. | Implemented in M3-01 as a durable top-level resource using backend `name` as Terraform's stable natural key. Before update/delete, resolve pfSense's current object ID from `GET /backends?name=...`. M3-03 also adds a lookup data source by exact name that returns selected scalar fields without requiring or exposing the transient REST `id`. Manage selected scalar fields only; keep embedded servers, ACLs, actions, error files, stats, and advanced pass-through fields out of scope pending UAT ownership confirmation. |
| `pfsense_haproxy_backend_server` | HAProxyBackendServer | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/server`; `GET /backend/servers` | Create requires `parent_id`, `name`, `address`, `port`. Patch requires `parent_id`, `id`. | Implemented in M3-02 as a backend child resource using `backend_name/server_name` as Terraform's stable natural key. Before create/update/delete, resolve the current parent backend ID by backend name; before update/delete, resolve the current child ID by server name under that parent. M3-03 also adds a lookup data source by exact parent/backend name and server name; it uses the parent REST `id` only for the child query and does not require or expose the child REST `id`. Manage only `address`, `port`, `status`, `weight`, `ssl`, and `sslserververify`; expose read-only `serverid`; defer `advanced` until UAT validates sensitivity and ownership. |
| `pfsense_haproxy_backend_acl` | HAProxyBackendACL | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/acl` | Create requires `parent_id`, `name`, `expression`, `value`. Patch requires `parent_id`, `id`. | Child resource under backend. |
| `pfsense_haproxy_backend_action` | HAProxyBackendAction | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/action` | Create requires `parent_id` plus action-specific fields in the public schema. | Child resource under backend. Conditional fields need UAT examples before strict Terraform validation. |
| `pfsense_haproxy_backend_error_file` | HAProxyBackendErrorFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/error_file` | Create requires `parent_id`, `errorcode`, `errorfile`. | Child resource under backend. Consider later if required for managed routes. |
| `pfsense_haproxy_frontend` | HAProxyFrontend | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend`; `GET /frontends` | Create requires `name` and `type`. Patch requires `id`. | Implemented in M4-01 as a durable top-level resource using frontend `name` as Terraform's stable natural key. Before update/delete, resolve pfSense's current object ID from `GET /frontends?name=...`. Manage only `name`, `type`, and selected scalar fields: `descr`, `status`, `max_connections`, `backend_serverpool`, `socket_stats`, `dontlognull`, `dontlog_normal`, `log_separate_errors`, `log_detailed`, `client_timeout`, `forwardfor`, and `httpclose`. Support `http` and `tcp` only; defer any separate `https` frontend type mode until UAT validates how it interacts with frontend address SSL offload and certificate attachments. Keep addresses, ACLs, actions, error files, `advanced`, `advanced_bind`, and default certificate ownership out of scope for this top-level resource. |
| `pfsense_haproxy_frontend_address` | HAProxyFrontendAddress | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/address`; `GET /frontend/addresses` | Create requires `parent_id`, `extaddr`, `extaddr_custom`. Patch requires `parent_id`, `id`. | Implemented in M4-02 as a frontend child resource using `frontend_name/extaddr/extaddr_custom_or_-/extaddr_port` as Terraform's stable natural key. Before create/update/delete, resolve the current parent frontend ID by frontend name; before update/delete, resolve the current child ID by exact address fields under that parent. Manage only `extaddr`, `extaddr_custom`, `extaddr_port`, and `extaddr_ssl`; defer `exaddr_advanced` and placement until UAT validates sensitivity and ordering semantics. |
| `pfsense_haproxy_frontend_acl` | HAProxyFrontendACL | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/acl` | Create requires `parent_id`, `name`, `expression`, `value`. Patch requires `parent_id`, `id`. | Child resource under frontend. |
| `pfsense_haproxy_frontend_action` | HAProxyFrontendAction | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/action` | Create requires `parent_id` plus action-specific fields in the public schema. | Child resource under frontend. Conditional fields need UAT examples before strict Terraform validation. |
| `pfsense_haproxy_frontend_certificate` | HAProxyFrontendCertificate | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/certificate`; `GET /frontend/certificates` | Create requires `parent_id`; schema exposes `ssl_certificate`. | Implemented in M4-03 as a frontend child resource using `frontend_name/ssl_certificate` as Terraform's stable natural key. Before create/delete, resolve the current parent frontend ID by frontend name; before delete, resolve the current child ID by exact certificate reference under that parent. Manage only existing `ssl_certificate` references; reject PEM/private-key-looking values; do not expose certificate bodies, private keys, placement, advanced fields, or a PATCH update path. |
| `pfsense_haproxy_frontend_error_file` | HAProxyFrontendErrorFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/error_file` | Create requires `parent_id`, `errorcode`, `errorfile`. | Child resource under frontend. Consider later if required for managed routes. |
| `pfsense_haproxy_file` | HAProxyFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/file`; `GET /files` | Create requires `name`, `content`. Patch requires `id`. | Defer until routes require managed HAProxy files. `content` must be sensitive in Terraform state and excluded from schema fixtures. |
| `pfsense_haproxy_settings_dns_resolver` | HAProxyDNSResolver | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/settings/dns_resolver` | Create requires `name`, `server`. Patch requires `id`. | Model as settings child only if needed. Pending UAT confirmation of nesting and IDs. |
| `pfsense_haproxy_settings_email_mailer` | HAProxyEmailMailer | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/settings/email_mailer` | Create requires `name`, `mailserver`. Patch requires `id`. | Model as settings child only if needed. Pending UAT confirmation of nesting and IDs. |
| `pfsense_haproxy_apply` | HAProxyApply | `GET/POST /api/v2/services/haproxy/apply` | `GET` reports `applied`; `POST` applies pending changes. | Implemented in M2-02 as a status data source and singleton action resource with fixed ID `apply`, user-controlled `triggers`, POST guarded by the shared client write guard, bounded polling, state-only delete, and no hidden apply behavior in other resources. |

## Pending UAT Questions

- Confirm the implemented scalar field response names and primitive types for
  `pfsense_haproxy_settings` on the approved UAT firewall.
- Confirm `advanced` live behavior on UAT. Upstream pfREST `Base64Field`
  documentation says API representation is decoded plain text while pfSense
  stores it encoded internally.
- Confirm no endpoint-side `apply` default is triggered by
  `PATCH /api/v2/services/haproxy/settings` when no apply control parameter is
  supplied.
- Confirm `GET /api/v2/services/haproxy/apply` returns a boolean `applied`
  field on UAT and whether any extra error/status fields should be exposed.
- Confirm `POST /api/v2/services/haproxy/apply` returns quickly enough for the
  current poll defaults or whether UAT needs different timeout guidance.
- Exact response envelope for create/update/delete, including whether object IDs
  are returned directly or must always be rediscovered from plural GET endpoints.
- Whether UAT uses numeric IDs, string IDs, or mixed IDs for every HAProxy model.
- Confirm `GET /api/v2/services/haproxy/backends?name=...` returns exact-name
  matches or, at minimum, a list containing `id` and `name` fields that can be
  filtered client-side.
- Confirm `GET /api/v2/services/haproxy/backend/servers?parent_id=...&name=...`
  returns exact-name child matches or, at minimum, a list containing `id`,
  `name`, `address`, and `port` fields that can be filtered client-side.
- Confirm `GET /api/v2/services/haproxy/frontends?name=...` returns exact-name
  matches or, at minimum, a list containing `id`, `name`, and `type` fields
  that can be filtered client-side.
- Confirm `GET /api/v2/services/haproxy/frontend/addresses?parent_id=...&extaddr=...&extaddr_custom=...&extaddr_port=...`
  returns exact frontend address matches or, at minimum, a list containing
  `id`, `extaddr`, `extaddr_custom`, and `extaddr_port` fields that can be
  filtered client-side.
- Confirm the implemented frontend scalar field response names and primitive
  types on the approved UAT firewall, including `httpclose` enum values and
  whether `forwardfor` is rejected for TCP frontends server-side.
- Confirm frontend address `placement` behavior before exposing any ordering
  control. The first implementation intentionally does not set placement.
- Confirm `GET /api/v2/services/haproxy/frontend/certificates?parent_id=...&ssl_certificate=...`
  returns exact frontend certificate attachment matches or, at minimum, a list
  containing `id` and `ssl_certificate` fields that can be filtered
  client-side.
- Confirm whether frontend certificate attachments support ordering/placement
  semantics on UAT. The first implementation intentionally does not set
  placement and treats changes as replacement-only.
- Whether published conditional fields such as `agent_port` and
  `persist_cookie_name` are required only when their enabling booleans are true
  on UAT.
- Whether `https` frontend type should become a separate resource mode after
  frontend certificate attachment behavior is validated on UAT.
- Whether conditional action fields can be omitted when unrelated to the chosen
  action.
- Whether HAProxy and HAProxy-devel expose identical schema paths on this UAT
  firewall.
