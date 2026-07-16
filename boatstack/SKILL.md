---
name: boatstack
description: Turn a product request into a question-led, specification-first implementation with test, review, and ship gates, then learn from the evidence without silently changing project rules. Use when planning or building a feature, creating an implementation PR, reviewing work against product intent, diagnosing repeated coding-agent failures, or exporting the same engineering loop to Cursor, Claude Code, Codex, and GitHub.
---

# Boatstack

Build the smallest complete product slice that can be independently verified. Implementation methods remain open: project facts, approval, and gate evidence are canonical; host-specific prompts are adapters. You are free in how you build. Only claims of completion require evidence.

## Start by selecting the operation

Map the request to one operation:

- `init`: inspect a repository and create or update `.product-loop/project.json`.
- `auto-plan`: refine a saved host Plan-mode file into a reviewable draft feature package; refuse when that file is absent.
- `plan-gate`: validate the Markdown draft, present it for explicit human acceptance, and record that acceptance in Markdown.
- `build`: activate the approved Markdown plan, then implement its tasks in bounded, reversible slices.
- `test-gate`: test requirements and relevant regressions using independent evidence.
- `review-gate`: review the diff against the spec, project invariants, risks, and known gaps.
- `ship-gate`: prepare a reviewable PR with evidence, rollback notes, and explicit gaps.
- `retro`: classify failures, propose a harness move, and gate it before promotion.
- `export`: generate thin Cursor, Claude Code, Codex, and GitHub adapters.

For the full state machine, read [workflow.md](references/workflow.md). For artifact meanings and templates, read [artifacts.md](references/artifacts.md).

## Bound the outcome

For ordinary feature work, define one bounded outcome:

1. one product domain;
2. one input/output contract;
3. one user-visible goal;
4. one next operator;
5. one verification boundary.

Because this workflow is also a reusable product, maintain delivery and improvement as separate paths:

- **Delivery path:** intent -> host Plan mode -> saved source plan -> questions -> spec -> approved plan -> code -> gates -> PR.
- **Improvement path:** traces -> failure classification -> proposed move -> paired evaluation -> promote/reject.

Never mix benchmark observations or speculative harness changes into the delivery path during an active feature. The improvement path may propose an experiment; only a passed promotion gate changes the canonical loop.

## Initialize from repository evidence

Inspect only the minimal relevant code and documentation. Look for:

- `AGENTS.md`, `CLAUDE.md`, `.cursor/rules`, constitutions, architecture docs, ADRs, prior feature specs, and open gap ledgers;
- entry points, schemas, public interfaces, decision-making functions, validators, tests, CI, deployment, and rollback paths;
- recent PRs touching the same domain;
- commands that actually build, lint, type-check, and test the affected slice.

Do not scan the entire repository by default. Record discovered paths and commands in `.product-loop/project.json`; preserve existing host configuration rather than replacing it.

## Run `auto-plan`

