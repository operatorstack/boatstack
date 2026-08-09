---
name: boatstack
description: Turn a product request into a question-led, specification-first implementation with test, review, and ship gates, then learn from the evidence without silently changing project rules. Use when planning or building a feature, creating an implementation PR, reviewing work against product intent, diagnosing repeated coding-agent failures, updating Boatstack itself, or exporting the same engineering loop to Cursor, Claude Code, Codex, and GitHub.
---

# Boatstack

Build the smallest complete product slice that can be independently verified. Implementation methods remain open: project facts, approval, and gate evidence are canonical; host-specific prompts are adapters. You are free in how you build. Only claims of completion require evidence.

## Start by selecting the operation

Map the request to one operation:

- `init`: inspect a repository and create or update `.product-loop/project.json`.
- `next`: report the verified current stage and exactly one next action without changing workflow or repository state.
- `run`: drive the verified feature through every delivery slice and PR publication, pausing at approval, product-decision, and publication boundaries.
- `root-cause`: read-only failure-mode-elimination diagnosis of a bug — classify the failure class below its symptom, produce a cited root-cause chain, and hand a class-eliminating source plan to `auto-plan`; never edits code or advances a gate.
- `auto-plan`: refine a saved host Plan-mode file into a reviewable draft feature package; refuse when that file is absent.
- `plan-gate`: validate the Markdown draft, present it for explicit human acceptance, and record that acceptance in Markdown.
- `build`: activate the approved Markdown plan, then implement only the active delivery slice's tasks.
- `repair`: classify a free-form post-build change, record it durably, and resume from the earliest affected stage without silently changing approved intent.
- `test-gate`: test requirements and relevant regressions using independent evidence.
- `review-gate`: review the diff against the spec, project invariants, risks, and known gaps.
- `ship-gate`: preview, then explicitly open or update, a reviewer-ready PR grounded in the approved diff and evidence.
- `boatstack-update`: check for a stable Boatstack release and prepare its infrastructure-only update branch and PR after explicit confirmation.
- `retro`: classify failures, propose a harness move, and gate it before promotion.
- `export`: generate thin Cursor, Claude Code, Codex, and GitHub adapters.

For the full state machine, read [workflow.md](references/workflow.md). For artifact meanings and templates, read [artifacts.md](references/artifacts.md).

## Report what is next

Run the project-local helper's read-only `next-status --repo . --json` inspection. Repository artifacts, managed delivery state, gate receipts, and the recorded PR identity are evidence; conversation, terminal, worktree, and process observations are context only. Never run the returned operation automatically. `NOT_STARTED` points to `auto-plan` (run it with the plan path via `--plan`); `PUBLISHED` means a PR exists but is not a verified merge; only `FEATURE_COMPLETE` requires no action. If state is ambiguous, stale, or invalid, name the blocker instead of choosing by recency or clearing artifacts. When an `AMBIGUOUS` block names only past deliveries the user no longer cares about, name the ignorable delivery slug(s) and offer to exclude them from ambiguity resolution; only after explicit user confirmation, add each slug with `.product-loop/boatstack ignore-delivery --repo . --feature <slug>` (a bounded, provenance-safe write to `workflow.ignored_deliveries` — never hand-edit config or delivery state). Any new, unlisted ambiguous delivery still pauses the workflow.

To see every feature at once, run the read-only `.product-loop/boatstack flow frontier --repo .`. It lists each delivery, its observed position, and who owes the next step. To wait for a published PR to move (checks finish, a review lands, a merge happens), run the read-only `.product-loop/boatstack flow watch --repo .`. The watch observes on an interval and exits when the frontier changes, when nothing can move, or at its timeout. It never acts on what it sees. When it exits, run `next-status` again and continue from the fresh state.

## Run to an explicit goal

