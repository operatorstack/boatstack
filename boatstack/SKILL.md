---
name: boatstack
description: Turn a product request into a question-led, specification-first implementation with test, review, and ship gates, then learn from the evidence without silently changing project rules. Use when planning or building a feature, creating an implementation PR, reviewing work against product intent, diagnosing repeated coding-agent failures, updating Boatstack itself, or exporting the same engineering loop to Cursor, Claude Code, Codex, and GitHub.
---

# Boatstack

Build the smallest complete product slice that can be independently verified. Implementation methods remain open: project facts, approval, and gate evidence are canonical; host-specific prompts are adapters. You are free in how you build. Only claims of completion require evidence.

## Start by selecting the operation

Map the request to one operation:

- `init`: inspect a repository and create or update `.product-loop/project.json`.
- `auto-plan`: refine a saved host Plan-mode file into a reviewable draft feature package; refuse when that file is absent.
- `plan-gate`: validate the Markdown draft, present it for explicit human acceptance, and record that acceptance in Markdown.
- `build`: activate the approved Markdown plan, then implement only the active delivery slice's tasks.
- `test-gate`: test requirements and relevant regressions using independent evidence.
- `review-gate`: review the diff against the spec, project invariants, risks, and known gaps.
- `ship-gate`: preview, then explicitly open or update, a reviewer-ready PR grounded in the approved diff and evidence.
- `boatstack-update`: check for a stable Boatstack release and prepare its infrastructure-only update branch and PR after explicit confirmation.
- `retro`: classify failures, propose a harness move, and gate it before promotion.
- `export`: generate thin Cursor, Claude Code, Codex, and GitHub adapters.

For the full state machine, read [workflow.md](references/workflow.md). For artifact meanings and templates, read [artifacts.md](references/artifacts.md).

## Enforce the irreversible-operation boundary

Read [irreversible-operation-boundary.md](references/irreversible-operation-boundary.md). Project hooks hard-deny high-confidence destructive shell and MCP operations on every supported agent call. Never request or invent an in-session bypass. After an external-write failure, preserve state, use read-only diagnosis, retain the immutable target boundary, and choose only proven transactional retry or fix-forward recovery. Source edits may be reviewed, but an executable destructive capability blocks activation and every later gate.

This enforcement is defense in depth, not a complete sandbox. Keep least-privilege service credentials and service-side destructive approval in place.

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

## Respond to the developer

Follow the **User-facing response contract** in `references/workflow.md` for every operation. Lead with the mapped plain-language outcome, show only decision-relevant content, end with one `### Next step`, and put machine status, helper output, fingerprints, artifact paths, receipts, and locks inside collapsed **Technical details**. Internal operations such as `check-plan`, `record-approval`, and `activate-plan` must not appear in the primary response.

Use the global, state-scoped reply shortcuts for finite input: `a` approves the pending plan, `o` opens the currently previewed feature/ad-hoc/update PR, `u` updates the currently previewed existing PR, and `r` accepts every recommendation displayed in the current finite-question response. Trim surrounding whitespace and match the complete reply case-insensitively. Bracketed forms such as `[o]`, embedded letters, and shortcuts from another state are ordinary text. Continue accepting `approve`, `open PR`, `update PR`, and `open update PR` for compatibility, but do not advertise them in user-facing responses.

Shortcuts do not bypass fingerprints, committed-diff checks, evidence, authentication, or manual commit/push prerequisites. Never interpret `r` as approval, publication, identity, secret input, permission escalation, policy bypass, destructive recovery authorization, or another safety exception. Free-text and operation-command prompts remain explicit. Use an explicit approval identity first; otherwise use the authenticated GitHub login when available. Ask once for a name or handle only when no trustworthy identity can be resolved. Never infer the approver from the filesystem username, commit history, or agent identity. If identity is missing after approval, preserve the current approval intent and ask only for identity; do not make the human approve the unchanged plan again.

## Run `auto-plan`