0. Require exactly one saved plan file created in the active host's Plan mode. First use the active plan path exposed in host/system conversation context, when available, and validate it with `.product-loop/bin/boatstack-helper check-source-plan --repo . --plan <host-path>`. Otherwise run `check-source-plan --repo .` to search only `.product-loop/intake/` and bounded repo-local host plan directories. If the result is missing or ambiguous, return `BLOCKED`; never choose by recency alone. An explicit `/auto-plan <path>` is only the ambiguity fallback. Do not write the missing source plan inside `auto-plan`.
1. Treat the supplied plan as an initial proposal, not approved truth. Record its path as `source_plan_path` in the structured plan.
2. Write the bounded outcome definition before proposing architecture.
3. Separate facts, decisions, unknowns, and safely deferrable gaps.
4. Answer discoverable code questions by inspection.
5. Ask the developer only questions whose answers materially change behavior, contracts, risk, or acceptance. Ask 1-3 concise questions at a time, give 2-3 mutually exclusive choices, recommend one, and explain the impact. If the host has no structured question tool, ask the same questions as plain text, return `WAITING_FOR_INPUT`, and do not select a default.
6. Record answers and provenance in the question ledger. An authoritative repository fact is `DISCOVERED`, an agent suggestion or inferred choice is `PROPOSED`, and only an explicit human response is `ANSWERED`. Every material proposal remains in `plan.md` as a `blocking_questions` ID until the human answers it. Never use labels such as “answered by plan default.”
7. Create the feature spec: problem, users, outcomes, non-goals, acceptance criteria, invariants, interfaces, failure behavior, observability, rollout, and rollback. Translate every accepted claim into an observable condition with a defensible oracle.
8. Run product, design, engineering, and developer-experience reviews only when applicable. If gstack is installed, its review skills can implement these lenses; do not require it.
9. If Spec Kit is installed, use its constitution/specify/clarify/plan/tasks/analyze/checklist flow as an artifact generator. The canonical artifact contract remains authoritative.
10. For every planned validation, record the exact `criteria` it can support plus `run`, `origin`, `oracle`, and `independence`. Commands, automated tests, external checks, and named human review procedures are all valid forms, but an ambiguous claim without a threshold/rubric and authorized decision remains `BLOCKED`.
11. Write only Markdown feature artifacts, including the canonical structured `plan.md`. Put its authoritative JSON inside the marked Boatstack block and run `boatstack-helper check-plan --plan <feature>/plan.md`; this command is read-only. If the host blocks its ordinary Markdown writer, pass the document to `boatstack-helper planning-write --repo . --feature <feature> --artifact <known-name>` on stdin. Never use arbitrary shell redirection to evade a host write boundary.
12. End with a **draft**, never an implied approval. Do not generate executable task state, JSON artifacts, locks, or implementation changes from `auto-plan`.

Do not treat an ADR as general project context. ADRs record accepted durable decisions. Use a question ledger for unknowns and a gap ledger for known divergence.

Treat repository-owned product context as canonical. Do not require it to be migrated or rewritten into a Boatstack memory. Specs, plans, summaries, and selected context are temporary task projections: keep them reviewable, link material claims back to their source paths, and never silently replace the source. Preserve the source; project only the relevant slice.

## Run `plan-gate`

1. Run the read-only Markdown preflight and retain its exact fingerprint:

```bash
.product-loop/bin/boatstack-helper check-plan \
  --plan .product-loop/features/<feature>/plan.md
```

2. Present the draft spec, plan, open decisions, accepted assumptions, gaps, risks, validation provenance, and `PLAN_FINGERPRINT` in a reviewable form.
3. Ask the developer to approve it or request changes. Silence, continued conversation, tool permission, and permission to build are not approval.
4. On changes, return to `auto-plan`, preserve the feedback in the question ledger, and issue a new draft.
5. On explicit approval, invoke `boatstack-helper record-approval` with the plan, named human, RFC3339 timestamp, and exact fingerprint returned before approval. It verifies the current plan and creates only `approval.md`.
6. End in Plan mode and tell the developer the feature is approved and ready for the host's normal Build transition. Do not compile tasks, create a lock, request Agent mode merely to write a file, or edit product code.

All files created or updated by `auto-plan` and `plan-gate` must be Markdown. gstack and Spec Kit may help produce those documents, but their implementation stages and non-Markdown executable state are deferred to `build`.

## Build without erasing evidence

- First confirm the host is in an execution-capable mode. If a requested transition is rejected or product-code writes remain unavailable, return `READY_FOR_BUILD` and stop without activating, compiling, or writing a lock.
- Before the first product-code edit, activate the exact approved Markdown plan:

```bash
.product-loop/bin/boatstack-helper activate-plan \
  --plan .product-loop/features/<feature>/plan.md \
  --approval .product-loop/features/<feature>/approval.md \
  --out-dir .product-loop/features/<feature>/compiled \
  --output .product-loop/features/<feature>/plan.lock.json
```

