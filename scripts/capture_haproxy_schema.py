#!/usr/bin/env python3
"""Capture redacted pfSense REST API HAProxy schema metadata.

This script only reads the schema endpoints. It does not call HAProxy config
CRUD endpoints and does not apply pending HAProxy changes.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import posixpath
import re
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


API_PREFIX = "/api/v2"
HAPROXY_PREFIX = f"{API_PREFIX}/services/haproxy"
OPENAPI_SCHEMA_PATH = f"{API_PREFIX}/schema/openapi"
NATIVE_SCHEMA_PATH = f"{API_PREFIX}/schema/native"
REDACTION_TEXT = "[REDACTED]"

SENSITIVE_KEY_RE = re.compile(
    r"(^|[_-])(api[_-]?key|authorization|bearer|cert|certificate|client[_-]?cert|"
    r"credential|jwt|key|p12|passwd|password|private[_-]?key|secret|token)([_-]|$)",
    re.IGNORECASE,
)
HAPROXY_MODEL_RE = re.compile(r"HAProxy[A-Za-z0-9_]*")
PEM_RE = re.compile(r"-----BEGIN [A-Z0-9 ]+-----")
JWT_RE = re.compile(r"^[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}$")
LONG_TOKEN_RE = re.compile(r"^[A-Za-z0-9+/=_-]{80,}$")
PRIVATE_CONFIG_RE = re.compile(r"<(?:pfsense|config)[\s>]", re.IGNORECASE)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Fetch /api/v2/schema/openapi and/or /api/v2/schema/native, filter "
            "pfSense-pkg-RESTAPI HAProxy metadata, redact sensitive values, and "
            "write JSON under docs/schema by default."
        )
    )
    parser.add_argument(
        "--schema",
        choices=("openapi", "native", "both"),
        default="both",
        help="Schema endpoint to capture. Default: both.",
    )
    parser.add_argument(
        "--output-dir",
        default="docs/schema",
        help="Directory for redacted JSON outputs. Default: docs/schema.",
    )
    parser.add_argument(
        "--timeout",
        default=os.getenv("PFSENSE_TIMEOUT", "30s"),
        help="HTTP timeout, for example 30s or 5.5. Default: PFSENSE_TIMEOUT or 30s.",
    )
    parser.add_argument(
        "--insecure",
        action="store_true",
        default=parse_bool(os.getenv("PFSENSE_INSECURE_TLS", "")),
        help="Skip TLS verification. Default: PFSENSE_INSECURE_TLS.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the planned schema requests and outputs without network access.",
    )
    return parser.parse_args()


def parse_bool(raw: str) -> bool:
    return raw.strip().lower() in {"1", "t", "true", "y", "yes", "on"}


def parse_timeout(raw: str) -> float:
    value = raw.strip().lower()
    if not value:
        return 30.0
    multipliers = {"ms": 0.001, "s": 1.0, "m": 60.0}
    for suffix, multiplier in multipliers.items():
        if value.endswith(suffix):
            return float(value[: -len(suffix)]) * multiplier
    return float(value)


def requested_schemas(schema: str) -> list[tuple[str, str]]:
    if schema == "openapi":
        return [("openapi", OPENAPI_SCHEMA_PATH)]
    if schema == "native":
        return [("native", NATIVE_SCHEMA_PATH)]
    return [("openapi", OPENAPI_SCHEMA_PATH), ("native", NATIVE_SCHEMA_PATH)]


def build_url(endpoint: str, request_path: str) -> str:
    parsed = urllib.parse.urlparse(endpoint.strip())
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValueError("PFSENSE_ENDPOINT must be an absolute http(s) URL")

    base_path = parsed.path.rstrip("/")
    api_path = "/" + request_path.lstrip("/")
    if base_path.endswith(API_PREFIX) and api_path.startswith(API_PREFIX):
        api_path = api_path[len(API_PREFIX) :] or "/"

    if api_path.startswith(API_PREFIX):
        joined_path = posixpath.join(base_path, api_path.lstrip("/"))
    elif base_path.endswith(API_PREFIX):
        joined_path = posixpath.join(base_path, api_path.lstrip("/"))
    else:
        joined_path = posixpath.join(base_path, API_PREFIX.lstrip("/"), api_path.lstrip("/"))

    if not joined_path.startswith("/"):
        joined_path = "/" + joined_path

    return urllib.parse.urlunparse(
        parsed._replace(path=joined_path, params="", query="", fragment="")
    )


def auth_headers() -> tuple[dict[str, str], str]:
    api_key = os.getenv("PFSENSE_API_KEY", "").strip()
    username = os.getenv("PFSENSE_USERNAME", "").strip()
    password = os.getenv("PFSENSE_PASSWORD", "")

    if api_key:
        return {"X-API-Key": api_key}, "api_key"
    if username and password:
        token = base64.b64encode(f"{username}:{password}".encode("utf-8")).decode("ascii")
        return {"Authorization": f"Basic {token}"}, "basic"
    return {}, "missing"


def fetch_json(url: str, headers: dict[str, str], insecure: bool, timeout: float) -> Any:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/json",
            "User-Agent": "terraform-provider-pfsense-restapi-haproxy-schema-capture",
            **headers,
        },
        method="GET",
    )
    context = ssl._create_unverified_context() if insecure else None
    with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
        body = response.read()
    return json.loads(body)


def collect_refs(value: Any) -> set[str]:
    refs: set[str] = set()
    if isinstance(value, dict):
        ref = value.get("$ref")
        if isinstance(ref, str) and ref.startswith("#/components/schemas/"):
            refs.add(ref.rsplit("/", 1)[-1])
        for item in value.values():
            refs.update(collect_refs(item))
    elif isinstance(value, list):
        for item in value:
            refs.update(collect_refs(item))
    return refs


def filter_openapi_schema(payload: dict[str, Any], captured_at: str) -> dict[str, Any]:
    paths = {
        path: value
        for path, value in sorted(payload.get("paths", {}).items())
        if path.startswith(HAPROXY_PREFIX)
    }
    if not paths:
        raise ValueError(f"OpenAPI schema did not contain {HAPROXY_PREFIX} paths")

    schemas = payload.get("components", {}).get("schemas", {})
    selected: set[str] = set()
    pending = list(collect_refs(paths) | {name for name in schemas if name.startswith("HAProxy")})
    while pending:
        name = pending.pop()
        if name not in schemas or name in selected:
            continue
        selected.add(name)
        pending.extend(collect_refs(schemas[name]) - selected)

    return {
        "capture": capture_metadata(captured_at, OPENAPI_SCHEMA_PATH),
        "openapi": payload.get("openapi"),
        "info": payload.get("info", {}),
        "paths": paths,
        "components": {
            "schemas": {name: schemas[name] for name in sorted(selected) if name in schemas}
        },
    }


def collect_haproxy_models(value: Any) -> set[str]:
    models: set[str] = set()
    if isinstance(value, dict):
        for item in value.values():
            models.update(collect_haproxy_models(item))
    elif isinstance(value, list):
        for item in value:
            models.update(collect_haproxy_models(item))
    elif isinstance(value, str):
        models.update(HAPROXY_MODEL_RE.findall(value))
    return models


def filter_native_schema(payload: dict[str, Any], captured_at: str) -> dict[str, Any]:
    endpoints = payload.get("endpoints", {})
    haproxy_endpoints = {
        path: value
        for path, value in sorted(endpoints.items())
        if "/services/haproxy" in path
    }
    if not haproxy_endpoints:
        raise ValueError("Native schema did not contain /services/haproxy endpoints")

    models = payload.get("models", {})
    selected_models = {
        name for name in models if isinstance(name, str) and name.startswith("HAProxy")
    }
    selected_models.update(collect_haproxy_models(haproxy_endpoints))

    return {
        "capture": capture_metadata(captured_at, NATIVE_SCHEMA_PATH),
        "version": payload.get("version"),
        "endpoints": haproxy_endpoints,
        "models": {name: models[name] for name in sorted(selected_models) if name in models},
    }


def capture_metadata(captured_at: str, source_path: str) -> dict[str, str]:
    return {
        "captured_at_utc": captured_at,
        "captured_by": "scripts/capture_haproxy_schema.py",
        "source_endpoint": REDACTION_TEXT,
        "source_path": source_path,
        "filter": f"HAProxy paths containing {HAPROXY_PREFIX} and referenced HAProxy schemas/models",
        "redaction": "Recursive key/value redaction was applied before writing this file.",
    }


def redact(value: Any, key: str = "") -> Any:
    if isinstance(value, dict):
        return {item_key: redact(item_value, item_key) for item_key, item_value in value.items()}
    if isinstance(value, list):
        return [redact(item, key) for item in value]
    if isinstance(value, str):
        if should_redact_string(key, value):
            return REDACTION_TEXT
        return value
    if key and SENSITIVE_KEY_RE.search(key) and value is not None:
        return REDACTION_TEXT
    return value


def should_redact_string(key: str, value: str) -> bool:
    stripped = value.strip()
    if not stripped:
        return False
    if key and SENSITIVE_KEY_RE.search(key) and not looks_like_schema_description(stripped):
        return True
    if PEM_RE.search(stripped):
        return True
    if PRIVATE_CONFIG_RE.search(stripped):
        return True
    if stripped.lower().startswith(("bearer ", "basic ")):
        return True
    if JWT_RE.match(stripped):
        return True
    return bool(LONG_TOKEN_RE.match(stripped) and not looks_like_schema_description(stripped))


def looks_like_schema_description(value: str) -> bool:
    return "<br>" in value or " " in value or value.startswith("#/")


def write_json(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def run() -> int:
    args = parse_args()
    schemas = requested_schemas(args.schema)
    output_dir = Path(args.output_dir)
    endpoint = os.getenv("PFSENSE_ENDPOINT", "").strip()
    headers, auth_method = auth_headers()

    if args.dry_run:
        display_endpoint = endpoint or "https://pfsense.example.invalid"
        for schema_name, schema_path in schemas:
            output_path = output_dir / f"haproxy-{schema_name}.redacted.json"
            print(f"would fetch {build_url(display_endpoint, schema_path)}")
            print(f"would write {output_path}")
        print(f"auth method: {auth_method}")
        print(f"insecure tls: {args.insecure}")
        return 0

    if not endpoint:
        print("PFSENSE_ENDPOINT is required", file=sys.stderr)
        return 2
    if auth_method == "missing":
        print(
            "Set PFSENSE_API_KEY or PFSENSE_USERNAME/PFSENSE_PASSWORD before capture",
            file=sys.stderr,
        )
        return 2

    try:
        timeout = parse_timeout(args.timeout)
    except ValueError as exc:
        print(f"invalid timeout {args.timeout!r}: {exc}", file=sys.stderr)
        return 2

    captured_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat()
    for schema_name, schema_path in schemas:
        url = build_url(endpoint, schema_path)
        try:
            payload = fetch_json(url, headers, args.insecure, timeout)
            if schema_name == "openapi":
                filtered = filter_openapi_schema(payload, captured_at)
            else:
                filtered = filter_native_schema(payload, captured_at)
            output_path = output_dir / f"haproxy-{schema_name}.redacted.json"
            write_json(output_path, redact(filtered))
            print(f"wrote {output_path}")
        except (OSError, urllib.error.URLError, json.JSONDecodeError, ValueError) as exc:
            print(f"failed to capture {schema_path}: {exc}", file=sys.stderr)
            return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(run())
