<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# Install Boatstack and ship a first feature

**For:** a product builder or engineer using Cursor, Codex, or Claude Code.
**Outcome:** install Boatstack in one infrastructure PR, then take one ordinary request through approval, build, evidence, review, and PR preparation.

Boatstack is repository-local. Install it once per Git clone and commit the shared workflow before starting product work. Linked Git worktrees reuse the clone's verified runtime automatically.

## 1. Install it separately

The easiest path is to paste the [agent installation prompt](../README.md#install-with-your-coding-agent) into your coding host. It asks the agent to create `chore/install-boatstack`, run the official installer, explain the generated files, run `doctor`, and prepare the installation PR without merging it.

For a manual install:

```bash
git switch -c chore/install-boatstack
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
```

On Windows PowerShell:

```powershell
git switch -c chore/install-boatstack
irm https://raw.githubusercontent.com/operatorstack/boatstack/main/install.ps1 | iex
```

Choose `core` unless you already want gstack, GitHub Spec Kit, or both. Confirm the real repository test command when asked. The installer previews paths, verifies the helper, installs portable host adapters, and runs:

```bash
.product-loop/bin/boatstack-helper doctor --repo .
```

Review and commit the paths printed by the installer. Merge this infrastructure PR before creating a feature branch. Later feature PRs then contain the product change and its evidence rather than one-time setup noise.

### Git worktrees

The installer keeps a versioned, verified runtime under Git's common directory. A linked worktree still starts without the ignored `.product-loop/bin/` directory, but its first guarded Cursor, Codex, or Claude call restores that local runtime automatically before evaluating the original command. This performs no download and changes no tracked files.

Different Boatstack versions use separate cached runtimes, so an older worktree is not silently run with a newer helper. A separate clone has a different Git common directory and still needs one installer run.

## 2. Start with the idea

Create a feature branch from the base containing Boatstack. Enter your host's Plan mode and describe the outcome in normal product language:

```text
Add account recovery without removing the existing passwordless sign-in flow.
```

Let the host inspect the relevant repository slice and save its plan. Boatstack uses a host-exposed path when available; otherwise save exactly one non-empty plan under `.product-loop/intake/`.

Run:

```text
/auto-plan
```

Boatstack can discover repository facts. It cannot choose product behavior for you. When different answers would materially change the feature, it asks in plain language and waits for your answer.

### What a ready plan looks like

```markdown
## Plan ready

The feature plan is complete, with scope, decisions, and known gaps recorded.

### Next step

Run `/plan-gate`.

<details>
<summary>Technical details</summary>
Plan paths, validation output, and fingerprint.
</details>
```

## 3. Review and approve

Run:

```text
/plan-gate
```

Read the intended outcome, exclusions, decisions, gaps, and planned checks. Request corrections when anything is wrong. When it matches what you want, reply:

```text
approve
```

Boatstack uses an explicit identity or your authenticated GitHub username for the approval record. Approval does not edit product code.

### While approval is waiting

```markdown
## Ready for your approval

This plan builds the agreed slice and keeps the listed non-goals outside it.

### Next step

Reply `approve`. If something is wrong, describe the change instead.

<details>
<summary>Technical details</summary>
Machine status, fingerprint, and artifact paths.
</details>
```

### After approval

```markdown
## Approved — ready to build

The reviewed plan is approved. No product code has changed yet.

### Next step

Enter your host's execution mode and run `/build`.

<details>
<summary>Technical details</summary>
Approver, timestamp, fingerprint, and approval-record path.
</details>
```

## 4. Build the approved change

Use Cursor, Codex, or Claude's normal transition out of Plan mode, then run `/build`. Boatstack verifies the approval, creates the machine task/evidence state, and locks it to the reviewed inputs before the first product edit. Internal plan phases remain tasks in one delivery. If the approved plan explicitly declares multiple PR-sized `delivery_slices`, Boatstack activates only the first slice; approval of the parent plan is not permission to publish any slice.

| Host | Planning surface | Build transition |
|---|---|---|
| Cursor | Plan mode | Accept Cursor's normal switch to Agent or Build mode |
| Codex | Plan mode in the app or supported client | Enter its normal execution-capable mode |
| Claude Code | Plan permission mode | Exit plan mode before `/build` |

If the host is still read-only, Boatstack reports that it is ready for build without creating compiled state. Switch modes and rerun `/build`.

## 5. Prove, review, and prepare the PR

Run the remaining gates:

```text
/test-gate
/review-gate
/ship-gate
```

- **Test gate:** connects the active slice's promised outcomes to current evidence and records a receipt bound to its committed diff.
- **Review gate:** checks that same diff against the approved intent, risks, invariants, and gaps, then records a second receipt.
- **Ship gate:** requires both current receipts and creates a reviewer-first title and body for that slice.

Boatstack shows the exact PR preview before changing GitHub. Reply `open PR` for a new PR. Reply `update PR` for an existing one. Any changed product diff or evidence makes the preview and gate receipts stale. A successful publication activates the next declared delivery slice. Direct pushes, direct PR mutations, and the ad-hoc PR route are denied while managed delivery is active. Merge and deploy remain separate human decisions.

After successful publication, Boatstack may show a collapsed notice when a newer stable release is available. The check is cached, never changes the feature branch, and never blocks shipping.

For an existing branch, ask naturally:

```text
Use Boatstack to improve this PR.
```

Boatstack summarizes what it can observe and labels unavailable approval or gate evidence `NOT_VERIFIED`; it does not invent a history the branch never had.

## Keeping Boatstack current

After the feature PR is merged, switch to a clean, current default branch and run:

```text
/boatstack-update
```

You may also ask, “Update Boatstack.” Boatstack checks the latest stable release, creates `chore/update-boatstack-v<version>`, preserves the current configuration and integrations, runs `doctor`, and shows the exact infrastructure diff. Product files are outside the allowed update scope.

When the preview is correct, reply:

```text
open update PR
```

Only that reply authorizes the update commit, push, and PR. Review and merge remain normal human decisions. If the command is run during feature work, Boatstack changes nothing and asks you to rerun it from the clean default branch after the feature PR merges.

Users on `v0.4.0` do not have this command yet. After `v0.5.0` is released, make the clean update branch yourself and run the installer pinned to that tag once:

```bash
git switch -c chore/update-boatstack-v0.5.0
BOATSTACK_MODE=update BOATSTACK_VERSION=v0.5.0 BOATSTACK_REPO="$PWD" BOATSTACK_YES=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/v0.5.0/install.sh)"
```

Windows PowerShell:

```powershell
git switch -c chore/update-boatstack-v0.5.0
$env:BOATSTACK_MODE="update"; $env:BOATSTACK_VERSION="v0.5.0"; $env:BOATSTACK_REPO=(Get-Location).Path; $env:BOATSTACK_YES="1"; irm https://raw.githubusercontent.com/operatorstack/boatstack/v0.5.0/install.ps1 | iex
```

Review the diff and open the update PR normally. After that bootstrap, future releases use `/boatstack-update`.

## When something blocks

- A product decision returns to you rather than being guessed.
- A changed plan returns to approval.
- Failed evidence returns to a bounded repair.
- A destructive recovery path remains denied.
- A pre-existing unrelated repository failure stays outside the feature unless separately authorized.

Continue with the [account-recovery walkthrough](account-recovery-walkthrough.md), inspect [what Boatstack generates](generated-files.md), or use [troubleshooting](troubleshooting.md).
