import json
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]


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
