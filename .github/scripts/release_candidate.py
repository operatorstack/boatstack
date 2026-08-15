#!/usr/bin/env python3
"""Classify an exact Boatstack source for a stable patch release."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


STABLE_TAG = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")


class ReleaseBlocked(RuntimeError):
    """The selected source cannot produce a stable release."""


def git(repository: Path, *arguments: str) -> str:
    result = subprocess.run(
        ["git", *arguments],
        cwd=repository,
        text=True,
        capture_output=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ReleaseBlocked(f"git {' '.join(arguments)} failed: {detail}")
    return result.stdout.strip()


def stable_tags(repository: Path, source: str) -> list[tuple[tuple[int, int, int], str]]:
    tags: list[tuple[tuple[int, int, int], str]] = []
    for tag in git(repository, "tag", "--merged", source, "--list", "v*").splitlines():
        match = STABLE_TAG.fullmatch(tag)
        if match is not None:
            tags.append((tuple(map(int, match.groups())), tag))
    return sorted(tags, reverse=True)


def classify(repository: Path, source: str) -> dict[str, str]:
    resolved_source = git(repository, "rev-parse", "--verify", f"{source}^{{commit}}")
    if resolved_source != source:
        raise ReleaseBlocked(f"release source must be an exact commit SHA: {source}")
    head = git(repository, "rev-parse", "HEAD")
    if head != source:
        raise ReleaseBlocked(f"checked-out source {head} does not match release source {source}")

    tags = stable_tags(repository, source)
    if not tags:
        raise ReleaseBlocked("stable patch releases require an existing vMAJOR.MINOR.PATCH tag")
    version, latest_tag = tags[0]

    rewritten = git(
        repository,
        "diff",
        "--name-only",
        "--diff-filter=MD",
        "--no-renames",
        latest_tag,
        source,
        "--",
        "release-notes/*.md",
    )
    if rewritten:
        raise ReleaseBlocked(f"Boatstack release notes are append-only: {rewritten.replace(chr(10), ', ')}")

    added = git(
        repository,
        "diff",
        "--name-only",
        "--diff-filter=A",
        "--no-renames",
        latest_tag,
        source,
        "--",
        "release-notes/*.md",
    )
    next_tag = f"v{version[0]}.{version[1]}.{version[2] + 1}"
    if added and git(repository, "tag", "--list", next_tag):
        raise ReleaseBlocked(f"next stable tag already exists: {next_tag}")

    return {
        "release_required": "true" if added else "false",
        "latest_tag": latest_tag,
        "next_tag": next_tag,
        "release_source": source,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--source", required=True)
    parser.add_argument("--github-output", type=Path)
    arguments = parser.parse_args()

    try:
        result = classify(arguments.repo.resolve(), arguments.source)
    except ReleaseBlocked as error:
        print(f"BLOCKED: {error}", file=sys.stderr)
        return 2

    if arguments.github_output is not None:
        with arguments.github_output.open("a", encoding="utf-8") as output:
            for key, value in result.items():
                output.write(f"{key}={value}\n")
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
