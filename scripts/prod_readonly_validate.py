#!/usr/bin/env python3
"""Run a GET-only pfSense HAProxy production discovery lane.

The script never calls Terraform and never sends write methods. It reads a
small allowlist of schema, status, top-level, and child-list HAProxy endpoints,
redacts values that may contain secrets, and writes private evidence artifacts.
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
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


API_PREFIX = "/api/v2"
READ_ONLY_CONFIRMATION = "READ_ONLY_PROD_VALIDATION"
DEFAULT_OUTPUT_DIR = "validation-evidence/prod-readonly"
REDACTION_TEXT = "[REDACTED]"

STATIC_GET_PATHS = (
    "/schema/openapi",
    "/schema/native",
    "/services/haproxy/settings",
    "/services/haproxy/apply",
    "/services/haproxy/backends",
    "/services/haproxy/frontends",
)
CHILD_COLLECTIONS = (
    "/services/haproxy/backend/servers",
    "/services/haproxy/backend/acls",
    "/services/haproxy/backend/actions",
    "/services/haproxy/frontend/addresses",
    "/services/haproxy/frontend/certificates",
    "/services/haproxy/frontend/acls",
    "/services/haproxy/frontend/actions",
)
SENSITIVE_KEY_RE = re.compile(
    r"(^|[_-])(api[_-]?key|authorization|bearer|cert|certificate|client[_-]?cert|"
    r"credential|jwt|key|p12|passwd|password|private[_-]?key|secret|token)([_-]|$)",
    re.IGNORECASE,
)
PEM_RE = re.compile(r"-----BEGIN [A-Z0-9 ]+-----")
PRIVATE_CONFIG_RE = re.compile(r"<(?:pfsense|config)[\s>]", re.IGNORECASE)
LONG_TOKEN_RE = re.compile(r"^[A-Za-z0-9+/=_-]{80,}$")


class MissingParentIDError(ValueError):
    """Raised when child discovery cannot safely continue for a parent object."""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="GET-only pfSense HAProxy production validation discovery."
    )
    parser.add_argument(
        "--output-dir",
        default=DEFAULT_OUTPUT_DIR,
        help=f"Directory for redacted evidence. Default: {DEFAULT_OUTPUT_DIR}.",
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
        help="Validate gates and print planned GET paths without network access.",
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


def build_url(endpoint: str, request_path: str, query: dict[str, str] | None = None) -> str:
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

    encoded_query = urllib.parse.urlencode(query or {})
    return urllib.parse.urlunparse(
        parsed._replace(path=joined_path, params="", query=encoded_query, fragment="")
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


def validate_prod_readonly_gate() -> None:
    if os.getenv("PFSENSE_VALIDATION_ENVIRONMENT", "").strip().lower() != "prod":
        raise ValueError("PFSENSE_VALIDATION_ENVIRONMENT must be prod")
    if os.getenv("PFSENSE_READONLY_CONFIRMATION", "").strip() != READ_ONLY_CONFIRMATION:
        raise ValueError(f"PFSENSE_READONLY_CONFIRMATION must be {READ_ONLY_CONFIRMATION}")
    if not os.getenv("PFSENSE_ENDPOINT", "").strip():
        raise ValueError("PFSENSE_ENDPOINT is required")
    _, auth_mode = auth_headers()
    if auth_mode == "missing":
        raise ValueError("PFSENSE_API_KEY or PFSENSE_USERNAME/PFSENSE_PASSWORD is required")


def fetch_json(url: str, headers: dict[str, str], insecure: bool, timeout: float) -> Any:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/json",
            "User-Agent": "terraform-provider-pfsense-restapi-haproxy-prod-readonly",
            **headers,
        },
        method="GET",
    )
    context = ssl._create_unverified_context() if insecure else None
    with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
        body = response.read()
    return json.loads(body)


def response_shape(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        items = extract_items(value)
        return {
            "type": "object",
            "keys": sorted(str(key) for key in value.keys()),
            "item_count": len(items),
            "item_keys": sorted({str(key) for item in items for key in item.keys()}),
        }
    if isinstance(value, list):
        items = [item for item in value if isinstance(item, dict)]
        return {
            "type": "array",
            "item_count": len(items),
            "item_keys": sorted({str(key) for item in items for key in item.keys()}),
        }
    return {"type": type(value).__name__}


def extract_items(payload: Any) -> list[dict[str, Any]]:
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    if not isinstance(payload, dict):
        return []
    for key in ("data", "items", "rows", "results"):
        value = payload.get(key)
        if isinstance(value, list):
            return [item for item in value if isinstance(item, dict)]
        if isinstance(value, dict):
            return [value]
    return []


def object_id(item: dict[str, Any]) -> str | None:
    for key in ("id", "_id", "uuid"):
        value = item.get(key)
        if value is not None and str(value).strip():
            return str(value)
    return None


def redact(value: Any, parent_sensitive: bool = False) -> Any:
    if isinstance(value, dict):
        redacted: dict[str, Any] = {}
        for key, item in value.items():
            sensitive = parent_sensitive or bool(SENSITIVE_KEY_RE.search(str(key)))
            redacted[key] = redact(item, sensitive)
        return redacted
    if isinstance(value, list):
        return [redact(item, parent_sensitive) for item in value]
    if isinstance(value, str):
        if parent_sensitive or PEM_RE.search(value) or PRIVATE_CONFIG_RE.search(value):
            return REDACTION_TEXT
        if LONG_TOKEN_RE.match(value.strip()):
            return REDACTION_TEXT
    return value


def collect_paths(discovered: dict[str, Any]) -> list[dict[str, Any]]:
    requests: list[dict[str, Any]] = [{"path": path, "query": {}} for path in STATIC_GET_PATHS]

    for backend in extract_items(discovered.get("/services/haproxy/backends")):
        parent_id = object_id(backend)
        if not parent_id:
            raise MissingParentIDError("backend object is missing id/_id/uuid for child discovery")
        for child_path in CHILD_COLLECTIONS[:3]:
            requests.append({"path": child_path, "query": {"parent_id": parent_id}})

    for frontend in extract_items(discovered.get("/services/haproxy/frontends")):
        parent_id = object_id(frontend)
        if not parent_id:
            raise MissingParentIDError("frontend object is missing id/_id/uuid for child discovery")
        for child_path in CHILD_COLLECTIONS[3:]:
            requests.append({"path": child_path, "query": {"parent_id": parent_id}})

    return requests


def write_evidence(output_dir: Path, discovered: dict[str, Any], requests: list[dict[str, Any]]) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    summary = {
        "generated_at_utc": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "mode": "prod-readonly",
        "write_methods_used": [],
        "request_count": len(requests),
        "paths": redact_requests(requests),
    }
    shapes = {redact_response_key(path): response_shape(payload) for path, payload in discovered.items()}
    (output_dir / "discovery-shapes.redacted.json").write_text(
        json.dumps({"summary": summary, "response_shapes": redact(shapes)}, indent=2, sort_keys=True)
        + "\n",
        encoding="utf-8",
    )
    (output_dir / "evidence-summary.md").write_text(render_summary(summary), encoding="utf-8")


def render_summary(summary: dict[str, Any]) -> str:
    lines = [
        "# PROD Read-Only HAProxy Validation Evidence",
        "",
        f"- Generated at UTC: `{summary['generated_at_utc']}`",
        "- Mode: `prod-readonly`",
        "- Write methods used: none",
        f"- GET requests attempted: `{summary['request_count']}`",
        "",
        "## Requests",
        "",
    ]
    for request in summary["paths"]:
        query = request["query"]
        suffix = f"?{urllib.parse.urlencode(query)}" if query else ""
        lines.append(f"- `GET {request['path']}{suffix}`")
    lines.append("")
    return "\n".join(lines)


def redact_requests(requests: list[dict[str, Any]]) -> list[dict[str, Any]]:
    redacted: list[dict[str, Any]] = []
    for request in requests:
        redacted.append(
            {
                "path": request["path"],
                "query": {key: "[present]" for key in sorted(request["query"].keys())},
            }
        )
    return redacted


def redact_response_key(key: str) -> str:
    parsed = urllib.parse.urlparse(key)
    if not parsed.query:
        return key
    query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    redacted_query = urllib.parse.urlencode([(name, "[present]") for name, _ in query])
    return urllib.parse.urlunparse(parsed._replace(query=redacted_query))


def run(args: argparse.Namespace) -> int:
    try:
        validate_prod_readonly_gate()
        timeout = parse_timeout(args.timeout)
        build_url(os.environ["PFSENSE_ENDPOINT"], STATIC_GET_PATHS[0])
    except ValueError as exc:
        print(f"prod-readonly validation error: {exc}", file=sys.stderr)
        return 2

    planned = [{"path": path, "query": {}} for path in STATIC_GET_PATHS]
    if args.dry_run:
        print(json.dumps({"mode": "dry-run", "requests": planned}, indent=2, sort_keys=True))
        return 0

    headers, _ = auth_headers()
    discovered: dict[str, Any] = {}
    for request in planned:
        path = request["path"]
        discovered[path] = fetch_json(
            build_url(os.environ["PFSENSE_ENDPOINT"], path),
            headers,
            args.insecure,
            timeout,
        )

    try:
        requests = collect_paths(discovered)
    except MissingParentIDError as exc:
        print(f"prod-readonly validation error: {exc}", file=sys.stderr)
        return 3
    for request in requests[len(planned) :]:
        discovered[f"{request['path']}?{urllib.parse.urlencode(request['query'])}"] = fetch_json(
            build_url(os.environ["PFSENSE_ENDPOINT"], request["path"], request["query"]),
            headers,
            args.insecure,
            timeout,
        )

    write_evidence(Path(args.output_dir), discovered, requests)
    return 0


def main() -> int:
    return run(parse_args())


if __name__ == "__main__":
    raise SystemExit(main())
