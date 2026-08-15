import json
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]


class DocumentationContractTests(unittest.TestCase):
    def test_required_ci_validates_documentation(self) -> None:
        ci = (REPO / ".github" / "workflows" / "ci.yml").read_text()
        self.assertIn("npm run docs:check", ci)

    def test_pages_deployment_is_main_only_and_release_independent(self) -> None:
        workflow = (REPO / ".github" / "workflows" / "docs-pages.yml").read_text()
        self.assertIn("branches: [main]", workflow)
        self.assertNotIn("pull_request:", workflow)
        self.assertIn("actions/configure-pages@v6", workflow)
        self.assertIn("actions/upload-pages-artifact@v5", workflow)
        self.assertIn("actions/deploy-pages@v5", workflow)
        self.assertIn("pages: write", workflow)
        self.assertIn("id-token: write", workflow)
        self.assertNotIn("release.yml", workflow)
        self.assertNotIn("gh release", workflow)

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
