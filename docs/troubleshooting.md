<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# Troubleshooting Boatstack

**For:** someone blocked during installation or a feature.
**Outcome:** identify the smallest safe action that restores the intended workflow.

Start with:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
```

## A command is denied as destructive

Boatstack has no in-session bypass. Preserve the current external state and diagnose with read-only commands. Replace the destructive capability with transactional or fix-forward behavior, or move intentional recovery into a separately controlled operator runbook.

If a safe diagnostic was denied, keep the denial output and report the smallest reproducible command. Do not rename or wrap it to evade the check.

## The safety helper or hook is missing

The hook fails closed. In a linked worktree, the first guarded call should restore the ignored local helper from the verified repository-family cache. If Boatstack reports that the shared runtime is missing, run the official installer once from any checkout belonging to that Git clone, run `doctor`, and reload the coding host. Do not copy an executable without its verified runtime manifest.

If the worktree expects a different Boatstack version or source commit, update or rebase its committed Boatstack infrastructure. Boatstack will not run a newer cached helper against an older worktree contract.

## Cursor cannot find a slash command

Cursor reads project commands from `.cursor/commands/*.md`:

```bash
ls .cursor/commands
.product-loop/bin/boatstack-helper doctor --repo .
```

Rerun the installer and reload Cursor when files are missing. Commit the restored adapter in a dedicated infrastructure PR.

## `/auto-plan` cannot find a source plan

Finish the host's Plan-mode exploration and save it. If the host does not expose the path, put exactly one non-empty plan under `.product-loop/intake/`, then rerun `/auto-plan`. Supply an explicit path only when Boatstack reports multiple candidates.

## Plan mode cannot write an artifact

Planning is Markdown-only. The adapter may use Boatstack's bounded planning writer for known feature documents; it must not use arbitrary shell redirection or edit product code. If the host cannot support the bounded write, keep the plan and report the missing permission rather than leaving Plan mode early.

## `/build` says it is ready but cannot start

The plan is approved, but the host remains read-only. Enter the host's normal execution-capable mode and rerun `/build`. Boatstack deliberately creates no compiled state or lock before that transition.

## Approval is stale

The source plan, feature spec, or complete plan changed after approval. Return to `/auto-plan`, review the new plan at `/plan-gate`, and approve it again. Never edit approval metadata manually.

## A gate passes with gaps

The proven criteria passed while named non-critical gaps remain. Each gap needs an impact, owner, reason, affected criteria, and revisit trigger. A critical correctness, safety, or acceptance gap blocks instead.

## An unrelated base-branch check fails

Reproduce the failure against the target branch. Keep its repair in a separate PR. Use a bypass only when repository policy permits it and a human explicitly authorizes it; do not hide unrelated edits in the approved feature.

## Non-interactive installation cannot find the tests

Boatstack detects common package-manager tests, `scripts/check.sh`, Go, Rust, Make, and Python/pytest projects. For a custom command, install interactively or define the real test command in `.boatstack-project.json`. Boatstack will not invent one merely to complete setup.

## A fresh clone has no helper

This is expected: the repository-family cache lives inside that clone's Git common directory and `.product-loop/bin/` is ignored. Run the installer once from the repository root. Future linked worktrees of that clone hydrate automatically without another download.

## `/boatstack-update` is postponed

Updates never share a feature branch. Finish and merge the current feature PR, switch to the configured default branch, pull its current remote state, confirm the worktree is clean, and rerun `/boatstack-update`. Boatstack does not stash, switch away from, or modify active product work.

## The update check is unavailable

Release discovery uses a short, unauthenticated request to GitHub and a 24-hour ignored cache. A timeout, rate limit, or malformed response never blocks `/ship-gate`. Retry `/boatstack-update` later; do not bypass checksum verification or install from an unverified asset.

## The update reports generated drift

Boatstack found an installed generated file that no longer matches its previous lock. Review the named path and move durable project-owned content into `.boatstack-project.json` or repository documentation. Do not overwrite the drift merely to make the update pass.

## The PR preview is stale

A new commit, changed evidence, changed approval artifact, or base-branch update invalidated the preview. Ask Boatstack to regenerate it. Do not copy the old body forward.

## A phased plan cannot push or open its next PR

Plan approval is not publication authority. Run `delivery-status` through the active
Boatstack operation and confirm that the intended delivery slice is active. Commit
only that slice's declared affected paths, then run `/test-gate`, `/review-gate`, and
`/ship-gate`. Direct pushes, GitHub CLI PR mutations, GitHub tool mutations, and the
ad-hoc PR route are denied until the managed publisher receives the explicit open or
update confirmation. Successful publication activates the next declared slice.

## GitHub CLI is unavailable

Boatstack retains the validated `pr.md`. Authenticate or install GitHub CLI and repeat the open/update confirmation, or copy the exact preview into GitHub manually. Neither path authorizes merge.
