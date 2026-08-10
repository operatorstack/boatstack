"""End-to-end evaluation of Detached Supervision.

Unlike the Go unit conformance tests, this harness builds the real
``boatstack-helper`` binary once and drives it against actual scratch git
repositories — attach, activate, guard, and detach — asserting at every step that
the plant/controller boundary holds: no Boatstack-owned file ever lands in the
target repository or its ``.git``, and the developer's own host config is never
clobbered. It is the "actually set up repos and evaluate the system works" check.

Run (from repo root):
    python -m unittest discover -s labs/12-product-engineering-loop/tests -p 'test_*.py'
"""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SKILL = REPO / "boatstack"

FORBIDDEN_IN_REPO = [
    ".product-loop",
    ".boatstack-project.json",
    ".claude",
    ".cursor",
    ".codex",
    ".gemini",
    ".agents",
    ".github/PULL_REQUEST_TEMPLATE/boatstack.md",
]

DESTRUCTIVE_EVENT = json.dumps(
    {
        "hook_event_name": "PreToolUse",
        "tool_name": "Bash",
        "tool_input": {"command": "git reset --hard HEAD~1"},
    }
)


class DetachedSupervisionEndToEnd(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.build_temp = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.build_temp.name) / (
            "boatstack-helper.exe" if os.name == "nt" else "boatstack-helper"
        )
        env = dict(os.environ)
        env["GOCACHE"] = str(Path(cls.build_temp.name) / "go-cache")
        env["GOMODCACHE"] = str(Path(cls.build_temp.name) / "go-mod")
        result = subprocess.run(
            ["go", "build", "-o", str(cls.binary), "./cmd/boatstack-helper"],
            cwd=SKILL,
            env=env,
            text=True,
            capture_output=True,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stdout + result.stderr)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.build_temp.cleanup()

    def setUp(self) -> None:
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        base = Path(self.work.name)
        self.state_root = base / "state"
        self.user_root = base / "user"
        self.repo = base / "app"
        for path in (self.state_root, self.user_root, self.repo):
            path.mkdir()
        self._git("init", "-b", "main")
        self._git("config", "user.name", "Boatstack Test")
        self._git("config", "user.email", "boatstack@example.invalid")
        self._git("remote", "add", "origin", "https://github.com/acme/app.git")
        (self.repo / "README.md").write_text("# app\n")
        (self.repo / "go.mod").write_text("module app\n\ngo 1.22\n")
        self._git("add", ".")
        self._git("commit", "-m", "init")

    # --- helpers -------------------------------------------------------------

    def _git(self, *args: str) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            ["git", "-C", str(self.repo), *args], text=True, capture_output=True
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def _env(self) -> dict:
        env = dict(os.environ)
        env["BOATSTACK_STATE_ROOT"] = str(self.state_root)
        env["BOATSTACK_USER_CONFIG_ROOT"] = str(self.user_root)
        return env

    def run_helper(self, *args: object, expected: int = 0, stdin: str | None = None):
        result = subprocess.run(
            [str(self.__class__.binary), *map(str, args)],
            cwd=str(self.repo),
            text=True,
            capture_output=True,
            input=stdin,
            env=self._env(),
        )
        self.assertEqual(
            result.returncode, expected, f"{args}\nSTDOUT:{result.stdout}\nSTDERR:{result.stderr}"
        )
        return result

    def helper_json(self, *args: object) -> dict:
        return json.loads(self.run_helper(*args).stdout)

    def porcelain(self) -> str:
        return subprocess.run(
            ["git", "-C", str(self.repo), "status", "--porcelain=v1", "--untracked-files=all"],
            text=True,
            capture_output=True,
        ).stdout.strip()

    def assert_repo_uncontaminated(self) -> None:
        for forbidden in FORBIDDEN_IN_REPO:
            self.assertFalse(
                (self.repo / forbidden).exists(),
                f"Boatstack file leaked into the repo: {forbidden}",
            )

    # --- tests ---------------------------------------------------------------

    def test_attach_leaves_repository_pristine_and_state_external(self) -> None:
        before = self.porcelain()
        result = self.helper_json("attach", "--repo", ".", "--mode", "detached")
        self.assertEqual(result["verification_status"], "VERIFIED")

        self.assertEqual(self.porcelain(), before, "attach changed the working tree")
        self.assert_repo_uncontaminated()

        control_root = Path(result["control_root"])
        self.assertTrue((control_root / ".product-loop" / "project.json").exists())
        self.assertTrue((control_root / "binding.json").exists())
        self.assertTrue((self.state_root / "boatstack" / "registry.json").exists())
        # The external shared runtime slot was populated so the guard has a helper.
        runtimes = self.state_root / "boatstack" / "runtimes"
        self.assertTrue(runtimes.exists() and any(runtimes.rglob("boatstack-helper*")))

        status = self.helper_json("detached-status", "--repo", ".")
        self.assertTrue(status["attached"] and status["verified"])

    def test_bootstrap_oracle_is_credential_free_and_executes_exact_output(self) -> None:
        attached = self.helper_json("attach", "--repo", ".", "--mode", "detached")
        source_plan = self.repo / "request.md"
        source_plan.write_text("# Source plan\n")
        document = "# Synthetic artifact\n\n`rm -rf /` is inert documentation.\n"

        prescription = json.loads(
            self.run_helper(
                "flow",
                "bootstrap",
                "--repo",
                ".",
                "--feature",
                "detached-bootstrap",
                "--source-plan",
                "request.md",
                "--artifact",
                "source-plan.md",
                "--shell",
                "posix",
                "--json",
                stdin=document,
            ).stdout
        )
        self.assertEqual(prescription["verification_status"], "VERIFIED")
        self.assertEqual(prescription["supervision_mode"], "detached")
        self.assertTrue(Path(prescription["helper_path"]).is_absolute())
        self.assertNotIn(".product-loop/boatstack planning-write", prescription["planning_envelope"])

        events = {
            "cursor": {"hook_event_name": "beforeShellExecution", "command": prescription["planning_envelope"]},
            "claude": {"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": {"command": prescription["planning_envelope"]}},
            "codex": {"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": {"command": prescription["planning_envelope"]}},
            "gemini": {"hook_event_name": "BeforeTool", "tool_name": "run_shell_command", "tool_input": {"command": prescription["planning_envelope"]}},
        }
        for host, event in events.items():
            admitted = self.run_helper(
                "ambient-safety-hook", "--host", host, "--repo", ".", stdin=json.dumps(event)
            )
            self.assertNotIn("deny", admitted.stdout.lower(), host)

        executed = subprocess.run(
            ["bash", "-c", prescription["planning_envelope"]],
            cwd=self.repo,
            env=self._env(),
            text=True,
            capture_output=True,
        )
        self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)
        artifact = Path(attached["control_root"]) / ".product-loop" / "features" / "detached-bootstrap" / "source-plan.md"
        self.assertEqual(artifact.read_text(), document)
        self.assert_repo_uncontaminated()

    def test_activate_installs_guard_preserving_user_hooks(self) -> None:
        self.run_helper("attach", "--repo", ".", "--mode", "detached")

        claude_config = self.user_root / ".claude" / "settings.json"
        claude_config.parent.mkdir(parents=True, exist_ok=True)
        claude_config.write_text(
            json.dumps(
                {
                    "theme": "dark",
                    "hooks": {
                        "PreToolUse": [
                            {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-own.sh"}]}
                        ]
                    },
                }
            )
        )

        installed = self.helper_json("activate", "--repo", ".")
        self.assertEqual(installed["verification_status"], "VERIFIED")

        text = claude_config.read_text()
        self.assertIn("my-own.sh", text)
        self.assertIn("ambient-safety-hook", text)
        self.assertIn("theme", text)

        # Idempotent: re-activating changes nothing.
        again = self.helper_json("activate", "--repo", ".", "--host", "claude")
        self.assertTrue(all(host["action"] == "unchanged" for host in again["hosts"]))

        # Deactivate removes only the ambient guard.
        self.run_helper("deactivate", "--repo", ".", "--host", "claude")
        after = claude_config.read_text()
        self.assertNotIn("ambient-safety-hook", after)
        self.assertIn("my-own.sh", after)

    def test_ambient_guard_enforces_managed_and_noops_unmanaged(self) -> None:
        # Unattached: the developer-level guard must not control this repository.
        unmanaged = self.run_helper("ambient-safety-hook", "--host", "claude", "--repo", ".", stdin=DESTRUCTIVE_EVENT)
        self.assertNotIn('"permissionDecision":"deny"', unmanaged.stdout)

        # Attached: the same destructive command is denied by the same engine.
        self.run_helper("attach", "--repo", ".", "--mode", "detached")
        managed = self.run_helper("ambient-safety-hook", "--host", "claude", "--repo", ".", stdin=DESTRUCTIVE_EVENT)
        self.assertIn('"permissionDecision":"deny"', managed.stdout)

    def test_detached_work_keeps_repo_product_only(self) -> None:
        self.run_helper("attach", "--repo", ".", "--mode", "detached")

        context = self.helper_json("context", "--repo", ".", "--operation", "build", "--host", "claude")
        self.assertEqual(context["mode"], "detached")
        self.assertTrue(context["attached"])
        self.assertNotEqual(context.get("next_operation", ""), "")

        # Boatstack operations are read-only against the plant: the repo is pristine.
        self.assertEqual(self.porcelain(), "")
        self.assert_repo_uncontaminated()

        # The only change that ever appears in the repo is product work.
        (self.repo / "feature.txt").write_text("product work\n")
        self.assertEqual(self.porcelain(), "?? feature.txt")

    def test_detach_removes_external_state_and_restores_embedded(self) -> None:
        attached = self.helper_json("attach", "--repo", ".", "--mode", "detached")
        control_root = Path(attached["control_root"])
        self.assertTrue(control_root.exists())

        removed = self.helper_json("detach", "--repo", ".")
        self.assertEqual(removed["verification_status"], "VERIFIED")
        self.assertTrue(removed["state_removed"])
        self.assertFalse(control_root.exists())

        status = self.helper_json("detached-status", "--repo", ".")
        self.assertFalse(status["attached"])
        self.assert_repo_uncontaminated()


if __name__ == "__main__":
    unittest.main()
