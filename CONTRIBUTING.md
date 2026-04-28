# Contributing

Changes should be delivered through focused issues and pull requests.

## Local checks

```bash
make lint
make test
python3 -m unittest scripts/capture_haproxy_schema_test.py
```

Acceptance tests require a real pfSense instance with pfSense-pkg-RESTAPI and the HAProxy package installed:

```bash
export PFSENSE_ENDPOINT=https://pfsense.example.com
export PFSENSE_API_KEY=...
export PFSENSE_INSECURE_TLS=true
export PFSENSE_TEST_ENVIRONMENT=uat
export PFSENSE_TEST_PREFIX=uat_haproxy_tf
make testacc
```

Acceptance tests mutate HAProxy package configuration and must run only against
UAT unless an issue explicitly targets production and the operator confirms the
window. The test runner is serialized with `go test -p=1`; do not add
`t.Parallel()` to acceptance tests. Resource names must use
`PFSENSE_TEST_PREFIX` so failed runs can be identified and cleaned up by prefix.

Optional live-test variables:

- `PFSENSE_TEST_CERTIFICATE_REF`: existing pfSense certificate reference for
  frontend certificate attachment tests.
- `PFSENSE_TEST_CUSTOM_IPV4`: custom bind IPv4 address for frontend address
  tests.
- `PFSENSE_TEST_CUSTOM_IPV6`: custom bind IPv6 address for frontend address
  tests.

Do not commit credentials, pfSense backups, certificates, or VPN material.

## Schema and fixture artifacts

Use `scripts/capture_haproxy_schema.py` for UAT schema capture. Commit only
reviewed, redacted JSON outputs and documentation under `docs/schema/`.

Never commit raw schema captures, live HAProxy configuration dumps, API keys,
passwords, JWTs, certificates, private keys, or pfSense `config.xml` backups.
