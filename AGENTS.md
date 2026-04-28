You are Codex running with GitHub access and the ability to open issues, create branches, open PRs, run CI locally when available, and update GitHub Actions workflows.

Goal
Build a production-quality Terraform provider for managing pfSense HAProxy package configuration through pfSense-pkg-RESTAPI v2 endpoints.

The provider MUST support:
- HAProxy settings and apply/reload operations
- Backends
- Backend servers
- Frontends
- Frontend bind/listen addresses
- Frontend ACLs and actions
- Frontend certificates
- HAProxy files/error files when needed for managed routes
- Import and drift detection for all durable resources

Non-goals / strict constraints
- Do not manage generic HAProxy Data Plane API. The target is pfSense HAProxy package via pfSense REST API.
- Do not scrape pfSense webConfigurator pages.
- Do not commit live pfSense credentials, API keys, certificates, VPN material, or config backups.
- Do not implement unrelated pfSense features unless required to support HAProxy resources.
- Do not run destructive acceptance tests against PROD without explicit operator confirmation.

Repository bootstrap
- Use Go and terraform-plugin-framework.
- Keep provider source as `d3vi1/pfsense-restapi-haproxy`.
- Keep repository name as `terraform-provider-pfsense-restapi-haproxy`.
- Maintain README, examples, AGENTS.md, CONTRIBUTING, CODEOWNERS, CI, and acceptance workflow.

Project management rules
- Create and maintain GitHub milestones and issues.
- Bootstrap may land directly on `main`.
- Every non-bootstrap issue must be implemented on its own branch and PR.
- Each PR must include tests where appropriate and documentation/examples for user-facing changes.
- At the end of each milestone, re-evaluate all issues for completeness, scope creep, and cross-issue work. Correct issue text/status before moving on.
- Use fresh agents for every RUN. Do not reuse sub-agent context across runs.

Review and testing model
- Every implementation issue must represent implementer, tester, adversarial reviewer, and documenter roles.
- For each PR: run local tests, review the diff adversarially, address findings, then merge only after CI is green.
- Acceptance tests require local-only or GitHub secret configuration.

Secrets and local test harness
- Use environment variables or ignored local files only.
- Expected environment variables:
  - `PFSENSE_ENDPOINT`
  - `PFSENSE_API_KEY`
  - `PFSENSE_USERNAME`
  - `PFSENSE_PASSWORD`
  - `PFSENSE_INSECURE_TLS`
  - `PFSENSE_TEST_PREFIX`
  - `PFSENSE_TEST_ENVIRONMENT` (`uat` or `prod`)
- Prefer API key authentication for automation. Username/password support exists only when the REST API endpoint requires it.

Stop conditions
- Stop before changing production pfSense HAProxy state unless the issue explicitly targets PROD and the operator confirms the window.
- Stop if pfSense REST API schemas differ from assumptions and real endpoint output is required.
- Stop if an operation could remove or replace existing unmanaged HAProxy configuration without an import/migration plan.
