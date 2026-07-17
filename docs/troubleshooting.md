<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# Troubleshooting Boatstack

## Cursor does not recognize a slash command

Cursor discovers project commands from `.cursor/commands/*.md`. Check the installation:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
ls .cursor/commands
```

If commands are missing, rerun the installer and reload the Cursor window. Commit the restored installation state in a dedicated PR; otherwise a cleanup, branch change, or fresh clone can remove untracked commands again.

## `/auto-plan` says no source plan exists

Boatstack will not invent a source plan. Finish the host's Plan-mode exploration and save it. If the host does not expose the active plan path, place exactly one non-empty plan under `.product-loop/intake/`, then rerun `/auto-plan`. Pass an explicit path only when discovery reports multiple candidates.

## Plan mode blocks the normal Write tool

Boatstack planning remains Markdown-only. The adapter may use the bounded `planning-write` helper for known feature documents; it must not use arbitrary redirection to bypass the host or write product code. If the host cannot support even that bounded operation, return `WAITING_FOR_HOST_WRITE_PERMISSION` instead of leaving planning early.

## `/build` says `READY_FOR_BUILD`

The plan is approved, but the host is still read-only. Accept the host's normal transition into its execution-capable surface and rerun `/build`. Boatstack does not compile tasks or create a lock until product-code writes are available.

## Approval is stale

The source plan, feature spec, or complete `plan.md` changed after approval. Return to `/auto-plan`, review the new fingerprint at `/plan-gate`, and approve the revised plan. Never edit the fingerprint in `approval.md` manually.

## A gate reports `PASS_WITH_GAPS`

The proven criteria passed, while named non-critical gaps remain. The evidence must identify their impact, owner, reason, affected criteria, and revisit trigger. Any critical safety, correctness, or product-acceptance gap is `BLOCKED`, not `PASS_WITH_GAPS`.

## A pre-push hook fails on unrelated base-branch code

Reproduce the failure against the target branch. If it is pre-existing, keep the repair in a separate PR. A bypass is allowed only when repository policy permits it and the human explicitly authorizes it; record that evidence. Do not quietly add unrelated repairs to the approved feature branch.

## Non-interactive installation cannot detect tests

Boatstack recognizes common package-manager tests, `scripts/check.sh`, Go, Rust, Make, and Python/pytest projects. If the repository uses a custom command, run the installer interactively or create `.boatstack-project.json` with the real test command. Boatstack will not invent a command merely to complete installation.

## A fresh clone has adapters but no helper

This is expected: `.product-loop/bin/` is machine-local and ignored. Rerun the installer from the repository root; the generated diff should remain clean when the committed configuration and installed Boatstack version match.

## The PR preview became stale

Boatstack binds `pr.md` to the current committed product diff and evidence. A new commit, amended evidence, changed approval artifact, or base-branch change invalidates that preview. Ask Boatstack to regenerate the PR; do not copy the old body forward.

## GitHub CLI is missing or signed out

Boatstack keeps the validated `pr.md` instead of discarding the work. Install or authenticate GitHub CLI and rerun the open/update confirmation, or copy the title and rendered body from the preview into GitHub manually. The manual path still does not authorize merge.
