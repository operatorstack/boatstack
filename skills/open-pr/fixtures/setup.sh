#!/usr/bin/env bash
# Test-only fixture: build a scratch repository with a local bare remote, a
# sealed review attestation, and a stub `gh`, so the workflow's real commands
# never push anywhere real and never open a real pull request. Runs with the
# skill directory as the working directory and YIELD_FIXTURE=1.
set -eu

root="$(git rev-parse --show-toplevel)"
scratch="$PWD/fixtures/tmp/repo"
rm -rf "$PWD/fixtures/tmp"
mkdir -p "$scratch"

git -C "$scratch" init -q -b main
git -C "$scratch" config user.email "open-pr-fixture@example.invalid"
git -C "$scratch" config user.name "Open PR Fixture"

mkdir -p "$scratch/.github/codex"
cp "$root/.github/codex/review-prompt.md" "$scratch/.github/codex/"
cp "$root/.github/codex/review-output-schema.json" "$scratch/.github/codex/"
printf 'package subject\n' > "$scratch/subject.go"
git -C "$scratch" add -A
git -C "$scratch" commit -qm "base"

git -C "$scratch" checkout -qb feature
printf 'package subject\n\nfunc Value() int { return 1 }\n' > "$scratch/subject.go"
git -C "$scratch" commit -qam "change under review"

# A local bare remote so the workflow's real `git push` has somewhere safe.
git init -q --bare "$PWD/fixtures/tmp/origin.git"
git -C "$scratch" remote add origin "$PWD/fixtures/tmp/origin.git"

# Drive the real reviewer to a sealed, committed attestation so the
# workflow's verification gate passes for the scratch head.
reviewer="$PWD/fixtures/tmp/boatstack-reviewer"
go build -C "$root/boatstack" -o "$reviewer" ./cmd/boatstack-reviewer
cat > "$PWD/fixtures/tmp/correct-review.json" <<'REVIEW'
{
  "findings": [],
  "overall_correctness": "patch is correct",
  "overall_explanation": "Fixture review: the single-function addition introduces no defect at the reporting threshold.",
  "overall_confidence_score": 0.9
}
REVIEW
"$reviewer" submit --repo "$scratch" --base main --findings "$PWD/fixtures/tmp/correct-review.json" --actor open-pr-fixture > /dev/null
"$reviewer" seal --repo "$scratch" --base main > /dev/null
git -C "$scratch" add .github/reviews
git -C "$scratch" commit -qm "Seal converged self-review attestation"

# A stub gh that records its invocation and prints a plausible URL.
mkdir -p "$PWD/fixtures/tmp/bin"
cat > "$PWD/fixtures/tmp/bin/gh" <<'STUB'
#!/bin/sh
printf '%s\n' "$@" > "$(dirname "$0")/../gh-invocation.txt"
echo "https://github.com/example/boatstack/pull/999"
STUB
chmod +x "$PWD/fixtures/tmp/bin/gh"

touch "$PWD/fixtures/tmp/active"
