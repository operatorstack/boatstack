#!/usr/bin/env python3
"""Validate Boatstack's append-only release-note contract."""

from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path


NAME_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$")
HEADING_PATTERN = re.compile(r"^### [^\s].+$")
RELEASE_NOTES = Path("release-notes")


def validate_release_note(path: Path) -> None:
    if not NAME_PATTERN.fullmatch(path.name):
        raise ValueError(f"{path}: name must match YYYY-MM-DD-<slug>.md")
    try:
        content = path.read_text(encoding="utf-8")
    except UnicodeDecodeError as error:
        raise ValueError(f"{path}: release note must be UTF-8") from error
    if not content.endswith("\n"):
        raise ValueError(f"{path}: release note must end with a newline")
    lines = content.splitlines()
    if not lines or not HEADING_PATTERN.fullmatch(lines[0]):
        raise ValueError(f"{path}: first line must be a level-three Markdown heading")
    if not any(line.strip() for line in lines[1:]):
        raise ValueError(f"{path}: release note must describe user impact")


def validate_directory(repo: Path) -> None:
    root = repo / RELEASE_NOTES
    if not root.is_dir():
        raise ValueError(f"{root}: release-note directory is missing")
    unexpected = sorted(
        path for path in root.iterdir()
        if not path.is_file() or path.is_symlink() or path.suffix != ".md"
    )
    if unexpected:
        raise ValueError(f"{unexpected[0]}: only direct Markdown files are allowed")
    notes = sorted(root.glob("*.md"))
    if not notes:
        raise ValueError(f"{root}: at least one release note is required")
    for note in notes:
        validate_release_note(note)


def git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, text=True, capture_output=True, check=False
    )
    if result.returncode != 0:
        raise ValueError(result.stderr.strip() or f"git {' '.join(args)} failed")
    return result.stdout.strip()


def check_policy(repo: Path, base: str, head: str) -> None:
    output = git(repo, "diff", "--name-status", "--no-renames", base, head)
    changes: list[tuple[str, Path]] = []
    for line in output.splitlines():
        if line:
            status, value = line.split("\t", 1)
            changes.append((status, Path(value)))
    if not changes:
        return
    note_changes = [
        (status, path)
        for status, path in changes
        if path.is_relative_to(RELEASE_NOTES)
    ]
    rewritten = [f"{status}\t{path}" for status, path in note_changes if status != "A"]
    if rewritten:
        raise ValueError(
            "release notes are append-only; add a correction fragment instead:\n  "
            + "\n  ".join(rewritten)
        )
    added = [repo / path for status, path in note_changes if status == "A"]
    if not added:
        raise ValueError("Boatstack changes require a new file under release-notes/")
    for note in sorted(added):
        validate_release_note(note)


def preflight(repo: Path, remote: str, base_branch: str, head: str) -> None:
    dirty = git(repo, "status", "--porcelain", "--untracked-files=all")
    if dirty:
        raise ValueError("commit or remove uncommitted changes before preflight")
    git(repo, "fetch", "--quiet", remote, base_branch)
    check_policy(repo, f"refs/remotes/{remote}/{base_branch}", head)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    validate = subparsers.add_parser("validate")
    validate.add_argument("--repo", type=Path, default=Path("."))
    check = subparsers.add_parser("check-policy")
    check.add_argument("--repo", type=Path, required=True)
    check.add_argument("--base", required=True)
    check.add_argument("--head", required=True)
    before = subparsers.add_parser("preflight")
    before.add_argument("--repo", type=Path, required=True)
    before.add_argument("--remote", default="origin")
    before.add_argument("--base-branch", default="main")
    before.add_argument("--head", default="HEAD")
    args = parser.parse_args()
    try:
        repo = args.repo.resolve()
        validate_directory(repo)
        if args.command == "check-policy":
            check_policy(repo, args.base, args.head)
        elif args.command == "preflight":
            preflight(repo, args.remote, args.base_branch, args.head)
    except ValueError as error:
        print(f"BLOCKED: {error}")
        return 1
    print("PASS: Boatstack release-note contract is satisfied")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
