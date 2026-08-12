import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[1] / "scripts" / "build_codex_github_review.py"
SPEC = importlib.util.spec_from_file_location("build_codex_github_review", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class CodexReviewPublishTests(unittest.TestCase):
    def test_invalid_inline_location_is_preserved_in_review_body(self) -> None:
        diff = """diff --git a/review.go b/review.go
--- a/review.go
+++ b/review.go
@@ -2 +2 @@
-old
+new
"""
        review = {
            "findings": [
                self.finding("anchored", 2),
                self.finding("not changed", 9),
            ],
            "overall_correctness": "patch is incorrect",
            "overall_explanation": "One location is outside the pull request diff.",
            "overall_confidence_score": 0.9,
        }
        payload = MODULE.build_review(
            review,
            commit="head",
            workspace="/workspace",
            allowed=MODULE.changed_lines(diff),
        )
        self.assertEqual(len(payload["comments"]), 1)
        self.assertEqual(payload["comments"][0]["line"], 2)
        self.assertIn("Findings without inline diff anchors", payload["body"])
        self.assertIn("not changed", payload["body"])
        self.assertIn("review.go:9-9 (RIGHT)", payload["body"])

    def test_outside_workspace_path_cannot_become_inline_comment(self) -> None:
        review = {
            "findings": [self.finding("outside", 2, path="/tmp/other.go")],
            "overall_correctness": "patch is incorrect",
            "overall_explanation": "The path is not repository-relative.",
            "overall_confidence_score": 0.8,
        }
        payload = MODULE.build_review(
            review,
            commit="head",
            workspace="/workspace",
            allowed={"LEFT": set(), "RIGHT": {("review.go", 2)}},
        )
        self.assertEqual(payload["comments"], [])
        self.assertIn("unresolved-path", payload["body"])

    def test_pull_request_diff_excludes_base_only_changes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory)
            self.git(repository, "init", "-q")
            self.git(repository, "config", "user.email", "review@example.invalid")
            self.git(repository, "config", "user.name", "Review Test")
            (repository / "base.txt").write_text("original\n")
            (repository / "head.txt").write_text("original\n")
            self.git(repository, "add", ".")
            self.git(repository, "commit", "-q", "-m", "initial")
            common = self.git(repository, "rev-parse", "HEAD")

            (repository / "base.txt").write_text("base only\n")
            self.git(repository, "commit", "-q", "-am", "base")
            base = self.git(repository, "rev-parse", "HEAD")

            self.git(repository, "checkout", "-q", "--detach", common)
            (repository / "head.txt").write_text("head only\n")
            self.git(repository, "commit", "-q", "-am", "head")
            head = self.git(repository, "rev-parse", "HEAD")

            diff = MODULE.pull_request_diff(str(repository), base, head)
            self.assertIn("head.txt", diff)
            self.assertNotIn("base.txt", diff)

    @staticmethod
    def finding(title: str, line: int, path: str = "/workspace/review.go") -> dict:
        return {
            "title": title,
            "body": "Finding detail.",
            "confidence_score": 0.95,
            "priority": 1,
            "code_location": {
                "absolute_file_path": path,
                "side": "RIGHT",
                "line_range": {"start": line, "end": line},
            },
        }

    @staticmethod
    def git(repository: Path, *args: str) -> str:
        return subprocess.run(
            ["git", *args],
            cwd=repository,
            check=True,
            text=True,
            capture_output=True,
        ).stdout.strip()


if __name__ == "__main__":
    unittest.main()
