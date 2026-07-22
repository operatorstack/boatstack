<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

<p align="center">
  <img src="assets/boatstack-mark.svg" width="96" height="96" alt="Boatstack stacked-bar mark">
</p>

<h1 align="center">Boatstack</h1>

<p align="center"><strong>Build freely. Prove it. Ship.</strong></p>

## Keep your software delivery process when you change coding agents

Boatstack is a repository-local delivery harness for Cursor, Codex, Claude Code, and Gemini CLI.

AI coding agents can write code quickly, but each tool brings its own planning flow, session state, and definition of “done.” Change agents and your delivery process often disappears with the chat.

<!-- boatstack-claim:portable-product-flow -->Boatstack keeps the delivery process in the repository. Plans, product decisions, tests, review findings, accepted gaps, and completion evidence stay connected from idea to pull request, regardless of which agent or model performs the work. Use Cursor, Codex, Claude Code, or Gemini CLI. Boatstack keeps the same approval, testing, review, and shipping boundaries across them.

**Your product development flow stays with the repository, not the coding agent.** Change agents, models, or specialist skills without rebuilding how your team plans, verifies, reviews, and ships software.

The coding agent executes the work. Boatstack supervises the delivery. Your repository owns the policy and evidence.

<p align="center">
  <img src="assets/boatstack-portability.svg" width="900" alt="Change the tools, keep the flow: coding agents, models, and specialist skills feed one repository-owned Boatstack flow that produces a reviewed pull request and useful context for the next feature">
</p>

| You change | You keep |
|---|---|
| Cursor, Codex, Claude Code, or Gemini CLI | The same path from approved plan to reviewed PR |
| Lower-cost, general, or frontier model | The same definition of done and proof requirements |
| React guidance, gstack, Spec Kit, or another skill | Human product decisions remain authoritative |
| Session, worktree, or feature | Decisions, open gaps, evidence, and verified delivery state |

## How it works

1. Save a plan in your coding agent.
2. Boatstack validates the plan and pauses for material product decisions.
3. The agent builds freely inside the approved scope.
4. Boatstack checks the promised outcomes against tests and evidence.
5. Review findings, risks, and accepted gaps become a focused PR brief.
6. The resulting context stays in the repository for the next feature.

## Each delivery makes the next one easier

Boatstack does not preserve an agent's private reasoning or replay old chats. It keeps the durable parts of delivery:
- approved product decisions
- unresolved gaps
- validation evidence
- review findings
- verified repository state

That means the next feature starts from recorded project knowledge instead of reconstructing intent from another agent session.

## Prevent systemic failure instead of patching symptoms

When coding agents or developers encounter a bug, they instinctively patch the local symptom. The underlying architectural flaw—like a leaky database edge that blindly accepts bad data—remains open, guaranteeing the exact same failure will happen again elsewhere.

When you ask for a fix during `/auto-plan`, Boatstack actively scans your codebase to determine if the bug is a symptom of a missing systemic boundary. Instead of silently patching the symptom, it pauses and asks if you want to establish a programmatic lock (like a database trigger or strict validator).

By turning one-off bug fixes into systemic constraints, your codebase gets safer with every agent run. Boatstack requires a negative test to prove the new lock is impenetrable. Upon publication, it extracts that verified boundary into the repository's global memory, ensuring all future agent runs are strictly bound by the new law of physics.

## Install with your coding agent

Copy this into Cursor, Codex, Claude Code, or Gemini CLI while the repository is open:

```text
Install Boatstack in this repository from https://github.com/operatorstack/boatstack. Detect whether you are running in Cursor, Codex, Claude Code, or Gemini CLI; create or use a chore/install-boatstack branch; run the official installer for this operating system; default to core unless I request gstack or Spec Kit; keep all portable host adapters; run Boatstack doctor; show me the generated files and installation diff; and prepare the installation PR without merging it or starting product work.
```

Install Boatstack in its own infrastructure PR and merge it before starting a feature. Install once per Git clone; linked worktrees reuse the verified runtime and restore their ignored local helper automatically.

## Start with two moves

