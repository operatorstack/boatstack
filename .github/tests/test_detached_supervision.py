"""End-to-end tests for the V2 detached identity and guard boundary."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
RUNTIME = REPO / "boatstack"


class DetachedSupervisionEndToEnd(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.build = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.build.name) / (
            "boatstack-helper.exe" if os.name == "nt" else "boatstack-helper"
        )
        result = subprocess.run(
            ["go", "build", "-o", str(cls.binary), "./cmd/boatstack-helper"],
            cwd=RUNTIME,
            text=True,
            capture_output=True,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stdout + result.stderr)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.build.cleanup()

    def setUp(self) -> None:
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        root = Path(self.work.name)
        self.state_root = root / "state"
        self.boatstack_home = root / "boatstack-home"
        self.repo = root / "repo"
        self.repo.mkdir()
        self._git(self.repo, "init", "-b", "main")
        self._git(self.repo, "config", "user.name", "Boatstack Test")
        self._git(self.repo, "config", "user.email", "boatstack@example.invalid")
        (self.repo / "README.md").write_text("# fixture\n")
        self._git(self.repo, "add", "README.md")
        self._git(self.repo, "commit", "-m", "fixture")
        digest = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        version = self.run_helper("version").stdout.strip()
        runtime_dir = self.boatstack_home / "runtimes" / f"{version}-{digest}"
        runtime_dir.mkdir(parents=True)
        runtime = runtime_dir / ("boatstack-runtime.exe" if os.name == "nt" else "boatstack-runtime")
        shutil.copy2(self.binary, runtime)
        runtime.chmod(0o755)

    def _git(self, repository: Path, *args: str) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            ["git", "-C", str(repository), *args], text=True, capture_output=True
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def _env(self) -> dict[str, str]:
        env = dict(os.environ)
        env["BOATSTACK_STATE_ROOT"] = str(self.state_root)
        env["BOATSTACK_HOME"] = str(self.boatstack_home)
        return env

    def run_helper(
        self,
        *args: object,
        cwd: Path | None = None,
        expected: int = 0,
    ) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            [str(self.binary), *map(str, args)],
            cwd=cwd or self.repo,
            env=self._env(),
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def helper_json(self, *args: object, cwd: Path | None = None) -> dict:
        return json.loads(self.run_helper(*args, cwd=cwd).stdout)

    def porcelain(self, repository: Path | None = None) -> str:
        repository = repository or self.repo
        return subprocess.run(
            ["git", "-C", str(repository), "status", "--porcelain=v1", "--untracked-files=all"],
            text=True,
            capture_output=True,
            check=True,
        ).stdout.strip()

    @staticmethod
    def goal_flags() -> tuple[str, ...]:
        return (
            "--goal-id", "bootstrap", "--goal-kind", "approved-plan",
            "--delivery", "bootstrap",
        )

    def attach(self, repository: Path | None = None) -> dict:
        repository = repository or self.repo
        return self.helper_json(
            "attach", "--repo", repository, *self.goal_flags(), "--human", "contract",
            "--param", "topology=detached", "--param", "config_authority=repository",
            cwd=repository,
        )

    def test_attach_and_detach_transfer_only_controller_authority(self) -> None:
        before = self.porcelain()
        attached = self.attach()
        self.assertEqual(self.porcelain(), before)
        self.assertEqual(attached["snapshot"]["invocation"]["topology"], "detached")
        self.assertEqual(attached["receipt"]["transition_id"], "repository.attach")
        bindings = list((self.state_root / "boatstack" / "v2").rglob("binding.json"))
        self.assertEqual(len(bindings), 1)
        binding = json.loads(bindings[0].read_text())
        self.assertEqual(binding["topology"], "detached")

        detached = self.helper_json(
            "detach", "--repo", self.repo, *self.goal_flags(), "--human", "contract"
        )
        self.assertEqual(detached["snapshot"]["invocation"]["topology"], "embedded")
        self.assertEqual(detached["receipt"]["transition_id"], "repository.detach")
        self.assertEqual(self.porcelain(), before)
        self.assertFalse(bindings[0].exists())

    def test_two_clones_never_share_a_detached_binding_alias(self) -> None:
        root = Path(self.work.name)
        origin = root / "origin.git"
        self._git(self.repo, "init", "--bare", origin)
        self._git(self.repo, "remote", "add", "origin", str(origin))
        self._git(self.repo, "push", "-u", "origin", "main")
        self._git(origin, "symbolic-ref", "HEAD", "refs/heads/main")
        clone = root / "clone"
        result = subprocess.run(
            ["git", "clone", str(origin), str(clone)], text=True, capture_output=True
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

        one = self.attach(self.repo)
        two = self.attach(clone)
        self.assertEqual(one["snapshot"]["invocation"]["repository_id"], two["snapshot"]["invocation"]["repository_id"])
        self.assertNotEqual(one["snapshot"]["invocation"]["git_common_id"], two["snapshot"]["invocation"]["git_common_id"])
        bindings = list((self.state_root / "boatstack" / "v2").rglob("binding.json"))
        self.assertEqual(len(bindings), 2)

    def test_detached_installation_and_engaged_guard_use_the_same_kernel(self) -> None:
        self.attach()
        config = Path(self.work.name) / "project.json"
        config.write_text(
            json.dumps(
                {
                    "schema_version": 2,
                    "project": {"name": "fixture", "default_branch": "main", "commands": {}},
                    "policy": {"plan_approval": "human", "visual_evidence": "optional"},
                    "hosts": ["cli", "cursor", "codex", "claude", "gemini", "mcp"],
                }
            )
        )
        initialized = self.helper_json(
            "init", "--repo", self.repo, "--human", "contract",
            "--param", f"config_path={config}",
        )
        self.assertEqual(initialized["snapshot"]["invocation"]["topology"], "detached")
        self.assertEqual(initialized["snapshot"]["runtime"]["value"], "verified")
        self.assertEqual(
            self.porcelain(),
            "\n".join(
                [
                    "?? .agents/skills/boatstack-autoplan/SKILL.md",
                    "?? .agents/skills/boatstack-autoplan/agents/openai.yaml",
                    "?? .agents/skills/boatstack-run/SKILL.md",
                    "?? .agents/skills/boatstack-run/agents/openai.yaml",
                    "?? .agents/skills/boatstack-update/SKILL.md",
                    "?? .agents/skills/boatstack-update/agents/openai.yaml",
                    "?? .boatstack/host-skills.json",
                    "?? .boatstack/project.json",
                    "?? .boatstack/runtime.json",
                    "?? .claude/skills/boatstack-autoplan/SKILL.md",
                    "?? .claude/skills/boatstack-run/SKILL.md",
                    "?? .claude/skills/boatstack-update/SKILL.md",
                    "?? .cursor/commands/boatstack-autoplan.md",
                    "?? .cursor/commands/boatstack-run.md",
                    "?? .cursor/commands/boatstack-update.md",
                    "?? .gemini/skills/boatstack-autoplan/SKILL.md",
                    "?? .gemini/skills/boatstack-run/SKILL.md",
                    "?? .gemini/skills/boatstack-update/SKILL.md",
                ]
            ),
        )

        self.helper_json(
            "apply", "--repo", self.repo, "--transition", "goal.configure",
            *self.goal_flags(), "--human", "contract",
            "--param", "goal_kind=approved-plan", "--param", "delivery_id=bootstrap",
        )
        self.helper_json(
            "apply", "--repo", self.repo, "--transition", "engagement.begin",
            *self.goal_flags(), "--repository-authority",
        )
        ordinary = self.helper_json(
            "guard", "--repo", self.repo, "--command", "go test ./..."
        )
        self.assertTrue(ordinary["guard"]["allowed"])
        managed = self.helper_json(
            "guard", "--repo", self.repo, "--command", "git push origin HEAD"
        )
        self.assertFalse(managed["guard"]["allowed"])
        self.assertEqual(managed["guard"]["required_transition"], "publication.execute")
        destructive = self.helper_json(
            "guard", "--repo", self.repo, "--command", "rm -rf build/*"
        )
        self.assertFalse(destructive["guard"]["allowed"])
        self.assertEqual(destructive["guard"]["intent"]["operation"], "filesystem.recursive-delete")

    def test_authority_free_frontier_does_not_block_authorized_plan_creation(self) -> None:
        # control-law: codex-mode-authority-survives-observation-and-effects
        goal = (
            "--goal-id", "codex-driver-authority-triggers",
            "--goal-kind", "open-or-updated-pr",
            "--delivery", "codex-driver-authority-triggers",
        )
        flow = ("--flow", "flow-codex-driver-authority-triggers")
        self.helper_json(
            "attach", "--repo", self.repo, *goal, *flow, "--human", "contract",
            "--param", "topology=detached", "--param", "config_authority=repository",
        )
        config = Path(self.work.name) / "driver-project.json"
        config.write_text(
            json.dumps(
                {
                    "schema_version": 2,
                    "project": {"name": "driver-fixture", "default_branch": "main", "commands": {}},
                    "policy": {"plan_approval": "human", "visual_evidence": "optional"},
                    "hosts": ["cli", "codex"],
                }
            )
        )
        self.helper_json(
            "init", "--repo", self.repo, *goal, *flow, "--human", "contract",
            "--param", f"config_path={config}",
        )
        self.helper_json(
            "apply", "--repo", self.repo, "--transition", "goal.configure",
            *goal, *flow, "--human", "contract",
            "--param", "goal_kind=open-or-updated-pr",
            "--param", "delivery_id=codex-driver-authority-triggers",
        )
        self.helper_json(
            "apply", "--repo", self.repo, "--transition", "engagement.begin",
            *goal, *flow, "--repository-authority",
        )

        before = self.porcelain()
        diagnostic = self.helper_json("status", "--repo", self.repo, *goal, *flow)
        self.assertEqual(diagnostic["decision"]["kind"], "FRONTIER")
        self.assertIn("plan.create", diagnostic["decision"]["candidates"])
        self.assertEqual(self.porcelain(), before)

        progressing = self.helper_json(
            "next", "--repo", self.repo, *goal, *flow,
            "--human", "contract", "--repository-authority",
        )
        self.assertEqual(progressing["decision"]["kind"], "CANDIDATE")
        self.assertEqual(progressing["decision"]["transition"]["id"], "plan.create")

        plan = Path(self.work.name) / "source-plan.md"
        plan.write_text("# Driver fix\n\nPreserve authority across resolution and effects.\n")
        parameters = (
            "--param", f"source_path={plan}",
            "--param", "delivery_id=codex-driver-authority-triggers",
        )
        prescribed = self.helper_json(
            "next", "--repo", self.repo, "--transition", "plan.create",
            *goal, *flow, "--human", "contract", *parameters,
        )
        self.assertEqual(prescribed["decision"]["kind"], "PRESCRIBED")
        self.assertEqual(prescribed["decision"]["transition"]["id"], "plan.create")

        applied_process = self.run_helper(
            "plan-create", "--repo", self.repo, *goal, *flow,
            "--human", "contract", *parameters,
        )
        applied = json.loads(applied_process.stdout)
        self.assertEqual(applied_process.stderr, "")
        self.assertEqual(applied["receipt"]["transition_id"], "plan.create")
        self.assertEqual(applied["receipt"]["flow_id"], "flow-codex-driver-authority-triggers")
        self.assertEqual(applied["receipt"]["outcome"], "succeeded")
        self.assertTrue(applied["receipt"]["target_fingerprint"])
        self.assertEqual(applied["receipt"]["recovery"], "recovery.resume")
        self.assertEqual(applied["snapshot"]["plan"]["value"], "draft")
        for field in ('"admission"', '"receipt"', '"snapshot"', '"target_fingerprint"', '"recovery"'):
            self.assertIn(field, applied_process.stdout)

        resolved = self.helper_json(
            "next", "--repo", self.repo, *goal, *flow, "--repository-authority",
        )
        self.assertEqual(resolved["decision"]["kind"], "PRESCRIBED")
        self.assertEqual(resolved["decision"]["transition"]["id"], "plan.validate")

    def test_one_delivery_context_rematerializes_repository_authority_after_initialization(self) -> None:
        # control-law: retained-repository-source-crosses-maintenance-receipt-once
        goal = (
            "--goal-id", "preserve-repository-authority-context",
            "--goal-kind", "open-or-updated-pr",
            "--delivery", "preserve-repository-authority-context",
        )
        flow = ("--flow", "flow-preserve-repository-authority-context")
        actor = ("--human", "contract")
        self.helper_json(
            "attach", "--repo", self.repo, *goal, *flow, *actor,
            "--param", "topology=detached", "--param", "config_authority=repository",
        )

        prescribed = self.helper_json(
            "next", "--repo", self.repo, *goal, *flow, *actor,
        )
        self.assertEqual(prescribed["decision"]["kind"], "CANDIDATE")
        self.assertEqual(
            prescribed["decision"]["transition"]["id"], "installation.initialize"
        )

        config = Path(self.work.name) / "retained-authority-project.json"
        config.write_text(
            json.dumps(
                {
                    "schema_version": 2,
                    "project": {
                        "name": "retained-authority-fixture",
                        "default_branch": "main",
                        "commands": {},
                    },
                    "policy": {"plan_approval": "human", "visual_evidence": "optional"},
                    "hosts": ["cli", "codex"],
                }
            )
        )
        canonical_config = json.loads(config.read_text())
        canonical_config["hosts"] = sorted(canonical_config["hosts"])
        canonical_config["policy"]["external_effect_authority"] = (
            "human-or-autonomy-plus-provider"
        )
        config_fingerprint = hashlib.sha256(
            json.dumps(canonical_config, separators=(",", ":")).encode()
        ).hexdigest()
        bound_initialization = self.helper_json(
            "next", "--repo", self.repo,
            "--transition", "installation.initialize", *goal, *flow, *actor,
            "--param", f"source_revision={self._git(self.repo, 'rev-parse', 'HEAD').stdout.strip()}",
            "--param", f"runtime_version={self.run_helper('version').stdout.strip()}",
            "--param", f"runtime_sha256={hashlib.sha256(self.binary.read_bytes()).hexdigest()}",
            "--param", f"config_path={config}",
            "--param", f"config_sha256={config_fingerprint}",
        )
        self.assertEqual(bound_initialization["decision"]["kind"], "PRESCRIBED")
        self.assertEqual(
            bound_initialization["decision"]["transition"]["id"],
            "installation.initialize",
        )
        initialized_process = self.run_helper(
            "init", "--repo", self.repo, *goal, *flow, *actor,
            "--param", f"config_path={config}",
        )
        initialized = json.loads(initialized_process.stdout)
        self.assertEqual(initialized["receipt"]["transition_id"], "installation.initialize")
        self.assertEqual(initialized["receipt"]["flow_id"], flow[1])
        self.assertEqual(initialized["snapshot"]["configuration"]["value"], "verified")
        for field in ('"admission"', '"receipt"', '"snapshot"', '"target_fingerprint"', '"recovery"'):
            self.assertIn(field, initialized_process.stdout)

        configured = self.helper_json(
            "apply", "--repo", self.repo, "--transition", "goal.configure",
            *goal, *flow, *actor,
            "--param", "goal_kind=open-or-updated-pr",
            "--param", "delivery_id=preserve-repository-authority-context",
        )
        self.assertEqual(configured["receipt"]["transition_id"], "goal.configure")

        engagement = self.helper_json(
            "next", "--repo", self.repo, *goal, *flow, *actor,
            "--repository-authority",
        )
        self.assertEqual(engagement["decision"]["kind"], "PRESCRIBED")
        self.assertEqual(engagement["decision"]["transition"]["id"], "engagement.begin")

        engaged_process = self.run_helper(
            "apply", "--repo", self.repo, "--transition", "engagement.begin",
            *goal, *flow, *actor, "--repository-authority",
        )
        engaged = json.loads(engaged_process.stdout)
        self.assertEqual(engaged["receipt"]["transition_id"], "engagement.begin")
        self.assertEqual(engaged["receipt"]["flow_id"], flow[1])
        self.assertEqual(
            {receipt["class"] for receipt in engaged["admission"]["authority"]["receipts"]},
            {"human", "repository-policy"},
        )
        for field in ('"admission"', '"receipt"', '"snapshot"', '"target_fingerprint"', '"recovery"'):
            self.assertIn(field, engaged_process.stdout)

        plan = self.helper_json(
            "next", "--repo", self.repo, *goal, *flow, *actor,
            "--repository-authority",
        )
        self.assertEqual(plan["decision"]["kind"], "CANDIDATE")
        self.assertEqual(plan["decision"]["transition"]["id"], "plan.create")

        plan_source = Path(self.work.name) / "retained-authority-plan.md"
        plan_source.write_text("# Retained authority\n\nContinue in one operation context.\n")
        bound = self.helper_json(
            "next", "--repo", self.repo, "--transition", "plan.create",
            *goal, *flow, *actor, "--repository-authority",
            "--param", f"source_path={plan_source}",
            "--param", "delivery_id=preserve-repository-authority-context",
        )
        self.assertEqual(bound["decision"]["kind"], "PRESCRIBED")
        self.assertEqual(bound["decision"]["transition"]["id"], "plan.create")

    def test_repository_authority_rematerialization_fails_closed_without_verified_config(self) -> None:
        # control-law: repository-authority-requires-exact-verified-fingerprint
        root = Path(self.work.name) / "unverified"
        root.mkdir()
        self._git(root, "init", "-b", "main")
        self._git(root, "config", "user.name", "Boatstack Test")
        self._git(root, "config", "user.email", "boatstack@example.invalid")
        (root / "README.md").write_text("# unverified fixture\n")
        self._git(root, "add", "README.md")
        self._git(root, "commit", "-m", "fixture")
        before = self.porcelain(root)
        result = self.run_helper(
            "next", "--repo", root,
            "--goal-id", "unverified-authority",
            "--goal-kind", "open-or-updated-pr",
            "--delivery", "unverified-authority",
            "--flow", "flow-unverified-authority",
            "--human", "contract", "--repository-authority",
            cwd=root, expected=1,
        )
        self.assertIn(
            "repository authority requires current verified configuration evidence",
            result.stderr,
        )
        self.assertEqual(self.porcelain(root), before)


if __name__ == "__main__":
    unittest.main()
