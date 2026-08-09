# Artifact contract

Artifacts separate facts, decisions, unknowns, incompleteness, and evidence. Combining them into one context document makes stale assumptions difficult to detect.

| Artifact | Purpose | Create or update when |
|---|---|---|
| Source plan | Host Plan-mode interpretation of ordinary product intent; required input and provenance for `auto-plan` | Before invoking `auto-plan`; keep hash-current through build |
| Project constitution | Stable principles and non-negotiable invariants | A rule should govern most future work |
| Repository map | Minimal entry points, interfaces, commands, and verification boundaries | The relevant architecture or tooling changes |
| Feature brief/spec | Product intent, outcomes, scenarios, acceptance criteria, non-goals | A product slice is proposed or its intent changes |
| Question ledger | Unknowns, choices, human answers, provenance, expiry | The repo cannot answer a material question |
| ADR | Accepted durable architecture decision and rationale | A meaningful architecture choice is accepted |
| Markdown plan | Human-readable plan plus its one marked structured block; canonical before and during build | A spec is resolved enough to propose tasks and checks |
| Approval receipt | Named human, timestamp, and fingerprint in Markdown; not executable state | The exact draft is explicitly approved in Plan mode |
| Autonomy receipt | Invocation-scoped target, policy decisions, repository/branch identity, evidence, and plan fingerprint in Markdown | An explicit goal-driven run selects `plan`, `verified`, or `pr` |
| Compiled tasks | Deterministic dependency graph generated from the approved Markdown plan | Build activation succeeds |
| Journey oracle manifest | Fingerprinted typed journey oracles compiled from the plan-level decision | Build activation succeeds |
| Journey results | PASS/FAIL and evidence bound to the oracle manifest, head commit, and diff | Before a relevant journey reaches test or review gate |
| Delivery state | Ignored worktree-local Git active-slice state bound to the approved plan lock; never an approval artifact | Build activation and successful slice publication |
| `changes.md` | Append-only, reviewable post-build observations with exact user message, expected/actual behavior, classification, evidence, and resolution | Controlled `record-change` transition |
| Repair state | Ignored delivery mode, resume stage, class-specific attempt counters, active mechanism observation, and superseded receipt references | Controlled repair and gate transitions |
| Recovery status | Read-only active/published delivery, PR lifecycle, branch/SHA identity, ambiguity, and safe next transition | Before responding to CI, review, publication denial, or ordinary correction language |
| Operation receipt | Ignored Git-common identity, fingerprinted authority, lease, durable attempt budget, expected postcondition, and secret-free completion observation | Before and after each managed mutation or external side effect |
| Installation repair receipt | Ignored Git-common installed/target version, direction, owned-state classifications, exact path hashes, repair fingerprint, and backup location | An update discovers or repairs Boatstack-owned control drift |
| Gate receipt | Machine-local test or review transition bound to one delivery slice, base/head branches, commit, product diff, and evidence hash | A slice passes test or review |
| Test plan | Requirement-to-evidence mapping with each validation's origin, falsifiable oracle, procedure, and independence | Planning and after discovered failure modes |
| Gap ledger | Known divergence between desired and current state | Work is deferred, partial, incompatible, or intentionally absent |
| Risk/threat note | Assets, actors, trust boundaries, abuse/failure paths | Security, data, tenancy, billing, auth, or destructive paths change |
| Side-effect declaration | Affected paths, immutable external target, reversibility, failure policy, and destructive flag | A task can write outside the repository |
| Runbook | Deploy, observe, recover, and roll back | Operational behavior changes |
| Evidence ledger | Commands, results, review evidence, screenshots, CI and runtime links | Every gate |
| PR visual manifest | Machine-local scenario, source revision, screenshot hashes, capture metadata, and publication state | Relevant PR capture and publication |
| PR preview | Exact reviewer-ready title/body plus a hidden fingerprint of the committed diff and evidence | Ship gate, before opening or updating GitHub |
| Move ledger | Failure class, intervention, prediction, paired result, decision | Improving the loop itself |

## ADR boundary

An ADR is not a dump of all project context. It records one durable decision:

- status: proposed, accepted, superseded, or rejected;
- context and forces;
- decision;
- alternatives;
- consequences and risks;
- verification and supersession rule.

Unknowns stay in the question ledger. Known incomplete work stays in the gap ledger. Temporary implementation detail stays in the plan or PR.