1. Create and save a plan in your coding tool's Plan mode.
2. Start Boatstack with the entry point for your host.

| Host | Start command |
|---|---|
| Claude Code | `/auto-plan` |
| Cursor | `/auto-plan` |
| Gemini CLI | `/auto-plan` |
| Codex | `$boatstack auto-plan` |

That is all you need to learn up front. Boatstack shows one next action at a time through approval, building, tests, review, and PR preparation.

When you return after an interruption, run `/boatstack-next` in Claude Code, Cursor, or Gemini CLI, or `$boatstack next` in Codex. Boatstack reports the repository-verified stage and one next action without changing state. It distinguishes a feature that has not started from one that is complete.

`$boatstack run` in Codex or `/boatstack-run` in Claude Code, Cursor, and Gemini CLI starts from one saved plan and continues through publication, pausing for approvals and product decisions. It checks branch freshness before delivery; it never merges or deploys.

In Claude Code, Cursor, and Gemini CLI, that guidance moves through `/plan-gate` → `/build` → `/test-gate` → `/review-gate` → `/ship-gate`. In Codex, use the same operation names after `$boatstack`.

> The diagram shows what Boatstack guides—not a checklist you need to memorize.

<p align="center">
  <img src="assets/boatstack-journey.svg" width="960" alt="One feature moves from idea through planning, approval, building, tests, review, and pull request; its retained plans, decisions, gaps, evidence, and code state combine with the next idea to create the next plan">
</p>

## Change course without losing the delivery

After Build, describe changes normally. Boatstack records them, preserves valid work, and resumes at the earliest boundary. You do not need to remember a repair command: ordinary CI failures, review findings, and denied publication attempts route automatically for active deliveries and published PRs.

```text
“This is wrong” → record → repair → test → review
                         ↘ changed intent → approve delta
```

Receipts remain as history; published corrections become independently approved linked deliveries. An open PR is updated after fresh gates, while merged or closed work receives a new PR.

## What you get

- **Change coding agents without changing how you ship.**
- **Resume work without reconstructing the previous chat.**
- **Stop agents from guessing material product decisions.**
- **Require evidence for every outcome the change claims to deliver.**
- **Create reviewer-ready PRs from the actual scope, changes, risks, and validation.**

<details>
<summary>Technical Features</summary>

- **A guided path from idea to PR.** `/auto-plan` starts a one-action-at-a-time delivery flow.
- **Instant orientation after a break.** `boatstack next` reconstructs the verified stage without treating chat or a running process as workflow evidence, so you resume in seconds instead of re-reading history.
- **Human decisions stay human.** Material product questions remain open until a person answers them, and implementation waits for explicit approval.
- **Evidence tied to the promise.** Tests and checks map to the outcomes the change claims to deliver instead of treating one green command as proof of everything.
- **Context that survives the feature.** Plans, decisions, gaps, evidence, and code state remain useful beyond the chat.
- **Conversational repair after Build.** Describe what changed; Boatstack preserves valid work and reruns only affected boundaries.
- **Safer agent execution.** High-confidence destructive recovery is stopped before execution; phased work is gated and published one approved delivery slice at a time.
- **Reviewer-ready pull requests.** Actual changes, evidence, risks, rollout, and rollback become a focused PR brief, so reviewers spend time on judgment, not reconstruction.
- **Optional repository changelog.** Require readable `CHANGELOG.md` entries grounded in actual changes.
- **Portable across your AI stack.** Hosts, models, and skills share one repository-owned delivery contract.
- **Repository-friendly maintenance.** Worktrees restore runtime; updates stay in separate infrastructure PRs.

</details>

## Configure repository policy

`.boatstack-project.json` controls the project commands and context Boatstack uses, which coding hosts it supports, and opt-in policies for changelogs, boundary analysis, high-risk review, and feature workspaces. [Choose the outcomes you want and see every configuration field](docs/configuration.md).

## How Boatstack fits into your AI stack