For `$boatstack run --to plan|verified|pr`, `/boatstack-run`, or a natural-language run request, resolve the target from the request. When it is absent, ask once for `plan`, `verified`, or `pr`. First run the read-only `next-status --repo . --json` and `operation-status --repo . --json`. Wait for an executing operation and reconcile unknown completion before retrying. When the host supplies the plan path, enter `auto-plan` with `--plan <path>`; when no plan path is supplied, stop and ask the user for the plan to build. Return **Feature complete** only for a verified completed feature, and stop on unverified, ambiguous, stale, or invalid state. Schema-v3 `check-plan` runs the Git freshness preflight before it displays the plan fingerprint. Record the selected target with `record-autonomy --plan <plan.md> --target <target>` after all material questions are answered or every remaining question has a valid `RESOLVED_BY_POLICY` autonomy decision. Target `plan` stops after the valid reviewable plan. Targets `verified` and `pr` activate with `--autonomy <autonomy.md>` and stop if that receipt becomes stale. A failed fetch, missing remote/base, stale base, upstream drift, wrong worktree, constrained branch mismatch, incomplete journey decision, or ineligible policy decision blocks without creating authority or consuming repair budget. Never repair freshness by merging, rebasing, switching or creating a constrained delivery branch, discarding changes, force-pushing, or broadening permissions.

After preflight, repeatedly run `next-status --repo . --json`, execute only its verified next operation, verify the resulting repository state, and resolve again. Continue across all declared slices until the selected target is reached. A policy receipt may resolve only a non-material, within-spec, reversible choice with one recommendation, repository evidence, no protected impact, and a runnable oracle. Record it as `RESOLVED_BY_POLICY`, never `ANSWERED`. Any failed or unknown condition pauses for the human. Target `verified` stops after current test and review evidence passes. Target `pr` supplies scoped authority for one normal open or update action recorded in `autonomy.md`; after the exact preview is revalidated, call `publish-pr --autonomy <autonomy.md>` without asking for `o` or `u`. A changed plan, repository, branch, PR action, preview, evidence, or target invalidates that path. Same-intent test/review failures may be repaired for at most three complete cycles per active slice. Stop on amendments, ambiguity, safety failures, stale evidence, unsupported recovery, branch mismatch, or exhausted repairs. Never force-push, merge, deploy, or execute a foreign program.

When `delivery.terminal` is `merged`, follow the post-publish prescriptions exactly. After publication, run `flow next` (or `next-status`). When it prescribes `flow watch`, run the watch and re-resolve when it exits. When checks fail, it prescribes `record-change --source-stage ci`; derive the exact message, classification, evidence, and changed repair mechanism from the failing check logs, never from memory, then repair, re-gate, and republish with `publish-pr --action update`. When the PR is observed merge-eligible, it prescribes the exact `gh pr merge` command; run it only as rendered, under the host's own permissions — Boatstack never merges, and you never merge without the prescription. A required review approval, a changes-requested verdict, a closed PR, or an unverifiable PR position always ends your turn at the operator frontier.

## Enforce the irreversible-operation boundary

Read [irreversible-operation-boundary.md](references/irreversible-operation-boundary.md). Project hooks hard-deny high-confidence destructive shell and MCP operations on every supported agent call. Never request or invent an in-session bypass. After an external-write failure, preserve state, use read-only diagnosis, retain the immutable target boundary, and choose only proven transactional retry or fix-forward recovery. Source edits may be reviewed, but an executable destructive capability blocks activation and every later gate.

This enforcement is defense in depth, not a complete sandbox. Keep least-privilege service credentials and service-side destructive approval in place. Read `authority_status` from `run-preflight`: `HOOK_GUARDED` never proves ambient cloud authority absent, while `CREDENTIAL_ENFORCED` means a trusted external attestor supplied a current repository-only receipt. Never strengthen the former into the latter in prose.

## Keep repository administration outside delivery

Branch synchronization, status, switching, worktree maintenance, and requests to discard local changes are repository administration, not product intent. Never route them to `auto-plan` or `repair` unless the exact target branch belongs to an active managed delivery. For an explicit branch and remote ref, use the project-local `workspace-sync` helper. It fetches the exact source, checkpoints branch and dirty-worktree state, aligns the branch in its owning worktree, and returns verified recovery refs.

