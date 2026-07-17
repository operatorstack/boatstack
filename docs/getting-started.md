<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# Install Boatstack and ship a first feature

Boatstack is repository-local. Install it once in an infrastructure PR, then create ordinary feature branches from the merged base.

## 1. Install on a clean branch

macOS or Linux:

```bash
git switch -c chore/install-boatstack
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
```

Windows PowerShell:

```powershell
git switch -c chore/install-boatstack
irm https://raw.githubusercontent.com/operatorstack/boatstack/main/install.ps1 | iex
```

Choose `core` unless you already want gstack review lenses, Spec Kit artifact generation, or both. Confirm the real test command when asked. The installer previews every path before writing it, verifies the helper checksum, and runs `doctor` after installation.

The install also merges Boatstack's fail-closed safety fragments into all portable host configurations. Review those fragments in the infrastructure PR. They deny high-confidence irreversible operations on every supported agent call; no approval phrase bypasses them.

Review the printed paths and repository facts:

```bash
git status --short
git diff -- .boatstack-project.json .product-loop .cursor .agents .claude .github/PULL_REQUEST_TEMPLATE
.product-loop/bin/boatstack-helper doctor --repo .
```

Use the exact `git add` command printed by the installer, then commit and push:

```bash
git commit -m "chore: install Boatstack"
git push -u origin chore/install-boatstack
```

Open and merge that infrastructure PR before starting a feature. The first PR is intentionally larger because it establishes the shared workflow; later feature PRs contain only the product change and its feature evidence. A fresh clone reruns the installer to restore the ignored platform helper without changing committed adapters.

## 2. Start one feature in Plan mode

Create a feature branch from the base containing the merged Boatstack installation. Open Cursor, Codex, or Claude in its planning surface and describe ordinary product intent:

```text
Add account recovery without removing the existing passwordless sign-in flow.
```

Let the host explore the smallest relevant repository slice and save its plan. Boatstack uses a host-exposed plan path when available; otherwise save one plan under `.product-loop/intake/`.

Run:

```text
/auto-plan
```

`/auto-plan` may answer discoverable repository facts, but it cannot answer product decisions for you. If it asks questions, answer them in plain text. Material agent suggestions stay `PROPOSED`; only your responses become `ANSWERED`. Re-run `/auto-plan` until the draft is ready.

## 3. Review and approve the exact plan

Run:

```text
/plan-gate
```

Boatstack presents the intended outcome, non-goals, decisions, known gaps, and validation plan. Request changes or explicitly reply `approve`. When available, Boatstack uses your authenticated GitHub username for the internal approval record; it asks for a name or handle only when no trustworthy identity is available. Approval does not build code.

### What Boatstack responses look like

Boatstack leads with the outcome and one action. Internal status codes, helper operations, fingerprints, and artifact paths remain available under **Technical details** instead of dominating the response.

After a successful `/auto-plan`:

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

When `/plan-gate` needs approval:

```markdown
## Ready for your approval

This plan builds the agreed slice and keeps the listed non-goals and gaps outside it.

### Next step

Reply `approve`. If something is wrong, describe the change instead.

<details>
<summary>Technical details</summary>
Machine status, fingerprint, and artifact paths.
</details>
```

After approval:

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

## 4. Enter the host's execution surface and build

Use the host's normal transition out of planning, then run:

```text
/build
```

Boatstack first activates the approved plan into compiled tasks, a requirement-to-test matrix, evidence skeleton, and content-addressed lock. Only then may the host edit product code. If the host rejects the mode transition, Boatstack returns `READY_FOR_BUILD` and creates no machine state.

Host notes:

| Host | Planning | Build transition | Boatstack adapter |
|---|---|---|---|
| Cursor | Plan mode; plain-text questions work when structured questions are unavailable | Accept Cursor's normal switch to Agent/Build mode | `.cursor/commands/*.md` |
| Codex | Plan mode in the app or supported client | Move to its normal execution-capable mode | `.agents/skills/boatstack/SKILL.md` |
| Claude Code | Start or switch to plan permission mode | Exit plan mode before `/build` | `.claude/skills/boatstack/SKILL.md` |

## 5. Prove, review, and ship

Run the gates in order:

```text
/test-gate
/review-gate
/ship-gate
```

- `test-gate` maps every acceptance criterion to current evidence.
- `review-gate` reviews the actual diff and may send the feature back for a local repair.
- `ship-gate` generates a reviewer-ready title and body from the approved intent, actual committed diff, evidence, decisions, and gaps.

For external writes, the gates also require immutable target identity, transactional or fix-forward failure behavior, an independent safety oracle, and an operational diff with no executable destructive recovery. Operator-only recovery remains outside the feature branch.

Boatstack shows the exact title and rendered body before changing GitHub. Reply `open PR` when the branch has no PR. Reply `update PR` when one already exists. It then rechecks the diff and evidence before publication. If anything changed after the preview, Boatstack regenerates it instead of publishing stale claims. Merge and deploy remain separate decisions.

You do not need another slash command for existing work. Ask naturally:

```text
Use Boatstack to improve this PR.
```

Without a managed feature package, Boatstack uses an evidence-limited brief: it summarizes the committed branch and observed checks, while marking unavailable approval or gate evidence `NOT_VERIFIED`. It never pretends the ad-hoc branch passed the full Boatstack workflow.

`PASS_WITH_GAPS` is honest success with explicitly owned, non-critical gaps. `BLOCKED` means the claim cannot progress. After fixing a review finding, rerun the affected gates. A check that already fails on the base branch belongs in a separate repair PR or an explicitly authorized repository-policy bypass—not an unrelated edit hidden in the feature branch.

See the [sanitized account-recovery walkthrough](account-recovery-walkthrough.md) for a complete realistic path, or the [diagram JSON example](../examples/diagram-json/README.md) for exact artifacts.
