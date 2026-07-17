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
  -> PLAN_APPROVED
  -> BUILD_ACTIVATION
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

## Irreversible-operation boundary

Every installed host routes supported shell and MCP events through Boatstack's immutable safety guard. High-confidence database, filesystem, Git-history, cloud-resource, and recovery destruction is always denied before execution. There is no prompt, approval reply, break-glass token, or in-session override. Source may be edited for review, but executable destructive capability blocks activation and gate progression until it is removed or moved to an operator-owned process.

After an external-write failure, preserve state and use only read-only diagnosis. Do not escalate privileges, broaden the target, or invent a reset. Use a transactional retry only when retry safety is demonstrated; otherwise stop and fix forward. Destructive recovery is operator-only outside Boatstack. See `irreversible-operation-boundary.md` for the classified operations and evaluation status.

Hooks are defense in depth rather than a complete sandbox. Protected systems still require least-privilege credentials, scoped service roles, backups, and service-side destructive approval. A missing, drifted, or failing helper denies execution and requires reinstall or repair.

## User-facing response contract

Helper commands and state labels are internal control machinery. Every normal response uses:

```markdown
## <Plain-language outcome>

<One or two sentence summary>

<Only the decision-relevant content for this operation>

### Next step

<Exactly one primary action>

<details>
<summary>Technical details</summary>

Machine status, helper output, fingerprints, paths, receipts, and locks.

</details>
```

Lead with a plain outcome, never a machine code such as `PASS`, `PLAN_APPROVED`, `BLOCKED`, `READY_FOR_BUILD`, `PASS_WITH_GAPS`, or `WAITING_FOR_INPUT`. Keep approval-relevant scope, non-goals, decisions, risks, and gaps visible. Move internal operations (`check-plan`, `record-approval`, `activate-plan`), hashes, paths, tables, receipts, locks, and raw output into **Technical details**. **Exactly one primary action:** end with the action that advances or unblocks the current state; a secondary option gets one short sentence. Never route past a blocked state.

| State | Outcome -> one next action |
|---|---|
| `auto-plan` ready / needs answers | **Plan ready** -> run `/plan-gate`; **I need your input** -> answer 1-3 material questions |
| `plan-gate` pending / approved | **Ready for your approval** -> reply `approve`; **Approved — ready to build** -> enter execution mode and run `/build` |
| `build` success / paused | **Build complete** -> run `/test-gate`; **Build needs a decision** -> answer the blocking question |
| `test-gate` pass / blocked | **Tests passed** -> run `/review-gate`; **Testing found a problem** -> perform or authorize the repair |
| `review-gate` pass / blocked | **Review passed** -> run `/ship-gate`; **Changes required** -> address the blocking finding |
| `ship-gate` preview / published | **PR ready** -> reply `open PR` or `update PR`; **PR opened** -> review the PR; never imply merge authorization |
| `boatstack-update` current / postponed / prepared / published / blocked | **Boatstack is current** -> no action required; **Update postponed** -> finish feature work and rerun from the clean default branch; **Boatstack update ready** -> reply `open update PR`; **Update PR opened** -> review the PR; **Update needs attention** -> address the one reported collision or health failure |
| `retro` | **Improvement proposed** -> review or authorize the experiment |

Normal approval is `approve`. Resolve `approved_by` from (1) an identity supplied with approval, (2) the authenticated GitHub login from `gh api user --jq .login` when available, or (3) one short identity follow-up. Never infer the approver from a filesystem username, commit history, or the coding agent. If identity is missing after approval, preserve the current fingerprint and approval intent, create no receipt, and ask only for identity; once resolved against the unchanged plan, do not require another `approve`. Keep identity and receipt data inside **Technical details**.

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

Tasks describe implementation, never publication authority. Internal phases remain
tasks inside one delivery slice. If the accepted product change intentionally needs
multiple PRs, `plan.md` declares ordered `delivery_slices`. Every task belongs to
exactly one slice; dependencies may point within the slice or to an earlier slice,
never forward. Optional base/head branch names are constraints, not permission to
create or push those branches. Approval accepts the delivery structure but does not
authorize any PR mutation.

An external-write task also names `affected_paths` and a compact `side_effects` record: operation kind, immutable target identity, reversibility, failure policy, and `destructive: false`. Ambiguous targets such as “local database” and rollback text such as “reset local DB” block approval. Ordinary tasks do not need side-effect ceremony.