For requests such as “ensure main is same as origin/main remove any current changes,” inspect only the named refs and worktree, then invoke `.product-loop/boatstack workspace-sync --repo . --branch main --source origin/main`. If the guard denies a raw hard reset or clean, report the denial and this single recovery action immediately. Do not inspect feature plans, scan the repository, search for the helper, or retry destructive Git.

## Bound the outcome

For ordinary feature work, define one bounded outcome:

1. one product domain;
2. one input/output contract;
3. one user-visible goal;
4. one next operator;
5. one verification boundary.

Because this workflow is also a reusable product, maintain delivery and improvement as separate paths:

- **Delivery path:** intent -> host Plan mode -> plan passed to auto-plan via `--plan` -> questions -> spec -> approved plan -> code -> gates -> PR.
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

Follow the **User-facing response contract** in `references/workflow.md` for every operation. Begin every Boatstack response with the status banner (`.product-loop/boatstack next-status --repo . --render`), then lead with the mapped plain-language outcome, show only decision-relevant content, end with one `### Next step`, and put machine status, helper output, fingerprints, artifact paths, receipts, and locks inside collapsed **Technical details**. Internal operations such as `check-plan`, `record-approval`, and `activate-plan` must not appear in the primary response. Write every response in Simplified Technical English: short sentences, the active voice, the present tense, one idea per sentence, the condition first, and the simple common word.

Use the global, state-scoped reply shortcuts for finite input: `a` approves the pending plan, `o` opens the currently previewed feature/ad-hoc/update PR, `u` updates the currently previewed existing PR, and `r` accepts every recommendation displayed in the current finite-question response. Trim surrounding whitespace and match the complete reply case-insensitively. Bracketed forms such as `[o]`, embedded letters, and shortcuts from another state are ordinary text. Continue accepting `approve`, `open PR`, `update PR`, and `open update PR` for compatibility, but do not advertise them in user-facing responses.

Shortcuts do not bypass fingerprints, committed-diff checks, evidence, authentication, or manual commit/push prerequisites. Never interpret `r` as approval, publication, identity, secret input, permission escalation, policy bypass, destructive recovery authorization, or another safety exception. Free-text and operation-command prompts remain explicit. Use an explicit approval identity first; otherwise use the authenticated GitHub login when available. Ask once for a name or handle only when no trustworthy identity can be resolved. Never invent a placeholder name (e.g., Sam, Eve). Never infer the approver from the filesystem username, commit history, or agent identity. If identity is missing after approval, preserve the current approval intent and ask only for identity; do not make the human approve the unchanged plan again.

## Handle new intent during active deliveries

Before starting `/auto-plan` for a new feature, check `next-status --repo . --json`. If there is already an active managed delivery on the current branch (e.g., Status is `BUILD`):
1. Stop and clarify the developer's intent. Ask: *"You have an active delivery (`<active-feature-slug>`). Are these new ideas amendments to this feature, or a completely separate feature?"*
2. If the developer confirms it is an **amendment**, do not start a new feature. Route to the `repair` operation, classify it as a `requirement_amendment`, and update the existing plan.
3. If the developer confirms it is a **completely separate feature**, proactively suggest worktree isolation to avoid branch entanglement. Ask: *"Since `<active-feature-slug>` is still active here, do you want to cut a new worktree (`feat/<new-feature-slug>`) to keep this work isolated? (Recommended)"*
4. If they accept isolation, route to the `workspace-cut` operation. If they explicitly choose to stack both features on the same branch, only then proceed with `/auto-plan` for the new feature.

## Run `auto-plan`