| Part | Its job |
|---|---|
| **Coding agent** — Cursor, Codex, Claude Code, or Gemini CLI | Executes the work in your repository |
| **Model** — lower-cost, general, or frontier | Reasons, writes, and evaluates within the agent |
| **Skill** — React guidance, gstack, Spec Kit, or another specialty | Adds expertise for a particular kind of work |
| **Boatstack** | Carries the delivery path, saved context, and proof of completion across them |

Boatstack is a repository-local delivery harness.

> **Designed for model flexibility · Quality uplift evaluation in progress**

- <!-- boatstack-claim:model-neutral-contract -->**Verified:** the same completion requirements apply regardless of model, provider, or price.
- <!-- boatstack-claim:cross-model-failures -->**Observed:** benchmark runs exposed failures in protocol handling, context, verification, and recovery—not only model capability.
- <!-- boatstack-claim:lower-cost-outcomes -->**Being evaluated:** whether this improves product quality, cost, or delivery time with lower-cost models.

This does not mean every model performs equally. [See the evidence and paired evaluation design](docs/why-these-steps.md#model-choice-and-budget).

## Built from failures observed in real coding work

They derive from coding failures observed in benchmark and product work—not guesses. When a failure reveals a reusable delivery problem rather than a project-specific mistake, Boatstack turns it into a boundary future runs can enforce. Each link explains what happened, what Boatstack does, and whether that behavior has actually been tested.

| What happened | What Boatstack does | Current evidence |
|---|---|---|
| <!-- boatstack-claim:human-decisions -->The agent guessed a product decision | Records a human answer and approval before code | Approval and drift tests |
| <!-- boatstack-claim:validation-provenance -->A passing test was used to support a broader claim | Links each promised outcome to its validation | Coverage and plan-compiler tests |
| <!-- boatstack-claim:irreversible-operations -->A failed write led to an invented reset path | Denies high-confidence destructive recovery | Hook behavior verified; outcome benefit still being evaluated |
| <!-- boatstack-claim:reviewer-ready-pr -->A PR lost decisions and accepted gaps | Builds a review brief from scope, diff, and evidence | Projection and stale-preview tests |
| <!-- boatstack-claim:phase-scoped-delivery -->A phased plan opened PRs during build | Gates and publishes one delivery slice at a time | Slice-state and bypass tests |
| <!-- boatstack-claim:git-worktree-activation -->A worktree had the hook but not its ignored helper | Restores the verified local runtime before judging the command | Linked-worktree and tamper tests |

[Read what happened, what is tested, and what remains open](docs/why-these-steps.md). The [claim record](docs/public-claims.json) keeps every material statement tied to its sources.

## A small example

A request asked to “Add a password reset button,” but the product used passwordless sign-in. Boatstack flagged the conflict. The developer chose dual authentication; later, review caught an unsafe recovery assumption and prompted a repair.

[Follow the sanitized walkthrough](docs/account-recovery-walkthrough.md) or [ship your first feature](docs/getting-started.md).

## Updates stay out of product work

<!-- boatstack-claim:visible-updates -->After a PR is published, Boatstack may report a new stable release without changing the feature branch. `/boatstack-update` prepares a separate infrastructure branch, shows the diff, and waits for `open update PR`. It never merges the update.

<details>
<summary><strong>Install manually</strong></summary>

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

The installer previews generated paths, verifies the platform helper, offers optional integrations, runs a smoke check, and prints the files to commit. Boatstack core requires no Python, Node, Go, or package manager.

</details>

## Find what you need

**Start:** [Getting started](docs/getting-started.md) · [Files](docs/generated-files.md) · [Troubleshooting](docs/troubleshooting.md)

**Inspect:** [Why these steps](docs/why-these-steps.md) · [Validation and evidence](docs/validation-and-evidence.md) · [Safety](docs/safety.md)

**Go deeper:** [Coding](docs/evidence-engineered-coding.md) · [Design](docs/research-and-design.md) · [Contributing](CONTRIBUTING.md)

## Project status

Boatstack is an open-source research prototype. Its workflow and enforcement behavior are tested, but the current record does not prove improved delivery success. A paired feature benchmark—same model, task, and budget—is the next evaluation.

Exact Intelligence Flow provenance and generated file hashes are recorded in [`UPSTREAM.json`](UPSTREAM.json).
