# M6 UAT Acceptance Evidence

Status: closeout evidence template for issue #18. This document does not claim a
live UAT run from the current worker environment. As of 2026-04-29, this
environment does not have `PFSENSE_*` variables configured, so the evidence here
is the reproducible runbook, cleanup contract, #42-#49 traceability, and
endpoint response shape ledger to complete during an approved UAT run.

Live UAT evidence must be filled only from an approved UAT firewall run. Do not
run these tests against production unless a future issue explicitly targets
production and the operator confirms the window.

## Runbook

Use a clean checkout of the target branch and run acceptance tests in the
foreground:

```bash
export PFSENSE_ENDPOINT=https://pfsense-uat.example.com
export PFSENSE_API_KEY=...
# Or, if API keys are unavailable:
export PFSENSE_USERNAME=...
export PFSENSE_PASSWORD=...

export PFSENSE_INSECURE_TLS=true
export PFSENSE_TIMEOUT=30s
export PFSENSE_TEST_ENVIRONMENT=uat
export PFSENSE_TEST_PREFIX=uat_haproxy_tf_issue18_YYYYMMDD

# Required for full #18 coverage; tests skip these slices when absent.
export PFSENSE_TEST_CERTIFICATE_REF=existing_uat_certificate_ref
export PFSENSE_TEST_CUSTOM_IPV4=192.0.2.10
export PFSENSE_TEST_CUSTOM_IPV6=2001:db8::10

make testacc
```

Replace placeholder endpoint, certificate, and custom IP values with approved
UAT-only values before running. The example custom IPs above document the
variable names; they are not production addresses.

`make testacc` sets `TF_ACC=1` and runs:

```bash
go test -tags=acc ./... -count=1 -p=1
```

The `-p=1` serialization is part of the safety contract because HAProxy package
writes mutate shared pfSense configuration.

## Safety And Cleanup Contract

- `PFSENSE_TEST_ENVIRONMENT` must be exactly `uat`; the acceptance precheck
  fails for any other value. Production is not an accepted target for #18.
- `PFSENSE_TEST_PREFIX` is required and must match `^[A-Za-z0-9._-]+$`.
  Resource names are generated as `prefix_suffix_8hex` using `crypto/rand`, so
  each run is prefix-scoped and collision-resistant.
- Prefixes must be unique per operator run, for example
  `uat_haproxy_tf_issue18_20260429`. Reusing a prefix is allowed only when the
  operator is intentionally resuming cleanup for that exact run.
- Acceptance resources stay disabled or unattached where possible. Backend
  server tests use disabled loopback targets, frontend tests use disabled
  frontends, and backend ACL/action tests do not attach the backend to a live
  frontend.
- Terraform acceptance destroy exercises provider delete paths after each
  `resource.TestCase`. Where a resource has additional live cleanup risk, add
  or keep a `CheckDestroy`/`TestCheckDestroy` style helper that queries the
  plural endpoint and proves no prefixed object remains.
- Child resource removal must be followed by an explicit
  `pfsense_haproxy_apply`. #47 and #48 include cleanup configurations that
  remove ACL/action children, change the apply trigger, and call
  `testAccCheckHaproxyApplyAfterDestroy()` so pfSense applies the deletion.
- Durable HAProxy resources must not auto-apply. Every acceptance configuration
  uses `pfsense_haproxy_apply` with explicit triggers and checks
  `status = "done"`.
- If an acceptance run fails before Terraform destroy, clean up every prefixed
  top-level object matching the exact `PFSENSE_TEST_PREFIX` and every HAProxy
  child below those parents. Several child resources use fixed local names such
  as `app01`, `host_acl`, `path_acl`, `route_app01`, or `imported_header`; they
  are safe to remove only when their parent backend/frontend is part of the same
  prefixed test run. Run `pfsense_haproxy_apply` after cleanup before declaring
  the firewall clean.

## #18 Traceability