0. Require exactly one saved plan file created in the active host's Plan mode. First use the active plan path exposed in host/system conversation context, when available, and validate it with `.product-loop/bin/boatstack-helper check-source-plan --repo . --plan <host-path>`. Otherwise run `check-source-plan --repo .` to search only `.product-loop/intake/` and bounded repo-local host plan directories. If the result is missing or ambiguous, return `BLOCKED`; never choose by recency alone. An explicit `/auto-plan <path>` is only the ambiguity fallback. Do not write the missing source plan inside `auto-plan`.
1. Treat the supplied plan as an initial proposal, not approved truth. Record its path as `source_plan_path` in the structured plan.
2. Write the bounded outcome definition before proposing architecture.
3. Separate facts, decisions, unknowns, and safely deferrable gaps.
4. Answer discoverable code questions by inspection.
5. Ask the developer only questions whose answers materially change behavior, contracts, risk, or acceptance. Ask 1-3 concise questions at a time and give each 2-3 mutually exclusive choices with compact inline-code keys (`1a`, `1b`, `1c`, then `2a`, `2b`, and so on). Suffix exactly one choice per question with `(Recommended)`, explain the impact, and end with one reply hint naming the keys or `r` for all recommendations. Use this format with structured question tools and plain text alike, then return `WAITING_FOR_INPUT`.
6. Treat a standalone `r` as explicit human acceptance only when every displayed question has exactly one recommendation. Echo the selected question-to-answer mapping before recording each as `ANSWERED`; otherwise ask again without choosing. An authoritative repository fact is `DISCOVERED`, an agent suggestion or inferred choice is `PROPOSED`, and only an explicit human response is `ANSWERED`. Every material proposal remains in `plan.md` as a `blocking_questions` ID until the human answers it. Never use labels such as “answered by plan default.”
7. Create the feature spec: problem, users, outcomes, non-goals, acceptance criteria, invariants, interfaces, failure behavior, observability, rollout, and rollback. Translate every accepted claim into an observable condition with a defensible oracle.
8. Run product, design, engineering, and developer-experience reviews only when applicable. If gstack is installed, its review skills can implement these lenses; do not require it.
9. If Spec Kit is installed, use its constitution/specify/clarify/plan/tasks/analyze/checklist flow as an artifact generator. The canonical artifact contract remains authoritative.
10. For every planned validation, record the exact `criteria` it can support plus `run`, `origin`, `oracle`, and `independence`. Commands, automated tests, external checks, and named human review procedures are all valid forms, but an ambiguous claim without a threshold/rubric and authorized decision remains `BLOCKED`.
11. For every external write, record `affected_paths` plus side-effect kind, immutable target identity, reversibility, failure policy, and `destructive: false`. Reject ambiguous reset rollback or target names.
12. Write only Markdown feature artifacts, including the canonical structured `plan.md`. Put its authoritative JSON inside the marked Boatstack block and run `boatstack-helper check-plan --plan <feature>/plan.md`; this command is read-only. If the host blocks its ordinary Markdown writer, pass the document to `boatstack-helper planning-write --repo . --feature <feature> --artifact <known-name>` on stdin. Never use arbitrary shell redirection to evade a host write boundary.
13. Keep implementation tasks separate from publication authority. Internal phases remain tasks inside one delivery slice. When the accepted outcome explicitly requires multiple PRs, declare ordered `delivery_slices`; assign every task exactly once and give each slice its own optional base/head branch contract. Plan approval approves this structure but never authorizes a push or PR.
14. End with a **draft**, never an implied approval. Do not generate executable task state, JSON artifacts, locks, or implementation changes from `auto-plan`.

Do not treat an ADR as general project context. ADRs record accepted durable decisions. Use a question ledger for unknowns and a gap ledger for known divergence.

Treat repository-owned product context as canonical. Do not require it to be migrated or rewritten into a Boatstack memory. Specs, plans, summaries, and selected context are temporary task projections: keep them reviewable, link material claims back to their source paths, and never silently replace the source. Preserve the source; project only the relevant slice.

## Run `plan-gate`

1. Run the read-only Markdown preflight and retain its exact fingerprint:

```bash
.product-loop/bin/boatstack-helper check-plan \
  --plan .product-loop/features/<feature>/plan.md
```

2. Present the draft spec, plan, open decisions, accepted assumptions, gaps, risks, validation provenance, and `PLAN_FINGERPRINT` in a reviewable form.
3. Ask the developer to approve it or request changes. End the pending response with exactly this Markdown: Reply `a` to approve. Silence, continued conversation, tool permission, permission to build, `[a]`, and an `a` embedded in other text are not approval.
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
- Activation also creates ignored delivery state bound to the plan lock. Read it with `delivery-status`; implement only the active slice's `task_ids`. A multi-slice plan advances only after the current slice publishes through `ship-gate`.
- Keep the source plan present and hash-current through completion of `build`.
- Choose any suitable model, tool, or implementation tactic inside the approved boundary. Boatstack controls transitions and claims, not local creativity.
- Work from approved tasks and acceptance criteria.
- Never push, open, update, ready, or merge a PR during `build`. The host hook denies direct publication while managed delivery is active; publication is reachable only through the confirmed `ship-gate` publisher.
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
- Commit the intentional active-slice product and evidence diff, then record the test result with `record-delivery-gate --feature <feature> --slice <slice> --gate test`. The receipt is bound to the base/head branches, commit, product diff, and evidence hash. Editing an evidence status is not a gate transition.

### Review gate

- Review the actual diff, not the intended plan alone.
- Check spec traceability, invariants, data/security/tenancy boundaries, failure behavior, backward compatibility, migrations, observability, tests, docs, and gaps.
- Use an independent reviewer for high-risk changes, repeated failures, or when the existing review evidence is circular.
- Convert actionable findings into tasks. Do not pass while critical findings are open.
- On pass, record `record-delivery-gate --feature <feature> --slice <slice> --gate review`. Review is rejected unless the same diff already has a test receipt; any later product change makes both receipts stale.

### Ship gate

