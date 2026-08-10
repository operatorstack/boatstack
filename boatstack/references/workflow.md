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

`repair-state` is the bounded recovery for the one state `recovery-status` cannot resolve: an unregistered feature draft whose `plan.md` never passed through the helper, so it has no plan lock and no delivery state. A malformed such draft makes the guard fail closed at `INVALID_STATE` and deny every product mutation. `repair-state` quarantines that directory out of `.product-loop/features/` into `<git-common-dir>/boatstack/quarantine/<feature>/<timestamp>` — reversible, never a hard delete — and returns the workflow to `auto-plan`. It resolves the sole malformed candidate when `--feature` is omitted and refuses ambiguity. It acts only on a directory carrying no durable authority: it refuses any feature with a valid saved plan, a plan lock, a `pr.md`, a managed delivery state, git-tracked files, or an active or published delivery. The guard allowlists it independent of stage but still rejects shell metacharacters and non-helper executables; gating for registered, active, or published deliveries is unchanged.

The **transactional mutation boundary** is the standing form of the same principle `repair-state` applies once: a supervisor that removes an actuator must still expose a bounded actuator capable of reaching every valid state, including reversing its own last move. When Boatstack promotes a managed artifact that spans files which must land together, the candidate bytes are submitted as a mutation set that the runtime confirms against per-file base hashes and a supervisor-authority token, writes atomically all-or-nothing, verifies after the write, and rolls back automatically on failure — recording a reversible receipt with per-file before/after hashes. A rejected mutation persists no identity, so a candidate recomputed against the current base and authority applies cleanly: refusal is fail-closed but never a deadlock. Plan activation promotes all five of its managed artifacts — the compiled `tasks.json`, `test-matrix.json`, `evidence.md`, `journey-oracles.json`, and the `plan.lock.json` — through a single mutation, so they land or fail together.

The boundary is **closed under inversion**: every receipt carries its own inverse bytes and *is* the undo command. Undo re-applies that inverse as an ordinary mutation through the same boundary (an explicit absent operation expresses a delete, so the inverse of a create is a first-class mutation), which makes undo atomic and verified, makes the base precondition the conflict guard (undo refuses rather than clobbering later work), and makes **redo just an undo of the undo receipt**. Two bounded verbs expose this and are allowlisted by the guard at any stage (like `repair-state`, still rejecting shell metacharacters and non-helper executables): `mutation-status` (read-only) lists or inspects receipts so an agent can find the one to reverse, and `undo --mutation <id>` reverses it. `undo` is state-aware — it refuses to reverse a plan activation once a delivery gate receipt exists, so it can never strand delivery state without its lock. This boundary governs Boatstack's own generated managed artifacts only, never coding-agent source-code editing.

The `SOURCE_PLAN` file is required from entry through completion of `BUILD`. After build, its path and hash remain recorded for provenance, but `TEST_GATE`, `REVIEW_GATE`, and `SHIP_GATE` do not require the original file to be present.

## Irreversible-operation boundary

Every installed host routes supported shell and MCP events through Boatstack's immutable safety guard. High-confidence database, filesystem, Git-history, cloud-resource, and recovery destruction is always denied before execution. There is no prompt, approval reply, break-glass token, or in-session override. Source may be edited for review, but executable destructive capability blocks activation and gate progression until it is removed or moved to an operator-owned process.

After an external-write failure, preserve state and use only read-only diagnosis. Do not escalate privileges, broaden the target, or invent a reset. Use a transactional retry only when retry safety is demonstrated; otherwise stop and fix forward. Destructive recovery is operator-only outside Boatstack. See `irreversible-operation-boundary.md` for the classified operations and evaluation status.

Repository administration is not a delivery transition. Branch synchronization, status, switching, worktree maintenance, and requests to discard local changes do not enter `auto-plan` or `repair` unless the exact target branch belongs to an active managed delivery. Use the project-local `workspace-sync` helper for recoverable branch alignment: it fetches the exact remote source, preserves the original branch and dirty worktree under verified Git refs, updates the branch in its owning worktree, and verifies the final ref and clean status. A raw destructive-Git denial must return this one recovery action immediately without plan inspection or repository-wide discovery.