## Gap boundary

A gap is an explicit difference between the accepted target and the current implementation. Record:

- expected state and actual state;
- impact and severity;
- reason it remains;
- owner;
- trigger or deadline for revisiting;
- affected acceptance criteria;
- whether it blocks ship.

`PASS_WITH_GAPS` is allowed only if project policy permits it and no gap is critical.

## Provenance

Every material statement should indicate whether it came from:

- the supplied host Plan-mode file;
- repository evidence;
- runtime evidence;
- a human answer;
- an accepted ADR;
- an assumption;
- an external source.

Generated artifacts include the canonical loop version and config hash. Human edits to generated adapters are drift and should be moved into project-owned context or canonical source.

A completed parent's delivery state, plan lock, and receipts remain immutable.
Post-publication observations append to its `changes.md`; the linked corrective
child owns all new approval, lock, gate, and publication evidence.

## Insight intake boundary

Each confirmed insight lives under `docs/insights/<id>/`. `capture.json` is the
immutable machine record, `insight.md` is its human-readable projection, and
`events.jsonl` is the append-only association, binding, evaluation, duplicate,
and disposition history. These files are product-intake artifacts. Every insight
mutation creates a Git diff that can move from nontechnical input to engineering
review through a pull request. No insight content lives in detached state or the
Git control directory.

## PR projection boundary

`pr.md` is a lossy review projection, not a replacement for the feature package. Its visible body contains only why, changed behavior, review order, evidence, gaps/risks, rollout, and rollback. Approval hashes, source paths, and host attribution remain in non-rendered metadata or collapsed provenance.

For managed work it lives under `.product-loop/features/<feature>/pr.md` and may claim only evidence present in the current approved package. For an existing or ad-hoc branch it lives under `.product-loop/pr-briefs/<branch>/pr.md`, uses observed branch facts, and labels missing approval or gate evidence `NOT_VERIFIED`. Both are committed with the branch. The preview file itself is excluded from the product-diff fingerprint.

Managed preview metadata also names the active delivery slice. The ignored delivery
state and gate receipts live under the current worktree's Git directory so branch
changes retain control state without blocking unrelated worktrees. They are runtime
control state, not durable product evidence; the PR links the committed evidence
ledger while the publisher rechecks the matching receipts.

## Planning boundary

`auto-plan` and `plan-gate` create or update Markdown only. `plan.md` is the canonical structured input. New schema-v3 plans require a journey-evidence decision, and schema-v3 `approval.md` binds human approval to the plan, displayed product baseline, exact branch/base/head relation, and compiled journey-manifest fingerprint. Activation repeats readiness and stores it in the immutable lock. Schema-v1/v2 receipts and active locks remain readable, but an unactivated legacy approval has no readiness authority and must be refreshed for a schema-v3 plan.

## Safety boundary

The generated host hook fragments and launchers are committed installation infrastructure. Their policy is immutable in project configuration. Cursor pre/post native, shell, and MCP events; Claude and Codex `PreToolUse`/`PostToolUse`; and Gemini `BeforeTool`/`AfterTool` project into one classifier and completion observer. The machine-local helper is ignored and restored by the installer. Safety evidence belongs in the feature evidence ledger: target identity, failure behavior, independent oracle, operational-diff scan, and the operator-only recovery boundary. A source edit is reviewable evidence, not permission to execute it.

Operation receipts live under the current worktree's Git directory at `boatstack/operations/v2`, never in Git history. (The Git-common `operations/v1` ledger is the orphaned pre-isolation layout; `doctor` prunes it.) They distinguish prepared, executing, unknown, retryable, and terminal work across turns and linked worktrees. Receipts contain hashes and bounded observations rather than commands, tool payloads, responses, credentials, or autonomous workflow intent. Terminal identities remain long enough to consume delayed duplicate events; old detail is compacted.

Installation repair receipts and backups live under Git-common `boatstack/updates/<version>` and `boatstack/repair-backups/<fingerprint>`. The checksum-verified target helper owns this recovery plane. Exact installed fragments migrate automatically; `--repair` covers only a displayed fingerprinted owned-state package. User-owned or ambiguous state is never converted into repair authority.

## PR visual evidence boundary

