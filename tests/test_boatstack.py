from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "boatstack"
EXPORTER = SKILL / "scripts" / "export_repo.py"
COMPILER = SKILL / "scripts" / "compile_plan.py"
APPROVER = SKILL / "scripts" / "approve_plan.py"
CONFIG = ROOT / "project.example.json"


class BoatstackDistributionTests(unittest.TestCase):
    def run_script(self, *args: object, expected: int = 0) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            [sys.executable, *map(str, args)], text=True, capture_output=True
        )
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def test_branded_multi_host_export_and_drift_check(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp)
            arguments = [
                EXPORTER,
                "--repo", repo,
                "--config", CONFIG,
                "--adapter-name", "boatstack",
            ]
            self.run_script(*arguments, "--write")
            result = self.run_script(*arguments, "--check")
            self.assertIn("PASS", result.stdout)
            self.assertTrue((repo / ".cursor/commands/plan-gate.md").is_file())
            self.assertTrue((repo / ".agents/skills/boatstack/SKILL.md").is_file())
            self.assertTrue((repo / ".claude/skills/boatstack/SKILL.md").is_file())

    def test_compiler_and_hash_lock_block_stale_plan(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            spec = root / "spec.md"
            plan = root / "plan.json"
            compiled = root / "compiled"
            lock = root / "plan.lock.json"
            spec.write_text("# Accepted spec\n")
            plan.write_text(json.dumps({
                "schema_version": 1,
                "feature_id": "feature-one",
                "acceptance_criteria": [{"id": "AC-1", "text": "observable result"}],
                "tasks": [{
                    "id": "T-1",
                    "title": "implement result",
                    "depends_on": [],
                    "acceptance_criteria": ["AC-1"],
                    "validation": ["python3 -m unittest"],
                }],
            }))
            self.run_script(COMPILER, "--plan", plan, "--out-dir", compiled)
            tasks = compiled / "tasks.json"
            self.run_script(
                APPROVER,
                "--spec", spec,
                "--plan", plan,
                "--tasks", tasks,
                "--approved-by", "Test Human",
                "--approved-at", "2026-07-16T12:00:00+00:00",
                "--source-commit", "test",
                "--output", lock,
            )
            self.run_script(
                APPROVER,
                "--spec", spec,
                "--plan", plan,
                "--tasks", tasks,
                "--output", lock,
                "--check",
            )
            plan.write_text(plan.read_text() + "\n")
            blocked = self.run_script(
                APPROVER,
                "--spec", spec,
                "--plan", plan,
                "--tasks", tasks,
                "--output", lock,
                "--check",
                expected=1,
            )
            self.assertIn("stale", blocked.stdout)

    def test_uncovered_acceptance_criterion_is_not_compiled(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            plan = root / "plan.json"
            plan.write_text(json.dumps({
                "schema_version": 1,
                "feature_id": "invalid",
                "acceptance_criteria": [
                    {"id": "AC-1", "text": "covered"},
                    {"id": "AC-2", "text": "not covered"},
                ],
                "tasks": [{
                    "id": "T-1",
                    "depends_on": [],
                    "acceptance_criteria": ["AC-1"],
                    "validation": ["python3 -m unittest"],
                }],
            }))
            result = self.run_script(
                COMPILER, "--plan", plan, "--out-dir", root / "compiled", expected=1
            )
            self.assertIn("uncovered acceptance criteria", result.stdout)


if __name__ == "__main__":
    unittest.main()
