# M6 Usage Documentation Gate

Status: issue #21 documentation gate.

## Scope

Issue #21 documents how the HAProxy Terraform provider should be used for UAT
and production ingress work, and records the ownership model needed before the
M6 milestone can close.

This issue does not require live UAT mutation or production access. Live UAT
acceptance evidence remains tracked by issue #18, and production read-only
evidence remains tracked by issue #19.

## Changed Docs List

- `README.md`
- `examples/provider.tf`
- `examples/prod-readonly-validation/main.tf`
- `docs/schema/README.md`
- `docs/schema/m6-usage-documentation-gate.md`
- `xconnector-doc/docs/40-terraform/pfsense-haproxy-provider.md` (prepared
  locally; pending companion docs repo branch/commit/PR)
- `xconnector-doc/docs/index.md` (prepared locally; pending companion docs repo
  branch/commit/PR)

## Validation Evidence

| Check | Result |
|-------|--------|
| Date | 2026-04-29 |
| Branch | `issue/21-documentation-gate` |
| Commit | PR head commit for `issue/21-documentation-gate`; exact final hash is recorded by the PR and CI. |
| `git diff --check` | PASS |
| `terraform fmt -check -recursive examples` | PASS |
| `go test ./...` | PASS |
| `make lint` | PASS, `0 issues.` |
| `python3 -m unittest scripts/capture_haproxy_schema_test.py scripts/prod_readonly_validate_test.py` | PASS, 14 tests. |
| `PFSENSE_VALIDATION_ENVIRONMENT=prod PFSENSE_READONLY_CONFIRMATION=READ_ONLY_PROD_VALIDATION PFSENSE_ENDPOINT=https://example.invalid PFSENSE_API_KEY=dummy python3 scripts/prod_readonly_validate.py --dry-run` | PASS, printed the static GET allowlist without network access. |
| `xconnector-doc` docs-only inspection | Local draft prepared and `git diff --check` passed; companion docs repo branch/commit/PR is pending and must be recorded before claiming the external documentation gate as complete. |

If Terraform cannot validate examples because the provider source is not
available from a registry in the worker environment, record the skipped command
and use a local provider development override for any deeper example validation.

## Live Environment Evidence

No live UAT or PROD mutation is required for issue #21.

Live UAT writes remain blocked behind the issue #18 runbook:

- approved UAT firewall target;
- `PFSENSE_TEST_ENVIRONMENT=uat`;
- unique `PFSENSE_TEST_PREFIX`;
- `make testacc`;
- endpoint shape evidence ledger update.

Production remains read-only by default:

- `PFSENSE_VALIDATION_ENVIRONMENT=prod`;
- `PFSENSE_READONLY_CONFIRMATION=READ_ONLY_PROD_VALIDATION`;
- GET-only discovery and data-source plan validation;
- no `terraform apply`, `make testacc`, or HAProxy apply POST.

## Closure Notes

Consultant note: the provider usage model is UAT-write and PROD-read-only by
default. Production writes require a separate tracked issue, an approved
operator window, explicit confirmation, and an import/migration plan for every
existing HAProxy object Terraform will own.

Close issue #21 only after:

- README usage and ownership sections are merged;
- examples show explicit UAT/PROD provider aliasing;
- xconnector-doc has a canonical provider usage page and index link committed
  in a companion docs repo PR or explicitly tracked follow-up;
- validation commands are recorded in the PR and this evidence file.
