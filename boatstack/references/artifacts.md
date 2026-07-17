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
| Test plan | Requirement-to-evidence mapping with each validation's origin, falsifiable oracle, procedure, and independence | Planning and after discovered failure modes |
| Gap ledger | Known divergence between desired and current state | Work is deferred, partial, incompatible, or intentionally absent |
| Risk/threat note | Assets, actors, trust boundaries, abuse/failure paths | Security, data, tenancy, billing, auth, or destructive paths change |
| Runbook | Deploy, observe, recover, and roll back | Operational behavior changes |
| Evidence ledger | Commands, results, review evidence, screenshots, CI and runtime links | Every gate |
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

## PR projection boundary

`pr.md` is a lossy review projection, not a replacement for the feature package. Its visible body contains only why, changed behavior, review order, evidence, gaps/risks, rollout, and rollback. Approval hashes, source paths, and host attribution remain in non-rendered metadata or collapsed provenance.

For managed work it lives under `.product-loop/features/<feature>/pr.md` and may claim only evidence present in the current approved package. For an existing or ad-hoc branch it lives under `.product-loop/pr-briefs/<branch>/pr.md`, uses observed branch facts, and labels missing approval or gate evidence `NOT_VERIFIED`. Both are committed with the branch. The preview file itself is excluded from the product-diff fingerprint.

## Planning boundary

`auto-plan` and `plan-gate` create or update Markdown only. `plan.md` is the canonical structured input and `approval.md` is the human-approval receipt. Compiled JSON and `plan.lock.json` begin only at `build` activation, after the receipt is verified. This keeps planning compatible with hosts that intentionally restrict Plan mode to documents.

## Templates

Copy only the templates required for the current slice from `assets/templates/`. Do not create empty ceremony. The feature spec, question ledger, test plan, gap ledger, and evidence ledger are the usual minimum for material product work.