| #18 acceptance item | Merged coverage | Closeout evidence status |
|---------------------|-----------------|--------------------------|
| `make testacc` creates, updates, applies, imports, and destroys UAT HAProxy config. | #42 adds the harness; #43-#49 add resource lifecycle tests. Create/update/apply/import are explicit in test steps. Destroy is exercised by Terraform acceptance teardown and provider delete paths. | Runbook above is ready. Live UAT run result is pending approved UAT execution. |
| Test names are prefixed and collision-resistant. | #42 adds `testAccPreCheck`, required `PFSENSE_TEST_PREFIX`, prefix regex validation, and `testAccResourceName` with an 8-hex random suffix. #44 also derives isolated port ranges from generated names. | Documented in the safety contract. |
| Cleanup behavior is documented. | #42 documents serialized UAT-only execution; #47 and #48 add explicit child-removal cleanup apply checks. | Documented in the safety contract. Live cleanup evidence pending UAT run. |
| Cover `pfsense_haproxy_frontend` create/update/import/delete for HTTP and TCP, TCP rejection of HTTP-only options, and HTTP-to-TCP clearing. | #43 adds disabled frontend HTTP-to-TCP import/apply coverage and a negative TCP HTTP-only field test. | Endpoint shape rows `/frontend` and `/frontends` pending UAT fill-in. |
| Cover `pfsense_haproxy_frontend_address` create/update/import/delete for IPv4, IPv6, `any`, and `custom`, including custom IPv6 canonicalization. | #44 adds built-in selector coverage for `any_ipv4`, `any_ipv6`, `localhost_ipv4`, and `localhost_ipv6`; custom IPv4/IPv6 coverage runs when `PFSENSE_TEST_CUSTOM_IPV4` and `PFSENSE_TEST_CUSTOM_IPV6` are set. #49 adds `extaddr_ssl` update steps before import for built-in and custom addresses, and asserts provider state/import IDs use the parsed canonical custom IPv6 form after planning and live read. | Endpoint shape rows `/frontend/address` and `/frontend/addresses` pending UAT fill-in. Closeout remains blocked until an approved UAT run records the response-shape evidence. |
| Cover `pfsense_haproxy_frontend_certificate` attach/import/delete of existing certificate references without PEM/private key material. | #45 adds certificate attachment coverage gated by `PFSENSE_TEST_CERTIFICATE_REF`, including SSL-enabled address dependency and import verification. | Endpoint shape rows `/frontend/certificate` and `/frontend/certificates` pending UAT fill-in. |
| Cover backend ACL/action create/update/import/delete, import adoption, deterministic order/reorder, and generated HAProxy validation where practical. | #46 adds backend/server foundation coverage. #47 adds backend ACL/action create, update/reorder, ACL imports, action import adoption through an unmanaged API-created action, explicit apply, and cleanup apply after child removal. Successful `pfsense_haproxy_apply.status = "done"` is the provider-observable generated-config validation point. | Endpoint shape rows `/backend`, `/backends`, `/backend/server`, `/backend/servers`, `/backend/acl`, `/backend/acls`, `/backend/action`, `/backend/actions`, and `/apply` pending UAT fill-in. |
| Cover frontend ACL/action create/update/import/delete, SNI/host route behavior, import adoption, deterministic order/reorder, explicit apply behavior, and generated HAProxy validation where practical. | #48 adds disabled frontend ACL/action create, update/reorder, ACL imports, action import adoption through an unmanaged API-created action, route to isolated backend, explicit apply, and cleanup apply after child removal. Successful `pfsense_haproxy_apply.status = "done"` is the provider-observable generated-config validation point. | Endpoint shape rows `/frontend/acl`, `/frontend/acls`, `/frontend/action`, `/frontend/actions`, and `/apply` pending UAT fill-in. |
| Record UAT response shape evidence for M4/M5 endpoints. | #42-#49 exercise the endpoints when run against UAT. | Ledger below is the required response shape evidence record. Fill it from the approved UAT run; do not backfill from public docs alone. |

## PR Coverage Ledger