- Require a clean, intentional diff; passing required checks; a filled evidence ledger; explicit known gaps; and rollout/rollback notes.
- Project only review-relevant context into `.product-loop/features/<feature>/pr.md`: why, changed behavior, review order, decisions, acceptance evidence, gaps, risks, rollout, rollback, and collapsed provenance.
- Treat the actual committed diff as what changed, approved artifacts as why it changed, and evidence as the only support for completion claims.
- In the visible Evidence table, link each managed claim to the current repository-relative evidence ledger using a readable link label; do not expose hashes or absolute paths.
- Always include why, what changed, review order, evidence, gaps/risks, rollout/rollback, and collapsed provenance. Add UI evidence, security/privacy, migration, or operations sections only when relevant.
- Internally generate the normalized context and preview skeleton with `pr-context --repo . --feature <feature>`, write `pr.md`, and validate it with `check-pr --repo . --preview <pr.md>`. Keep these helper names and their fingerprints out of the primary response.
- Inspect the projected changed files, diff stat, high-risk matches, and actual diff before composing the brief. Commit messages are navigation aids, not proof of what changed.
- Show the exact title and rendered body before any GitHub mutation. If no PR exists, render the one next action as: Reply `o` to open PR. If one exists, render: Reply `u` to update PR.
- After that exact confirmation, commit only the reviewed `pr.md`, rerun the preview check, require the same preview fingerprint, then invoke the internal publisher with the selected open/update action. It rechecks the current committed diff, approval, lock, and evidence and performs only a normal push. Any intervening change invalidates the preview and requires regeneration; never force-push.
- The publisher additionally requires current test and review receipts for the active slice. Successful publication marks only that slice published and activates the next slice. Plan approval, a prose phase label, or a previous slice's receipts cannot authorize a later slice.
- Keep model attribution inside collapsed provenance. Create or update the PR, but keep merge and deploy as separate authorized actions.
- Only after successful PR publication, perform the bounded cached release check. If a newer stable Boatstack release should be announced, keep `Review the PR` as the one next action and put the no-mutation update notice in collapsed details. Release lookup failure never changes the ship result.
- Never hide failed experiments, skipped checks, or `PASS_WITH_GAPS` behind a green summary.
- If a required check also fails on the base branch, record that comparison and recommend a separate repair PR. Do not edit unrelated code in the approved feature branch. A bypass is valid only when repository policy permits it and the human explicitly authorizes it; otherwise return to planning for any scope expansion.

Gate statuses are `PASS`, `PASS_WITH_GAPS`, and `BLOCKED`. Critical safety, correctness, or product-acceptance gaps always produce `BLOCKED`.

## Update Boatstack separately

Treat `boatstack-update` as infrastructure maintenance, never as feature work:

1. Run the current local helper's `doctor`, then force the cached stable-release check. If Boatstack is current, return **Boatstack is current** with no action required.
2. Fetch the configured default branch without editing product files. Require that branch to be current and clean; otherwise return **Update postponed** and change nothing.
3. Create only `chore/update-boatstack-v<version>`. Run the installer fetched from the exact release tag in update mode with the exact version, repository path, and non-interactive preview acceptance.
4. Preserve `.boatstack-project.json`, all portable adapters, optional integration selections, and unrelated host settings. Block on generated drift, collisions, missing provenance, a failed checksum, a failed `doctor`, or any product-file change.
5. Show the version transition, release notes, integration state, changed infrastructure paths, exact diff, checksums, rollout, and rollback. Respond **Boatstack update ready** and render the one next action as: Reply `o` to open update PR.
6. Only the state-scoped `o` or compatible full reply authorizes staging the reviewed infrastructure paths, committing, normally pushing, and opening the update PR. Never merge it. If GitHub publication is unavailable, retain the prepared branch and provide one manual action.

Natural requests such as “Update Boatstack” use this operation. `doctor` may display a cached notice but must remain offline. Do not perform release discovery during planning, approval, build, test, review, or PR preview.

## Improve an existing PR without a public command

When the user naturally asks Boatstack to prepare, improve, summarize, or update a PR and no managed feature package is available:

1. Do not invent a `/pr-brief` command or require the user to learn another operation.
2. Project the current committed branch diff, commits, observed checks, and relevant repository context into `.product-loop/pr-briefs/<branch>/pr.md`.
3. Use the same reviewer-first title/body contract as `ship-gate`, but label missing approval or gate evidence `NOT_VERIFIED`. Never imply Boatstack approved the plan or passed a gate that did not run.
4. Add conditional security/privacy, migration, UI evidence, or operations sections only when the diff makes them relevant.
5. Preview the exact title and rendered body. Render only Reply `o` to open PR. or Reply `u` to update PR., as appropriate.
6. Internally run `pr-context --repo .` without a feature, validate with `check-pr`, and keep those mechanics out of the primary response.
7. After confirmation, commit only `pr.md`, recheck the exact preview fingerprint and committed diff, then publish with the selected open/update action. If anything changed, regenerate instead of publishing stale text.

This is a two-slice ZCA projection: the reviewer brief minimizes review effort, while collapsed provenance preserves the evidence boundary. The projection must not become a dump of every generated artifact.

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
