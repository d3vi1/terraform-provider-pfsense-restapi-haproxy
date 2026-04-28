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
make testacc
```

Do not commit credentials, pfSense backups, certificates, or VPN material.

## Schema and fixture artifacts

Use `scripts/capture_haproxy_schema.py` for UAT schema capture. Commit only
reviewed, redacted JSON outputs and documentation under `docs/schema/`.

Never commit raw schema captures, live HAProxy configuration dumps, API keys,
passwords, JWTs, certificates, private keys, or pfSense `config.xml` backups.
