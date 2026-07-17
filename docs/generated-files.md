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
| `.cursor/`, `.agents/`, and `.claude/` Boatstack adapters | Portable host commands and skills | Commit |
| `.github/PULL_REQUEST_TEMPLATE/boatstack.md` | Fallback PR structure | Commit |
| `.cursor/hooks.json`, `.claude/settings.json`, `.codex/hooks.json` | Boatstack fragments merged with existing host settings | Review and commit |
| `.product-loop/bin/` | Verified machine-local helper | Never commit; it is ignored |

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

## Fresh clones and updates

Committed adapters survive a clone; the ignored helper does not. Rerun the installer to restore it. For an update, use a separate `chore/update-boatstack` branch, inspect the generated diff and version provenance, and merge it before unrelated product work.

If generated state looks wrong, run:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
```

Do not delete adapters merely to make a feature diff smaller. If the original installation was never committed, stop and create its infrastructure PR first.
