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
  -> REPAIR (when ordinary conversation reveals a change)
  -> TEST_GATE
  -> REVIEW_GATE
  -> SHIP_GATE
  -> PR_OPEN
  -> PUBLISHED (open, closed, or remotely unverified PR)
  -> CORRECTIVE_CHILD (when CI, review, or ordinary conversation reports a correction)
  -> FEATURE_COMPLETE (only after verified merge)
  -> WORKSPACE_CLEANUP (when workspace management is on and the feature's PR has merged)
  -> RETRO
```

Each transition emits an artifact and evidence. A host adapter may change how a command is invoked, but it must not skip a transition or redefine a gate.

After build activation, persistent host adapters route ordinary change language through `REPAIR` before product edits. Same-intent implementation, verification, and review repairs resume at the earliest affected stage and supersede only downstream receipts. Changed or ambiguous intent enters `AMENDMENT_REQUIRED` and cannot pass a gate until a newly approved plan revision is activated. Existing `/test-gate` and `/review-gate` operations remain rerunnable; there are no repair-specific gates.

A published delivery cannot be reset. Its correction uses a deterministic new feature id and declares `parent_delivery` as the published feature, producing a separate plan lock, delivery state, and receipts while preserving the original evidence. If the recorded PR is verified open and still owns the recorded head branch, the child updates that PR. Merged or closed work uses a fresh branch and PR; unknown PR state may be planned but cannot select a publication destination.

`recovery-status` is the read-only resolver for CI failures, review findings, denied publication, and ordinary corrections. It selects by explicit feature, current active branch, current published branch, recorded PR identity, or one unambiguous candidate. It never chooses by recency. A stale reported head SHA, branch mismatch, or multiple match returns a blocker instead of drafting against the wrong delivery.

The `SOURCE_PLAN` file is required from entry through completion of `BUILD`. After build, its path and hash remain recorded for provenance, but `TEST_GATE`, `REVIEW_GATE`, and `SHIP_GATE` do not require the original file to be present.

## Irreversible-operation boundary

Every installed host routes supported shell and MCP events through Boatstack's immutable safety guard. High-confidence database, filesystem, Git-history, cloud-resource, and recovery destruction is always denied before execution. There is no prompt, approval reply, break-glass token, or in-session override. Source may be edited for review, but executable destructive capability blocks activation and gate progression until it is removed or moved to an operator-owned process.

After an external-write failure, preserve state and use only read-only diagnosis. Do not escalate privileges, broaden the target, or invent a reset. Use a transactional retry only when retry safety is demonstrated; otherwise stop and fix forward. Destructive recovery is operator-only outside Boatstack. See `irreversible-operation-boundary.md` for the classified operations and evaluation status.

Hooks are defense in depth rather than a complete sandbox. Protected systems still require least-privilege credentials, scoped service roles, backups, and service-side destructive approval. A missing, drifted, or failing helper denies execution and requires reinstall or repair. Cursor's exact `MainThreadShellExec not initialized` error occurs before the Boatstack hook starts; preserve fail-closed behavior, reload the Cursor window, and retry before diagnosing the Boatstack installation.

## User-facing response contract

Helper commands and state labels are internal control machinery. Every normal response uses
the structure below, with a host-compatible rendering for **Technical details**.

Cursor and Claude Code use a collapsed disclosure:

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

Codex and any host without verified HTML disclosure support use portable Markdown instead:

```markdown
## <Plain-language outcome>

<One or two sentence summary>

<Only the decision-relevant content for this operation>

### Next step

<Exactly one primary action>

### Technical details

Machine status, helper output, fingerprints, paths, receipts, and locks.
```

Never emit raw `<details>` or `<summary>` tags in Codex. Unknown hosts default to the portable Markdown form; rich disclosure is an explicit host capability, not
an assumption about generic Markdown support. This presentation difference must
not change the information, ordering, gate semantics, or one-action boundary.

Lead with a plain outcome, never a machine code such as `PASS`, `PLAN_APPROVED`, `BLOCKED`, `READY_FOR_BUILD`, `PASS_WITH_GAPS`, or `WAITING_FOR_INPUT`. Keep approval-relevant scope, non-goals, decisions, risks, and gaps visible. Move internal operations (`check-plan`, `record-approval`, `activate-plan`), hashes, paths, tables, receipts, locks, and raw output into **Technical details**. **Exactly one primary action:** end with the action that advances or unblocks the current state; a secondary option gets one short sentence. Never route past a blocked state.

| State | Outcome -> one next action |
|---|---|
| `next`, `/boatstack-next`, `$boatstack next` not started / active / complete / ambiguous | **Start a Boatstack feature** -> save a Plan-mode file or run `auto-plan`; **Next Boatstack stage** -> run the one repository-backed operation; **Feature complete** -> no action required; **Boatstack state needs attention** -> resolve the named ambiguity or invalid evidence |
| `run`, `/boatstack-run`, `$boatstack run` not started / complete / paused / blocked | **Start a Boatstack feature** -> save a Plan-mode file; **Feature ready for review** -> review the published PRs; **Boatstack run paused** -> provide the one required approval, confirmation, or product answer; **Boatstack run needs attention** -> resolve the named freshness, safety, state, or repair blocker |
| `auto-plan` ready / needs answers | **Plan ready** -> run `/plan-gate`; **I need your input** -> answer with the displayed choice keys or `r` for all recommendations |
| `plan-gate` pending / approved | **Ready for your approval** -> reply `a` to approve; **Approved — ready to build** -> enter execution mode and run `/build` |
| `build` success / paused | **Build complete** -> run `/test-gate`; **Build needs a decision** -> answer the blocking question |
| `repair`, `/repair`, `$boatstack repair` not started / pre-build / same intent / amendment | **No active delivery to repair** -> run `auto-plan` or the verified pre-build gate; **Repair recorded** -> perform the reported resume stage; **Plan amendment required** -> review the proposed intent delta |
| `test-gate` pass / blocked | **Tests passed** -> run `/review-gate`; **Testing found a problem** -> perform or authorize the repair |
| `review-gate` pass / blocked | **Review passed** -> run `/ship-gate`; **Changes required** -> address the blocking finding |
| `ship-gate` preview / published | **PR ready** -> reply `o` to open or `u` to update the previewed PR; **PR opened** -> review the PR; never imply merge authorization |
| `boatstack-update` current / postponed / prepared / published / blocked | **Boatstack is current** -> no action required; **Update postponed** -> finish feature work and rerun from the clean default branch; **Boatstack update ready** -> reply `o` to open the update PR; **Update PR opened** -> review the PR; **Update needs attention** -> address the one reported collision or health failure |
| `retro` | **Improvement proposed** -> review or authorize the experiment |
| `workspace-cut` (surfaced by `boatstack-next` at approved -> build) | **Fresh workspace cut** -> build on the new branch/worktree; **Workspace already fresh** -> continue to build |
| `workspace-cleanup` (surfaced by `boatstack-next` after publication) | **Workspace ready to clean up** -> reply `c` to remove the worktree and branch, or `k` to keep; **Workspace kept** -> no action required; **Workspace still open** -> the PR is not merged yet, keep waiting or override explicitly |

### Foreground run coordinator

`run` is an opt-in foreground coordinator over the existing operations, not a second state machine. It first resolves the read-only repository state, enters `auto-plan` when one saved source plan exists, asks for a saved Plan-mode file when none exists, returns **Feature complete** without requiring a remote only for completed work, and stops on unverified or blocked state. Before the first delivery-stage mutation it runs the versioned Git preflight, which fetches `origin`, requires the fetched remote base, verifies that the current named branch contains that base, rejects a behind or diverged upstream, and enforces any active slice branch constraints. Planning and approval remain local and do not require a remote. It never merges, rebases, switches or creates constrained branches, discards changes, force-pushes, merges a PR, or deploys.

After preflight, resolve the repository-backed next operation, execute exactly that canonical operation, verify the resulting state, and resolve again through all declared delivery slices. Pause for `a`, a material product answer, and `o` or `u`; after the valid state-scoped reply, continue in the current host session. The invocation does not replace either human authorization. Automatically record and repair same-intent test or review failures for at most three complete repair-and-gate cycles per active slice per invocation. Stop immediately for requirement amendments, ambiguous or stale state, unsafe capability, unsupported recovery, branch mismatch, or exhausted repairs. Store no durable run/autopilot mode; re-invocation reconstructs progress from canonical repository state.

### Reply shortcuts

Finite input uses one global, state-scoped reply grammar:

| Reply | Valid pending state | Meaning | Compatible full reply |
|---|---|---|---|
| `a` | Reviewed plan awaiting approval | Approve the exact plan fingerprint | `approve` |
| `o` | New feature, ad-hoc, or Boatstack-update PR preview | Open the exact previewed PR | `open PR` or `open update PR` |
| `u` | Existing PR preview | Update the exact previewed PR | `update PR` |
| `r` | One or more finite questions with exactly one marked recommendation each | Accept every recommendation displayed in that response | Explicitly name the recommended choices |
| `c` | Published feature whose merged workspace can be reclaimed | Clean up the feature's worktree and branch | `clean up` |
| `k` | Published feature whose workspace can be reclaimed | Keep the workspace for now | `keep` |

Trim surrounding whitespace and match shortcuts case-insensitively against the complete reply. Bracketed forms such as `[o]`, embedded letters, and shortcuts from another state are ordinary text. Continue accepting the full replies for compatibility, but do not advertise them in user-facing responses.

Before `c` removes a workspace, confirm the merge and safety gates in the `WORKSPACE_CLEANUP` contract. `c` never discards uncommitted or unmerged work and never deletes remote branches or merges anything; it only reclaims the local worktree and branch of an already-published feature.

Shortcuts never bypass gate prerequisites. Before `o` or `u` mutates GitHub, recheck the preview fingerprint, committed diff, evidence, authentication, and any required manual commit or push. Never interpret `r` as plan approval, PR publication, identity, secret input, permission escalation, policy bypass, destructive recovery authorization, or another exceptional safety decision. Free-text and operation-command prompts remain explicit.

For each finite product question, show 2-3 mutually exclusive choices with compact inline-code keys and exactly one label suffixed `(Recommended)`. With one question, use `1a`, `1b`, and `1c`; with multiple questions, continue with `2a`, `2b`, and so on. End with one reply hint using the keys and `r`. A standalone `r` is valid only when every displayed question has exactly one recommendation; echo the question-to-answer mapping before recording each answer as `ANSWERED` with explicit human provenance. Otherwise ask again without choosing.

For plan approval, resolve `approved_by` from (1) an identity supplied with approval, (2) the authenticated GitHub login from `gh api user --jq .login` when available, or (3) one short identity follow-up. Never invent a placeholder name (e.g., Sam, Eve). Never infer the approver from a filesystem username, commit history, or the coding agent. If identity is missing after approval, preserve the current fingerprint and approval intent, create no receipt, and ask only for identity; once resolved against the unchanged plan, do not require approval again. Keep identity and receipt data inside **Technical details**.

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

When `workflow.maintain_changelog` is enabled, every delivery slice includes
`CHANGELOG.md` in its affected paths. This is product-owned reader documentation,
not a generated Boatstack artifact.

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

When `workflow.pr_visual_evidence` is `suggest` or `require`, every managed plan also records a `pr_visual_evidence` decision. A relevant decision defines one to three scenarios with an entry surface, required state, viewport, and expected visible outcomes. A not-relevant decision records its reason. Planning may discover repository-owned visual tooling but must not require Storybook, Playwright, or another framework-specific dependency.

### `PLAN -> PLAN_GATE`

Run `boatstack-helper check-plan --plan <feature>/plan.md` and present the full draft and returned fingerprint. When `workflow.human_plan_approval` is `true`, require an exact standalone `a`, the compatible full reply `approve`, or a change request, and end the pending response with: Reply `a` to approve. When it is `false`, report that Build will create a policy-activation lock and do not create or imply human approval. The check is read-only.

### `PLAN_GATE -> PLAN_APPROVED`

When human approval is enabled, invoke `record-approval` with the named human, RFC3339 timestamp, and exact fingerprint; it creates only `approval.md`. When disabled, skip that operation and preserve the checked plan for policy activation. Remain in the host's Plan mode; do not compile machine artifacts or edit product code.

Ask 1-3 finite questions using the global keyed-choice format whether the host renders them through a structured question tool or plain text, then return `WAITING_FOR_INPUT`. Never convert an unavailable question UI into permission to choose a default. A standalone `r` is an explicit human acceptance of all recommendations displayed in that response, not an agent-selected default. Authoritative repository facts are `DISCOVERED`; agent suggestions and repository-derived product choices are `PROPOSED`; only explicit human responses are `ANSWERED`. Every material proposal remains in `blocking_questions` until answered.

### `PLAN_APPROVED -> BUILD_ACTIVATION -> PLAN_LOCKED`

At the host's normal Build transition, first confirm the host is in an execution-capable mode. If the transition is rejected or product-code writes remain unavailable, return `READY_FOR_BUILD` without compiling or writing a lock. Once execution is available and before the first product-code edit, `activate-plan` deterministically:

1. parse and validate the marked structured block in `plan.md`;
2. hash the complete source plan, spec, and `plan.md`, matching them to `approval.md` when human approval is enabled;
3. compile the task graph, requirement-test traceability rows, and evidence skeleton without adding semantics;
4. record authorization mode, timestamp, source commit, and all artifact hashes in plan-lock schema v2, plus approver provenance only for human authorization;
5. write the lock last and recheck it before permitting implementation.

Activation also initializes ignored, worktree-local Git delivery state bound to the lock.
One implicit `delivery` slice preserves the ordinary one-feature/one-PR flow. An
explicit multi-slice plan starts only its first slice in `BUILD`; later slices remain
`PENDING`.

Missing required human approval, unresolved `blocking_questions`, or any change to the source plan, spec, or complete `plan.md` blocks activation and returns the feature to `PLAN_GATE`. A failed or partial compilation never creates a valid lock. Existing schema-v1 human locks remain valid; policy activation always writes schema v2.

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

When `workflow.maintain_changelog` is enabled, update `CHANGELOG.md` before
recording test evidence. Add a concise bullet under the current Unreleased heading and one of
`Added`, `Changed`, `Fixed`, `Removed`, `Security`, `Documentation`, or
`Maintenance`. Describe the actual reader-visible outcome, not the commit, PR,
Boatstack artifacts, or test commands. Add only the category needed by the entry;
do not add empty category headings. If the file does not exist, create the
documented minimal skeleton with `## [Unreleased] - YYYY-MM-DD` and its first
entry. If it exists, add to the current file without rewriting its released
history or existing layout.

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

For relevant visual scenarios, resolve capture capability in this order: repository-owned visual tooling, a host browser against the existing development server, one human-supplied launch instruction, then explicit machine-local runtime setup. Capture must not edit source, dependency manifests, lockfiles, or test configuration. Bind each PNG to the current commit, product diff, scenario, viewport, SHA-256, and a `clean` or `human-reviewed` privacy receipt. `suggest` records unavailable capture as a visible gap; `require` retains a ship blocker.

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

With changelog maintenance enabled, the review recorder also compares the merge-base
and current `CHANGELOG.md`. It requires a new categorized `Unreleased` bullet and the
reviewer checks that its wording is supported by the actual diff.

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

PR schema v3 always records `pr_visual_evidence_policy`, `pr_visual_evidence_status`, `pr_visual_evidence_count`, and `pr_visual_evidence_fingerprint`. Relevant or unresolved PRs contain a structured **Visual evidence** section. Show the exact local images and public-repository privacy warning before confirmation. The state-scoped `o` or `u` authorizes the fingerprinted PR package: title, body, and one Boatstack-owned visual-evidence comment. A host browser may upload or update that comment; otherwise expose the exact local PNGs for manual attachment. If the PR mutation succeeds but attachment fails, preserve the PR, record `visual_pending`, and fix forward. Under `require`, do not mark managed delivery published until the attachment is observed.

Before publication, show the exact title and rendered body. Use **PR ready** and exactly one action. When no PR exists, render: Reply `o` to open PR. When one exists, render: Reply `u` to update PR. Only the corresponding state-scoped shortcut or compatible full reply authorizes opening or updating the PR. After confirmation, commit only the reviewed `pr.md`, recheck the same preview fingerprint, committed product diff, plan approval, build lock, test evidence, and review evidence, then perform a normal push and the selected GitHub action. Any drift blocks publication and requires a new preview; never force-push.

For managed work, publication also requires current test and review receipts for the
active delivery slice. Successful publication marks only that slice `PUBLISHED` and
activates the next slice as `BUILD`. No parent-plan approval, prior phase receipt, or
context summary can skip these transitions.

Opening or updating a PR does not authorize merge or deployment.

After successful publication only, the publisher may use the ignored 24-hour release cache to report an available stable Boatstack version. The primary response and next action remain **PR opened -> Review the PR**. Put the maintenance notice in collapsed details, state that no files changed, and direct the user to run `/boatstack-update` from the clean default branch after the feature PR merges. Suppress repeated notices for seven days unless a different release appears. Release lookup failure never changes the ship result.

## Boatstack updates

`boatstack-update` is an infrastructure operation, not part of a feature plan. It first forces release discovery and proves the current installation is healthy. If the repository is not on its clean, current default branch, it changes nothing and returns **Update postponed**.

For an available version, create `chore/update-boatstack-v<version>`, run the installer pinned to that release in update mode, preserve the repository configuration, adapters, integrations, and unrelated host settings, then run `doctor`. Show the release notes and link, exact generated diff, checksums, changed paths, integration state, rollout, and rollback. Product paths or generated-state drift are blocking.

Use **Boatstack update ready** and exactly one action: Reply `o` to open update PR. Only the state-scoped `o` or compatible full reply authorizes staging the reported infrastructure paths, committing, pushing normally, and opening the update PR. The PR body records old/new versions, release provenance, changed generated files, doctor result, integration state, rollout, and revert instructions. If publication is unavailable, retain the prepared branch and provide one manual action. Never merge automatically.

## Existing and ad-hoc PRs

There is no public `/pr-brief` operation. When the user asks in natural language for Boatstack to prepare, improve, summarize, or update an existing PR without a managed feature package:

1. project the committed branch diff, commits, observed checks, and minimal relevant repository context;
2. store the exact preview at `.product-loop/pr-briefs/<branch>/pr.md`;
3. use the same reviewer-first format, but mark unavailable approval and gate evidence `NOT_VERIFIED`;
4. never claim that Boatstack approved the work or that an unrun gate passed;
5. when `workflow.maintain_changelog` is enabled, require a new categorized
   `CHANGELOG.md` entry under `## Unreleased`;
6. preview first, then require `o` to open or `u` to update the PR and recheck the diff before publication.

Adaptive sections for security/privacy, migrations, UI evidence, or operations appear only when relevant. Model attribution belongs inside collapsed provenance. If GitHub CLI authentication is unavailable, keep the validated preview and provide one manual publication action instead of losing the work.

### `PLAN_APPROVED -> WORKSPACE_CUT`

When `workspace.enabled` is set and an approved feature is still on the default branch with no branch or worktree of its own, `boatstack-next` routes to `workspace-cut` before `build`. The `workspace-cut` operation fetches `origin`, cuts a fresh branch from the up-to-date default branch, and in `worktree` mode adds a linked worktree; in `branch` mode it switches in place. It never rewrites history, reuses an existing branch, or names the workspace after the base branch. Once the feature already has its own branch or worktree, this step is skipped and the flow proceeds straight to `build`, so a workspace you cut yourself is respected.

### `PR_OPEN -> WORKSPACE_CLEANUP`

When `workspace.enabled` is set, `boatstack-next` surfaces `workspace-cleanup` for a published feature whose managed worktree still exists locally. The `workspace-cleanup` operation checks the pull request's merge state (GitHub CLI, falling back to local ancestry) and reports it. When `workspace.cleanup_after` is `merge`, cleanup is offered only once the PR is confirmed merged; while it is still open, the workspace is kept and the human may keep waiting or override explicitly. Cleanup never removes a workspace with uncommitted or unmerged work without an explicit forced override, and it reclaims only the local worktree and branch — it never deletes a remote branch or merges anything. In `confirm` mode the human reclaims the workspace with the exact reply `c` (or keeps it with `k`); `auto` mode reclaims a merged workspace without a prompt; `off` disables cleanup. A fresh feature workspace is likewise cut from the up-to-date default branch when a new feature begins, so work never starts on a stale branch.

### `PR_OPEN -> RETRO`

Record unexpected friction and outcomes. A retro may propose a loop move, but it may not mutate durable instructions automatically.

## Gate semantics

- `PASS`: required evidence is present; no gate-blocking gap remains.
- `PASS_WITH_GAPS`: no critical gap remains; each accepted gap has impact, owner, and trigger, and `workflow.allow_pass_with_gaps` is enabled.
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
