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

`doctor` proves the generated host contract, not host activation. In Codex, trust the exact linked-worktree path, open `/hooks`, review and trust the current Boatstack hook hash, and start a new task. In Claude Code, reload and use `/hooks` to confirm the `PreToolUse` hook; Bash is required. In Cursor, reload the window and confirm both pre-execution hooks are enabled. Cursor hooks remain defense in depth because host-side output handling can change independently of Boatstack.

If the worktree expects a different Boatstack version or source commit, update or rebase its committed Boatstack infrastructure. Boatstack will not run a newer cached helper against an older worktree contract.

## Cursor reports `MainThreadShellExec not initialized`

This is a Cursor host initialization failure: Boatstack's hook process did not start. Keep the hook fail-closed, run **Developer: Reload Window** in Cursor, and retry the Boatstack operation. Do not reinstall Boatstack for this error alone. Reinstall only when Boatstack itself reports a missing, drifted, unsafe, or checksum-invalid helper or shared runtime.

## A host reports `HOST_PAYLOAD_MALFORMED`

Boatstack received a hook event without a decodable command or tool call. It fails closed, but no unsafe operation was detected. Retry once with an explicit non-empty command. If the same code repeats, stop agent shell and tool retries, preserve edits, and run this from a normal terminal outside the blocked agent path:

```bash
.product-loop/bin/boatstack-helper diagnose-hook --host cursor --repo .
```

Replace `cursor` with `claude` or `codex` for those hosts. A passing probe proves the installed wrapper, shared runtime, decoder, and canonical allow response; it cannot reveal the live payload emitted by the coding host. For Cursor, start a new task after a passing probe. Do not reinstall or hydrate Boatstack unless it separately reports a missing, drifted, unsafe, or checksum-invalid runtime.

## A published PR fails CI or receives review feedback

Describe the failure normally. Boatstack resolves the current branch and recorded PR, preserves the published parent, and prepares a corrective delivery for approval. Do not manually repeat a push or PR mutation denied by the safety hook. If several features match, choose from the named candidates; if GitHub is unavailable, the correction may be planned but its PR destination remains unverified until publication.

## Repair reports no matching delivery

Recovery compares an exact requested change with an activated or published baseline. If no feature matches the current branch or recorded PR, save a new host Plan-mode file. If a draft or approved feature already exists, run the one planning or build operation reported by the status check; do not create or clear delivery state manually.

## Boatstack reports invalid or orphaned delivery state

Preserve the named plan, lock, preview, receipts, and managed state. A missing `plan.lock.json`, stale lock hash, or orphan `pr.md` cannot be repaired by choosing the newest artifact or deleting state. Restore the missing tracked evidence from version control or the originating feature branch, then rerun `/boatstack-next`. If the evidence cannot be restored, stop and prepare a separately reviewed recovery rather than resetting progress in place.

## Cursor cannot find a slash command

Cursor reads project commands from `.cursor/commands/*.md`:

```bash
ls .cursor/commands
.product-loop/bin/boatstack-helper doctor --repo .
```

Rerun the installer and reload Cursor when files are missing. Commit the restored adapter in a dedicated infrastructure PR.

## Claude Code cannot find a slash command

Claude Code reads Boatstack's user-facing workflow skills from `.claude/skills/<operation>/SKILL.md`. The central `.claude/skills/boatstack/SKILL.md` router is intentionally hidden from slash suggestions and remains available for natural-language requests.

```bash
ls .claude/skills
.product-loop/bin/boatstack-helper doctor --repo .
```

If Boatstack created `.claude/skills/` while Claude Code was already running, reload Claude Code once. Rerun the installer when `doctor` reports a missing generated skill, and never replace a user-owned skill with the same name without reviewing the collision.

## `/auto-plan` cannot find a source plan

Finish the host's Plan-mode exploration and save it. If the host does not expose the path, put exactly one non-empty plan under `.product-loop/intake/`, then rerun `/auto-plan`. Supply an explicit path only when Boatstack reports multiple candidates.

## Plan mode cannot write an artifact

Planning is Markdown-only. The adapter may use Boatstack's bounded planning writer for known feature documents; it must not use arbitrary shell redirection or edit product code. If the host cannot support the bounded write, keep the plan and report the missing permission rather than leaving Plan mode early.

## `/build` says it is ready but cannot start

