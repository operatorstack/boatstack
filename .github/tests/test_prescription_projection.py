"""Contract tests for the shared raw-CLI prescription projection."""

from __future__ import annotations

import unittest

from boatstack_test_support import prescription_cli_arguments


class PrescriptionProjectionContract(unittest.TestCase):
    def setUp(self) -> None:
        self.prescription = {
            "id": "prx-example",
            "expected_state_revision": 41,
            "expected_program_fingerprint": "a" * 64,
            "expected_snapshot_fingerprint": "snapshot-example",
            "authority_fingerprint": "auth-example",
            "required_capabilities": ["command.execute", "repository.write"],
            "effective_capabilities": ["command.execute", "repository.write"],
        }

    def test_projection_preserves_complete_current_identity(self) -> None:
        self.assertEqual(
            prescription_cli_arguments(self.prescription, "correlation-example"),
            (
                "--correlation", "correlation-example",
                "--prescription-id", "prx-example",
                "--expected-state-revision", "41",
                "--expected-program-fingerprint", "a" * 64,
                "--expected-snapshot-fingerprint", "snapshot-example",
                "--authority-fingerprint", "auth-example",
                "--required-capability", "command.execute",
                "--required-capability", "repository.write",
                "--effective-capability", "command.execute",
                "--effective-capability", "repository.write",
            ),
        )

    def test_missing_current_identity_fails_closed(self) -> None:
        del self.prescription["authority_fingerprint"]
        with self.assertRaises(KeyError):
            prescription_cli_arguments(self.prescription, "correlation-example")


if __name__ == "__main__":
    unittest.main()
