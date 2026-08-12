"""Shared strict projections for Boatstack repository tests."""

from __future__ import annotations

from collections.abc import Mapping


def prescription_cli_arguments(
    prescription: Mapping[str, object], correlation: str
) -> tuple[str, ...]:
    """Project one complete current prescription into raw apply CLI arguments."""
    arguments = [
        "--correlation",
        correlation,
        "--prescription-id",
        str(prescription["id"]),
        "--expected-instance-id",
        str(prescription["expected_instance_id"]),
        "--expected-state-revision",
        str(prescription["expected_state_revision"]),
        "--expected-program-fingerprint",
        str(prescription["expected_program_fingerprint"]),
        "--expected-snapshot-fingerprint",
        str(prescription["expected_snapshot_fingerprint"]),
        "--expected-objective-binding-fingerprint",
        str(prescription["expected_objective_binding_fingerprint"]),
        "--authority-fingerprint",
        str(prescription["authority_fingerprint"]),
    ]
    for capability in prescription["required_capabilities"]:
        arguments.extend(("--required-capability", str(capability)))
    for capability in prescription["effective_capabilities"]:
        arguments.extend(("--effective-capability", str(capability)))
    return tuple(arguments)