When `workflow.pr_visual_evidence` is enabled, the approved plan records whether screenshots are relevant and names no more than three review scenarios. PNG bytes and capability receipts live under Git-common Boatstack state; committed ledgers retain only compact metadata and hashes. PR schema v3 binds the policy, status, count, and manifest fingerprint to the preview. Screenshots are human-review evidence rather than mechanical correctness proof.

## State ownership

Every tree Boatstack manages has one declared owner, class, and partition. The
authoritative registry is `StateRegistry` in the runtime; this table mirrors it
and a conformance test holds the two together, so neither can drift silently.
Partitions: `checkout` lives in the working tree, `per-worktree` under the
worktree's own Git directory, `git-common` shared by every worktree of the
clone, `external` outside the repository (Detached Supervision).

| Name | Class | Partition | Owned by |
| --- | --- | --- | --- |
| project-config | committed-generated | checkout | init, update, export |
| source-config | committed-generated | checkout | init, migrate-config, update |
| generated-references | committed-generated | checkout | init, update, export |
| guard-hooks | committed-generated | checkout | init, update, export |
| runtime-launchers | committed-generated | checkout | init, update, export |
| generated-lock | committed-generated | checkout | init, update, export |
| planning-artifacts | committed-planning | checkout | planning-write |
| approval-receipt | committed-planning | checkout | record-approval |
| autonomy-receipt | committed-planning | checkout | record-autonomy |
| plan-lock | committed-planning | checkout | activate-plan |
| compiled-artifacts | committed-planning | checkout | activate-plan |
| pr-preview | committed-planning | checkout | ship-gate, publish-pr |
| change-ledger | committed-planning | checkout | record-change |
| discard-archive | committed-planning | checkout | discard-delivery |
| pr-briefs | committed-planning | checkout | pr-context |
| verified-boundaries | committed-planning | checkout | record-delivery-gate |
| insight-artifacts | committed-insight | checkout | insight |
| worktree-helper | checkout-runtime | checkout | init, update, hydrate-runtime, activate-worktree-runtime |
| managed-worktrees | checkout-runtime | checkout | workspace-cut, workspace-cleanup, workspace-reap |
| delivery-state | runtime-worktree | per-worktree | delivery transitions |
| operation-ledger | runtime-worktree | per-worktree | run-preflight, publishers |
| flow-logs | runtime-worktree | per-worktree | flow |
| guard-denial-ledger | runtime-worktree | per-worktree | safety-hook, ambient-safety-hook |
| runtime-slots | runtime-shared | git-common | init, update, hydrate-runtime |
| runtime-bootstrap-slots | runtime-shared | git-common | init, update, hydrate-runtime |
| mutation-receipts | runtime-shared | git-common | activate-plan, undo |
| update-previews | runtime-shared | git-common | prepare-update-pr, publish-update-pr |
| repair-receipts | runtime-shared | git-common | update |
| visual-evidence | runtime-shared | git-common | evidence verbs |
| quarantine | runtime-shared | git-common | repair-state |
| host-hook-config | host-activation | checkout | activation merge only |
| detached-registry | detached | external | attach, detach |
| detached-repositories | detached | external | attach, detach, activate |

In detached mode, `WorkspaceContext` remaps the controller bundle and every
feature package to the external repository control root. Source plans stay at
their declared repository paths. Installation, update, hydration, host-hook,
and managed-worktree paths remain repository-owned.

Direct `.product-loop` literals are frozen by a conformance inventory. Each
production file is classified as one of: canonical owner, controller bundle or
syntax, embedded installation, product-diff syntax, policy syntax, repository
workspace, or user guidance. A new unclassified literal fails the test. Runtime
controller reads and writes must use `WorkspaceContext.GeneratedRoot`,
`FeatureRoot`, or `FeatureDir`.

## Detached feature reattachment

Run `.product-loop/boatstack attach --repo . --force` to reattach an older embedded
open-feature package. Boatstack verifies the plan and approval or autonomy
fingerprints, copies the package atomically, and verifies the copied hash. The
machine result is `IMPORTED`, `UNCHANGED`, `CONFLICTING`, or `REJECTED`.
Conflicts and stale receipts fail closed. Boatstack never chooses by recency and
never deletes the embedded source package.

## Templates

Copy only the templates required for the current slice from `assets/templates/`. Do not create empty ceremony. The feature spec, question ledger, test plan, gap ledger, and evidence ledger are the usual minimum for material product work.
