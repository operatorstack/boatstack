#!/usr/bin/env python3
"""Build a GitHub review while preserving findings without valid diff anchors."""

from __future__ import annotations

import argparse
import json
import os
import posixpath
import re
import subprocess
from pathlib import Path


HUNK = re.compile(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@")


def normalize_path(value: str, workspace: str) -> str | None:
    candidate = value.replace("\\", "/")
    root = workspace.replace("\\", "/").rstrip("/")
    if candidate.startswith(root + "/"):
        candidate = candidate[len(root) + 1 :]
    while candidate.startswith("./"):
        candidate = candidate[2:]
    candidate = posixpath.normpath(candidate)
    if (
        not candidate
        or candidate == "."
        or candidate.startswith("/")
        or candidate == ".."
        or candidate.startswith("../")
        or "/../" in candidate
    ):
        return None
    return candidate


def header_path(line: str) -> str | None:
    value = line[4:].split("\t", 1)[0]
    if value == "/dev/null":
        return None
    if value.startswith(("a/", "b/")):
        value = value[2:]
    return value


def changed_lines(diff: str) -> dict[str, set[tuple[str, int]]]:
    allowed: dict[str, set[tuple[str, int]]] = {"LEFT": set(), "RIGHT": set()}
    old_path: str | None = None
    new_path: str | None = None
    old_line = 0
    new_line = 0
    in_hunk = False
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            old_path = new_path = None
            in_hunk = False
            continue
        if not in_hunk and line.startswith("--- "):
            old_path = header_path(line)
            continue
        if not in_hunk and line.startswith("+++ "):
            new_path = header_path(line)
            continue
        match = HUNK.match(line)
        if match:
            old_line = int(match.group(1))
            new_line = int(match.group(3))
            in_hunk = True
            continue
        if not in_hunk or line.startswith("\\"):
            continue
        if line.startswith("-"):
            if old_path is not None:
                allowed["LEFT"].add((old_path, old_line))
            old_line += 1
        elif line.startswith("+"):
            if new_path is not None:
                allowed["RIGHT"].add((new_path, new_line))
            new_line += 1
        elif line.startswith(" "):
            old_line += 1
            new_line += 1
        else:
            in_hunk = False
    return allowed


def finding_body(finding: dict[str, object]) -> str:
    return (
        f"[P{finding['priority']}] {finding['title']}\n\n"
        f"{finding['body']}\n\nConfidence: {finding['confidence_score']}"
    )


def build_review(
    review: dict[str, object],
    *,
    commit: str,
    workspace: str,
    allowed: dict[str, set[tuple[str, int]]],
) -> dict[str, object]:
    body = (
        "Codex automated review\n\n"
        f"Verdict: {review['overall_correctness']}\n"
        f"Confidence: {review['overall_confidence_score']}\n\n"
        f"{review['overall_explanation']}"
    )
    comments: list[dict[str, object]] = []
    unanchored: list[str] = []
    for finding in review["findings"]:  # type: ignore[index]
        location = finding["code_location"]
        path = normalize_path(location["absolute_file_path"], workspace)
        side = location["side"]
        start = location["line_range"]["start"]
        end = location["line_range"]["end"]
        valid_anchor = (
            path is not None
            and side in allowed
            and start <= end
            and all((path, line) in allowed[side] for line in range(start, end + 1))
        )
        text = finding_body(finding)
        if valid_anchor:
            comment: dict[str, object] = {
                "path": path,
                "line": end,
                "side": side,
                "body": text,
            }
            if start != end:
                comment["start_line"] = start
                comment["start_side"] = side
            comments.append(comment)
            continue
        display_path = path or "unresolved-path"
        unanchored.append(f"{text}\n\nLocation: {display_path}:{start}-{end} ({side})")
    if unanchored:
        body += "\n\nFindings without inline diff anchors\n\n" + "\n\n---\n\n".join(unanchored)
    return {"commit_id": commit, "event": "COMMENT", "body": body, "comments": comments}


def pull_request_diff(workspace: str, base: str, head: str) -> str:
    merge_base = subprocess.run(
        ["git", "merge-base", base, head],
        cwd=workspace,
        check=True,
        text=True,
        capture_output=True,
    ).stdout.strip()
    return subprocess.run(
        [
            "git",
            "-c",
            "core.quotePath=false",
            "diff",
            "--no-ext-diff",
            "--no-renames",
            "--unified=0",
            merge_base,
            head,
        ],
        cwd=workspace,
        check=True,
        text=True,
        capture_output=True,
    ).stdout


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--workspace", required=True)
    parser.add_argument("--base", required=True)
    parser.add_argument("--head", required=True)
    args = parser.parse_args()
    diff = pull_request_diff(args.workspace, args.base, args.head)
    review = json.loads(Path(args.input).read_text())
    payload = build_review(
        review,
        commit=args.head,
        workspace=os.path.abspath(args.workspace),
        allowed=changed_lines(diff),
    )
    Path(args.output).write_text(json.dumps(payload, indent=2) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