0. Require the plan file produced in the active host's Plan mode, passed explicitly. Validate it with `.product-loop/boatstack check-source-plan --repo . --plan <host-path>`. Boatstack never scans directories for plans, so `--plan` is required and no unshipped saved plan becomes ambient context. If no plan path is supplied or the file is missing, empty, or unreadable, return `BLOCKED`; do not write or guess the missing source plan inside `auto-plan`. Because its hash is re-checked through `build`, point `--plan` at a durable in-repo path that stays present and unchanged; a path outside the repository is rejected.
1. Treat the supplied plan as an initial proposal, not approved truth. Record its path as `source_plan_path` in the structured plan.
2. Write the bounded outcome definition before proposing architecture.
3. Separate facts, decisions, unknowns, and safely deferrable gaps.
4. Before proposing implementation tasks, inspect the repository and verify any assumptions about API routes, data access, UI components, authentication, server actions, streams, jobs, and external services. Do not guess application architecture.
5. If `workflow.boundary_analysis` is `true` in `project.json`: Evaluate if the requested change is a symptom of a missing systemic boundary (e.g., deficient data normalization, leaky validation, missing authorization edge). If it is, perform a rapid codebase scan for other vulnerabilities sharing this failure mode. Present this as a material product decision, showing concrete codebase evidence of the blast radius. Offer tiered implementation paths: [1a] Symptom Patch (fix only the requested route), or [1b] Programmatic Enforcement (refactor the edge and install a programmatic boundary to mathematically prevent this). If the user chooses programmatic enforcement, explicitly structure the plan into two delivery slices: Slice 1 establishes the programmatic boundary (hook, trigger, or strict test), and Slice 2 implements the feature using that boundary.
- When `workflow.pr_visual_evidence` is `suggest` or `require`, also record one structural `pr_visual_evidence` decision reused through test, review, and ship. Changes below `project.visual_surfaces[].paths` are relevant. Use one to three scenarios naming user context, user goal, journey step, reviewer context, entry, state, viewport, surface, and expected visible outcomes, or `not_relevant` with a non-empty reason for review. Discover repository-owned visual tooling but do not add or require framework-specific tooling.
6. Express verified architectural information as typed `architecture_facts`. Each architecture fact must reference evidence IDs produced by Boatstack repository inspection. Do not create or invent evidence IDs. Reading one arbitrary repository file does not ground an unrelated architectural claim.
7. When an architectural question cannot be verified, record it in `architecture_unknowns`. Do not create an implementation task that depends on an unresolved architecture unknown. Create a bounded discovery task instead.
8. Every architecture-sensitive task must reference the facts it depends on through `requires_facts`.
9. Ask the developer only questions whose answers materially change behavior, contracts, risk, or acceptance. Ask 1-3 concise questions at a time and give each 2-3 mutually exclusive choices with compact inline-code keys (`1a`, `1b`, `1c`, then `2a`, `2b`, and so on). Suffix exactly one choice per question with `(Recommended)`, explain the impact, and end with one reply hint naming the keys or `r` for all recommendations. Use this format with structured question tools and plain text alike, then return `WAITING_FOR_INPUT`.
10. Treat a standalone `r` as explicit human acceptance only when every displayed question has exactly one recommendation. Echo the selected question-to-answer mapping before recording each as `ANSWERED`; otherwise ask again without choosing. An authoritative repository fact is `DISCOVERED`, an agent suggestion or inferred choice is `PROPOSED`, and only an explicit human response is `ANSWERED`. Every material proposal remains in `plan.md` as a `blocking_questions` ID until the human answers it. Never use labels such as “answered by plan default.”
11. Create the feature spec: problem, users, outcomes, non-goals, acceptance criteria, invariants, interfaces, failure behavior, observability, rollout, and rollback. Translate every accepted claim into an observable condition with a defensible oracle.
12. Run product, design, engineering, and developer-experience reviews only when applicable. If gstack is installed, its review skills can implement these lenses; do not require it.
13. If Spec Kit is installed, use its constitution/specify/clarify/plan/tasks/analyze/checklist flow as an artifact generator. The canonical artifact contract remains authoritative.
14. For every planned validation, record the exact `criteria` it can support plus `run`, `origin`, `oracle`, and `independence`. Commands, automated tests, external checks, and named human review procedures are all valid forms, but an ambiguous claim without a threshold/rubric and authorized decision remains `BLOCKED`.
14. For every external write, record `affected_paths` plus side-effect kind, immutable target identity, reversibility, failure policy, and `destructive: false`. Reject ambiguous reset rollback or target names.
15. Write only Markdown feature artifacts, including the canonical structured `plan.md`. Author every feature artifact through the owned channel: pass the complete document to `.product-loop/boatstack planning-write --repo . --feature <feature> --artifact <known-name>` using the literal planning transport in `.product-loop/workflow.md` — a single-quoted heredoc in a POSIX shell or the UTF-8-scoped single-quoted here-string in PowerShell. This is the primary writer for `.product-loop/features/`, not a fallback, and it remains available after the planning latch denies raw writes. Send the complete envelope in one tool call. Never run the helper without input, split the envelope across calls, use an expansion-capable delimiter, target another repository or helper, or paste Markdown at a shell prompt. Put the authoritative JSON inside the marked Boatstack block and run `.product-loop/boatstack check-plan --plan <feature>/plan.md`; this command is read-only. The host's ordinary Markdown writer may be used only where the host explicitly permits it. Never use arbitrary shell redirection to evade a host write boundary.
16. Keep implementation tasks separate from publication authority. Internal phases remain tasks inside one delivery slice. When the accepted outcome explicitly requires multiple PRs, declare ordered `delivery_slices`; assign every task exactly once and give each slice its own optional base/head branch contract. Plan approval approves this structure but never authorizes a push or PR.
17. End with a **draft**, never an implied approval. Do not generate executable task state, JSON artifacts, locks, or implementation changes from `auto-plan`.

