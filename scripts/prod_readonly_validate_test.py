#!/usr/bin/env python3
"""Unit tests for prod_readonly_validate.py safety behavior."""

from __future__ import annotations

import importlib.util
import argparse
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


SCRIPT_PATH = pathlib.Path(__file__).with_name("prod_readonly_validate.py")
SPEC = importlib.util.spec_from_file_location("prod_readonly_validate", SCRIPT_PATH)
assert SPEC is not None
prod_readonly_validate = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(prod_readonly_validate)


class GateTests(unittest.TestCase):
    def test_dry_run_requires_prod_confirmation(self) -> None:
        result = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), "--dry-run"],
            check=False,
            env={
                "PFSENSE_ENDPOINT": "https://pfsense.example.com",
                "PFSENSE_API_KEY": "secret",
                "PFSENSE_VALIDATION_ENVIRONMENT": "uat",
                "PFSENSE_READONLY_CONFIRMATION": "READ_ONLY_PROD_VALIDATION",
            },
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 2)
        self.assertIn("PFSENSE_VALIDATION_ENVIRONMENT must be prod", result.stderr)
        self.assertNotIn("Traceback", result.stderr)

    def test_dry_run_lists_only_static_get_paths(self) -> None:
        result = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), "--dry-run"],
            check=False,
            env={
                "PFSENSE_ENDPOINT": "https://pfsense.example.com",
                "PFSENSE_API_KEY": "secret",
                "PFSENSE_VALIDATION_ENVIRONMENT": "prod",
                "PFSENSE_READONLY_CONFIRMATION": "READ_ONLY_PROD_VALIDATION",
            },
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 0)
        self.assertIn("/services/haproxy/backends", result.stdout)
        self.assertNotIn("POST", result.stdout)
        self.assertNotIn("DELETE", result.stdout)


class DiscoveryTests(unittest.TestCase):
    def test_collect_paths_adds_child_gets_by_parent_id(self) -> None:
        discovered = {
            "/services/haproxy/backends": {"data": [{"id": "10", "name": "app"}]},
            "/services/haproxy/frontends": {"data": [{"id": "20", "name": "web"}]},
        }

        requests = prod_readonly_validate.collect_paths(discovered)

        self.assertIn(
            {"path": "/services/haproxy/backend/servers", "query": {"parent_id": "10"}},
            requests,
        )
        self.assertIn(
            {"path": "/services/haproxy/frontend/actions", "query": {"parent_id": "20"}},
            requests,
        )

    def test_collect_paths_handles_singleton_parent_payloads(self) -> None:
        discovered = {
            "/services/haproxy/backends": {"data": {"id": "10", "name": "app"}},
            "/services/haproxy/frontends": {"data": {"id": "20", "name": "web"}},
        }

        requests = prod_readonly_validate.collect_paths(discovered)

        self.assertIn(
            {"path": "/services/haproxy/backend/actions", "query": {"parent_id": "10"}},
            requests,
        )
        self.assertIn(
            {"path": "/services/haproxy/frontend/certificates", "query": {"parent_id": "20"}},
            requests,
        )

    def test_singleton_parent_without_id_fails(self) -> None:
        discovered = {
            "/services/haproxy/backends": {"data": {"name": "app"}},
            "/services/haproxy/frontends": {"data": []},
        }

        with self.assertRaises(prod_readonly_validate.MissingParentIDError):
            prod_readonly_validate.collect_paths(discovered)

    def test_redact_requests_hides_parent_id_values(self) -> None:
        redacted = prod_readonly_validate.redact_requests(
            [{"path": "/services/haproxy/backend/servers", "query": {"parent_id": "10"}}]
        )

        self.assertEqual(
            redacted,
            [{"path": "/services/haproxy/backend/servers", "query": {"parent_id": "[present]"}}],
        )
        self.assertNotIn("10", repr(redacted))

    def test_redact_response_key_hides_parent_id_values(self) -> None:
        key = prod_readonly_validate.redact_response_key(
            "/services/haproxy/backend/servers?parent_id=10"
        )

        self.assertEqual(key, "/services/haproxy/backend/servers?parent_id=%5Bpresent%5D")
        self.assertNotIn("10", key)

    def test_runtime_uses_get_only_requests(self) -> None:
        responses = {
            "/api/v2/schema/openapi": {},
            "/api/v2/schema/native": {},
            "/api/v2/services/haproxy/settings": {"data": {}},
            "/api/v2/services/haproxy/apply": {"data": {"applied": True}},
            "/api/v2/services/haproxy/backends": {"data": [{"id": "10", "name": "app"}]},
            "/api/v2/services/haproxy/frontends": {"data": []},
            "/api/v2/services/haproxy/backend/servers": {"data": []},
            "/api/v2/services/haproxy/backend/acls": {"data": []},
            "/api/v2/services/haproxy/backend/actions": {"data": []},
        }
        requested_methods: list[str] = []

        def fake_fetch(url: str, headers: dict[str, str], insecure: bool, timeout: float) -> object:
            parsed = prod_readonly_validate.urllib.parse.urlparse(url)
            requested_methods.append("GET")
            return responses[parsed.path]

        with tempfile.TemporaryDirectory() as tmpdir, mock.patch.dict(
            os.environ,
            {
                "PFSENSE_ENDPOINT": "https://pfsense.example.com",
                "PFSENSE_API_KEY": "secret",
                "PFSENSE_VALIDATION_ENVIRONMENT": "prod",
                "PFSENSE_READONLY_CONFIRMATION": "READ_ONLY_PROD_VALIDATION",
            },
            clear=True,
        ), mock.patch.object(prod_readonly_validate, "fetch_json", side_effect=fake_fetch):
            code = prod_readonly_validate.run(
                argparse.Namespace(output_dir=tmpdir, timeout="30s", insecure=False, dry_run=False)
            )

        self.assertEqual(code, 0)
        self.assertTrue(requested_methods)
        self.assertEqual(set(requested_methods), {"GET"})

    def test_fetch_json_constructs_get_request(self) -> None:
        seen_methods: list[str] = []

        class FakeResponse:
            def __enter__(self) -> "FakeResponse":
                return self

            def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
                return None

            def read(self) -> bytes:
                return b"{}"

        def fake_urlopen(request: object, timeout: float, context: object = None) -> FakeResponse:
            seen_methods.append(request.get_method())
            return FakeResponse()

        with mock.patch.object(prod_readonly_validate.urllib.request, "urlopen", side_effect=fake_urlopen):
            prod_readonly_validate.fetch_json(
                "https://pfsense.example.com/api/v2/services/haproxy/settings",
                {},
                False,
                30,
            )

        self.assertEqual(seen_methods, ["GET"])

    def test_collect_paths_fails_when_parent_id_missing(self) -> None:
        discovered = {
            "/services/haproxy/backends": {"data": [{"name": "app"}]},
            "/services/haproxy/frontends": {"data": []},
        }

        with self.assertRaises(prod_readonly_validate.MissingParentIDError):
            prod_readonly_validate.collect_paths(discovered)

if __name__ == "__main__":
    unittest.main()