| PR | Merged commit | Acceptance slice | Primary files |
|----|---------------|------------------|---------------|
| #42 | `7efd963` | UAT-only acceptance harness, serialized runner, auth/precheck contract, required prefix. | `internal/provider/acceptance_test.go`, `.github/workflows/acceptance.yml`, `README.md`, `CONTRIBUTING.md` |
| #43 | `7a849f7` | Disabled frontend lifecycle, HTTP-to-TCP update, import, TCP HTTP-only field rejection. | `internal/provider/haproxy_frontend_acc_test.go` |
| #44 | `7864943` | Frontend address built-in selectors plus optional custom IPv4/IPv6 coverage and imports. | `internal/provider/haproxy_frontend_address_acc_test.go` |
| #45 | `25112bc` | Frontend certificate attachment using an existing certificate reference and import. | `internal/provider/haproxy_frontend_certificate_acc_test.go` |
| #46 | `50abf26` | Backend plus disabled backend server lifecycle, safe update, apply, imports. | `internal/provider/haproxy_backend_server_acc_test.go` |
| #47 | `68845cd` | Backend ACL/action lifecycle, reorder, imports, import adoption, cleanup apply. | `internal/provider/haproxy_backend_acl_action_acc_test.go` |
| #48 | `f12f8d5` | Frontend ACL/action lifecycle, reorder, imports, import adoption, cleanup apply. | `internal/provider/haproxy_frontend_acl_action_acc_test.go` |
| #49 | Pending | Frontend address `extaddr_ssl` update coverage for built-in/custom addresses and custom IPv6 canonical state assertions. | `internal/provider/haproxy_frontend_address_acc_test.go` |

## Endpoint Response Shape Ledger

Fill the `UAT evidence` column with a redacted run artifact reference, a checked
schema capture, or a short operator note from the approved UAT run. Never paste
credentials, certificate bodies, private keys, bearer tokens, raw config dumps,
or customer-sensitive HAProxy payloads into this file.