Hooks are defense in depth rather than a complete sandbox. Protected systems still require least-privilege credentials, scoped service roles, backups, and service-side destructive approval. `run-preflight` reports `HOOK_GUARDED` for this default posture and never presents it as credential isolation. Repositories configured for `credential-enforced` mode block before delivery mutation unless a trusted external attestor supplies a current repository-only receipt; only that path reports `CREDENTIAL_ENFORCED`. A missing, drifted, or failing helper denies execution and requires reinstall or repair. Cursor's exact `MainThreadShellExec not initialized` error occurs before the Boatstack hook starts; preserve fail-closed behavior, reload the Cursor window, and retry before diagnosing the Boatstack installation.

## User-facing response contract

Helper commands and state labels are internal control machinery. Every normal response uses
the structure below, with a host-compatible rendering for **Technical details**.

### Boatstack banner

Begin every Boatstack response with the status banner, so the reader can tell Boatstack's
output apart from ordinary prose and see where their work stands at a glance. Emit the exact
output of `.product-loop/boatstack next-status --repo . --render` verbatim (a fenced code block or as
plain lines), above the `## <Plain-language outcome>` heading. The banner is presentation only:
it does not replace the single `### Next step`, does not add a second action, and never
introduces machine codes or internal stage names (the renderer already hides them). The `--json`
projection remains the source of truth for decisions and belongs in **Technical details**, not
the banner. Skip the banner only for replies that are not about a Boatstack operation.

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

### The operator frontier

Every next step belongs to one actor: the operator or the agent. A step belongs to the operator only when it needs operator knowledge or authority — an approval (`a`), a publish decision (`o`/`u`), a cleanup decision (`c`/`k`), a feature choice, a source-plan path, or a correction fact. Every other step belongs to the agent, including steps whose evidence the agent produces by doing the work: build sub-actions, plan checks, test runs, and the review protocol. The helper computes the actor (`next_actor` in `flow next --json`) and marks agent-owned steps in the rendered response with "This step is mine to do."

End a working response only at the operator frontier: the final `### Next step` must belong to the operator, or state that no action is required. Never end a working response by describing work the agent still has to do — do the work, re-render `next-status --repo . --format response`, and continue until the next step belongs to the operator. Presenting the agent's own pending work as the operator's next step is a contract violation.

The read-only `next` status query is the one exception, because a status question must not mutate anything. When the rendered step is marked as the agent's, the one next action is the delegation reply: reply `g`, and the agent executes the marked step and continues to the operator frontier under the same bounds as the foreground run coordinator. Stop immediately when executing a step does not change the prescribed next step — repetition without progress is a stall, never a loop. Report the block plainly and hand the turn to the operator.

**Write in Simplified Technical English.** Use short sentences, the active voice, and the present tense. State one idea per sentence, put the condition first, and choose the simple, common word. Keep a term consistent, and write positively. This applies to every operation, including the review findings and the PR brief. It does not change the fixed outcome labels, the single `### Next step`, the collapsed **Technical details**, or the reply keys.

