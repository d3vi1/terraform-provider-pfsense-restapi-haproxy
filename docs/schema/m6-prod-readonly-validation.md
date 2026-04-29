# M6 PROD Read-Only Validation Lane

Status: issue #19 runbook and evidence format. This lane is intentionally
read-only. It must not run `terraform apply`, `make testacc`, `go test -tags=acc`,
or `POST /services/haproxy/apply` against production.

## Safety Contract

- `PFSENSE_VALIDATION_ENVIRONMENT` must be exactly `prod`.
- `PFSENSE_READONLY_CONFIRMATION` must be exactly
  `READ_ONLY_PROD_VALIDATION`.
- The default workflow and script use only HTTP `GET`.
- The workflow runs only from `main`, checks out `main`, uses the protected
  `production-readonly` GitHub environment, and scopes production secrets only
  to the precheck/discovery steps.
- Evidence is shape/count/status only by default and is written under ignored
  `validation-evidence/` paths for local runs. The public GitHub workflow does
  not upload production evidence artifacts. It must not include full production
  HAProxy payloads.
- Production write/import/apply work requires a separate issue, an approved
  operator window, and explicit confirmation. This lane does not provide that
  path.

## Manual Workflow

Run the `prod-readonly-validation` GitHub Actions workflow manually and type:

```text
READ_ONLY_PROD_VALIDATION
```

The workflow maps only production-specific secrets into the script:

- `PFSENSE_PROD_ENDPOINT`
- `PFSENSE_PROD_API_KEY`, or `PFSENSE_PROD_USERNAME` and
  `PFSENSE_PROD_PASSWORD`
- `PFSENSE_PROD_INSECURE_TLS`
- `PFSENSE_PROD_TIMEOUT`

The public repository workflow does not upload evidence artifacts. Local runs
write:

- `discovery-shapes.redacted.json`
- `evidence-summary.md`

## Local Read-Only Discovery

```bash
export PFSENSE_ENDPOINT=https://pfsense-prod.example.com
export PFSENSE_API_KEY=...
export PFSENSE_VALIDATION_ENVIRONMENT=prod
export PFSENSE_READONLY_CONFIRMATION=READ_ONLY_PROD_VALIDATION

python3 scripts/prod_readonly_validate.py --output-dir validation-evidence/prod-readonly
```

Dry-run mode validates the gates and prints the static GET allowlist without
network access:

```bash
python3 scripts/prod_readonly_validate.py --dry-run
```

The script reads only:

- `GET /api/v2/schema/openapi`
- `GET /api/v2/schema/native`
- `GET /api/v2/services/haproxy/settings`
- `GET /api/v2/services/haproxy/apply`
- `GET /api/v2/services/haproxy/backends`
- `GET /api/v2/services/haproxy/frontends`
- child plural GET endpoints by discovered `parent_id`

If any discovered backend or frontend lacks an `id`, `_id`, or `uuid`, the
script fails instead of silently skipping child endpoint evidence.

## Terraform Data-Source Plan Check

The example in `examples/prod-readonly-validation/` contains data sources only.
It is for read-only provider validation:

```bash
cd examples/prod-readonly-validation
terraform init -backend=false
terraform validate
terraform plan -input=false -lock=false -detailed-exitcode -no-color
```

Allowed detailed-exitcode values:

- `0`: no diff.
- `2`: output values or refresh-derived changes are present; inspect but do not
  apply.

Exit code `1` is a failed validation and must be investigated before relying on
the lane.

## Import/Refresh Checklist

Import validation is an operator checklist, not part of the default workflow.
It writes only local scratch Terraform state and performs provider reads:

```bash
mkdir -p .local-secrets/prod-import-check
cp examples/prod-readonly-validation/main.tf .local-secrets/prod-import-check/main.tf
cd .local-secrets/prod-import-check

export TF_VAR_pfsense_endpoint="${PFSENSE_ENDPOINT}"
export TF_VAR_pfsense_api_key="${PFSENSE_API_KEY:-}"
export TF_VAR_pfsense_username="${PFSENSE_USERNAME:-}"
export TF_VAR_pfsense_password="${PFSENSE_PASSWORD:-}"
export TF_VAR_pfsense_insecure_tls="${PFSENSE_INSECURE_TLS:-false}"
export TF_VAR_pfsense_timeout="${PFSENSE_TIMEOUT:-30s}"

terraform init -backend=false

cat > imports.tf <<'HCL'
resource "pfsense_haproxy_backend" "example" {
  name = "<backend_name>"
}

resource "pfsense_haproxy_backend_server" "example" {
  backend_name = "<backend_name>"
  name         = "<server_name>"
  address      = "127.0.0.1"
  port         = 1
}
HCL

terraform import pfsense_haproxy_backend.example '<backend_name>'
terraform import pfsense_haproxy_backend_server.example '<backend_name>/<server_name>'

terraform plan -refresh-only -input=false -lock=false -detailed-exitcode -no-color
```

Do not import or apply `pfsense_haproxy_apply` as a resource in production from
this lane. Do not run `terraform apply`. The placeholder server `address` and
`port` values above are only Terraform configuration stubs required before
`terraform import`; `terraform plan -refresh-only` refreshes them from pfSense
without proposing writes.

## Evidence Summary

Record the following before closing issue #19:

- Date, operator, target marked as production, and commit tested.
- Confirmation that the workflow/script used only GET requests.
- Local evidence path or workflow run log reference. Do not upload production
  evidence artifacts from the public repository workflow.
- `terraform validate` and data-source-only plan result, if run.
- Any drift or diagnostics observed, without committing production names,
  addresses, certificates, tokens, or raw configuration.
