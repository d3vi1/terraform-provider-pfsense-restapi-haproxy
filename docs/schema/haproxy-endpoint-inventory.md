# HAProxy Endpoint Inventory

Status: pending UAT confirmation. The live UAT schema probes for
`https://192.168.51.254/api/v2/schema/openapi` and
`https://192.168.31.254/api/v2/schema/openapi` timed out from this environment
on 2026-04-28, so this inventory uses the primary pfREST public OpenAPI reference
instead of a live fixture.

Primary references:

- https://pfrest.org/SWAGGER_AND_OPENAPI/
- https://pfrest.org/NATIVE_SCHEMA/
- https://pfrest.org/api-docs/

The public OpenAPI reference reports `pfSense REST API Documentation` version
`v2.7.6`. Every HAProxy endpoint below lists `pfSense-pkg-haproxy` as its
required package and supports `BasicAuth`, `JWTAuth`, and `KeyAuth` in the
published documentation.

| Path | Methods | Type | Model | Required package | UAT |
|------|---------|------|-------|------------------|-----|
| `/api/v2/services/haproxy/apply` | GET, POST | Singular | HAProxyApply | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/acl` | GET, POST, PATCH, DELETE | Singular | HAProxyBackendACL | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/acls` | GET, DELETE | Plural | HAProxyBackendACL | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/action` | GET, POST, PATCH, DELETE | Singular | HAProxyBackendAction | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/actions` | GET, DELETE | Plural | HAProxyBackendAction | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend` | GET, POST, PATCH, DELETE | Singular | HAProxyBackend | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/error_file` | GET, POST, PATCH, DELETE | Singular | HAProxyBackendErrorFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/errorfiles` | GET, DELETE | Plural | HAProxyBackendErrorFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/backend/server` | GET, POST, PATCH, DELETE | Singular | HAProxyBackendServer | pfSense-pkg-haproxy | Implemented in M3-02; UAT pending |
| `/api/v2/services/haproxy/backend/servers` | GET, DELETE | Plural | HAProxyBackendServer | pfSense-pkg-haproxy | Implemented in M3-02; UAT pending |
| `/api/v2/services/haproxy/backends` | GET, PUT, DELETE | Plural | HAProxyBackend | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/file` | GET, POST, PATCH, DELETE | Singular | HAProxyFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/files` | GET, PUT, DELETE | Plural | HAProxyFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/acl` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontendACL | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/acls` | GET, DELETE | Plural | HAProxyFrontendACL | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/action` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontendAction | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/actions` | GET, DELETE | Plural | HAProxyFrontendAction | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/address` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontendAddress | pfSense-pkg-haproxy | Implemented in M4-02 for POST/PATCH/DELETE; UAT pending |
| `/api/v2/services/haproxy/frontend/addresses` | GET, DELETE | Plural | HAProxyFrontendAddress | pfSense-pkg-haproxy | Implemented in M4-02 for GET lookup only; plural DELETE intentionally unused; UAT pending |
| `/api/v2/services/haproxy/frontend/certificate` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontendCertificate | pfSense-pkg-haproxy | Implemented in M4-03 for POST/DELETE; PATCH intentionally unused; UAT pending |
| `/api/v2/services/haproxy/frontend/certificates` | GET, DELETE | Plural | HAProxyFrontendCertificate | pfSense-pkg-haproxy | Implemented in M4-03 for GET lookup only; plural DELETE intentionally unused; UAT pending |
| `/api/v2/services/haproxy/frontend` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontend | pfSense-pkg-haproxy | Implemented in M4-01; UAT pending |
| `/api/v2/services/haproxy/frontend/error_file` | GET, POST, PATCH, DELETE | Singular | HAProxyFrontendErrorFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontend/error_files` | GET, DELETE | Plural | HAProxyFrontendErrorFile | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/frontends` | GET, PUT, DELETE | Plural | HAProxyFrontend | pfSense-pkg-haproxy | Implemented in M4-01 for GET lookup only; plural PUT/DELETE intentionally unused; UAT pending |
| `/api/v2/services/haproxy/settings/dns_resolver` | GET, POST, PATCH, DELETE | Singular | HAProxyDNSResolver | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/settings/dns_resolvers` | GET, DELETE | Plural | HAProxyDNSResolver | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/settings/email_mailer` | GET, POST, PATCH, DELETE | Singular | HAProxyEmailMailer | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/settings/email_mailers` | GET, DELETE | Plural | HAProxyEmailMailer | pfSense-pkg-haproxy | Pending |
| `/api/v2/services/haproxy/settings` | GET, PATCH | Singular | HAProxySettings | pfSense-pkg-haproxy | Pending |

## Notes For UAT Confirmation

- Confirm whether UAT exposes the same path list and method set.
- Confirm package name and version metadata from `/api/v2/schema/native`.
- Confirm singular object ID behavior and child `parent_id` behavior from live
  schema metadata before implementing import or drift detection.
- Do not use plural `PUT` replacement or plural `DELETE` endpoints from
  Terraform resources until an import/migration plan exists.
