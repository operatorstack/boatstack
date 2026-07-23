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
| Compiled tasks | Deterministic dependency graph generated from the approved Markdown plan | Build activation succeeds |
| Delivery state | Ignored worktree-local Git active-slice state bound to the approved plan lock; never an approval artifact | Build activation and successful slice publication |
| `changes.md` | Append-only, reviewable post-build observations with exact user message, expected/actual behavior, classification, evidence, and resolution | Controlled `record-change` transition |
| Repair state | Ignored delivery mode, resume stage, active observation, attempt count, and superseded receipt references | Controlled repair and gate transitions |
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

## PR projection boundary

`pr.md` is a lossy review projection, not a replacement for the feature package. Its visible body contains only why, changed behavior, review order, evidence, gaps/risks, rollout, and rollback. Approval hashes, source paths, and host attribution remain in non-rendered metadata or collapsed provenance.

For managed work it lives under `.product-loop/features/<feature>/pr.md` and may claim only evidence present in the current approved package. For an existing or ad-hoc branch it lives under `.product-loop/pr-briefs/<branch>/pr.md`, uses observed branch facts, and labels missing approval or gate evidence `NOT_VERIFIED`. Both are committed with the branch. The preview file itself is excluded from the product-diff fingerprint.

Managed preview metadata also names the active delivery slice. The ignored delivery
state and gate receipts live under the current worktree's Git directory so branch
changes retain control state without blocking unrelated worktrees. They are runtime
control state, not durable product evidence; the PR links the committed evidence
ledger while the publisher rechecks the matching receipts.

## Planning boundary

`auto-plan` and `plan-gate` create or update Markdown only. `plan.md` is the canonical structured input and schema-v2 `approval.md` binds human approval to both the plan fingerprint and the displayed pre-activation product-diff baseline. Compiled JSON and `plan.lock.json` begin only at `build` activation, after the receipt and unchanged baseline are verified. Schema-v1 receipts remain compatible only when that baseline is clean. This keeps planning compatible with hosts that intentionally restrict Plan mode to documents while preserving edits that predated managed authority.

## Safety boundary

The generated host hook fragments and launchers are committed installation infrastructure. Their policy is immutable in project configuration. Cursor pre/post native, shell, and MCP events; Claude and Codex `PreToolUse`/`PostToolUse`; and Gemini `BeforeTool`/`AfterTool` project into one classifier and completion observer. The machine-local helper is ignored and restored by the installer. Safety evidence belongs in the feature evidence ledger: target identity, failure behavior, independent oracle, operational-diff scan, and the operator-only recovery boundary. A source edit is reviewable evidence, not permission to execute it.

Operation receipts live under Git-common `boatstack/operations/v1`, never in Git history. They distinguish prepared, executing, unknown, retryable, and terminal work across turns and linked worktrees. Receipts contain hashes and bounded observations rather than commands, tool payloads, responses, credentials, or autonomous workflow intent. Terminal identities remain long enough to consume delayed duplicate events; old detail is compacted.

Installation repair receipts and backups live under Git-common `boatstack/updates/<version>` and `boatstack/repair-backups/<fingerprint>`. The checksum-verified target helper owns this recovery plane. Exact installed fragments migrate automatically; `--repair` covers only a displayed fingerprinted owned-state package. User-owned or ambiguous state is never converted into repair authority.

## PR visual evidence boundary

When `workflow.pr_visual_evidence` is enabled, the approved plan records whether screenshots are relevant and names no more than three review scenarios. PNG bytes and capability receipts live under Git-common Boatstack state; committed ledgers retain only compact metadata and hashes. PR schema v3 binds the policy, status, count, and manifest fingerprint to the preview. Screenshots are human-review evidence rather than mechanical correctness proof.

## Templates

Copy only the templates required for the current slice from `assets/templates/`. Do not create empty ceremony. The feature spec, question ledger, test plan, gap ledger, and evidence ledger are the usual minimum for material product work.
