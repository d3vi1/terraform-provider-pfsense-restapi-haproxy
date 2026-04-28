#!/usr/bin/env python3
"""Unit tests for capture_haproxy_schema.py redaction behavior."""

from __future__ import annotations

import importlib.util
import pathlib
import subprocess
import sys
import unittest


SCRIPT_PATH = pathlib.Path(__file__).with_name("capture_haproxy_schema.py")
SPEC = importlib.util.spec_from_file_location("capture_haproxy_schema", SCRIPT_PATH)
assert SPEC is not None
capture_haproxy_schema = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(capture_haproxy_schema)


class RedactionTests(unittest.TestCase):
    def test_sensitive_scalar_with_spaces_is_redacted(self) -> None:
        payload = {"api_key": "abc def"}

        self.assertEqual(
            capture_haproxy_schema.redact(payload),
            {"api_key": capture_haproxy_schema.REDACTION_TEXT},
        )

    def test_sensitive_parent_context_redacts_nested_defaults(self) -> None:
        payload = {
            "stats_password": {
                "type": "string",
                "description": "Password used by HAProxy statistics.",
                "default": "abc def",
                "example": "secret with spaces",
            }
        }

        self.assertEqual(
            capture_haproxy_schema.redact(payload),
            {
                "stats_password": {
                    "type": "string",
                    "description": "Password used by HAProxy statistics.",
                    "default": capture_haproxy_schema.REDACTION_TEXT,
                    "example": capture_haproxy_schema.REDACTION_TEXT,
                }
            },
        )

    def test_pem_blocks_are_redacted_without_sensitive_key(self) -> None:
        payload = {"example": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}

        self.assertEqual(
            capture_haproxy_schema.redact(payload),
            {"example": capture_haproxy_schema.REDACTION_TEXT},
        )


class CLITests(unittest.TestCase):
    def test_dry_run_invalid_endpoint_returns_user_facing_error(self) -> None:
        result = subprocess.run(
            [
                sys.executable,
                str(SCRIPT_PATH),
                "--dry-run",
            ],
            check=False,
            env={"PFSENSE_ENDPOINT": "not-a-url"},
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 2)
        self.assertIn("invalid PFSENSE_ENDPOINT", result.stderr)
        self.assertNotIn("Traceback", result.stderr)


if __name__ == "__main__":
    unittest.main()