The plan is authorized, but the host remains read-only. Enter the host's normal execution-capable mode and rerun `/build`. Boatstack deliberately creates no compiled state or lock before that transition.

## Approval is stale

The source plan, feature spec, or complete plan changed after approval. Return to `/auto-plan`, review the new plan at `/plan-gate`, and approve it again. Never edit approval metadata manually.

## Build does not create `approval.md`

Check `workflow.human_plan_approval`. When it is `false`, this is expected: activation writes a fingerprinted schema-v2 plan lock with `authorization_mode: policy` and does not claim human approval.

## A gate passes with gaps

The proven criteria passed while named non-critical gaps remain. Each gap needs an impact, owner, reason, affected criteria, and revisit trigger. A critical correctness, safety, or acceptance gap blocks instead.

If `workflow.allow_pass_with_gaps` is `false`, resolve the gaps and record `PASS`; changing evidence text alone cannot bypass the controller.

## High-risk review requires reviewer provenance

The current diff matches `project.high_risk_paths` and independent review is enabled. Rerun review with a real `--reviewer-identity` and `--review-method human_peer` or `separate_agent`. Boatstack retains these fields in the review receipt.

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

## A tool call repeats or publication appears stuck

Run `.product-loop/bin/boatstack-helper operation-status --repo . --json`. `EXECUTING` means the exact call already has a live lease, so wait instead of launching it again. `RECONCILE_REQUIRED` means Boatstack did not observe completion; verify the reported Git, GitHub, file, browser, or MCP postcondition before retrying. A successful operation whose response was lost is recovered from that observation. Do not reset the task, repeat a denied push, or open another PR.

Operation receipts are shared by linked worktrees and retry budgets survive new chats and host restarts. If more than one unfinished operation matches, rerun status with the reported operation ID rather than choosing the newest. The receipts contain fingerprints and secret-free observations; no command payload or credential should be added to them.

## An update PR response was interrupted

Keep the update branch and rerun the Boatstack update publication step with the same displayed preview fingerprint. The deterministic publisher queries the exact head branch first and returns the existing PR when GitHub accepted the earlier request. If the update diff changed, regenerate and review the preview; never bypass it with a direct push or `gh pr create`.

## The PR preview is stale

A new commit, changed evidence, changed approval artifact, or base-branch update invalidated the preview. Ask Boatstack to regenerate it. Do not copy the old body forward.

## Visual evidence is unavailable or stale

Confirm the development launch instruction and retry the bounded health probe. Boatstack reuses a machine capability receipt only while its Boatstack version, lockfile, launch command, browser version, framework configuration, and health state still match. Under `suggest`, keep the missing screenshot visible as a PR gap or attach the displayed local PNG manually. Under `require`, recapture and publish to the same PR; do not open a duplicate PR. If the PR already opened before upload failed, preserve it and fix forward from `visual_pending`.

## A phased plan cannot push or open its next PR

Plan approval is not publication authority. Run `delivery-status` through the active
Boatstack operation and confirm that the intended delivery slice is active. Commit
only that slice's declared affected paths, then run `/test-gate`, `/review-gate`, and
`/ship-gate`. Direct pushes, GitHub CLI PR mutations, GitHub tool mutations, and the
ad-hoc PR route are denied until the managed publisher receives the explicit open or
update confirmation. Successful publication activates the next declared slice.

## GitHub CLI is unavailable

Boatstack retains the validated `pr.md`. Authenticate or install GitHub CLI and repeat the open/update confirmation, or copy the exact preview into GitHub manually. Neither path authorizes merge.

## A product edit is denied before build

After `auto-plan` saves a feature plan, Boatstack owns the workflow boundary even though implementation has not started. Review the reported stage and continue with `plan-gate` when the plan is draft, or `build` when it is approved or policy-ready. Product files remain unchanged until build activation creates a current lock. Do not retry through another editor, shell redirection, package installer, or MCP tool; all supported host events share the same decision. If the denial reports ambiguous, stale, or invalid state, follow its single recovery operation.

If `check-plan` reports a non-empty product baseline, inspect its exact diff and changed paths. Pass the displayed baseline fingerprint to `record-approval`. A changed fingerprint means the pre-existing edits drifted; preserve them, review the new diff, and approve again. Schema-v1 receipts remain valid only when this baseline is clean.