Do not treat an ADR as general project context. ADRs record accepted durable decisions. Use a question ledger for unknowns and a gap ledger for known divergence.

Treat repository-owned product context as canonical. Do not require it to be migrated or rewritten into a Boatstack memory. Specs, plans, summaries, and selected context are temporary task projections: keep them reviewable, link material claims back to their source paths, and never silently replace the source. Preserve the source; project only the relevant slice.

## Run `plan-gate`

1. Run the read-only Markdown preflight and retain its exact fingerprint:

```bash
.product-loop/boatstack check-plan \
  --plan .product-loop/features/<feature>/plan.md
```

2. Present the draft spec, plan, open decisions, accepted assumptions, gaps, risks, validation provenance, `PLAN_FINGERPRINT`, and `READINESS_FINGERPRINT` in a reviewable form. A schema-v3 plan must decide `journey_evidence`: `relevant` with complete typed runnable oracles, or `not_relevant` with a reason.
3. When `workflow.human_plan_approval` is true, ask the developer to approve it or request changes and end with: Reply `a` to approve. When false, state that Build will create a policy-activation lock and do not imply human approval.
4. On changes, return to `auto-plan`, preserve the feedback in the question ledger, and issue a new draft.
5. When human approval is enabled, invoke `.product-loop/boatstack record-approval` with the plan, named human, RFC3339 timestamp, and exact fingerprint. When disabled, create no `approval.md`.
6. End in Plan mode and tell the developer the feature is authorized for the host's normal Build transition. Do not compile tasks, create a lock, request Agent mode merely to write a file, or edit product code.

All files created or updated by `auto-plan` and `plan-gate` must be Markdown. gstack and Spec Kit may help produce those documents, but their implementation stages and non-Markdown executable state are deferred to `build`.

## Build without erasing evidence

- First confirm the host is in an execution-capable mode. If a requested transition is rejected or product-code writes remain unavailable, return `READY_FOR_BUILD` and stop without activating, compiling, or writing a lock.
- Before the first product-code edit, activate the exact authorized Markdown plan. Include `--approval` only when `workflow.human_plan_approval` is true:

```bash
.product-loop/boatstack activate-plan \
  --plan .product-loop/features/<feature>/plan.md \
  --out-dir .product-loop/features/<feature>/compiled \
  --output .product-loop/features/<feature>/plan.lock.json
```