Run only relevant review lenses:

- product/taste: value, scope, user journey, non-goals;
- design: states, accessibility, responsive behavior, content;
- engineering: boundaries, data flow, state, failure modes, security, migrations;
- developer experience: APIs, naming, discoverability, operability.

If gstack is installed, its review skills can execute these lenses. If Spec Kit is installed, it can generate and cross-check the spec, plan, tasks, and checklists. Their output is normalized into this artifact contract.

`plan.md` is the canonical structured plan. Its human-readable prose and one marked JSON block are a single approval surface. Until `BUILD_ACTIVATION`, feature artifacts are Markdown only; no compiled task graph, machine lock, or executable state exists.

Validation must be derived before implementation. Each check records:

- `run`: an executable command or a specific human/external procedure;
- `criteria`: only the acceptance claims this procedure can actually support;
- `origin`: the acceptance criterion, repository invariant, human decision, risk, or external contract that requires it;
- `oracle`: the fixture, schema, threshold, rubric, external fact, or authorized judgment capable of falsifying the claim;
- `independence`: whether the oracle is pre-existing, contract-derived, external, human, or implementation-authored.

Subjective work is not exempt from validation. Convert ambiguity into an approved reference, rubric, scenario, threshold, and evidence owner. If materially different interpretations remain or no defensible oracle exists, keep the plan `BLOCKED` at `PLAN_GATE`.

### `PLAN -> PLAN_GATE`

Run `boatstack-helper check-plan --plan <feature>/plan.md`, present the full draft and returned fingerprint, then require an explicit human `approve` or a change request. The check is read-only. Do not interpret silence, a new implementation question, a tool permission, or permission to build as plan approval.

### `PLAN_GATE -> PLAN_APPROVED`

After explicit approval, invoke the deterministic `record-approval` operation with the named human, RFC3339 timestamp, and exact approval fingerprint. It rechecks the plan and creates only `approval.md`. This receipt is the only new gate artifact. Remain in the host's Plan mode; do not compile machine artifacts or edit product code.

If the host lacks a structured question tool, ask 1-3 plain-text questions and return `WAITING_FOR_INPUT`. Never convert an unavailable question UI into permission to choose a default. Authoritative repository facts are `DISCOVERED`; agent suggestions and repository-derived product choices are `PROPOSED`; only explicit human responses are `ANSWERED`. Every material proposal remains in `blocking_questions` until answered.

### `PLAN_APPROVED -> BUILD_ACTIVATION -> PLAN_LOCKED`

At the host's normal Build transition, first confirm the host is in an execution-capable mode. If the transition is rejected or product-code writes remain unavailable, return `READY_FOR_BUILD` without compiling or writing a lock. Once execution is available and before the first product-code edit, `activate-plan` deterministically:

1. parse and validate the marked structured block in `plan.md`;
2. hash the complete source plan, spec, and `plan.md` and match them to `approval.md`;
3. compile the task graph, requirement-test traceability rows, and evidence skeleton without adding semantics;
4. record approver, timestamp, source commit, and all artifact hashes in `plan.lock.json`;
5. write the lock last and recheck it before permitting implementation.

Activation also initializes ignored, worktree-local Git delivery state bound to the lock.
One implicit `delivery` slice preserves the ordinary one-feature/one-PR flow. An
explicit multi-slice plan starts only its first slice in `BUILD`; later slices remain
`PENDING`.

Missing approval, unresolved `blocking_questions`, or any change to the source plan, approved spec, or complete `plan.md` blocks activation and returns the feature to `PLAN_GATE`. A failed or partial compilation never creates a valid lock.

### `PLAN_LOCKED -> BUILD`

Read the active delivery state and implement only that slice's `task_ids`. Within it,
implement one coherent task slice at a time. After each task slice:

1. run the cheapest relevant check;
2. compare the diff to the task contract;
3. preserve the known-good state;
4. record deviations or new unknowns;
5. continue, ask, or re-plan explicitly.

Commits are allowed during build. Direct `git push`, `gh pr create/edit/ready/merge`,
and equivalent GitHub mutations are not implementation tactics: the host hook denies
them while managed delivery is active. Do not route a managed branch through the
ad-hoc PR path.

Scan operational changes and configured `high_risk_paths` before activation and after relevant edits. A dangerous capability may remain visible as source for review, but it cannot execute and blocks progression until removed or isolated behind the operator boundary.

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

