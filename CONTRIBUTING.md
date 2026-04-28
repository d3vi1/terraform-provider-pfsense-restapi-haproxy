# Contributing

Changes should be delivered through focused issues and pull requests.

## Local checks

```bash
make lint
make test
```

Acceptance tests require a real pfSense instance with pfSense-pkg-RESTAPI and the HAProxy package installed:

```bash
export PFSENSE_ENDPOINT=https://pfsense.example.com
export PFSENSE_API_KEY=...
export PFSENSE_INSECURE_TLS=true
make testacc
```

Do not commit credentials, pfSense backups, certificates, or VPN material.
