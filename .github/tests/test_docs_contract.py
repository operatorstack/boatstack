import json
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]


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


class DocumentationContractTests(unittest.TestCase):
    def test_required_ci_validates_documentation(self) -> None:
        ci = (REPO / ".github" / "workflows" / "ci.yml").read_text()
        self.assertIn("npm run docs:check", ci)

    def test_pages_deployment_is_main_only_and_release_independent(self) -> None:
        workflow = (REPO / ".github" / "workflows" / "docs-pages.yml").read_text()
        self.assertEqual(
            workflow_triggers(workflow),
            {"push": {"branches": ["main"]}, "workflow_dispatch": {}},
        )
        self.assertEqual(workflow.count("if: github.ref == 'refs/heads/main'"), 2)
        self.assertIn("npm run docs:check", workflow)
        self.assertIn("actions/configure-pages@v6", workflow)
        self.assertIn("actions/upload-pages-artifact@v5", workflow)
        self.assertIn("actions/deploy-pages@v5", workflow)
        self.assertIn("pages: write", workflow)
        self.assertIn("id-token: write", workflow)
        self.assertNotIn("release.yml", workflow)
        self.assertNotIn("gh release", workflow)

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
