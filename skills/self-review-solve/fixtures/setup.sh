#!/usr/bin/env bash
# Test-only fixture: build a scratch repository so the workflow's real
# commands never touch the actual repository's review state. Runs with the
# skill directory as the working directory and YIELD_FIXTURE=1.
set -eu

root="$(git rev-parse --show-toplevel)"
scratch="$PWD/fixtures/tmp/repo"
rm -rf "$PWD/fixtures/tmp"
mkdir -p "$scratch"

git -C "$scratch" init -q -b main
git -C "$scratch" config user.email "self-review-fixture@example.invalid"
git -C "$scratch" config user.name "Self Review Fixture"

mkdir -p "$scratch/.github/codex"
cp "$root/.github/codex/review-prompt.md" "$scratch/.github/codex/"
cp "$root/.github/codex/review-output-schema.json" "$scratch/.github/codex/"
printf 'package subject\n' > "$scratch/subject.go"
git -C "$scratch" add -A
git -C "$scratch" commit -qm "base"

git -C "$scratch" checkout -qb feature
printf 'package subject\n\nfunc Value() int { return 1 }\n' > "$scratch/subject.go"
git -C "$scratch" commit -qam "change under review"

# Regression: untracked files never affect what a review binds, so the
# workflow's gates must ignore them just as the reviewer does.
printf 'editor scratch\n' > "$scratch/untracked-note.txt"

touch "$PWD/fixtures/tmp/active"
