<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

<p align="center">
  <img src="assets/boatstack-mark.svg" width="96" height="96" alt="Boatstack stacked-node B mark">
</p>

<h1 align="center">Boatstack</h1>

<p align="center"><strong>Build freely. Prove it. Ship.</strong></p>

## Your product development flow—not your coding agent's

<!-- boatstack-claim:portable-product-flow -->Boatstack gives your repository one path from idea to PR. Use that path from Cursor, Codex, or Claude Code, with the model and specialist skills that fit the work. Plans, decisions, gaps, evidence, and review findings stay with the project.

Change the tool without rebuilding how you ship or redefining what “done” means. Boatstack carries the workflow and saved project state—not an agent's private chat history or a command already in progress.

<p align="center">
  <img src="assets/boatstack-portability.svg" width="900" alt="Coding agents, models, and specialist skills feed one repository-owned Boatstack flow that produces a reviewed pull request and useful context for the next feature">
</p>

| You change | Boatstack keeps |
|---|---|
| Cursor, Codex, or Claude Code | The same path from planning through PR preparation |
| Lower-cost, general, or frontier model | The same approval, testing, and review requirements |
| React guidance, gstack, Spec Kit, or another skill | Human approval and evidence remain authoritative |
| Session, worktree, or feature | Durable decisions, gaps, evidence, and code state in the repository |

## Install with your coding agent

Copy this into Cursor, Codex, or Claude Code while the repository is open:

```text
Install Boatstack in this repository from https://github.com/operatorstack/boatstack. Detect whether you are running in Cursor, Codex, or Claude Code; create or use a chore/install-boatstack branch; run the official installer for this operating system; default to core unless I request gstack or Spec Kit; keep all portable host adapters; run Boatstack doctor; show me the generated files and installation diff; and prepare the installation PR without merging it or starting product work.
```

Install Boatstack in its own infrastructure PR and merge it before starting a feature. Install once per Git clone; linked worktrees reuse the verified runtime and restore their ignored local helper automatically.

## Start with two moves

1. Create and save a plan in your coding tool's Plan mode.
2. Run `/auto-plan`.

That is all you need to learn up front. Boatstack shows one next action at a time through approval, building, tests, review, and PR preparation.

When you are ready, that guidance moves through `/plan-gate` → `/build` → `/test-gate` → `/review-gate` → `/ship-gate`.

> The diagram shows what Boatstack guides—not a checklist you need to memorize.

<p align="center">
  <img src="assets/boatstack-journey.svg" width="960" alt="One feature moves from idea through planning, approval, building, tests, review, and pull request; its retained plans, decisions, gaps, evidence, and code state combine with the next idea to create the next plan">
</p>

## What Boatstack guides

| Without Boatstack | With Boatstack |
|---|---|
| The agent guesses an important product decision | It asks, records your answer, and waits for approval |
| “Tests passed” is treated as proof of everything | Each promised outcome names the check that can disprove it |
| A failed external operation leads to a risky reset | High-confidence destructive recovery is stopped before execution |
| The PR loses the reasoning behind the work | Decisions, evidence, gaps, rollout, and rollback stay attached |
| Every feature starts from an empty conversation | Useful state from the previous feature informs the next plan |

You remain free to build however the work requires. Boatstack governs claims of approval, completion, review, and shipping—not the implementation technique.

## Use the tools that fit the work

- **Cursor, Codex, and Claude Code:** portable repository adapters expose the same workflow.
- **Models:** choose a lower-cost, general, or frontier model without silently changing the completion standard.
- **Skills:** add specialist guidance such as React best practices. Boatstack can also use optional gstack and GitHub Spec Kit outputs.

In technical terms, Boatstack is a repository-local software-delivery harness for AI coding agents. Skills contribute expertise; models perform work; Boatstack keeps the product-development path and evidence contract consistent.

> **Designed for model flexibility · Quality uplift evaluation in progress**

- <!-- boatstack-claim:model-neutral-contract -->**Verified:** the same completion requirements apply regardless of model, provider, or price.
- <!-- boatstack-claim:cross-model-failures -->**Observed:** benchmark runs exposed failures in protocol handling, context, verification, and recovery—not only model capability.
- <!-- boatstack-claim:lower-cost-outcomes -->**Being evaluated:** whether this improves product quality, cost, or delivery time with lower-cost models.

This does not mean every model performs equally. [See the evidence and paired evaluation design](docs/why-these-steps.md#model-choice-and-budget).

## Why these steps?

They come from coding failures observed in benchmark and product-repository work—not guesses. Every safeguard links what happened, what Boatstack now does, and whether that behavior has actually been tested.

| What happened | What Boatstack does | Current evidence |
|---|---|---|
| <!-- boatstack-claim:human-decisions -->The agent guessed a product decision | Records a human answer and approval before code | Approval and drift tests |
| <!-- boatstack-claim:validation-provenance -->A passing test was used to support a broader claim | Links each promised outcome to its validation | Coverage and plan-compiler tests |
| <!-- boatstack-claim:irreversible-operations -->A failed write led to an invented reset path | Denies high-confidence destructive recovery | Hook behavior verified; outcome benefit still being evaluated |
| <!-- boatstack-claim:reviewer-ready-pr -->A PR lost decisions and accepted gaps | Builds a review brief from scope, diff, and evidence | Projection and stale-preview tests |
| <!-- boatstack-claim:git-worktree-activation -->A worktree had the hook but not its ignored helper | Restores the verified local runtime before judging the command | Linked-worktree and tamper tests |

[Read what happened, what is tested, and what remains open](docs/why-these-steps.md). The [claim record](docs/public-claims.json) keeps every material statement tied to its sources.

## A small example

A request said, “Add a password reset button,” but the product used passwordless sign-in and had no reset route. Boatstack surfaced the conflict instead of building the button literally. The human chose dual authentication; later, review caught an unsafe recovery-session assumption and returned the change for repair before PR preparation.

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

**Start:** [Getting started](docs/getting-started.md) · [Generated files](docs/generated-files.md) · [Troubleshooting](docs/troubleshooting.md)

**Inspect:** [Why these steps](docs/why-these-steps.md) · [Validation and evidence](docs/validation-and-evidence.md) · [Safety](docs/safety.md)

**Go deeper:** [Evidence-engineered coding](docs/evidence-engineered-coding.md) · [Research and design](docs/research-and-design.md) · [Contributing](CONTRIBUTING.md)

## Project status

Boatstack is an open-source research prototype. Its workflow and enforcement behavior are tested, but the current record does not prove improved product-delivery success. A paired feature-building benchmark—same model, task, and budget with and without Boatstack—is the next evaluation.

Exact Intelligence Flow provenance and generated file hashes are recorded in [`UPSTREAM.json`](UPSTREAM.json).
