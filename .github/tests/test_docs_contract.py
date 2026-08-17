import json
import re
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]


MARKDOWN_LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")


def unresolved_markdown_links(path: Path) -> list[str]:
    failures: list[str] = []
    for target in MARKDOWN_LINK.findall(path.read_text()):
        target = target.strip().split(" ", 1)[0].strip("<>")
        if target.startswith(("http://", "https://", "mailto:", "#")):
            continue
        relative = target.split("#", 1)[0]
        if relative and not (path.parent / relative).resolve().exists():
            failures.append(target)
    return failures


def yaml_scalar(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def workflow_triggers(workflow: str) -> dict[str, dict[str, list[str]]]:
    lines = workflow.splitlines()
    try:
        start = lines.index("on:") + 1
    except ValueError as error:
        raise AssertionError("workflow has no top-level on mapping") from error

    triggers: dict[str, dict[str, list[str]]] = {}
    current: str | None = None
    for line in lines[start:]:
        content = line.split("#", 1)[0].rstrip()
        if not content.strip():
            continue
        indent = len(content) - len(content.lstrip())
        if indent == 0:
            break
        stripped = content.strip()
        if indent == 2 and stripped.endswith(":"):
            current = stripped[:-1]
            triggers[current] = {}
            continue
        if indent == 4 and current and ":" in stripped:
            key, value = (part.strip() for part in stripped.split(":", 1))
            if value.startswith("[") and value.endswith("]"):
                triggers[current][key] = [
                    item.strip() for item in value[1:-1].split(",") if item.strip()
                ]
    return triggers


def workflow_jobs(workflow: str) -> dict[str, dict[str, object]]:
    jobs: dict[str, dict[str, object]] = {}
    in_jobs = False
    current_job: dict[str, object] | None = None
    current_section = ""
    current_step: dict[str, str] | None = None

    for line in workflow.splitlines():
        content = line.split("#", 1)[0].rstrip()
        if not content.strip():
            continue
        indent = len(content) - len(content.lstrip())
        stripped = content.strip()
        if indent == 0:
            if in_jobs and stripped != "jobs:":
                break
            in_jobs = stripped == "jobs:"
            continue
        if not in_jobs:
            continue
        if indent == 2 and stripped.endswith(":"):
            current_job = {"permissions": {}, "steps": []}
            jobs[stripped[:-1]] = current_job
            current_section = ""
            current_step = None
            continue
        if current_job is None:
            continue
        if indent == 4:
            if stripped == "permissions:":
                current_section = "permissions"
            elif stripped == "steps:":
                current_section = "steps"
            elif ":" in stripped:
                key, value = (part.strip() for part in stripped.split(":", 1))
                current_job[key] = yaml_scalar(value)
                current_section = ""
            continue
        if indent == 6 and current_section == "permissions" and ":" in stripped:
            key, value = (part.strip() for part in stripped.split(":", 1))
            permissions = current_job["permissions"]
            assert isinstance(permissions, dict)
            permissions[key] = yaml_scalar(value)
            continue
        if indent == 6 and current_section == "steps" and stripped.startswith("- "):
            current_step = {}
            steps = current_job["steps"]
            assert isinstance(steps, list)
            steps.append(current_step)
            remainder = stripped[2:]
            if ":" in remainder:
                key, value = (part.strip() for part in remainder.split(":", 1))
                current_step[key] = yaml_scalar(value)
            continue
        if indent == 8 and current_section == "steps" and current_step is not None:
            if ":" in stripped:
                key, value = (part.strip() for part in stripped.split(":", 1))
                current_step[key] = yaml_scalar(value)
    return jobs


def assert_pages_contract(testcase: unittest.TestCase, workflow: str) -> None:
    testcase.assertEqual(
        workflow_triggers(workflow),
        {"push": {"branches": ["main"]}, "workflow_dispatch": {}},
    )
    jobs = workflow_jobs(workflow)
    testcase.assertEqual(set(jobs), {"build", "deploy"})
    testcase.assertEqual(jobs["build"].get("if"), "github.ref == 'refs/heads/main'")
    testcase.assertEqual(jobs["deploy"].get("if"), "github.ref == 'refs/heads/main'")
    testcase.assertEqual(jobs["build"]["permissions"], {"contents": "read"})
    testcase.assertEqual(
        jobs["deploy"]["permissions"],
        {"pages": "write", "id-token": "write"},
    )
    build_steps = jobs["build"]["steps"]
    deploy_steps = jobs["deploy"]["steps"]
    testcase.assertIsInstance(build_steps, list)
    testcase.assertIsInstance(deploy_steps, list)
    testcase.assertIn("npm run docs:check", [step.get("run") for step in build_steps])
    testcase.assertIn("actions/configure-pages@v6", [step.get("uses") for step in build_steps])
    testcase.assertIn("actions/upload-pages-artifact@v5", [step.get("uses") for step in build_steps])
    testcase.assertIn("actions/deploy-pages@v5", [step.get("uses") for step in deploy_steps])
    testcase.assertNotIn("release.yml", json.dumps(jobs))
    testcase.assertNotIn("gh release", json.dumps(jobs))


class DocumentationContractTests(unittest.TestCase):
    def test_documentation_entrypoint_links_resolve(self) -> None:
        for path in (
            REPO / "README.md",
            REPO / "docs" / "index.md",
            REPO / "docs" / "concepts" / "index.md",
            REPO / "docs" / "architecture" / "index.md",
        ):
            self.assertEqual(unresolved_markdown_links(path), [], path)

    def test_concept_and_architecture_indexes_are_complete(self) -> None:
        concepts = {
            "supervisory-control.md",
            "control-programs-flows-and-runs.md",
            "state-observation-objectives-and-targets.md",
            "transitions-operators-effects-and-capabilities.md",
            "authority-identity-and-delegation.md",
            "prescriptions-verification-receipts-and-recovery.md",
            "invocation-parameters-and-foreground-work.md",
        }
        architecture = {
            "kernel.md",
            "compiler-and-artifacts.md",
            "runtime-persistence-and-control-bundles.md",
            "surfaces-and-host-projections.md",
            "software-delivery-domain.md",
            "conformance-and-generated-evidence.md",
        }
        for directory, expected in (("concepts", concepts), ("architecture", architecture)):
            index = (REPO / "docs" / directory / "index.md").read_text()
            for name in expected:
                self.assertTrue((REPO / "docs" / directory / name).exists(), name)
                self.assertIn(f"({name})", index)

    def test_v1_documents_are_not_retained_as_current_or_historical_authority(self) -> None:
        for name in (
            "boatstack-kernel.md",
            "boatstack-closure-report.md",
            "boatstack-v1-authority-inventory.md",
        ):
            self.assertFalse(any((REPO / "docs").rglob(name)), name)
        history = (REPO / "docs" / "history" / "index.md").read_text().lower()
        self.assertIn("does not define current", history)

    def test_generated_architecture_evidence_declares_ownership(self) -> None:
        ownership = (REPO / "docs" / "generated-files.md").read_text()
        generated = (
            "boatstack-transition-catalog.md",
            "boatstack-transition-catalog.mmd",
            "boatstack-standard-flow.mmd",
            "boatstack-locus-safety.json",
            "boatstack-locus-liveness.json",
        )
        for name in generated:
            self.assertIn(name, ownership)
            self.assertIn(f"catalog --format", ownership)
        for name in generated[:3]:
            text = (REPO / "docs" / "architecture" / name).read_text()[:300]
            self.assertRegex(text, r"(?i)generated.*do not edit")

    def test_projection_reference_matches_canonical_vocabulary_and_paths(self) -> None:
        source = (REPO / "boatstack" / "internal" / "hostprojection" / "projection.go").read_text()
        canonical = set(re.findall(r'^\s*\w+\s+ID\s+=\s+"([a-z]+)"', source, re.MULTILINE))
        self.assertEqual(canonical, {"codex", "claude", "cursor", "gemini"})
        generated = (REPO / "docs" / "generated-files.md").read_text().lower()
        surfaces = (REPO / "docs" / "architecture" / "surfaces-and-host-projections.md").read_text().lower()
        for projection in canonical:
            self.assertIn(projection, generated)
            self.assertIn(projection, surfaces)
        for path in (
            ".agents/skills/<program>-<entry>/skill.md",
            ".claude/skills/<program>-<entry>/skill.md",
            ".cursor/commands/<program>-<entry>.md",
            ".gemini/skills/<program>-<entry>/skill.md",
        ):
            self.assertIn(path, generated)

    def test_public_docs_exclude_private_and_volatile_content(self) -> None:
        public = "\n".join(
            path.read_text(errors="replace")
            for path in (REPO / "docs").rglob("*.md")
        ) + (REPO / "README.md").read_text()
        for forbidden in (
            "/Users/",
            "ChatGPT conversation",
            "local-LLM",
        ):
            self.assertNotIn(forbidden, public)
        for concept in (REPO / "docs" / "concepts").glob("*.md"):
            self.assertIsNone(
                re.search(r"\b\d+\s+(?:executable\s+)?transitions\b", concept.read_text(), re.IGNORECASE),
                concept,
            )

    def test_control_program_schema_reference_matches_sdk_constant(self) -> None:
        source = (REPO / "packages" / "boatstack" / "src" / "index.ts").read_text()
        revision = re.search(r"CONTROL_PROGRAM_SCHEMA_REVISION = (\d+)", source)
        self.assertIsNotNone(revision)
        reference = (REPO / "docs" / "control-program-ir.md").read_text()
        self.assertIn(f"`schema_revision: {revision.group(1)}`", reference)

    def test_all_typedoc_project_documents_exist(self) -> None:
        config = json.loads((REPO / "typedoc.json").read_text())
        for document in config["projectDocuments"]:
            self.assertTrue((REPO / document).is_file(), document)

    def test_required_ci_validates_flow_sdk_and_documentation(self) -> None:
        ci = (REPO / ".github" / "workflows" / "ci.yml").read_text()
        steps = workflow_jobs(ci)["flow-sdk"]["steps"]
        commands = [step.get("run") for step in steps]
        self.assertIn(
            "npm ci && npm run test:flow-sdk && npm run docs:check", commands
        )
        self.assertTrue(
            any(
                command and "TestSoftwareDeliverySugar" in command
                for command in commands
            )
        )

    def test_pages_deployment_is_main_only_and_release_independent(self) -> None:
        workflow = (REPO / ".github" / "workflows" / "docs-pages.yml").read_text()
        assert_pages_contract(self, workflow)

    def test_trigger_parser_rejects_comment_substitution(self) -> None:
        malformed = """on:
  push:
    # branches: [main]
  pull_request:
  workflow_dispatch:

jobs: {}
"""
        self.assertEqual(
            workflow_triggers(malformed),
            {"push": {}, "pull_request": {}, "workflow_dispatch": {}},
        )

    def test_job_parser_rejects_commented_controls(self) -> None:
        malformed = """on:
  push:
    branches: [main]
  workflow_dispatch:

jobs:
  build:
    # if: github.ref == 'refs/heads/main'
    permissions:
      # contents: read
    steps:
      - name: Missing controls
        # run: npm run docs:check
        # uses: actions/upload-pages-artifact@v5
  deploy:
    # if: github.ref == 'refs/heads/main'
    permissions:
      # pages: write
      # id-token: write
    steps:
      - name: Missing deployment
        # uses: actions/deploy-pages@v5
"""
        with self.assertRaises(AssertionError):
            assert_pages_contract(self, malformed)

    def test_typedoc_covers_both_public_packages(self) -> None:
        config = json.loads((REPO / "typedoc.json").read_text())
        self.assertEqual(config["entryPointStrategy"], "packages")
        self.assertEqual(
            set(config["entryPoints"]),
            {
                "packages/boatstack",
                "packages/boatstack-software-delivery",
            },
        )
        self.assertEqual(config["out"], "build/docs/html")
        self.assertEqual(config["json"], "build/docs/api.json")
        self.assertTrue(config["treatWarningsAsErrors"])


if __name__ == "__main__":
    unittest.main()
