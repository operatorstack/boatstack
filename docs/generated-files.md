<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# What Boatstack adds to a repository

**For:** anyone reviewing an installation or feature PR.
**Outcome:** know what to commit, what can be edited, and what Boatstack regenerates.

Boatstack creates installation state once and feature evidence repeatedly. Keeping those two groups separate is what makes later product diffs understandable.

## Installation PR

| Path | What it is | What you do |
|---|---|---|
| `.boatstack-project.json` | Project-owned repository facts and commands | Review and edit |
| `.product-loop/` references, templates, hooks, and generated lock | Shared Boatstack runtime | Commit; regenerate rather than hand-edit |
| `.cursor/`, `.agents/`, and `.claude/` Boatstack adapters | Cursor commands, the Codex router, and Claude's visible workflow skills plus hidden natural-language router | Commit |
| `.github/PULL_REQUEST_TEMPLATE/boatstack.md` | Fallback PR structure | Commit |
| `.cursor/hooks.json`, `.claude/settings.json`, `.codex/hooks.json` | Boatstack fragments merged with existing host settings | Review and commit |
| `.product-loop/bin/` | Verified worktree-local helper and install lock | Never commit; it is ignored and hydrates automatically |
| `release-notes/*.md` | Canonical user-facing messages reused by sync PRs and tagged releases | Generated; edit in Intelligence Flow |

The installer prints the exact staging command and runs `doctor`. Put this state in `chore/install-boatstack`, review it once, and merge it before feature work.

## Feature PR

Boatstack stores feature artifacts under `.product-loop/features/<feature>/`:

| Artifact | Why it exists |
|---|---|
| `source-plan.md` | Preserves the host's first interpretation of the request |
| `feature-spec.md` | Defines the accepted outcome and exclusions |
| `questions.md` | Separates repository facts, proposals, human answers, and unknowns |
| `gaps.md` | Keeps deferred or incomplete work visible |
| `test-plan.md` | Connects promised outcomes to checks |
| `plan.md` | Holds the human-readable approved plan |
| `approval.md` | Records who approved which exact plan |
| `compiled/` and `plan.lock.json` | Prove that build activated the approved inputs without drift |
| `evidence.md` | Records commands, results, review findings, and gate status |
| `pr.md` | Contains the exact reviewer-ready title and body preview |

These artifacts travel with the feature because they explain what was agreed and what supports completion. Changing the source plan, spec, or plan invalidates approval until the plan gate runs again.

## Existing branches

When Boatstack improves a branch that did not use the full workflow, it stores the preview under `.product-loop/pr-briefs/<branch>/pr.md`. It summarizes the actual commits, diff, and observed checks without creating approval or gate history. Missing evidence remains `NOT_VERIFIED`.

The preview's frontmatter is publication metadata; the remaining Markdown is the exact GitHub body. The preview is excluded from its own product-diff fingerprint, but any other diff or evidence change makes it stale.

## Worktrees, fresh clones, and updates

One verified runtime is cached under the clone's Git common directory and keyed by Boatstack version, source commit, operating system, and architecture. Linked worktrees share that cache. Their first guarded command atomically restores the ignored local helper and install lock, then evaluates the original command. Hydration uses no network and produces no tracked diff.

Independent clones do not share a Git common directory. Committed adapters survive a clone, but the ignored helper and repository-family cache do not; run the installer once in the new clone.

For an update, run `/boatstack-update` from a clean, current default branch. Boatstack creates `chore/update-boatstack-v<version>`, verifies the tagged release and checksum, preserves integrations, and shows the exact generated diff before asking for `open update PR`. Release-check state in `.product-loop/bin/update-state.json` and the platform helper remain ignored; the adapters, generated lock, hook fragments, and merged host settings belong in the update PR.

An update refuses feature branches, dirty worktrees, stale default branches, changed generated files, and user-owned collisions. It never merges its own PR.

If generated state looks wrong, run:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
```

Do not delete adapters merely to make a feature diff smaller. If the original installation was never committed, stop and create its infrastructure PR first.
