<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# What Boatstack generates

Boatstack creates two different kinds of repository state. Keeping them separate is what makes feature diffs reviewable.

## Installation state: commit in its own PR

| Path | Ownership | What to do |
|---|---|---|
| `.boatstack-project.json` | Project-owned input | Review and edit repository facts, real commands, context starting points, and policy. |
| `.product-loop/project.json`, references, templates, and `generated.lock.json` | Boatstack-generated | Commit; regenerate through the installer instead of editing directly. |
| `.cursor/commands/` and `.cursor/rules/` | Boatstack-generated Cursor adapter | Commit so slash commands survive clones, branch changes, and cleanup. |
| `.agents/skills/boatstack/` | Boatstack-generated Codex/open-agent adapter | Commit. |
| `.claude/skills/boatstack/` | Boatstack-generated Claude adapter | Commit. |
| `.github/PULL_REQUEST_TEMPLATE/boatstack.md` | Boatstack-generated PR adapter | Commit. |
| `.product-loop/bin/` | Machine-local | Do not commit. It contains the verified platform helper and local install lock and is ignored. |

The installation manifest `.product-loop/generated.lock.json` describes generated infrastructure. It is different from a feature's `plan.lock.json`, which proves that a specific approved plan activated without drift.

## Feature state: commit with the feature PR

| Artifact | Meaning |
|---|---|
| `source-plan.md` | Preserved host Plan-mode interpretation of the original request. |
| `feature-spec.md` | Accepted outcome, boundaries, behavior, and criteria. |
| `questions.md` | Discovered facts, proposed choices, human answers, and open unknowns. |
| `gaps.md` | Known incomplete or deferred work with impact and revisit trigger. |
| `test-plan.md` | Criterion-to-oracle and validation design. |
| `plan.md` | Canonical human-readable and structured approved plan. |
| `approval.md` | Named human, timestamp, and exact plan fingerprint. |
| `compiled/` | Build-time task graph, test matrix, and evidence skeleton. |
| `plan.lock.json` | Content-addressed build activation record. |
| `evidence.md` | Commands, results, findings, runtime checks, and gate status. |
| `pr.md` | Exact reviewer-ready title/body preview, bound to the committed product diff and current evidence. |

These files travel with the product diff because they explain what was approved and why completion is defensible. Changes to the source plan, spec, or `plan.md` invalidate approval until the plan gate runs again.

For an existing or ad-hoc branch, Boatstack stores the same exact preview under `.product-loop/pr-briefs/<branch>/pr.md`. It is committed with that branch but does not create approval, lock, or gate provenance. Missing evidence stays visibly `NOT_VERIFIED`.

The `pr.md` frontmatter is non-rendered publication metadata; the remaining Markdown is the exact GitHub body. Edit the reviewer narrative through Boatstack, preview it, then explicitly reply `open PR` or `update PR`. Any product diff or evidence change makes the preview stale. The preview artifact itself is excluded from the product-diff fingerprint so committing it does not invalidate itself.

## Fresh clones and updates

Committed adapters remain available after cloning. Restore only the ignored helper by rerunning the installer from the repository root. For an update, create a new `chore/update-boatstack` branch, rerun the installer, inspect the generated diff and version provenance, and merge it as a separate infrastructure PR.

Never delete untracked adapters merely to make a feature diff smaller. If Boatstack was installed without committing its infrastructure, stop and create the installation PR first. Run this read-only check whenever commands disappear or generated state looks suspicious:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
```
