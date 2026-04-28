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
- Model Terraform resource IDs to preserve both parent and child API IDs for
  child resources. The exact serialized ID format remains pending UAT response
  confirmation.
- Do not use plural `PUT` replace-all endpoints or plural query `DELETE`
  endpoints for normal single-resource lifecycle operations.
- HAProxy write endpoints do not apply pending service changes immediately in
  the public OpenAPI metadata. Use an explicit apply workflow instead of hidden
  automatic reloads.
- Mark actual secret-bearing attributes as sensitive when implemented. Public
  schema names include `stats_password`, `HAProxyFile.content`, certificate
  references, and custom/advanced HAProxy text fields that may carry private
  material.

## Resource Mapping

| Terraform resource | API model | Primary endpoints | Documented create/update shape | Decision |
|--------------------|-----------|-------------------|--------------------------------|----------|
| `pfsense_haproxy_settings` | HAProxySettings | `GET/PATCH /api/v2/services/haproxy/settings` | Settings patch body is partial; no create/delete. | Implemented as split model in M2-01: data source reads settings; resource is singleton import-first with fixed ID `settings`, create blocked, update PATCH only, delete state-only, no apply/reload. |
| `pfsense_haproxy_backend` | HAProxyBackend | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend`; `GET /backends` | Create requires `name`, `agent_port`, and `persist_cookie_name` in public OpenAPI. Patch requires `id`. | Durable top-level resource. Keep embedded collections read-only or ignored when managed by child resources. |
| `pfsense_haproxy_backend_server` | HAProxyBackendServer | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/server` | Create requires `parent_id`, `name`, `address`, `port`. Patch requires `parent_id`, `id`. | Child resource under backend. Terraform ID must retain backend API ID and server API ID. |
| `pfsense_haproxy_backend_acl` | HAProxyBackendACL | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/acl` | Create requires `parent_id`, `name`, `expression`, `value`. Patch requires `parent_id`, `id`. | Child resource under backend. |
| `pfsense_haproxy_backend_action` | HAProxyBackendAction | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/action` | Create requires `parent_id` plus action-specific fields in the public schema. | Child resource under backend. Conditional fields need UAT examples before strict Terraform validation. |
| `pfsense_haproxy_backend_error_file` | HAProxyBackendErrorFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/backend/error_file` | Create requires `parent_id`, `errorcode`, `errorfile`. | Child resource under backend. Consider later if required for managed routes. |
| `pfsense_haproxy_frontend` | HAProxyFrontend | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend`; `GET /frontends` | Create requires `name` and `type`. Patch requires `id`. | Durable top-level resource. Keep addresses, ACLs, actions, certificates, and error files as child resources where possible. |
| `pfsense_haproxy_frontend_address` | HAProxyFrontendAddress | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/address` | Create requires `parent_id`, `extaddr`, `extaddr_custom`. Patch requires `parent_id`, `id`. | Child resource under frontend. |
| `pfsense_haproxy_frontend_acl` | HAProxyFrontendACL | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/acl` | Create requires `parent_id`, `name`, `expression`, `value`. Patch requires `parent_id`, `id`. | Child resource under frontend. |
| `pfsense_haproxy_frontend_action` | HAProxyFrontendAction | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/action` | Create requires `parent_id` plus action-specific fields in the public schema. | Child resource under frontend. Conditional fields need UAT examples before strict Terraform validation. |
| `pfsense_haproxy_frontend_certificate` | HAProxyFrontendCertificate | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/certificate` | Create requires `parent_id`; schema exposes `ssl_certificate`. | Child resource under frontend. Treat certificate identifiers as sensitive-adjacent and never capture certificate bodies. |
| `pfsense_haproxy_frontend_error_file` | HAProxyFrontendErrorFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/frontend/error_file` | Create requires `parent_id`, `errorcode`, `errorfile`. | Child resource under frontend. Consider later if required for managed routes. |
| `pfsense_haproxy_file` | HAProxyFile | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/file`; `GET /files` | Create requires `name`, `content`. Patch requires `id`. | Defer until routes require managed HAProxy files. `content` must be sensitive in Terraform state and excluded from schema fixtures. |
| `pfsense_haproxy_settings_dns_resolver` | HAProxyDNSResolver | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/settings/dns_resolver` | Create requires `name`, `server`. Patch requires `id`. | Model as settings child only if needed. Pending UAT confirmation of nesting and IDs. |
| `pfsense_haproxy_settings_email_mailer` | HAProxyEmailMailer | `GET/POST/PATCH/DELETE /api/v2/services/haproxy/settings/email_mailer` | Create requires `name`, `mailserver`. Patch requires `id`. | Model as settings child only if needed. Pending UAT confirmation of nesting and IDs. |
| `pfsense_haproxy_apply` | HAProxyApply | `GET/POST /api/v2/services/haproxy/apply` | `GET` reports `applied`; `POST` applies pending changes. | Not a durable config resource. Implement as an explicit apply/action workflow after CRUD resources exist. |

## Pending UAT Questions

- Confirm the implemented scalar field response names and primitive types for
  `pfsense_haproxy_settings` on the approved UAT firewall.
- Confirm whether `advanced` is returned and accepted as Base64 text, matching
  the upstream `Base64Field` model.
- Confirm no endpoint-side `apply` default is triggered by
  `PATCH /api/v2/services/haproxy/settings` when no apply control parameter is
  supplied.
- Exact response envelope for create/update/delete, including where object IDs are
  returned.
- Whether UAT uses numeric IDs, string IDs, or mixed IDs for every HAProxy model.
- Whether published required fields such as `agent_port` and
  `persist_cookie_name` are truly required by UAT for backend creation.
- Whether conditional action fields can be omitted when unrelated to the chosen
  action.
- Whether HAProxy and HAProxy-devel expose identical schema paths on this UAT
  firewall.
