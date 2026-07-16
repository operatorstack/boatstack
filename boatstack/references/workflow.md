# Canonical workflow

## State machine

```text
INTENT
  -> SOURCE_PLAN
  -> PROJECT
  -> QUESTIONS
  -> SPEC
  -> PLAN
  -> PLAN_GATE
  -> PLAN_LOCKED
  -> BUILD
  -> TEST_GATE
  -> REVIEW_GATE
  -> SHIP_GATE
  -> PR_OPEN
  -> RETRO
```

Each transition emits an artifact and evidence. A host adapter may change how a command is invoked, but it must not skip a transition or redefine a gate.

The `SOURCE_PLAN` file is required from entry through completion of `BUILD`. After build, its path and hash remain recorded for provenance, but `TEST_GATE`, `REVIEW_GATE`, and `SHIP_GATE` do not require the original file to be present.

## State contracts

### `INTENT -> SOURCE_PLAN`

Begin in the active coding host's Plan mode. Explore the ordinary product intent without editing implementation files, then save that host-generated plan as a file. Invoke `auto-plan` without a path in the normal case.

Before repository inspection, run:

```bash
.product-loop/bin/boatstack-helper check-source-plan --repo . --plan <host-context-path>
# If the host exposes no active path:
.product-loop/bin/boatstack-helper check-source-plan --repo .
```

The host/system conversation path is authoritative when present. Fallback discovery checks `.product-loop/intake/` and bounded repo-local host plan directories; it never scans the whole repository or selects a file solely because it is newest. If the file is missing, ambiguous, empty, or unreadable, `auto-plan` is `BLOCKED` and may request an explicit path. It must not manufacture the missing input. This source plan is an initial proposal rather than human approval.

### `SOURCE_PLAN -> PROJECT`

Define the request as:

- domain;
- affected actor;
- input and output;
- user-visible outcome;
- next operator;
- verification boundary.

Reject a scope definition that combines unrelated domains or cannot name an observable outcome.

### `PROJECT -> QUESTIONS`

Inspect the minimal code paths and durable project context. Classify every uncertainty:

- **discoverable fact:** answer through repository or runtime inspection;
- **product decision:** ask the developer or stakeholder;
- **technical decision:** propose options and record the accepted rationale;
- **deferrable gap:** record it with impact and trigger;
- **irrelevant:** exclude it from the slice.

Questions are required when different answers change an external contract, data model, safety boundary, user experience, acceptance criterion, or irreversible implementation choice.

### `QUESTIONS -> SPEC`

The spec must contain:

- problem and target user;
- desired outcome and metrics;
- non-goals;
- user stories or scenarios;
- acceptance criteria;
- current and proposed interfaces;
- invariants and trust boundaries;
- failure, empty, loading, and recovery behavior;
- observability;
- migration, rollout, and rollback;
- linked questions, ADRs, and gaps.

Do not encode guessed answers as facts. Mark a reversible assumption and give it an expiry trigger.

### `SPEC -> PLAN`

Create tasks in dependency order. Each task names:

- files or components likely affected;
- contract or acceptance criteria served;
- validation procedure, its origin, its oracle, and its independence;
- rollback boundary;
- unknowns that would stop implementation.

Run only relevant review lenses:

- product/taste: value, scope, user journey, non-goals;
- design: states, accessibility, responsive behavior, content;
- engineering: boundaries, data flow, state, failure modes, security, migrations;
- developer experience: APIs, naming, discoverability, operability.

If gstack is installed, its review skills can execute these lenses. If Spec Kit is installed, it can generate and cross-check the spec, plan, tasks, and checklists. Their output is normalized into this artifact contract.

Validation must be derived before implementation. Each check records:

- `run`: an executable command or a specific human/external procedure;
- `criteria`: only the acceptance claims this procedure can actually support;
- `origin`: the acceptance criterion, repository invariant, human decision, risk, or external contract that requires it;
- `oracle`: the fixture, schema, threshold, rubric, external fact, or authorized judgment capable of falsifying the claim;
- `independence`: whether the oracle is pre-existing, contract-derived, external, human, or implementation-authored.

Subjective work is not exempt from validation. Convert ambiguity into an approved reference, rubric, scenario, threshold, and evidence owner. If materially different interpretations remain or no defensible oracle exists, keep the plan `BLOCKED` at `PLAN_GATE`.

### `PLAN -> PLAN_GATE`

Present the full draft and require an explicit human `approve` or a change request. Do not interpret silence, a new implementation question, or a tool permission as plan approval.

### `PLAN_GATE -> PLAN_LOCKED`

After approval, deterministically:

1. hash the approved spec and plan;
2. hash the saved source Plan-mode file;
3. compile the approved structured plan into the task graph, requirement-test traceability rows, evidence skeleton, and expected gate commands without adding semantics;
4. record approver, timestamp, source commit, and all artifact hashes in `plan.lock.json`;
5. verify every task maps to at least one acceptance criterion or declared enabling dependency.

Any later change to the source plan, approved spec, or structured plan before build completes invalidates the lock and returns the feature to `PLAN_GATE`.

### `PLAN_LOCKED -> BUILD`

Implement one coherent task slice at a time. After each slice:

1. run the cheapest relevant check;
2. compare the diff to the task contract;
3. preserve the known-good state;
4. record deviations or new unknowns;
5. continue, ask, or re-plan explicitly.

### `BUILD -> TEST_GATE`

Crossing this boundary ends the requirement to keep loading or checking the source Plan-mode file. Its recorded path and hash preserve provenance. Subsequent gates judge the approved intent against the actual diff and evidence.

Create requirement-to-evidence traceability. Use this evidence ladder:

1. syntax, schema, and load/collect checks;
2. unit and contract tests;
3. integration and end-to-end tests;
4. differential, property, or mutation checks where useful;
5. staging/runtime verification;
6. human acceptance for product behavior.

The riskier the slice, the less acceptable same-model, self-authored tests are as the only oracle.

### `TEST_GATE -> REVIEW_GATE`

Review only after required mechanical checks pass, unless reviewing a failure is the goal. The reviewer inspects the actual diff and reports findings by severity with file/line evidence, consequence, and correction.

### `REVIEW_GATE -> SHIP_GATE`

Require:

- all critical findings resolved;
- acceptance criteria traced to evidence;
- required commands passed;
- docs and durable decisions updated;
- gaps explicit;
- deployment and rollback understood;
- secrets and unintended artifacts excluded.

### `SHIP_GATE -> PR_OPEN`

Create a PR with the feature spec, decision links, test evidence, review findings, gaps, rollout, and rollback. Opening a PR does not authorize merge or deployment.

### `PR_OPEN -> RETRO`

Record unexpected friction and outcomes. A retro may propose a loop move, but it may not mutate durable instructions automatically.

## Gate semantics

- `PASS`: required evidence is present; no gate-blocking gap remains.
- `PASS_WITH_GAPS`: no critical gap remains; each accepted gap has impact, owner, and trigger.
- `BLOCKED`: required evidence failed or a critical unknown/gap remains.

## State routing

The workflow never branches on model provider, model name, price, or presumed capability. Route only from observed state:

- unresolved product choice -> ask the human;
- undiscovered code fact -> inspect the minimal relevant slice;
- high-risk boundary -> require independent evidence and the configured reviewer;
- repeated tactic without new evidence -> stop and re-diagnose;
- converging work at a budget boundary -> resume from checkpoint if policy permits;
- weak or circular oracle -> add an independent verification source;
- changed approved intent -> invalidate the plan lock and return to `PLAN_GATE`.

The same state contract applies whether the repository uses a local model, a cheap API model, or a frontier model.