- Activation verifies the approval fingerprint, compiles `tasks.json`, `test-matrix.json`, and the evidence skeleton, writes the content-addressed lock last, and rechecks it. It adds no semantics. Missing approval, open blocking questions, or any change to the source plan, spec, or complete `plan.md` returns `BLOCKED`.
- Keep the source plan present and hash-current through completion of `build`.
- Choose any suitable model, tool, or implementation tactic inside the approved boundary. Boatstack controls transitions and claims, not local creativity.
- Work from approved tasks and acceptance criteria.
- Preserve the last known-good state; repair locally instead of restarting a near-correct implementation.
- Re-scope context at task boundaries. Include relevant source, interfaces, invariants, and tests—not arbitrary history.
- Stop and ask when implementation exposes a new product decision or a high-impact irreversible choice.
- Log deviations from the plan. Update the spec when product intent changes; add an ADR only when a durable architectural decision changes.
- Do not repeat the same failed tactic more than twice without re-diagnosing the failure class.

Do not branch the workflow on model brand, price, or a guessed capability tier. Branch only on observable work state: unresolved ambiguity, risk, convergence, repeated tactics, tool results, test fidelity, and gate evidence. A repository may choose any implementation model; the contract and gates stay the same.

## Enforce the gates

### Test gate

- After build completes, the source Plan-mode file is no longer a runtime prerequisite. Test, review, and ship use the approved lock, actual diff, and accumulated evidence; provenance remains recorded in the lock.
- Derive tests from acceptance criteria and affected contracts, not only from the implementation.
- Run existing relevant tests plus targeted new tests, linters, type checks, builds, and runtime checks.
- Treat model-authored tests and same-model self-review as evidence, not ground truth.
- Validate that tests load and exercise the intended interface. For high-risk code, add an independent oracle such as contract fixtures, mutation testing, differential checks, staging verification, or human acceptance.
- A failing check blocks the gate. A skipped check must include a reason and risk owner.

### Review gate

- Review the actual diff, not the intended plan alone.
- Check spec traceability, invariants, data/security/tenancy boundaries, failure behavior, backward compatibility, migrations, observability, tests, docs, and gaps.
- Use an independent reviewer for high-risk changes, repeated failures, or when the existing review evidence is circular.
- Convert actionable findings into tasks. Do not pass while critical findings are open.

### Ship gate

- Require a clean, intentional diff; passing required checks; a filled evidence ledger; explicit known gaps; and rollout/rollback notes.
- Create a PR, but keep merge and deploy as separate authorized actions.
- Never hide failed experiments, skipped checks, or `PASS_WITH_GAPS` behind a green summary.
- If a required check also fails on the base branch, record that comparison and recommend a separate repair PR. Do not edit unrelated code in the approved feature branch. A bypass is valid only when repository policy permits it and the human explicitly authorizes it; otherwise return to planning for any scope expansion.

Gate statuses are `PASS`, `PASS_WITH_GAPS`, and `BLOCKED`. Critical safety, correctness, or product-acceptance gaps always produce `BLOCKED`.

## Learn without overfitting

Read [failure-moves.md](references/failure-moves.md) before proposing a loop change.

1. Classify the observed failure below the surface symptom.
2. State a mechanism and the exact failure population the move targets.
3. Estimate cost, risk, and possible regressions.
4. Run a cheap smoke test, then a paired representative evaluation.
5. Keep a holdout or independent acceptance boundary.
6. Promote only a clear non-regressing result; otherwise record `REJECT` or `WASH`.

More steps, more context, stronger wording, more tests, or more retries are not improvements by themselves. Preserve negative results in the move ledger.

## Export host adapters

Read [portability.md](references/portability.md), then use:

```bash
.product-loop/bin/boatstack-helper export --repo /path/to/repo --config /path/to/project.json --write
```

Run with `--check` in CI to detect drift. The exporter writes generated files only and refuses to overwrite user-owned files. Review the generated diff in a branch and ship it through a PR.
