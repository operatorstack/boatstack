from __future__ import annotations

import json
import os
import re
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
RUNTIME = REPO / "boatstack"
CONFIG = REPO / "project.example.json"


class RepositoryContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.build = tempfile.TemporaryDirectory()
        cls.helper = Path(cls.build.name) / (
            "boatstack-helper.exe" if os.name == "nt" else "boatstack-helper"
        )
        result = subprocess.run(
            ["go", "build", "-o", str(cls.helper), "./cmd/boatstack-helper"],
            cwd=RUNTIME,
            text=True,
            capture_output=True,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stdout + result.stderr)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.build.cleanup()

    def run_command(self, *args: object, cwd: Path | None = None, expected: int = 0):
        result = subprocess.run(
            [*map(str, args)], cwd=cwd, text=True, capture_output=True
        )
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def run_helper(self, *args: object, expected: int = 0):
        return self.run_command(self.helper, *args, expected=expected)

    def test_active_workflows_have_no_intelligence_flow_path(self) -> None:
        workflows = REPO / ".github" / "workflows"
        self.assertFalse((workflows / "sync-upstream.yml").exists())
        for workflow in workflows.glob("*.yml"):
            value = workflow.read_text()
            self.assertNotIn("operatorstack/intelligence-flow", value, workflow)
            self.assertNotIn("sync/intelligence-flow-", value, workflow)
            self.assertNotIn("UPSTREAM.json", value, workflow)

    def test_release_authority_uses_boatstack_revision(self) -> None:
        release = (REPO / ".github" / "workflows" / "release.yml").read_text()
        automatic = (REPO / ".github" / "workflows" / "auto-release.yml").read_text()
        self.assertIn('source_commit="$(git rev-parse HEAD)"', release)
        self.assertNotIn("IMPORT_PROVENANCE.json", release)
        self.assertNotIn("UPSTREAM.json", release)
        self.assertIn('workflows: ["Verify Boatstack distribution"]', automatic)
        self.assertIn("github.event.workflow_run.event == 'push'", automatic)
        self.assertIn("github.event.workflow_run.head_branch == 'main'", automatic)
        self.assertIn("repositories: boatstack", automatic)

    def test_current_public_surface_is_boatstack_owned(self) -> None:
        current = [REPO / "README.md", REPO / "CONTRIBUTING.md", *sorted((REPO / "docs").glob("*"))]
        forbidden = (
            "Generated from operatorstack/intelligence-flow",
            "Edit the upstream public source",
            "edit in Intelligence Flow",
            "generated content distribution",
        )
        for path in current:
            if not path.is_file() or path.suffix not in {".md", ".json"}:
                continue
            value = path.read_text()
            for phrase in forbidden:
                self.assertNotIn(phrase, value, path)
        self.assertFalse((REPO / "UPSTREAM.json").exists())

    def test_document_links_claims_and_assets_are_valid(self) -> None:
        def anchors(document: Path) -> set[str]:
            result = set()
            for heading in re.findall(r"^#{1,6}\s+(.+?)\s*$", document.read_text(), re.MULTILINE):
                plain = re.sub(r"<[^>]+>", "", heading).strip().lower()
                plain = re.sub(r"[^\w\s-]", "", plain)
                result.add(re.sub(r"\s+", "-", plain))
            return result

        documents = [REPO / "README.md", *sorted((REPO / "docs").glob("*.md"))]
        for document in documents:
            for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", document.read_text()):
                if target.startswith(("http://", "https://", "#", "mailto:")):
                    continue
                relative, _, anchor = target.partition("#")
                resolved = (document.parent / relative).resolve()
                self.assertTrue(resolved.exists(), f"broken link {target} in {document}")
                if anchor and resolved.suffix == ".md":
                    self.assertIn(anchor, anchors(resolved), f"broken anchor {target}")

        configuration = (REPO / "docs" / "configuration.md").read_text()
        for example in re.findall(r"```json\n(.*?)\n```", configuration, re.DOTALL):
            json.loads(example)

        claims = json.loads((REPO / "docs" / "public-claims.json").read_text())
        self.assertNotIn("source_commit", claims)
        allowed = set(claims["statuses"])
        for claim in claims["claims"]:
            self.assertIn(claim["status"], allowed)
            self.assertRegex(claim["last_verified_version"], r"^v\d+\.\d+\.\d+$")
            readable, _, anchor = claim["readable_evidence"].partition("#")
            readable_path = REPO / "docs" / readable
            self.assertTrue(readable_path.is_file(), claim["id"])
            self.assertIn(anchor, anchors(readable_path), claim["id"])
            for evidence in claim["implementation"] + claim["verification"]:
                self.assertTrue((REPO / "docs" / evidence).resolve().is_file(), evidence)

        for name in ("boatstack-mark.svg", "boatstack-journey.svg", "boatstack-portability.svg"):
            path = REPO / "assets" / name
            root = ET.parse(path).getroot()
            self.assertEqual(root.attrib.get("role"), "img", name)
            value = path.read_text()
            self.assertIn("<title", value, name)
            self.assertIn("<desc", value, name)

    def test_public_examples_exclude_private_context(self) -> None:
        paths = [
            REPO / "docs" / "account-recovery-walkthrough.md",
            RUNTIME / "testdata" / "reviewer-pr-body.md",
        ]
        for path in paths:
            value = path.read_text()
            for private in ("Tax" + "Weave", "/Users/", "bigboateng", "cursor_password_reset_button_addition"):
                self.assertNotIn(private, value, path)

    def test_export_and_drift_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp)
            self.run_helper("export", "--repo", target, "--config", CONFIG, "--adapter-name", "boatstack", "--write")
            checked = self.run_helper("export", "--repo", target, "--config", CONFIG, "--adapter-name", "boatstack", "--check")
            self.assertIn("PASS", checked.stdout)
            self.assertTrue((target / ".cursor" / "commands" / "auto-plan.md").is_file())
            self.assertTrue((target / ".agents" / "skills" / "boatstack" / "SKILL.md").is_file())
            (target / ".cursor" / "commands" / "auto-plan.md").write_text("drift\n")
            drift = self.run_helper("export", "--repo", target, "--config", CONFIG, "--adapter-name", "boatstack", "--check", expected=1)
            self.assertIn("drift", (drift.stdout + drift.stderr).lower())

    def test_plan_activation_and_pr_preview_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp)
            self.run_command("git", "init", "-b", "main", cwd=repo)
            self.run_command("git", "config", "user.name", "Boatstack Test", cwd=repo)
            self.run_command("git", "config", "user.email", "boatstack@example.invalid", cwd=repo)
            (repo / ".product-loop").mkdir()
            config = json.loads(CONFIG.read_text())
            config["project"]["default_branch"] = "main"
            (repo / ".product-loop" / "project.json").write_text(json.dumps(config) + "\n")
            (repo / "README.md").write_text("# Fixture\n")
            self.run_command("git", "add", ".", cwd=repo)
            self.run_command("git", "commit", "-m", "base", cwd=repo)
            bare = repo / ".git" / "origin.git"
            self.run_command("git", "init", "--bare", bare)
            self.run_command("git", "remote", "add", "origin", bare, cwd=repo)
            self.run_command("git", "push", "-u", "origin", "main", cwd=repo)
            self.run_command("git", "switch", "-c", "feat/direct", cwd=repo)
            (repo / "feature.txt").write_text("value\n")
            self.run_command("git", "add", "feature.txt", cwd=repo)
            self.run_command("git", "commit", "-m", "feature", cwd=repo)
            context = json.loads(self.run_helper("pr-context", "--repo", repo).stdout)
            self.assertEqual(context["mode"], "ad-hoc")
            template = self.run_helper("pr-context", "--repo", repo, "--format", "template")
            self.assertIn("boatstack_pr_version: 4", template.stdout)
            self.assertIn("## Review order", template.stdout)

        demo = REPO / "labs" / "diagram-json"
        checked = self.run_helper("check-plan", "--plan", demo / "plan.md")
        self.assertIn("PASS", checked.stdout)

    def test_installers_verify_downloads_and_support_updates(self) -> None:
        shell = (REPO / "install.sh").read_text()
        powershell = (REPO / "install.ps1").read_text()
        for expected in ("sha256sum", "BOATSTACK_INTEGRATIONS", "BOATSTACK_MODE", "BOATSTACK_VERSION", "--repair"):
            self.assertIn(expected, shell)
        for expected in ("Get-FileHash", "BOATSTACK_INTEGRATIONS", "BOATSTACK_MODE", "BOATSTACK_VERSION", "--repair"):
            self.assertIn(expected, powershell)


if __name__ == "__main__":
    unittest.main()