| Endpoint | Used by | Minimum response shape to confirm on UAT | UAT evidence |
|----------|---------|-------------------------------------------|--------------|
| `/services/haproxy/backend` | `pfsense_haproxy_backend` parent create/update/delete for backend server and ACL/action slices | `POST`/`PATCH` accept `name` and selected scalar fields; `PATCH`/`DELETE` accept transient `id`; response envelope does not need to be used for identity because the provider re-queries `/backends`. | Pending approved UAT run. |
| `/services/haproxy/backends` | `pfsense_haproxy_backend` parent lookup/import/read for backend server and ACL/action slices | `GET` returns a list or envelope of backend objects with at least `id` and `name`; name query filtering is exact or client-side exact filtering is possible. | Pending approved UAT run. |
| `/services/haproxy/frontend` | `pfsense_haproxy_frontend` singular create/update/delete | `POST`/`PATCH` accept `name`, `type`, and selected scalar fields; `PATCH`/`DELETE` accept transient `id`; response envelope does not need to be used for identity because the provider re-queries `/frontends`. | Pending approved UAT run. |
| `/services/haproxy/frontends` | `pfsense_haproxy_frontend` lookup/import/read | `GET` returns a list or envelope of frontend objects with at least `id`, `name`, and `type`; name query filtering is exact or client-side exact filtering is possible. | Pending approved UAT run. |
| `/services/haproxy/frontend/address` | `pfsense_haproxy_frontend_address` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `extaddr`, `extaddr_custom`, `extaddr_port`, and `extaddr_ssl`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/frontend/addresses` | `pfsense_haproxy_frontend_address` lookup/import/read | `GET` accepts `parent_id` and address fields or returns a filterable list with at least `id`, `extaddr`, `extaddr_custom`, `extaddr_port`, and `extaddr_ssl`; custom IPv6 values are parseable and stable enough for the provider to normalize into canonical Terraform state. | Pending approved UAT run. |
| `/services/haproxy/frontend/certificate` | `pfsense_haproxy_frontend_certificate` singular create/delete | `POST` accepts `parent_id` and existing `ssl_certificate` reference; `DELETE` accepts `parent_id` and child `id`; response must not require PEM or private key material in Terraform. | Pending approved UAT run. |
| `/services/haproxy/frontend/certificates` | `pfsense_haproxy_frontend_certificate` lookup/import/read | `GET` accepts `parent_id` and `ssl_certificate` or returns a filterable list with at least `id` and `ssl_certificate`; no certificate body or private key is needed for state. | Pending approved UAT run. |
| `/services/haproxy/backend/acl` | `pfsense_haproxy_backend_acl` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `name`, `expression`, `value`, boolean flags, and optional `placement`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/backend/acls` | `pfsense_haproxy_backend_acl` lookup/import/read/order | `GET` accepts `parent_id` and `name` or returns a filterable ordered list with at least `id`, `name`, `expression`, and `value`; list order is stable enough to derive `position`. | Pending approved UAT run. |
| `/services/haproxy/backend/action` | `pfsense_haproxy_backend_action` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `action`, `acl`, action-specific fields such as `server`, `name`, and `fmt`, plus optional `placement`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/backend/actions` | `pfsense_haproxy_backend_action` lookup/import/read/order | `GET` returns an ordered list or envelope with transient `id`, `action`, `acl`, and action-specific fields; duplicate normalized payloads are detectable; list order is stable enough to derive `position`. | Pending approved UAT run. |
| `/services/haproxy/frontend/acl` | `pfsense_haproxy_frontend_acl` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `name`, `expression`, `value`, boolean flags, and optional `placement`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/frontend/acls` | `pfsense_haproxy_frontend_acl` lookup/import/read/order | `GET` accepts `parent_id` and `name` or returns a filterable ordered list with at least `id`, `name`, `expression`, and `value`; list order is stable enough to derive `position`. | Pending approved UAT run. |
| `/services/haproxy/frontend/action` | `pfsense_haproxy_frontend_action` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `action`, `acl`, action-specific fields such as `backend`, `name`, and `fmt`, plus optional `placement`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/frontend/actions` | `pfsense_haproxy_frontend_action` lookup/import/read/order | `GET` returns an ordered list or envelope with transient `id`, `action`, `acl`, and action-specific fields; route actions expose enough data to match `use_backend`; list order is stable enough to derive `position`. | Pending approved UAT run. |
| `/services/haproxy/backend/server` | `pfsense_haproxy_backend_server` singular create/update/delete | `POST`/`PATCH` accept `parent_id`, `name`, `address`, `port`, `status`, `weight`, `ssl`, and `sslserververify`; `PATCH`/`DELETE` accept transient child `id`. | Pending approved UAT run. |
| `/services/haproxy/backend/servers` | `pfsense_haproxy_backend_server` lookup/import/read | `GET` accepts `parent_id` and `name` or returns a filterable list with at least `id`, `name`, `address`, and `port`; optional `serverid` can be recorded for drift visibility. | Pending approved UAT run. |
| `/services/haproxy/apply` | `pfsense_haproxy_apply` data source/resource and cleanup apply | `GET` returns an `applied` boolean; `POST` starts applying pending HAProxy changes; polling reaches `applied = true` and Terraform exposes `status = "done"` after create/update and after cleanup child removal. | Pending approved UAT run. |

## Closeout Criteria For Approved UAT Run

Before closing #18, attach or link the approved UAT run evidence and update this
document or the issue with:

- Date, operator, target marked as UAT, and exact branch/commit tested.
- Environment contract used, with secrets masked and only token presence noted.
- `make testacc` command and final exit code.
- Confirmation that `PFSENSE_TEST_CERTIFICATE_REF`, `PFSENSE_TEST_CUSTOM_IPV4`,
  and `PFSENSE_TEST_CUSTOM_IPV6` were set and that the certificate and custom
  address slices ran. A #18 closeout run is incomplete if those slices are
  skipped.
- Response shape evidence for every ledger row above.
- Cleanup confirmation by `PFSENSE_TEST_PREFIX`, including explicit
  `pfsense_haproxy_apply` after child removal.
