# HAProxy Schema Capture Workflow

This directory is for redacted pfSense-pkg-RESTAPI schema metadata used to design
the Terraform provider resources. It must never contain live credentials,
certificates, private keys, API tokens, VPN material, pfSense `config.xml`
backups, or unredacted HAProxy configuration dumps.

## Sources

- pfREST guide: https://pfrest.org/SWAGGER_AND_OPENAPI/
- Native schema guide: https://pfrest.org/NATIVE_SCHEMA/
- Public OpenAPI reference: https://pfrest.org/api-docs/

The public OpenAPI reference is useful for initial endpoint inventory. UAT remains
the source of truth for this deployment because installed pfSense, package, and
pfSense-pkg-RESTAPI versions can differ from the public reference.

## Capture

Use only approved UAT targets. The script reads schema endpoints only:

```bash
export PFSENSE_ENDPOINT=https://pfsense.example.com
export PFSENSE_API_KEY=...
export PFSENSE_INSECURE_TLS=true

python3 scripts/capture_haproxy_schema.py --schema both --output-dir docs/schema
```

Username/password fallback is supported when API keys are unavailable:

```bash
export PFSENSE_USERNAME=...
export PFSENSE_PASSWORD=...
```

The script fetches:

- `/api/v2/schema/openapi`
- `/api/v2/schema/native`

It filters HAProxy paths containing `/api/v2/services/haproxy`, keeps referenced
HAProxy schemas/models, redacts sensitive scalar values, and writes:

- `docs/schema/haproxy-openapi.redacted.json`
- `docs/schema/haproxy-native.redacted.json`

Raw captures are intentionally ignored by Git and must not be committed.

## Pre-Commit Review

Before committing any generated JSON:

```bash
python3 scripts/capture_haproxy_schema.py --dry-run
jq empty docs/schema/haproxy-*.redacted.json
rg -n -- '-----BEGIN|PRIVATE KEY|<pfsense|Authorization:|Bearer [A-Za-z0-9]|Basic [A-Za-z0-9]|PFSENSE_(API_KEY|PASSWORD)' docs/schema
```

If the final `rg` command returns anything other than intentional policy text in
Markdown docs, stop and redact or remove the artifact before committing.

## Current UAT Status

As of 2026-04-28, live UAT schema capture is blocked from this environment:

- `https://192.168.51.254/api/v2/schema/openapi` timed out.
- `https://192.168.31.254/api/v2/schema/openapi` timed out.

No live UAT fixture is committed for this issue. The endpoint inventory and
resource decisions in this directory are based on primary pfREST documentation
and are marked pending UAT confirmation.

For M2-01, `pfsense_haproxy_settings` uses the upstream
`HAProxySettings.inc` scalar model as a conservative implementation reference:
Terraform manages only scalar fields, treats `advanced` as sensitive, and does
not manage nested DNS resolver or email mailer children until UAT confirms their
ownership model.

For M2-02, `pfsense_haproxy_apply` uses the upstream `HAProxyApply.inc` model as
a conservative implementation reference: `GET /services/haproxy/apply` is
assumed to return an `applied` boolean, and `POST /services/haproxy/apply` is
assumed to start HAProxy configuration application. The Terraform resource keeps
apply explicit with user-controlled `triggers`; settings and other durable
resources must not auto-apply pending changes.
