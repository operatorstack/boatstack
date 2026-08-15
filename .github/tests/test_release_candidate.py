from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / ".github" / "scripts" / "release_candidate.py"


class ReleaseCandidateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary.name)
        self.git("init", "--initial-branch=main")
        self.git("config", "user.email", "release-test@example.invalid")
        self.git("config", "user.name", "Release Test")
        self.write("release-notes/base.md", "### Base release\n")
        self.commit("Create base release")
        self.git("tag", "v1.2.3")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def git(self, *arguments: str) -> str:
        result = subprocess.run(
            ["git", *arguments],
            cwd=self.repository,
            text=True,
            capture_output=True,
            check=True,
        )
        return result.stdout.strip()

    def write(self, name: str, content: str) -> None:
        path = self.repository / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def commit(self, message: str) -> str:
        self.git("add", ".")
        self.git("commit", "-m", message)
        return self.git("rev-parse", "HEAD")

    def classify(self, source: str | None = None) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                "python3",
                str(SCRIPT),
                "--repo",
                str(self.repository),
                "--source",
                source or self.git("rev-parse", "HEAD"),
            ],
            text=True,
            capture_output=True,
        )

    def classify_with_output(self, output: Path) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                "python3",
                str(SCRIPT),
                "--repo",
                str(self.repository),
                "--source",
                self.git("rev-parse", "HEAD"),
                "--github-output",
                str(output),
            ],
            text=True,
            capture_output=True,
        )

    def test_no_unreleased_note_is_a_no_op(self) -> None:
        result = self.classify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["release_required"], "false")

    def test_added_note_selects_next_patch_tag(self) -> None:
        self.write("release-notes/change.md", "### Changed behavior\n")
        source = self.commit("Add release-bearing change")
        result = self.classify(source)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            {
                "latest_tag": "v1.2.3",
                "next_tag": "v1.2.4",
                "release_required": "true",
                "release_source": source,
            },
        )

    def test_github_output_matches_json_result(self) -> None:
        self.write("release-notes/change.md", "### Changed behavior\n")
        self.commit("Add release-bearing change")
        output = self.repository / "github-output"
        result = self.classify_with_output(output)
        self.assertEqual(result.returncode, 0, result.stderr)
        values = dict(line.split("=", 1) for line in output.read_text().splitlines())
        self.assertEqual(values, json.loads(result.stdout))

    def test_modified_release_note_is_blocked(self) -> None:
        self.write("release-notes/base.md", "### Rewritten release\n")
        self.commit("Rewrite release note")
        result = self.classify()
        self.assertEqual(result.returncode, 2)
        self.assertIn("release notes are append-only", result.stderr)

    def test_deleted_release_note_is_blocked(self) -> None:
        (self.repository / "release-notes/base.md").unlink()
        self.git("add", "-u")
        self.git("commit", "-m", "Delete release note")
        result = self.classify()
        self.assertEqual(result.returncode, 2)
        self.assertIn("release notes are append-only", result.stderr)

    def test_source_must_match_checked_out_head(self) -> None:
        tagged_source = self.git("rev-parse", "HEAD")
        self.write("release-notes/change.md", "### Changed behavior\n")
        self.commit("Add release-bearing change")
        result = self.classify(tagged_source)
        self.assertEqual(result.returncode, 2)
        self.assertIn("does not match release source", result.stderr)

    def test_prerelease_and_malformed_tags_do_not_replace_latest_stable(self) -> None:
        self.git("tag", "v9.0.0-rc.1")
        self.git("tag", "version-nine")
        self.write("release-notes/change.md", "### Changed behavior\n")
        self.commit("Add release-bearing change")
        result = self.classify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["latest_tag"], "v1.2.3")

    def test_existing_candidate_tag_on_unrelated_history_is_blocked(self) -> None:
        unrelated = self.git("commit-tree", "HEAD^{tree}", "-m", "Unrelated release")
        self.git("tag", "v1.2.4", unrelated)
        self.write("release-notes/change.md", "### Changed behavior\n")
        self.commit("Add release-bearing change")
        result = self.classify()
        self.assertEqual(result.returncode, 2)
        self.assertIn("next stable tag already exists", result.stderr)

    def test_repository_without_stable_tag_is_blocked(self) -> None:
        self.git("tag", "-d", "v1.2.3")
        self.git("tag", "v1.2.3-rc.1")
        result = self.classify()
        self.assertEqual(result.returncode, 2)
        self.assertIn("existing vMAJOR.MINOR.PATCH tag", result.stderr)


if __name__ == "__main__":
    unittest.main()