External-write evidence must establish immutable target identity, transactional or fix-forward behavior, and an independent safety oracle. A dry run that only prints the intended command does not prove the live target or failure behavior.

Before passing the gate, commit the intentional active-slice product and evidence diff
and invoke the deterministic delivery-gate recorder for `test`. It captures the slice,
base/head branches, HEAD, product-diff hash, and evidence hash. A `PASS` string edited
into Markdown is evidence content, not a state transition.

### `TEST_GATE -> REVIEW_GATE`

Review only after required mechanical checks pass, unless reviewing a failure is the goal. The reviewer inspects the actual diff and reports findings by severity with file/line evidence, consequence, and correction.

On pass, invoke the same recorder for `review`. It accepts only the active slice and
only when the test receipt matches the current diff. Any product or evidence change
afterward makes the receipts stale and routes back through test and review.

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

Project the approved feature and actual committed diff into a reviewer-ready title and body:

- why the change exists;
- what changed, grouped by reviewer concern;
- the shortest useful review order;
- decisions that materially shaped the diff;
- acceptance and check evidence with source references;
- known gaps, risks, rollout, and rollback;
- collapsed approval, evidence, and coding-host provenance.

Store the exact preview at `.product-loop/features/<feature>/pr.md`. Its non-rendered frontmatter records the title, base/head branches, managed feature, and context fingerprint; the remaining Markdown is the exact GitHub body. The preview artifact itself is excluded from the product-diff fingerprint so committing it does not create a self-referential hash.

Before publication, show the exact title and rendered body. Use **PR ready** and exactly one action: `Reply open PR` when no PR exists, or `Reply update PR` when one exists. Only that explicit reply authorizes opening or updating the PR. After confirmation, commit only the reviewed `pr.md`, recheck the same preview fingerprint, committed product diff, plan approval, build lock, test evidence, and review evidence, then perform a normal push and the selected GitHub action. Any drift blocks publication and requires a new preview; never force-push.

For managed work, publication also requires current test and review receipts for the
active delivery slice. Successful publication marks only that slice `PUBLISHED` and
activates the next slice as `BUILD`. No parent-plan approval, prior phase receipt, or
context summary can skip these transitions.

Opening or updating a PR does not authorize merge or deployment.

After successful publication only, the publisher may use the ignored 24-hour release cache to report an available stable Boatstack version. The primary response and next action remain **PR opened -> Review the PR**. Put the maintenance notice in collapsed details, state that no files changed, and direct the user to run `/boatstack-update` from the clean default branch after the feature PR merges. Suppress repeated notices for seven days unless a different release appears. Release lookup failure never changes the ship result.

## Boatstack updates

`boatstack-update` is an infrastructure operation, not part of a feature plan. It first forces release discovery and proves the current installation is healthy. If the repository is not on its clean, current default branch, it changes nothing and returns **Update postponed**.

For an available version, create `chore/update-boatstack-v<version>`, run the installer pinned to that release in update mode, preserve the repository configuration, adapters, integrations, and unrelated host settings, then run `doctor`. Show the release notes and link, exact generated diff, checksums, changed paths, integration state, rollout, and rollback. Product paths or generated-state drift are blocking.

Use **Boatstack update ready** and exactly one action: `Reply open update PR`. Only that reply authorizes staging the reported infrastructure paths, committing, pushing normally, and opening the update PR. The PR body records old/new versions, release provenance, changed generated files, doctor result, integration state, rollout, and revert instructions. If publication is unavailable, retain the prepared branch and provide one manual action. Never merge automatically.

## Existing and ad-hoc PRs

There is no public `/pr-brief` operation. When the user asks in natural language for Boatstack to prepare, improve, summarize, or update an existing PR without a managed feature package:

1. project the committed branch diff, commits, observed checks, and minimal relevant repository context;
2. store the exact preview at `.product-loop/pr-briefs/<branch>/pr.md`;
3. use the same reviewer-first format, but mark unavailable approval and gate evidence `NOT_VERIFIED`;
4. never claim that Boatstack approved the work or that an unrun gate passed;
5. preview first, then require `open PR` or `update PR` and recheck the diff before publication.

Adaptive sections for security/privacy, migrations, UI evidence, or operations appear only when relevant. Model attribution belongs inside collapsed provenance. If GitHub CLI authentication is unavailable, keep the validated preview and provide one manual publication action instead of losing the work.

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
