---
name: boatstack
description: Turn a product request into a question-led, specification-first implementation with test, review, and ship gates, then learn from the evidence without silently changing project rules. Use when planning or building a feature, creating an implementation PR, reviewing work against product intent, diagnosing repeated coding-agent failures, or exporting the same engineering loop to Cursor, Claude Code, Codex, and GitHub.
---

# Boatstack

Build the smallest complete product slice that can be independently verified. Keep the workflow model-neutral: project facts and gate evidence are canonical; host-specific prompts are adapters.

## Start by selecting the operation

Map the request to one operation:

- `init`: inspect a repository and create or update `.product-loop/project.json`.
- `auto-plan`: turn product intent into a reviewable draft feature package.
- `plan-gate`: present the draft for explicit human acceptance, then freeze its approved contents and generate the executable package.
- `build`: implement approved tasks in bounded, reversible slices.
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

- **Delivery path:** intent -> questions -> spec -> plan -> code -> gates -> PR.
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

1. Write the bounded outcome definition before proposing architecture.
2. Separate facts, decisions, unknowns, and safely deferrable gaps.
3. Answer discoverable code questions by inspection.
4. Ask the developer only questions whose answers materially change behavior, contracts, risk, or acceptance. Ask 1-3 concise questions at a time, give 2-3 mutually exclusive choices, recommend one, and explain the impact.
5. Record answers and provenance in the question ledger.
6. Create the feature spec: problem, users, outcomes, non-goals, acceptance criteria, invariants, interfaces, failure behavior, observability, rollout, and rollback.
7. Run product, design, engineering, and developer-experience reviews only when applicable. If gstack is installed, its review skills can implement these lenses; do not require it.
8. If Spec Kit is installed, use its constitution/specify/clarify/plan/tasks/analyze/checklist flow as an artifact generator. The canonical artifact contract remains authoritative.
9. End with a **draft**, never an implied approval. Do not generate executable task state or start implementation from `auto-plan` alone.

Do not treat an ADR as general project context. ADRs record accepted durable decisions. Use a question ledger for unknowns and a gap ledger for known divergence.

## Run `plan-gate`

1. Present the draft spec, plan, open decisions, accepted assumptions, gaps, risks, and proposed verification in a reviewable form.
2. Ask the developer to approve it or request changes. Silence and continued conversation are not approval.
3. On changes, return to `auto-plan`, preserve the feedback in the question/decision ledger, and issue a new draft.
4. On explicit approval, deterministically compile the already-approved structured plan into the task graph, requirement-test traceability rows, evidence skeleton, and expected gate commands. Do not add semantics during compilation.
5. Calculate content hashes and write `plan.lock.json` with the approver, timestamp, source commit, spec hash, plan hash, and task-graph hash.
6. If the spec or plan changes later, invalidate the lock and return to this gate.

`build` must refuse to run when the plan lock is absent, stale, or does not match the approved artifacts.

The reference implementation performs the post-approval materialization and lock in this order:

```bash
python3 .product-loop/tools/compile_plan.py \
  --plan .product-loop/features/<feature>/plan.json \
  --out-dir .product-loop/features/<feature>/compiled

python3 .product-loop/tools/approve_plan.py \
  --spec .product-loop/features/<feature>/spec.md \
  --plan .product-loop/features/<feature>/plan.json \
  --tasks .product-loop/features/<feature>/compiled/tasks.json \
  --approved-by "<human identity>" \
  --output .product-loop/features/<feature>/plan.lock.json
```

The first command validates and compiles already-approved semantics; it must not invent new tasks or acceptance criteria.

## Build without erasing evidence

- Work from approved tasks and acceptance criteria.
- Preserve the last known-good state; repair locally instead of restarting a near-correct implementation.
- Re-scope context at task boundaries. Include relevant source, interfaces, invariants, and tests—not arbitrary history.
- Stop and ask when implementation exposes a new product decision or a high-impact irreversible choice.
- Log deviations from the plan. Update the spec when product intent changes; add an ADR only when a durable architectural decision changes.
- Do not repeat the same failed tactic more than twice without re-diagnosing the failure class.

Do not branch the workflow on model brand, price, or a guessed capability tier. Branch only on observable work state: unresolved ambiguity, risk, convergence, repeated tactics, tool results, test fidelity, and gate evidence. A repository may choose any implementation model; the contract and gates stay the same.

## Enforce the gates

### Test gate

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
python3 boatstack/scripts/export_repo.py --adapter-name boatstack --repo /path/to/repo --config /path/to/project.json --write
```

Run with `--check` in CI to detect drift. The exporter writes generated files only and refuses to overwrite user-owned files. Review the generated diff in a branch and ship it through a PR.
