#!/usr/bin/env python3
"""Create or verify a human-approved, hash-addressed plan lock."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def git_commit(cwd: Path) -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=cwd, text=True, capture_output=True
    )
    return result.stdout.strip() if result.returncode == 0 else "unknown"


def expected(args: argparse.Namespace) -> dict[str, object]:
    approved_at = args.approved_at
    if not approved_at:
        approved_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat()
    return {
        "schema_version": 1,
        "status": "APPROVED",
        "approved_by": args.approved_by,
        "approved_at": approved_at,
        "source_commit": args.source_commit or git_commit(args.spec.parent),
        "spec_path": str(args.spec),
        "spec_sha256": sha256(args.spec),
        "plan_path": str(args.plan),
        "plan_sha256": sha256(args.plan),
        "task_graph_path": str(args.tasks),
        "task_graph_sha256": sha256(args.tasks),
        "invalidated_at": None,
        "invalidation_reason": None,
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Create a plan lock only after a human explicitly approves the draft."
    )
    parser.add_argument("--spec", type=Path, required=True)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--tasks", type=Path, required=True)
    parser.add_argument("--approved-by")
    parser.add_argument("--approved-at")
    parser.add_argument("--source-commit")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()

    for path in [args.spec, args.plan, args.tasks]:
        if path is not None and not path.is_file():
            parser.error(f"required approved artifact does not exist: {path}")

    if args.check:
        if not args.output.is_file():
            print(f"BLOCKED: plan lock is missing: {args.output}")
            return 1
        try:
            lock = json.loads(args.output.read_text())
        except (OSError, ValueError, TypeError) as exc:
            print(f"BLOCKED: plan lock is unreadable: {exc}")
            return 1
        mismatches = []
        for label, path in [("spec", args.spec), ("plan", args.plan), ("task_graph", args.tasks)]:
            expected_hash = sha256(path)
            if lock.get(f"{label}_sha256") != expected_hash:
                mismatches.append(label)
        if lock.get("status") != "APPROVED" or lock.get("invalidated_at"):
            mismatches.append("status")
        if not lock.get("approved_by"):
            mismatches.append("approver")
        if mismatches:
            print("BLOCKED: stale or invalid plan lock: " + ", ".join(mismatches))
            return 1
        print("PASS: approved plan lock matches the current artifacts")
        return 0

    if not args.approved_by or not args.approved_by.strip():
        parser.error("--approved-by must name the human who explicitly approved the plan")
    lock = expected(args)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(lock, indent=2, sort_keys=True) + "\n")
    print(f"wrote approved plan lock: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