For human authorization, add `--approval .product-loop/features/<feature>/approval.md`.

- Activation atomically repeats readiness, verifies the plan fingerprint and any required approval, compiles `tasks.json`, `test-matrix.json`, `journey-oracles.json`, and the evidence skeleton, then writes a schema-v3 readiness-bound lock with `authorization_mode: human` or `policy`. Existing active schema-v1/v2 locks remain readable. Missing required approval, open blocking questions, or any changed input returns `BLOCKED`.
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

## Repair from ordinary conversation

Before any product edit or explicit `repair`, run `recovery-status` with the exact requested change and observed source stage. It resolves active work and published work associated with the current branch or recorded PR. Automatically use repair for ordinary CI failures, review findings, denied publication, problems, and modifications even when the user does not name Boatstack or a slash command. Active work resumes through `record-change`; a published parent returns `CORRECTIVE_CHILD_REQUIRED` and a deterministic child id. Never ask the user to manually repeat a denied push or PR mutation.

If Cursor reports `MainThreadShellExec not initialized`, the host failed before Boatstack's hook process started. Keep the hook fail-closed and make **Developer: Reload Window** the primary recovery, then retry the operation. Recommend the verified installer only when Boatstack itself reports a missing, drifted, unsafe, or checksum-invalid helper/runtime.

If any host reports `HOST_PAYLOAD_MALFORMED`, Boatstack received an event it could not safely decode; no unsafe operation was detected. Retry once with an explicit non-empty command. If the same code repeats, stop shell and tool retries, preserve current edits, and run `.product-loop/boatstack diagnose-hook --host <host> --repo .` from an external terminal. For Cursor, start a new task after the probe. The diagnostic proves the installed guard with a canonical event but cannot inspect the live event supplied by the host. Do not recommend reinstall or hydration unless Boatstack separately reports a missing, drifted, unsafe, or checksum-invalid runtime.

Same-intent repair resumes at the helper-reported stage and reuses the existing gates. Pass `--mechanism` for every repair classification. Implementation, verification, and review repairs each have an independent three-attempt budget. Requirement amendments and readiness recovery consume none. An identical failure-class, evidence, and mechanism retry is denied. A requirement amendment or ambiguous expected behavior blocks product edits and returns to a concise Plan Gate delta. Never edit `changes.md`, ignored delivery state, or receipts directly; those are emitted by controlled transitions. Conversation history is never workflow authority.

A published delivery is immutable. Record the append-only observation without changing its state, then automatically prepare a one-slice correction under the suggested feature id with `parent_delivery` set to the published feature. Present the inherited intent, observed failure, existing local diff, verification, and PR destination, then pause for the normal fingerprinted human approval. The corrective child receives its own lock and full gates. A verified open PR reuses its head branch and is updated; merged or closed work uses a fresh branch and PR. Unknown PR state may be planned but blocks destination-specific publication.

## Enforce the gates

### Test gate

- After build completes, the source Plan-mode file is no longer a runtime prerequisite. Test, review, and ship use the approved lock, actual diff, and accumulated evidence; provenance remains recorded in the lock.
- Derive tests from acceptance criteria and affected contracts, not only from the implementation.
- Run existing relevant tests plus targeted new tests, linters, type checks, builds, and runtime checks.
- When `journey_evidence` is relevant, run every compiled oracle and import typed results with `record-journey-results --feature <feature> --results <json>`. Test and review gates reject missing, failed, manifest-mismatched, head-mismatched, or diff-stale results.
- For relevant PR visual scenarios, use the repository runner first, then a host browser against the existing development server, one supplied launch instruction, or an explicitly approved machine-local runtime. A harness may write `BOATSTACK_CAPTURE_RECEIPT` with scenario id, reached state or URL, named check results, and overall result; without it the PNG is only `CAPTURED`, never scenario-verified. Do not modify repository dependencies or configuration for capture. A human must review each exact PNG for secrets and private data and record `human-reviewed` before any external upload; keep the images outside the repository.
- Treat model-authored tests and same-model self-review as evidence, not ground truth.
- Validate that tests load and exercise the intended interface. For high-risk code, add an independent oracle such as contract fixtures, mutation testing, differential checks, staging verification, or human acceptance.
- A failing check blocks the gate. A skipped check must include a reason and risk owner. `PASS_WITH_GAPS` is accepted only when `workflow.allow_pass_with_gaps` is true.
- Commit the intentional active-slice product and evidence diff, then record the test result with `record-delivery-gate --feature <feature> --slice <slice> --gate test`. The receipt is bound to the base/head branches, commit, product diff, and evidence hash. Editing an evidence status is not a gate transition.