| State | Outcome -> one next action |
|---|---|
| `next`, `/boatstack-next`, `$boatstack next` not started / active / complete / ambiguous | **Start a Boatstack feature** -> save a Plan-mode file or run `auto-plan`; **Next Boatstack stage** -> run the one repository-backed operation (when the rendered step is marked as the agent's, reply `g` to have the agent do it and continue to the operator frontier); **Feature complete** -> no action required; **Boatstack state needs attention** -> resolve the named ambiguity (address the invalid evidence, or, when the block names only past deliveries, ignore a named past delivery after explicit user confirmation) |
| `run`, `/boatstack-run`, `$boatstack run` not started / complete / paused / blocked | **Start a Boatstack feature** -> save a Plan-mode file; **Feature ready for review** -> review the published PRs; **Boatstack run paused** -> provide the one required approval, confirmation, or product answer; **Boatstack run needs attention** -> resolve the named freshness, safety, state, or repair blocker |
| `insight-capture` | **Insight ready to save** -> reply `s` to save the exact fingerprint-bound Value Map preview as a tracked `docs/insights/<id>/` repository diff; **Insight saved as a repository diff** -> review or publish that intake artifact separately; no delivery is created |
| `insight-frontier` | **Insight frontier ready** -> review the independent captures needing classification, delivery, evidence, or human completion; this read-only view never replaces the delivery frontier |
| `root-cause`, `/root-cause`, `$boatstack root-cause` | **Root cause found** -> save the diagnosis as a source plan and run `auto-plan` with it via `--plan`; the operation is read-only and never edits code or advances a gate |
| `auto-plan` ready / needs answers | **Plan ready** -> run `/plan-gate`; **I need your input** -> answer with the displayed choice keys or `r` for all recommendations |
| `plan-gate` pending / approved | **Ready for your approval** -> reply `a` to approve; **Approved — ready to build** -> enter execution mode and run `/build` |
| `build` success / paused | **Build complete** -> run `/test-gate`; **Build needs a decision** -> answer the blocking question |
| `repair`, `/repair`, `$boatstack repair` not started / pre-build / same intent / amendment | **No active delivery to repair** -> run `auto-plan` or the verified pre-build gate; **Repair recorded** -> perform the reported resume stage; **Plan amendment required** -> review the proposed intent delta |
| `test-gate` pass / blocked | **Tests passed** -> run `/review-gate`; **Testing found a problem** -> perform or authorize the repair |
| `review-gate` pass / blocked | **Review passed** -> run `/ship-gate`; **Changes required** -> address the blocking finding |
| `ship-gate` preview / published | **PR ready** -> reply `o` to open or `u` to update the previewed PR; **PR opened** -> review the PR; never imply merge authorization |
| `boatstack-update` current / postponed / prepared / published / blocked | **Boatstack is current** -> no action required; **Update postponed** -> finish feature work and rerun from the clean default branch; **Boatstack update ready** -> reply `o` to open the update PR; **Update PR opened** -> review the PR; **Update needs attention** -> address the one reported collision or health failure |
| `retro` | **Improvement proposed** -> review or authorize the experiment |
| `workspace-cut` (surfaced after plan validation and before approval or autonomy) | **Fresh workspace ready** -> continue every later command from the returned destination; **Workspace already current** -> continue there |
| `workspace-cleanup` (surfaced by `boatstack-next` after publication) | **Workspace ready to clean up** -> reply `c` to remove the worktree and branch, or `k` to keep; **Workspace kept** -> no action required; **Workspace still open** -> the PR is not merged yet, keep waiting or override explicitly |

### Foreground run coordinator

`run` is an opt-in foreground coordinator over the existing operations, not a second state machine. It accepts `--to plan|verified|pr`; when the request names no target, the host asks once. The target is recorded in a fingerprinted `autonomy.md` receipt bound to the plan, repository, branch, eligible policy decisions, and, for `pr`, one open or update action. `plan` stops at a valid reviewable plan. `verified` uses policy activation and stops after test and review gates. `pr` continues through exact preview validation and one normal publication without a second confirmation. Receipt drift fails closed. Human-driven runs without an autonomy receipt retain the existing approval and publication confirmations. The coordinator never merges, rebases, switches or creates constrained branches, discards changes, force-pushes, merges a PR, or deploys.

After preflight, resolve the repository-backed next operation, execute exactly that canonical operation, verify the resulting state, and resolve again through all declared delivery slices. When the resolved block names only past deliveries, the coordinator may offer to ignore a named past delivery (adding its slug to `workflow.ignored_deliveries`) only after explicit user confirmation; any new, unlisted ambiguous delivery still pauses. Pause for `a`, a material product answer, and `o` or `u`; after the valid state-scoped reply, continue in the current host session. The invocation does not replace either human authorization. Automatically record and repair same-intent test or review failures for at most three complete repair-and-gate cycles per active slice per invocation. Stop immediately for requirement amendments, ambiguous or stale state, unsafe capability, unsupported recovery, branch mismatch, or exhausted repairs. Store no durable run/autopilot mode; re-invocation reconstructs progress from canonical repository state.

### Reply shortcuts

The exact reply `s` saves only the currently displayed insight preview. It must match the source, Product Value Map, topics, nonce, and preview fingerprint checked in the same host state. It does not approve a plan, bind a delivery, complete an insight, or grant PR authority.

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

Begin in the active coding host's Plan mode. Explore the ordinary product intent without editing implementation files, then save that host-generated plan as a durable file. Invoke `auto-plan` with the plan's path.

For bug-shaped intent (a crash, stack trace, or failing signal), the read-only `root-cause` operation is the optional on-ramp to this state: it classifies the failure against [failure-moves.md](failure-moves.md), produces a cited root-cause chain, and proposes the structural change that eliminates the failure class rather than patching the instance, formatted as the source plan you then save and pass to `auto-plan --plan`. It never edits code, writes artifacts, or advances a gate.

Before repository inspection, run:

```bash
.product-loop/boatstack check-source-plan --repo . --plan <host-context-path>
```

Boatstack never scans directories for plans, so `--plan` is required and no unshipped saved plan becomes ambient context. If no plan path is supplied, or the file is missing, empty, or unreadable, `auto-plan` is `BLOCKED` and must request the plan to build. It must not manufacture the missing input. Because the file's hash is recorded and re-checked through `BUILD`, `--plan` must point at a durable in-repo path that stays present and unchanged; a path outside the repository is rejected. This source plan is an initial proposal rather than human approval.

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

### Literal planning transport

Feature artifacts are authored through the owned channel `.product-loop/boatstack planning-write --repo . --feature <feature> --artifact <name>`. The complete Markdown document and command must cross the host hook in one literal envelope. The hook binds the command to the current repository's project-local helper, validates the command and closing delimiter, treats the body as data, and denies truncation or trailing commands before the shell runs.

In Bash, zsh, and Git Bash, use a single-quoted heredoc. The closing token must not occur as a line in the Markdown; choose another simple token when it does. In Git Bash on Windows, append `.exe` to the project-local helper path.

```bash
.product-loop/boatstack planning-write --repo . --feature <feature> --artifact <name> <<'BOATSTACK_PLAN_EOF'
<complete Markdown document>
BOATSTACK_PLAN_EOF
```

In Windows PowerShell, keep UTF-8 local to a child scope and use a single-quoted here-string. If the document contains a line beginning with the PowerShell closing mark `'@`, use the Git Bash form with a non-colliding token.

```powershell
& {
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
@'
<complete Markdown document>
'@ | & '.product-loop\boatstack.ps1' planning-write --repo . --feature <feature> --artifact <name>
}
```

Send the complete envelope in one shell-tool call. Do not run a bare `planning-write`, split the command and body across calls, prepend or append another command, use an unquoted or double-quoted delimiter, or paste the Markdown at an interactive shell prompt. `PLANNING_TRANSPORT_INVALID` means nothing ran; correct the envelope instead of manually replaying its body. The host's own Markdown writer is permitted only where the host allows it; arbitrary redirection to a feature path never is.

Validation must be derived before implementation. Each check records:

- `run`: an executable command or a specific human/external procedure;
- `criteria`: only the acceptance claims this procedure can actually support;
- `origin`: the acceptance criterion, repository invariant, human decision, risk, or external contract that requires it;
- `oracle`: the fixture, schema, threshold, rubric, external fact, or authorized judgment capable of falsifying the claim;
- `independence`: whether the oracle is pre-existing, contract-derived, external, human, or implementation-authored.

Subjective work is not exempt from validation. Convert ambiguity into an approved reference, rubric, scenario, threshold, and evidence owner. If materially different interpretations remain or no defensible oracle exists, keep the plan `BLOCKED` at `PLAN_GATE`.

When `workflow.pr_visual_evidence` is `suggest` or `require`, every managed plan also records a `pr_visual_evidence` decision. A relevant decision defines one to three scenarios with an entry surface, required state, viewport, and expected visible outcomes. A not-relevant decision records its reason. Planning may discover repository-owned visual tooling but must not require Storybook, Playwright, or another framework-specific dependency.

### `PLAN -> PLAN_GATE`

Run `.product-loop/boatstack check-plan --plan <feature>/plan.md` and present the full draft, plan fingerprint, and product baseline returned by the check. A non-empty baseline includes its exact diff, changed paths, and SHA-256 so edits that existed when managed planning began remain visible and preserved. When `workflow.human_plan_approval` is `true`, require an exact standalone `a`, the compatible full reply `approve`, or a change request, and end the pending response with: Reply `a` to approve. When it is `false`, report that Build will create a policy-activation lock and do not create or imply human approval. The check is read-only.

### `PLAN_GATE -> PLAN_APPROVED`

When human approval is enabled, invoke `record-approval` with the named human, RFC3339 timestamp, exact plan fingerprint, and displayed baseline-diff fingerprint (omitted only for a clean baseline); it recomputes both and creates only `approval.md`. When disabled, skip that operation and preserve the checked plan and baseline for policy activation. Remain in the host's Plan mode; do not compile machine artifacts or edit product code.

Ask 1-3 finite questions using the global keyed-choice format whether the host renders them through a structured question tool or plain text, then return `WAITING_FOR_INPUT`. Never convert an unavailable question UI into permission to choose a default. A standalone `r` is an explicit human acceptance of all recommendations displayed in that response, not an agent-selected default. Authoritative repository facts are `DISCOVERED`; agent suggestions and repository-derived product choices are `PROPOSED`; only explicit human responses are `ANSWERED`. Every material proposal remains in `blocking_questions` until answered.

### `PLAN_APPROVED -> BUILD_ACTIVATION -> PLAN_LOCKED`

At the host's normal Build transition, first confirm the host is in an execution-capable mode. If the transition is rejected or product-code writes remain unavailable, return `READY_FOR_BUILD` without compiling or writing a lock. Once execution is available and before the first product-code edit, `activate-plan` deterministically:

1. parse and validate the marked structured block in `plan.md`;
2. hash the complete source plan, spec, `plan.md`, and pre-activation product baseline, matching them to `approval.md` when human approval is enabled;
3. compile the task graph, requirement-test traceability rows, and evidence skeleton without adding semantics;
4. record authorization mode, timestamp, source commit, artifact hashes (the compiled task-graph hash bound from the in-memory candidate), readiness fingerprint, and baseline diff/path provenance in plan-lock schema v3, plus approver provenance only for human authorization;
5. promote all five artifacts (`compiled/tasks.json`, `compiled/test-matrix.json`, `compiled/evidence.md`, `compiled/journey-oracles.json`, and `plan.lock.json`) through the transactional mutation boundary as one mutation, whose post-write check re-validates the compiled JSON, asserts the evidence ledger is non-empty, and rechecks the promoted lock before permitting implementation — so all five land all-or-nothing and the reversible receipt can undo the whole activation.

Activation also initializes ignored, worktree-local Git delivery state bound to the lock.
One implicit `delivery` slice preserves the ordinary one-feature/one-PR flow. An
explicit multi-slice plan starts only its first slice in `BUILD`; later slices remain
`PENDING`.

Because delivery state is keyed to the lock hash, re-activating an amended plan (a
widened tail slice, a new phase, any edit that changes the lock) **reconciles** rather
than resets: the already-published prefix is preserved verbatim — its status, PR, and
branch bookkeeping intact — the active pointer holds, and only the recomputable tail is
re-derived (the active slice restarts at `BUILD`, the rest `PENDING`), recording the
superseded lock. An amendment that would drop, reorder, rename, or change an
already-published slice, or any edit to a fully-published (immutable) delivery, is
refused before the transactional promote — nothing half-applies — and routes to a
corrective child delivery (see "A published delivery cannot be reset"). Published status
is read from the pointer and slice status, never from `pr_state`.

Missing required human approval, unresolved `blocking_questions`, or any change to the source plan, spec, complete `plan.md`, or displayed product baseline blocks activation and returns the feature to `PLAN_GATE`. Existing schema-v1 approval receipts remain valid only with a clean product baseline. A failed or partial compilation never creates a valid lock. Existing schema-v1 human locks remain valid; policy activation always writes schema v2.

After `auto-plan` successfully saves a feature plan, managed authority is latched before activation. Reads and bounded Markdown planning transitions remain available — `planning-write` is the channel that stays open for authoring planning Markdown while the latch holds — but native edits, mutation-capable MCP tools, and shell commands not proven read-only are denied until activation creates a current lock. Approval itself does not authorize product edits. Ambiguous, stale, malformed, or unverifiable phase state fails closed with one recovery operation; repositories with no saved managed plan retain ordinary unmanaged behavior.

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

Review only after required mechanical checks pass, unless reviewing a failure is the goal. The reviewer inspects the actual diff and reports findings by severity with file/line evidence, consequence, and correction. Write each finding in Simplified Technical English.

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

Project the approved feature and actual committed diff into a reviewer-ready title and body. Write the brief in Simplified Technical English:

- why the change exists;
- what changed, grouped by reviewer concern;
- the shortest useful review order;
- decisions that materially shaped the diff;
- acceptance and check evidence with source references;
- known gaps, risks, rollout, and rollback;
- collapsed approval, evidence, and coding-host provenance.

Store the exact preview at `.product-loop/features/<feature>/pr.md`. Its non-rendered frontmatter records the title, base/head branches, managed feature, and context fingerprint; the remaining Markdown is the exact GitHub body. The preview artifact itself is excluded from the product-diff fingerprint so committing it does not create a self-referential hash.

PR schema v4 always records `pr_visual_evidence_policy`, `pr_visual_evidence_status`, `pr_visual_evidence_count`, `pr_visual_evidence_fingerprint`, `pr_visual_privacy_status`, and `pr_visual_privacy_receipt_fingerprint`. Relevant or unresolved PRs contain a structured **Visual evidence** section. Show the exact local images and external-host privacy warning before confirmation. Automated `clean` capture requires a separate human receipt from `review-pr-visual-evidence` that binds the exact manifest fingerprint and PNG hashes; changed pixels invalidate it. The state-scoped `o` or `u` authorizes the fingerprinted PR package: title, body, and one Boatstack-owned visual-evidence comment. After human privacy review, Boatstack uploads to the configured external host (Litterbox for 72 hours by default), verifies every returned URL, and writes only hosted Markdown image links. If upload, URL verification, or comment mutation fails, preserve the PR and comment identity, record `visual_pending`, and retry the same fingerprint. Never attach PNG files directly or commit them to a branch. Under `require`, do not mark managed delivery published until the hosted comment is observed.

Before publication, show the exact title and rendered body. Use **PR ready** and exactly one action. When no PR exists, render: Reply `o` to open PR. When one exists, render: Reply `u` to update PR. Only the corresponding state-scoped shortcut or compatible full reply authorizes opening or updating the PR. After confirmation, commit only the reviewed `pr.md`, recheck the same preview fingerprint, committed product diff, plan approval, build lock, test evidence, and review evidence, then let the deterministic publisher perform a normal push and the selected GitHub action. It records the fingerprinted package before execution. A lost response enters reconciliation against the exact remote branch and PR; it never opens another PR blindly. Any package drift blocks publication and requires a new preview; never force-push.

For managed work, publication also requires current test and review receipts for the
active delivery slice. Successful publication marks only that slice `PUBLISHED` and
activates the next slice as `BUILD`. No parent-plan approval, prior phase receipt, or
context summary can skip these transitions.

Advancing the `BUILD` pointer does not revoke correctability of the slice just
published. While its PR is not terminal (not merged or closed), a `PUBLISHED` slice
**remains re-gateable and updatable in place**: `record-delivery-gate --slice <id>`
and `pr-context --slice <id>` redirect to that slice, and `publish-pr --action update`
re-targets its still-open PR without advancing the pointer a second time. Correctability
ends only when the PR reaches a terminal state, observed by the recovery/next resolver
and cached on the slice; from there correction routes to a corrective child delivery
(see "A published delivery cannot be reset"). This keeps multi-slice deliveries flowing
while never stranding a slice whose postcondition has not yet been observed.

Opening or updating a PR does not authorize merge or deployment.

After successful publication only, the publisher may use the ignored 24-hour release cache to report an available stable Boatstack version. The primary response and next action remain **PR opened -> Review the PR**. Put the maintenance notice in collapsed details, state that no files changed, and direct the user to run `/boatstack-update` from the clean default branch after the feature PR merges. Suppress repeated notices for seven days unless a different release appears. Release lookup failure never changes the ship result.

## Boatstack updates

`boatstack-update` is an infrastructure operation, not part of a feature plan. It first forces release discovery and inspects the current installation. If the repository is not on its current default branch, or contains changes outside verified Boatstack-owned repair paths, it changes nothing and returns **Update postponed**.

For an available version, create `chore/update-boatstack-v<version>` and download and checksum-verify the target helper before consulting the installed runtime. The target helper classifies hook fragments, generated locks, helper provenance, and marker-bounded interceptors. Exact installed state migrates automatically. Recoverable owned drift is fingerprinted and, interactively, offered as **Repair Boatstack-owned state and continue the update? [y/N]**; noninteractive updates stop with one `--repair` retry. Repair backs up the exact paths in Git-common state and remains in the same update PR. User-owned, mixed, malformed, symlinked, or product state stays blocked. Downgrades require both `--repair` and `--allow-downgrade`.

If the ignored local install lock is missing, malformed, or carries a development identity, the verified target helper derives the prior stable version only from `HEAD:.product-loop/generated.lock.json`. That committed pin makes the local provenance path repairable without trusting drifted worktree bytes. Every mutable controller target is paired with its owning storage boundary: embedded worktree state uses the worktree Git directory, embedded shared state uses the Git common directory, and detached state uses the external Boatstack control root. Effectful callers validate the paired boundary and never reconstruct it from the repository path.

The detached helper and its generated feature paths necessarily live inside that protected external root. Host hooks admit those paths only when a literal command uses the exact regular, non-symlink helper bound by the current repository's verified workspace context, every controller operand stays inside the same root, and the requested transition belongs to the resolved workflow position. Read-only helper observations remain read-only. A sibling helper, mixed controller roots, raw file operation, malformed command, or stage-invalid transition remains denied without changing controller state.

A terminal update receipt is consumed only while its target postcondition still holds. Before returning success for a prior `install-update`, Boatstack checks the target generated bundle, host hooks, execution interceptors, committed runtime pin, shared and local runtime identity, and preserved integrations. If an operator restored the old committed pin or otherwise removed that local atomic result, Boatstack records `POSTCONDITION_MISSING`, reopens only that `ATOMIC_LOCAL` update, and performs a fresh bounded attempt. PR publication and other external operations retain terminal replay suppression and are never reopened by this rule.

`update -binary <path>` installs the passed binary's **own self-reported version**, not the running helper's. Because each helper embeds its own version-bound generated bundle and compile-time constants, an older helper cannot correctly install a newer one in-process; when the passed binary self-reports a different identity, the whole update is re-executed by that binary so it installs itself — its bundle, constants, version-keyed shared-runtime slot, and durable receipt are then authoritative by construction, and the hand-off terminates in a single hop. The write boundary refuses to install a `-binary` whose self-report disagrees with the process running it, and re-hashes the freshly written slot against its manifest, rolling back on mismatch — so a runtime can never be labeled one version while carrying another's bytes.

The runtime bytes never travel through Git — only the guard's baked version path and the committed version pin do — so a teammate who pulls a merged version bump, or clones fresh, starts with the new pointers but an **empty**, gitignored, version-keyed shared slot. Rather than fail-close every such teammate until they re-install by hand, the safety guard **auto-hydrates** an absent slot: it runs the tag-pinned, `.sha256`-verified installer in a branch-free, slot-only `hydrate` mode, serialized clone-wide by an atomic `mkdir` lock (peers wait briefly for the slot to appear) and bounded by a timeout, then falls through to the existing missing/symlink/manifest/checksum gates. Hydration is strictly additive: those gates remain the sole authority for execution and stay fail-closed, so a disabled, timed-out, or failed hydration simply denies — now with the exact one-line self-heal command embedded in the message. The `hydrate-runtime` helper subcommand it invokes rewrites no committed generated file and requires no dedicated branch; it refuses to populate a slot whose identity disagrees with the worktree's pin, and since the installer downloads the exact pinned version first, running equals installed by construction (the runtime-cache re-hash-and-rollback is the backstop). This is a deliberate posture change — the guard runs a fetched installer on cold start — bounded by tag pinning, HTTPS, sidecar verification, the guard's own checksum re-verify before `exec`, the clone-wide lock, the timeout, and the `BOATSTACK_AUTO_HYDRATE=0` kill switch (with a `BOATSTACK_HYDRATE_COMMAND` override). It never becomes a new authority for execution.

Before a durable update attempt is created, Boatstack verifies the dedicated branch, base commit, repair classification, and current diff. Invalid workspace state consumes no retry budget. The update transaction then reuses one semantic ownership projection for admission, mutation, final verification, staging, and preview. Generated files must match their prepared bytes, host-hook files must preserve their non-Boatstack JSON, and `.cursorrules`, `CLAUDE.md`, and `GEMINI.md` must preserve everything outside their single Boatstack marker boundary.

The update transaction is a durable atomic-local operation. It preserves repository configuration, adapters, integrations, and unrelated host settings, then runs `doctor`. After installation, `prepare-update-pr` verifies that every changed path is Boatstack-owned and atomically stores the exact non-empty publication package in Git-common runtime state. Show release and repair provenance, the exact generated diff, checksums, changed paths, integration state, rollout, and rollback.

Use **Boatstack update ready** and exactly one action: Reply `o` to open update PR. Only the state-scoped `o` or compatible full reply authorizes `publish-update-pr` with that preview fingerprint. The publisher stages only the approved paths, reuses or creates the exact update commit, pushes normally, and reconciles the head branch before opening at most one PR. The PR body records release provenance, changed generated files, verification, rollout, and revert instructions. If a response is lost after GitHub accepted the request, the next invocation observes and returns the existing PR. If publication is unavailable, retain the prepared branch and provide one manual action. Never merge automatically.

## Durable operation boundary

During an active managed delivery, every mutation-capable host call receives a
single-use lease bound to its tool, target, argument fingerprint, plan authority,
and persistent attempt number. Post-tool events complete that attempt. Identical
active work reports **wait**; an already successful fingerprint is not relaunched.
Unknown completion reports **reconcile** and checks the expected Git, GitHub,
filesystem, browser, or MCP postcondition before any retry.

`operation-status --repo . --json` is read-only. An omitted operation ID resolves
only when at most one unfinished operation matches the current branch; ambiguity
is explicit and never resolved by recency. Receipts are ignored Git-common state
shared by linked worktrees. They store hashes and bounded facts, not commands,
secrets, user content, or autonomous execution intent.

`boatstack-run` consults this state before advancing. Its three-cycle repair
budgets are the delivery state's durable schema-v2 counters for implementation,
verification, and review failures, not a counter reset by a new conversation,
process, host, or async notification. Every repair records its mechanism. An
identical class, evidence, and mechanism retry is denied; requirement amendments
and readiness recovery consume no counter.

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

### `DRAFT_PLAN -> WORKSPACE_CUT -> APPROVAL OR AUTONOMY`

When `workspace.enabled` is set and a draft plan passes validation, `boatstack-next` routes to `workspace-cut` before human approval or autonomy is recorded. The operation fetches `origin` and selects the feature branch from that exact base. It creates a branch or worktree, adopts an unowned exact-base branch, or reuses a matching current worktree. It returns the destination repository, branch, base commit, plan fingerprint, controller mode, and outcome. The host continues every later command from that destination, so approval, autonomy, readiness, and activation bind the final feature branch.

In embedded mode, the complete planning package moves transactionally. Boatstack verifies the destination fingerprint before removing the source. In detached mode, the destination registers against the same repository controller identity. A divergent, dirty, owned, or conflicting destination fails closed before plan authority moves. A failed copy, registration, cleanup, or postcondition check restores the source package and removes partial branch, worktree, and controller authority. Workspace-disabled repositories keep their manual branch policy.

### `PR_OPEN -> WORKSPACE_CLEANUP`

When `workspace.enabled` is set, `boatstack-next` surfaces `workspace-cleanup` for a published feature whose managed worktree still exists locally. The `workspace-cleanup` operation checks the pull request's merge state (GitHub CLI, falling back to local ancestry) and reports it. When `workspace.cleanup_after` is `merge`, cleanup is offered only once the PR is confirmed merged; while it is still open, the workspace is kept and the human may keep waiting or override explicitly. Cleanup never removes a workspace with uncommitted or unmerged work without an explicit forced override, and it reclaims only the local worktree and branch — it never deletes a remote branch or merges anything. In `confirm` mode the human reclaims the workspace with the exact reply `c` (or keeps it with `k`); `auto` mode reclaims a merged workspace without a prompt; `off` disables cleanup. A fresh feature workspace is likewise cut from the up-to-date default branch when a new feature begins, so work never starts on a stale branch.

### `PR_OPEN -> MERGED` (only when `delivery.terminal` is `merged`)

With the default `published` terminal, the flow ends at an open PR, exactly as before. With `delivery.terminal: merged`, the read-only advisors keep prescribing until the PR is observed merged, from the live PR observation (never from anyone's claim):

- Checks running: the advisor prescribes `flow watch` (agent). The watch exits on change; resolve again.
- Checks failing: the advisor prescribes `record-change --source-stage ci` (agent). The failing check names ride along; the exact message and classification are derived from the check logs, then the correction re-passes its gates and republishes with `publish-pr --action update`.
- Merge eligible (checks green, reviews satisfied, clean merge state): the advisor prescribes the exact `gh pr merge <url> --squash` command (agent). This is prescribe-only: the command carries a foreign program, which the execute driver refuses categorically, so Boatstack can never merge — the agent runs `gh` under the host's own authority, and only as rendered.
- Review required, changes requested, PR closed, or an unverifiable position: nothing is prescribed; the step is the operator's.

The merged observation ends the flow (`FEATURE_COMPLETE`), which is also the workspace-cleanup/reap checkpoint.

### `PR_OPEN -> WATCH`

A published pull request changes asynchronously: checks finish, reviews land, merges happen. `flow watch` is the bounded waiting primitive for that interval. It re-observes the read-only frontier on an interval and exits when a row's position or owner changes, when nothing on the frontier can move, or when its timeout passes (distinct exit code). It performs no writes and executes no operation — observation and actuation stay separate, so waiting can never become acting. When the watch exits, resolve `next-status` again and continue from the fresh state.

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