### Review gate

- Review the actual diff, not the intended plan alone.
- Check spec traceability, invariants, data/security/tenancy boundaries, failure behavior, backward compatibility, migrations, observability, tests, docs, and gaps.
- When configured high-risk paths changed, use a human peer or separate agent and record `--reviewer-identity` with `--review-method human_peer|separate_agent`.
- Convert actionable findings into tasks. Do not pass while critical findings are open.
- On pass, record `record-delivery-gate --feature <feature> --slice <slice> --gate review`. Review is rejected unless the same diff already has a test receipt; any later product change makes both receipts stale.

### Ship gate

- Require a clean, intentional diff; passing required checks; a filled evidence ledger; explicit known gaps; and rollout/rollback notes.
- Project only review-relevant context into `.product-loop/features/<feature>/pr.md`: why, changed behavior, review order, decisions, acceptance evidence, gaps, risks, rollout, rollback, and collapsed provenance.
- Treat the actual committed diff as what changed, approved artifacts as why it changed, and evidence as the only support for completion claims.
- In the visible Evidence table, link each managed claim to the current repository-relative evidence ledger using a readable link label; do not expose hashes or absolute paths.
- Always include why, what changed, review order, evidence, gaps/risks, rollout/rollback, and collapsed provenance. Add UI evidence, security/privacy, migration, or operations sections only when relevant.
- When PR visual evidence is relevant or unresolved, show the exact fingerprinted PNGs and external-host privacy warning, include the structural Visual evidence table, and treat `o` or `u` as approval of the PR body plus one evidence comment. Boatstack defaults to Litterbox with a 72-hour expiry, verifies every hosted URL, and puts only hosted Markdown image links in the comment. Never attach image files to the PR or commit an evidence branch. `suggest` retains a visible gap, while `require` blocks completed publication. Preserve an opened PR and retry the same fingerprint and comment from `visual_pending` rather than opening a duplicate.
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
5. Run `prepare-update-pr --repo . --version <version> --json`; show its exact non-empty fingerprinted package with the version transition, release notes, integration state, changed infrastructure paths, checksums, rollout, and rollback. Respond **Boatstack update ready** and render the one next action as: Reply `o` to open update PR.
6. Only the state-scoped `o` or compatible full reply authorizes `publish-update-pr` with that preview fingerprint. The publisher owns staging, the exact commit, normal push, and one PR. If its response is interrupted, inspect `operation-status` and reconcile the exact branch/PR rather than repeating GitHub mutation. Never merge it. If GitHub publication is unavailable, retain the prepared branch and provide one manual action.

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

For a retro over past sessions, run the read-only `.product-loop/boatstack retro derive --input <transcript> [--input <transcript> ...]`. It detects operator instructions that recur across sessions and classifies each as a missing observation, verb, setpoint, or guard, with a suggested typed promotion. It reads only the transcript files the user names, works fully offline, and writes nothing. A recurring instruction is evidence of a missing typed control — promote it by hand through the normal reviewed delivery flow; never turn it into a saved prompt, and never apply a proposal automatically.

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
.product-loop/boatstack export --repo /path/to/repo --config /path/to/project.json --write
```

Run with `--check` in CI to detect drift. The exporter writes generated files only and refuses to overwrite user-owned files. Review the generated diff in a branch and ship it through a PR.
