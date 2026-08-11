# Boatstack V2 authoritative delivery kernel

Status: normative implementation specification
Base revision: `c5b5e10cdcf4d97b645d705cb164e762acf93ff1` (`origin/main`, including PR #185)
Rewrite branch: `rewrite/v2-delivery-kernel`
Scope: one flag-day rewrite and one final pull request; no merge is authorized by this document

> Boatstack V2 is a flag-day replacement. Existing machine-local state may be
> discarded and regenerated. No V1 runtime remains after cutover.

This document is the source of truth for the V2 implementation. If code and this
document disagree, the discrepancy is a release blocker: either the code must be
corrected or this document must be deliberately amended with matching tests.
The [replacement closure report](boatstack-v2-closure-report.md) binds its frozen
V1 counts to the implemented V2 evidence.

## ZCA projection and decisions

The existing implementation is projected into two minimal, jointly shipped
slices. They are logical ownership boundaries, not rollout phases.

| Slice | Domain | Structure | Goal | Operator | Immediate value |
| --- | --- | --- | --- | --- | --- |
| 1. Authoritative kernel | Repository-local delivery control | One evidence-backed composite snapshot and one transition catalog | Every managed state has a safe path to progress, recovery, authority frontier, or terminal | Observe, resolve, admit, execute, verify, record, recover | One executable delivery law replaces distributed state and authority reconstruction |
| 2. Product surfaces | Shipped CLI, hooks, SDK/MCP, hosts, and renderers | One adapter protocol projected from kernel decisions and prescriptions | Every consumer observes and requests the same semantics | Decode, invoke, render | Hosts stop acting as independent controllers while useful workflows remain available |

Canonical form for slice 1: one domain, the `Snapshot` schema, the configured
`Goal`, and the `Engine.Apply` operator. Canonical form for slice 2: one domain,
the `SurfaceRequest`/`SurfaceResponse` schema, the same goal, and the adapter
projection operator.

Known constraints are the flag-day cutover, explicit effectful identity,
repository-owned policy and durable evidence, fail-closed ambiguity, inertness
outside managed scope, two logical slices in one PR, and no V1 authority after
cutover. Unknown constraints to close with code and fixtures are the complete
reader/writer/surface inventory, provider settlement behavior after an uncertain
external request, platform-specific atomic filesystem behavior, and the exact
set of SDK/MCP hosts present at cutover. Optimizer weights, an Observatory
product integration, and UI presentation are non-critical to this rewrite.

The implementation must answer these technical questions without asking for a
product decision: which sites control state, which resources each effect owns,
which external outcomes can be proved, and which platform primitive provides
atomic replacement. A user decision is required only if a new transition would
change who may authorize an effect or what counts as a delivery terminal.

## 1. Product contract

Boatstack is a repository-local supervisory controller for software delivery by
humans and coding agents. The agent writes software. Boatstack deterministically
observes the delivery plant, retains explicit identity, establishes engagement,
resolves legal managed events, binds authority, owns transactional effects,
verifies postconditions, records receipts, recovers from interruption, and
establishes whether the configured goal is terminal.

The repository owns policy and committed evidence. The kernel owns delivery
decisions. CLI, hooks, Cursor, Codex, Claude Code, Gemini CLI, SDK, MCP, and future
hosts are adapters. They never infer lifecycle, identity, authority, effect
permission, recovery, or completion independently.

Observable behavior is classified only as follows:

- **PRESERVE:** installation, initialization, update, doctor, embedded/detached/
  hybrid operation, deterministic runtime hydration, explicit repository and
  worktree identity, planning and approval, autonomy, workspaces, build/test/
  review/change/journey gates, goal-driven run, interruption and resume,
  amendments, invalid-plan recovery, publication and correction, merged
  terminals, visual evidence, safety hooks, configuration, cleanup/reap,
  abandonment, portable host guidance, evidence, receipts, and passive
  retrospectives.
- **NORMALIZE:** every preserved behavior crosses the V2 observation, resolution,
  admission, effect, verification, and receipt contracts. Commands and output
  text may change. Machine state, schemas, file layouts, Go APIs, and adapter
  protocols may change without compatibility shims. Visual capture is the
  `evidence.visual.attach` transition. Historical insight extraction is the
  read-only retrospective projection.
- **REMOVE:** ambient engagement, path-only effect identity, first-match alias
  selection, inferred authority, duplicated host logic, unverified success,
  state-repairing reads, V1 state migration, runtime fallback, independent
  insight/capture writers, and every other accidental or unsafe V1 behavior.

There is deliberately no backward-compatibility promise. Historical behavior is
evidence about product value and failure classes, not a language or API that V2
must refine. Existing repositories may be reinstalled or reattached. Committed
plans, specifications, approvals, evidence, PR briefs, configuration, and policy
are read as product inputs when they satisfy V2 schemas; accidental V1 machine
state is discarded.

## 2. Historical failure synthesis

The history through PR #185 converges on one structural diagnosis:

> Boatstack V1 distributed transition authority across independently reconstructed,
> control-insufficient projections of lifecycle, engagement, workspace,
> publication, configuration, runtime, and host state.

Local repairs repeatedly added a distinction or precedence rule to one resolver
while another resolver, renderer, writer, or host retained a different model.
The V2 class-eliminating change is not another precedence rule. It is one runtime
snapshot, one transition registry, one supervisor, one admission path, one effect
boundary, and one independently verified receipt protocol.

The detailed episode inventory and fixture mapping are in Appendix A. The
structural classes carried into V2 are:

- control-insufficient state projection;
- split transition, identity, configuration, and completion authority;
- non-injective repository/worktree reverse lookup;
- ambient engagement and saved-plan leakage;
- workspace, publication, Git ancestry, and goal-terminal conflation;
- stale or self-invalidating runtime/configuration mutation;
- non-atomic multi-resource and externally uncertain effects;
- surface, shell, and host prescription divergence;
- missing recovery coreachability and incomplete event/writer inventories.

The old implementation is permitted only as a fixture source and historical
oracle while developing this branch. It is not linked into the final runtime.

## 3. Formal discrete-event system model

Let the plant state be:

```text
x_t = (
  invocation identity,
  engagement,
  repository and worktree state,
  delivery state,
  plan and approval state,
  configuration authority,
  runtime state,
  verification state,
  publication and CI state,
  recovery state,
  active transaction state
)
```

The read-only observer produces `o_t = H(x_t, evidence_t)`. Canonicalization
produces the control-sufficient `z_t = P(o_t)`. Events are partitioned into
controllable Boatstack events `Sigma_c` and uncontrollable observed plant events
`Sigma_u`. For goal `g` and authority set `a`, the supervisor returns the
admissible set `S(z_t, g, a) subseteq Sigma_c`; deterministic policy selects at
most one prescribed event. Execution is accepted only as:

```text
z_t -- prescribe(e) --> admission
    -- execute(e) --> unverified plant
    -- observe --> o_t+1
    -- verify(target(e)) --> z_t+1 + immutable receipt
```

The protocol phases are `DORMANT`, `OBSERVED`, `PRESCRIBED`, `ADMITTED`,
`EXECUTING_LOCAL`, `EXECUTING_EXTERNAL`, `VERIFYING`, `ACTIVE`, `RECOVERY`,
`UNRESOLVED`, `FRONTIER`, `TERMINAL`, and `ABANDONED`. These phases describe the
kernel protocol; orthogonal state facets below describe the plant.

Marked outcomes are `FRONTIER`, `TERMINAL`, and `ABANDONED`. `RECOVERY` and
`UNRESOLVED` must have bounded registered paths to a marked outcome or back to
`ACTIVE`. Forbidden counterfactual states are `UNADMITTED_EFFECT` and
`ACCEPTED_MIXED_EPOCH`.

Normative properties within declared managed scope:

1. Safety: forbidden states and events are unreachable.
2. Inertness: ordinary repository work outside active managed scope is not
   blocked or mutated.
3. Coreachability: every reachable nonterminal managed state can reach the goal,
   a typed recovery path, an authority frontier, or safe abandonment/refusal.
4. Projection fidelity: `P(x1) = P(x2)` implies equal admissible controllable
   event sets. A distinguishing legal action requires a distinguishing facet.
5. Determinism: identical snapshot, goal, authority, and request yield identical
   decisions and typed prescriptions.
6. Resource preservation: missing, stale, ambiguous, conflicting, or unknown
   evidence never grants delete, publish, overwrite, or advance authority.
7. Event completeness: every controlling reader, writer, resolver, renderer,
   surface, and effect is classified by the registry or a proved noncontrolling
   exclusion.

The runtime transition catalog is the model used for reachability. Tests derive
the graph from executable registry entries; no manually mirrored graph exists.

## 4. State and identity model

`Snapshot` is an immutable typed composite. It is never represented by one flat
enum and controlling multi-state facts are never booleans.

The executable catalog declares exactly 17 controlling facets:

| Facet | Required distinctions |
| --- | --- |
| Phase | dormant, observed, prescribed, admitted, local/external execution, verifying, active, recovery, unresolved, frontier, terminal, abandoned |
| Topology | embedded, detached, hybrid |
| Engagement | dormant, command-scoped, active, stale, conflicting, invalid |
| Delivery | uninitialized, planning, approved, active slice, gates satisfied, published, amendment, invalid, recovery, discarded, terminal |
| Workspace | absent, cut, active, published, landed, abandoned, attention-required |
| Plan | absent, draft, valid, approved, locked, stale, invalid, amendment-required |
| Configuration | verified, stale, divergent, conflicting, unsupported |
| Configuration policy | plan-approval authority, visual-evidence requirement, independent-review policy plus derived high-risk-change fact, external-effect authority, enabled hosts |
| Runtime | absent, hydrating, verified, stale, invalid, conflicting, wrong source/version, partially published |
| Publication | none, candidate, open, closed-unmerged, merged, unavailable, conflicting, published-not-landed |
| Verification | unverified, current, stale, failed, unresolved |
| Recovery | none, resumable, rollback, compensation, reconcile, escalated |
| Transaction | none, staged, local-applied, external-uncertain, verifying, committed, compensating |
| Recovery info | exact transaction, cause, source phase, permitted exits, budget, resumption target |
| Transaction info | exact transition, status, resource digests, external possibility |
| Terminal | nonterminal, established, stale, unknown, conflicting |
| Goal | target kind, subject delivery, evidence predicate, frontier policy |

Every controlling fact is a `Fact[T]` containing value/status, evidence source,
revision or fingerprint, observation time when freshness matters, and explicit
unknown/conflict information. `unknown`, `absent`, `false`, `stale`, `ambiguous`,
and `conflicting` are distinct values.

Every effectful entry point requires an `InvocationContext` carrying repository,
Git-common, worktree, branch/ref, controller, topology, invoking path, exact
executing-runtime path/fingerprint, host identity, and correlation ID. Effectful identity is never reverse-derived
from a controller path, plan path, generated file, branch name, translated CWD,
or first registry match. Read-only discovery may return candidates and ambiguity;
mutation refuses ambiguity before acquiring an effect lock.

## 5. Observation model

`plant.Observer.Observe(ctx, ObservationRequest)` is the only read boundary that
creates snapshots. The request carries the exact invocation. Only the engine's
immediate post-effect verification may additionally exclude the current
admission's pending journal; correlation IDs never hide interrupted work.
It reads Git, repository/worktree layout, the strict repository configuration,
the selected runtime bytes, durable delivery state, detached binding, and active
transaction/recovery journals. Provider observation is an explicit registered
`publication.observe` or `publication.reconcile` effect; the resulting durable
publication fact is then read through this observer.

Observation never writes, repairs, hydrates, locks for mutation, or chooses a
transition. Each provider returns typed known, absent, unknown, stale, and
conflicting facts with evidence. External provider failure remains `unknown` and
is not collapsed to false or complete.

`model.Canonicalize(observation)` validates cross-facet reachability constraints
and fingerprints canonical bytes. Workspace status, next status, cleanup,
activation, safety, publication, and adapters consume this snapshot rather than
recomputing lifecycle subsets.

The snapshot fingerprint covers every fact used by source predicates, authority,
admission, effects, postconditions, and goal termination. Display-only facts are
explicitly excluded and may not become controlling without a schema change.

## 6. Event vocabulary

Events have stable semantic IDs independent of CLI verbs or Go function names.
They are one of:

- `owned-local` (`Sigma_c`): Boatstack can perform a local transactional effect;
- `owned-external` (`Sigma_c`): Boatstack can request an external effect under a
  preview/authority/idempotency/settlement protocol;
- `authority` (`Sigma_c`): a human, policy, or autonomy receipt changes the
  admitted set;
- `observed-external` (`Sigma_u`): the plant changed outside Boatstack;
- `recovery` (`Sigma_c`): a bounded resume, rollback, reconcile, escalation, or
  abandonment event. External effects that have no proven inverse reconcile or
  escalate; V2 does not register a generic fake compensation;
- `query`: a read-only surface operation that cannot alter kernel state and is
  not counted as a managed transition.

Uncontrollable events are incorporated only by re-observation. A host may report
an observation trigger but may not assert the resulting fact. Queries such as
status, next-status, doctor, and event streaming return projections and never
gain event authority merely because they are commands.

## 7. Transition registry

The initial V2 catalog contains **61 semantic events**. This count is generated
from code and must remain synchronized with this table.

| Family | Count | Required IDs |
| --- | ---: | --- |
| Invocation and engagement | 6 | `engagement.begin`, `engagement.renew`, `engagement.release`, `invocation.rebind`, `repository.attach`, `repository.detach` |
| Installation, runtime, configuration | 8 | `runtime.hydrate`, `runtime.replace`, `runtime.reconcile`, `configuration.initialize`, `configuration.mutate`, `configuration.reconcile`, `installation.initialize`, `installation.update` |
| Goal and plan | 9 | `goal.configure`, `plan.create`, `plan.validate`, `plan.approve`, `plan.activate`, `plan.amend`, `plan.approve-amendment`, `plan.invalidate`, `plan.abandon` |
| Workspace | 8 | `workspace.cut`, `workspace.sync`, `workspace.activate`, `workspace.publish`, `workspace.cleanup`, `workspace.reap`, `workspace.abandon`, `workspace.reconcile` |
| Delivery gates and evidence | 8 | `gate.build.record`, `gate.test.record`, `gate.review.record`, `gate.change.record`, `gate.journey.record`, `evidence.visual.attach`, `evidence.approval.revoke`, `delivery.slice.advance` |
| Publication | 6 | `publication.preview`, `publication.execute`, `publication.observe`, `publication.reconcile`, `publication.correct`, `publication.abandon` |
| Recovery | 3 | `recovery.resume`, `recovery.rollback`, `recovery.escalate` |
| Observed external | 13 | `external.files-changed`, `external.head-changed`, `external.branch-changed`, `external.runtime-disappeared`, `external.configuration-drifted`, `external.lease-expired`, `external.host-interrupted`, `external.ci-completed`, `external.pr-opened`, `external.pr-updated`, `external.pr-closed`, `external.pr-merged`, `external.provider-unavailable` |

Every `Transition` declaration contains: ID and schema version; source predicate;
event class and controllability; goal relevance; required identity, authority,
evidence, and fingerprints; admission predicate; owned resources; local/external
effects; idempotency binding; typed prescription; expected target predicate;
independent verifier; interruption points; rollback/compensation; reversibility;
terminal effect; recovery transition; privacy and telemetry classifications; and
consumer-neutral cost class.

The registry enforces unique IDs, complete effect ownership, valid recovery
targets, verifier presence, terminal consistency, prescription renderability,
and reachability. CLI verbs and handlers map to IDs; they are not IDs. POSIX,
PowerShell, SDK/MCP, and host instructions are renderings of the same typed
prescription. The registry is executable runtime authority, not a shadow model.

The checked [catalog table](boatstack-v2-transition-catalog.md) and
[Mermaid graph](boatstack-v2-transition-catalog.mmd) are deterministic
projections of this registry. Golden tests reject either artifact when it drifts.

## 8. Supervisory control law

`supervisor.Resolve(snapshot, goal, authority, optionalObservedEvent)` is pure and
deterministic. It evaluates the executable registry and returns exactly one:

- `PRESCRIBED`: one exact next transition and prescription;
- `TERMINAL`: goal predicate established by current terminal evidence;
- `FRONTIER`: a genuine human/reasoning authority decision is required;
- `BLOCKED`: a known recoverable condition plus its registered recovery event;
- `REFUSED`: the request is outside admissible managed behavior;
- `UNRESOLVED`: evidence is insufficient or contradictory.

Precedence is invariant, not surface policy: recovery outranks ordinary slice
position; configured terminal outranks publication convenience; an active
managed delivery outranks weak ancestry/provider projections; durable
publication evidence is required before ancestry can establish landing;
repository presence is not engagement; a saved plan is not active authority.

Resolution never fabricates progress. If several controllable events remain
equally admissible after declared deterministic priority, the answer is
`FRONTIER` or `UNRESOLVED`, never map-order selection or first-match behavior.

## 9. Admission and authority model

Knowledge, precondition evidence, authority, and proof of effect are four
separate objects. `Admission` binds the exact transition ID/version, snapshot
fingerprint, invocation identity, goal and plan lock, observation/configuration
fingerprints, source revision, branch/worktree, authority receipt, provider
preview, idempotency key, and expiry.

`admission.Admit` re-observes or compares current controlling fingerprints before
any writer runs. A stale prescription fails without mutation. Human approval,
autonomy, repository policy, and provider authority are typed, scoped, expiring,
and non-substitutable unless the transition explicitly allows alternatives.

Hooks, CLI, renderers, SDK/MCP, and hosts may carry explicit caller attestations,
but cannot derive repository authority, weaken admission, cache authority past
expiry, or reinterpret it. Human and provider receipts are command-scoped audit
attestations, not operating-system authentication; the external provider still
must settle the requested operation. Repository-policy authority is derived only
inside the facade from the exact canonical configuration evidence. Ordinary work
outside active scope remains inert. Managed work fails closed when identity,
evidence, or authority is missing, stale, ambiguous, or conflicting.

## 10. Effect and transaction model

Every managed writer implements a registry-owned effect port and is unreachable
without a valid `Admission`. Status, renderers, hooks, parsing, observation,
validation, path resolution, and safety classification are read-only.

Local transitions follow one journaled protocol:

1. validate admission against the exact source snapshot;
2. acquire a partition-scoped lock keyed by repository/worktree/resources;
3. capture exact prior bytes and external preconditions;
4. stage all local writes;
5. verify staged representations;
6. install effects in declared order;
7. install the authoritative binding/state last;
8. re-observe independently;
9. verify the target predicate;
10. append the immutable receipt and commit journal;
11. release the lock.

Failure restores exact prior bytes where reversible. A mixed epoch is never an
accepted snapshot. An irreversible or unknown external outcome produces a typed
reconciliation state and preserves local resources.

Clone-family journals, locks, receipts, and process events use a fixed external
flow root keyed by repository ID and Git-common ID. Worktree state remains
partitioned by exact worktree ID. `workspace.cut` stages a parked source state
and an authoritative destination state, then verifies from the destination.
Cleanup verifies the preserved source checkout, removes the destination from a
neutral directory, and transfers terminal state back to that source.

External effects use `preview -> authority -> execute -> observe -> reconcile`.
Request acceptance and effect settlement are distinct. Idempotency binds exact
request bytes and provider identity. An unknown outcome is not blindly retried;
the kernel observes by idempotency/correlation key or enters attention.

No successful command may invalidate the evidence needed to verify its own
target state. Telemetry and ancillary services are never part of commit success.

## 11. Verification and receipt model

The effect implementation cannot certify itself. A transition's verifier reads a
fresh observation and evaluates the catalog's target predicate. Success requires
both effect completion and postcondition truth. Otherwise the engine enters the
declared rollback, compensation, or recovery path and returns non-success.

`TransitionReceipt` is immutable and content-addressed. It binds schema, flow
and sequence IDs, transition ID/version, admission and goal IDs, source and
target fingerprints, authority classes, idempotency key, timestamps/duration,
outcome, postcondition verifier, recovery/terminal classification, and a
privacy-safe failure class. The admission ID transitively binds invocation,
authority receipts, parameters, and expiry. A receipt never embeds arbitrary
output, source, prompts, or secrets.

Receipts are the only accepted evidence that a managed transition occurred.
Plan approvals, publication settlement, and terminal claims point to exact
receipts. Idempotency replay validates the stored receipt identity, returns it
with a fresh current snapshot, and never repeats the effect.

Build, test, review, change, and journey gates copy and independently re-read a
strict schema-1 passed-evidence document whose bytes, gate, producer, completion
time, and source revision are bound by admission. The admission also carries the
observer-derived product worktree fingerprint. Kernel-generated plans,
approvals, evidence, and publication previews are excluded from that product
fingerprint so recording proof cannot invalidate itself; configuration remains
included. Build and test additionally execute the exact configured command
inside the admitted effect boundary, reject commands classified as destructive
or managed bypasses, persist no command output, and install no gate evidence on
a nonzero exit.

## 12. Recovery model

Recovery is a normal registry family. Every transition declares interruption
points, recovery transition, reversibility, authority, and owned resources. The
journal records the exact interrupted transaction and resources; observation
derives its bounded resume, rollback, reconcile, compensation, or escalation
set and resumption target.

On startup and before a new mutation, observation inspects transaction journals
and external correlation keys. `RECOVERY` outranks slice and publication status.
A recovery resolver may prescribe only the transition declared by the interrupted
effect or a safe escalation/abandonment path.

No damaged artifact grants authority. Unknown or contradictory state preserves
workspaces, unpublished commits, evidence, and external uncertainty. Recovery
decisions name the controlling reason and registered recovery or termination
path. Repair budgets are monotonic and bounded; exhaustion produces `FRONTIER`
or safe abandonment rather than an infinite retry loop.

## 13. Goal and terminal semantics

`Goal` is configured before managed execution and identifies the subject delivery
and one terminal predicate: approved plan, verified implementation, open/updated
PR, merged delivery, or safely abandoned delivery. It also declares required
evidence freshness and whether a frontier is acceptable as a stopped outcome.

Terminal is evidence, not a local phase label. Examples:

- approved-plan terminal requires the exact plan lock and current approval;
- verified terminal requires declared gates against the current source revision;
- PR terminal requires durable provider evidence for the current publication;
- merged terminal requires durable merged publication evidence plus the configured
  repository/workspace relation;
- abandonment requires explicit authority and a receipt proving resource policy.

Local green tests, ancestry equality, workspace cleanup eligibility, saved plan
presence, or an agent's completion assertion cannot establish a goal. External
unknown never establishes terminal. Once terminal, unrelated local projections
cannot resume the flow without a new goal or registered correction transition.

## 14. Package and dependency architecture

All V2 implementation lives below `boatstack/`; the top-level `boatstack` package
is a product facade with no independent durable state or decision law.
Dependencies point downward in this table and are acyclic.

| Package | Owns | Public boundary and verifier | Allowed dependencies | Forbidden dependencies |
| --- | --- | --- | --- | --- |
| `internal/kernel/model` | typed facts, identity, snapshot, goal, fingerprints | constructors/canonical encoding; schema and invariant tests | standard library | plant, effects, surfaces, facade |
| `internal/kernel/catalog` | 61 transition declarations and catalog invariants | read-only registry; uniqueness/completeness/reachability verifier | model | effects implementations, surfaces |
| `internal/kernel/supervisor` | admissible-set and deterministic outcome law | pure `Resolve`; exhaustive reachable-state/property tests | model, catalog | I/O, effects, surfaces |
| `internal/kernel/protocol` | prescriptions, admission, receipts, recovery records | typed codecs and content identity verifier | model, catalog | concrete I/O and surfaces |
| `internal/kernel/durable` | strict machine-state and detached-binding codecs | canonical encode/decode and invariant validation | model, catalog | observation, effects, surfaces |
| `internal/kernel/ports` | observer, clock, lock, journal, local/external effect ports | compile-time narrow interfaces and fakes | model, protocol | concrete adapters |
| `internal/kernel/reducer` | the sole durable lifecycle reduction for admitted controllable events | `Apply` plus exhaustive catalog coverage tests | model, catalog, durable, protocol | I/O, plant, surfaces |
| `internal/kernel/engine` | observe-resolve-admit-execute-reobserve-verify-record orchestration | `Resolve`, `Apply`, `Recover`; protocol/conformance tests | model, catalog, supervisor, protocol, ports | concrete surfaces and host logic |
| `internal/plant` | Git/worktree identity, layout, configuration, runtime, durable-state and journal observation | one read-only composite observer; fact/fingerprint fixtures | model, protocol, ports, durable codecs | engine decisions, mutating effects, surfaces |
| `internal/effects` | transactions, local/external effect drivers and recovery | port implementations; fault-injection/postcondition tests | model, catalog, durable, protocol, ports, reducer, shared supervisor command classifier | surfaces and independent lifecycle reduction |
| `internal/surfaces` | request decoding and decision/prescription rendering | CLI/hook/host/SDK/MCP adapter protocol; golden parity tests | model, protocol, engine facade interfaces | plant/effect implementations, lifecycle logic |
| top-level `boatstack` | product construction and stable V2 facade | dependency injection and public operations; end-to-end tests | engine, plant, effects, surfaces | independent durable state or alternate decisions |
| `cmd/boatstack-helper` | process startup and command parsing | parse -> facade request -> render; command tests | top-level facade/surfaces | direct plant writes or workflow decisions |
| `sdk` | public Go aliases and client | schema-2 request/response and one facade delegate | top-level facade and public aliases | internal decision or effect implementations |
| `analysis` | passive retrospective API | bounded deterministic report | `internal/retromine` | lifecycle decisions or managed writes |

Pure deterministic helpers may be moved or reused. Package creation is justified
only by owned state, invariant, plant interface, effect boundary, or surface
projection. The kernel never imports CLI/hosts; observer never imports writers;
renderers never import effects; adapters never decide lifecycle; effect packages
cannot bypass admission; test helpers cannot become production authorities.

## 15. CLI, hook, SDK, MCP, and host-adapter contracts

All surfaces use the same versioned protocol:

```text
SurfaceRequest {
  schema_version, operation(resolve|apply|recover|doctor|catalog|events|guard),
  repository, host, correlation_id, flow_id?, goal?, transition_id?,
  authority?, parameters?, idempotency_key?, command?
}

SurfaceResponse {
  schema_version, operation, goal?, snapshot?, decision?, admission?, receipt?,
  replayed?, catalog?, events?, doctor?, guard?, error?
}
```

The CLI maps verbs to queries or semantic transition IDs and invokes the facade.
`cmd/boatstack-helper` performs parsing and dispatch only. Hooks make one bounded
query/admission request and fail according to the returned typed decision; they
never inspect state files to reconstruct policy.

SDK and MCP expose the protocol, not internal Go packages. The facade resolves
explicit repository/worktree and executing-runtime identity before observation;
hosts supply the repository, host, correlation, goal, transition, authority, and
typed parameters. Cursor, Codex, Claude, Gemini, CLI, and MCP prescriptions are
projections of one command AST plus host capability data. Host capability can
affect rendering, never admissibility or target semantics.

POSIX, PowerShell, and supported Git Bash prescriptions are semantic projections
of one command AST. Golden parity tests compare normalized operations, resources,
authority prompts, and postconditions rather than fragile whitespace.

Status, next-status, doctor, catalog, guard, and event export are read-only
queries. Retrospective analysis is passive. Visual evidence enters lifecycle
state only through `evidence.visual.attach`; no independent insight or capture
writer remains.

## 16. Process telemetry contract

Receipts are the factual source. The facade exposes a passive JSONL reader,
`boatstack events [--follow] --format jsonl`, over committed receipt projections.
Telemetry is consumer-neutral and privacy-safe.

Allowlisted fields are schema version, flow ID, sequence, timestamp, goal ID,
transition ID, source/target fingerprints, outcome, duration, recovery and
authority classifications, terminal status, and controlled failure class.
Prompts, reasoning, source code, diffs, arbitrary command output, secrets,
environment variables, and user documents are prohibited.

Telemetry read/write failure cannot block, admit, mutate, recover, or change a
transition. `J_flow`, `J_cost`, summaries, and regret are downstream projections
of receipts. The kernel contains no optimizer weights and this rewrite does not
build Observatory.

## 17. Test and formal-property strategy

Tests exercise the runtime catalog, supervisor, engine, and concrete ports. The
registry generates the reachable graph, event inventory, diagrams, surface
prescriptions, and completeness expectations. Static source inventory classifies
every controlling reader, resolver, renderer, surface, and managed writer as one
registry relation or an explicit noncontrolling exclusion.

Required properties are: safety; inertness/nonblockingness outside scope;
coreachability inside scope; projection fidelity; deterministic resolution;
explicit uncertainty; identity fidelity; event and writer completeness; consumer
parity; postcondition fidelity; interruption safety; idempotency; bounded
recovery; terminal correctness; preservation under ambiguity; no
self-invalidating success; no host decisions; no lifecycle decisions outside the
kernel; and no path-only effect identity.

Reachable-state generation avoids the full facet Cartesian product. Dangerous
compositions receive exhaustive fixtures; remaining independent dimensions use
pairwise generation across topology, workspace/Git relation, engagement,
publication, delivery, authority, configuration, runtime, host, shell, and every
transaction interruption boundary. External tests cover failure before request,
unknown after request, settlement before receipt, and restart reconciliation.

### Locus preimplementation disposition

All formal claims below concern assumed design models until implementation binds
the catalog to code. They are theorem-only or advisory, not live-system proof.

| Claim/operator | Result | Claim level | Remaining obligation/disposition |
| --- | --- | --- | --- |
| `practice.root-cause` | distributed transition authority over control-insufficient projections; result `res-b027d5...` | advisory | close with source inventory and historical fixtures |
| boundary conformance | one exact admitted transition gates every managed effect; seven conformance classes | advisory | bind every surface and writer |
| `verification.safety-reachability` | `UNADMITTED_EFFECT` and `ACCEPTED_MIXED_EPOCH` unreachable in guarded model | theorem-only | event completeness and code fidelity |
| `verification.guard-essentiality` | exact-admission guard is essential; removing it yields `DORMANT -> OBSERVED -> PRESCRIBED -> UNADMITTED_EFFECT` | theorem-only | implementation mutation test |
| `control.nonblockingness` | all 13 live protocol states reachable and coreachable; no blocking state | theorem-only | event completeness |
| `control.supervisory-rw` | full-observation internal model controllable | theorem-only | bind internal events to catalog |
| `control.diagnosability` | partial surface projection diagnosable | theorem-only | consumer parity fixtures |
| `control.supervisory-rw` on partial observation | refused because unobservable events make that operator inapplicable | correct refusal | diagnosability is the applicable surface claim |
| `verification.conservative-feature-extension` | refused: `intentional-redesign` | correct refusal | none; V2 has no compatibility obligation |
| `verification.trace-refinement` | corrected abstract protocol refines a minimal control envelope | non-normative theorem-only | not a V2 release gate or V1 compatibility claim |

Derivation `drv-bbc6258499be4e1739a9d344f1d211682476da18be46c4bcee80227ed55f7d82`
has current claim `theorem-only`. The explicit `verified` frontier terminates as
`work-remaining`; rank 1 is
`discharge-obligation:control.nonblockingness:event-completeness`. V2 therefore
cannot claim verified liveness until the real reader/writer/event/surface
inventory is bound and accepted.

Capability analysis records three separate dispositions without modifying Locus:

- `verification.event-surface-completeness`: extend verifier coverage over the
  source-generated runtime catalog (advisory admission `adm-708d...`);
- `control.projection-fidelity`: a genuinely distinct finite-state operator is
  warranted because existing safety/refinement operators do not compare action
  equivalence classes (advisory admission `adm-d641...`);
- `verification.failure-class-elimination`: compose root cause, safety, guard
  essentiality, and non-normative control-envelope refinement; no primitive is
  needed (advisory admission `adm-6770...`).

### Locus postimplementation disposition

The executable registry now deterministically generates the checked
[safety model](boatstack-v2-locus-safety.json) and
[liveness model](boatstack-v2-locus-liveness.json). Both contain exactly the 61
runtime events. The liveness abstraction expands the declared phase predicates
to 363 inferred stable-phase edges over eight reachable phases; the safety
model adds one guarded counterfactual edge and `UNADMITTED_EFFECT` state.
Repository and Go tests reject byte drift or an alphabet mismatch.

Observed Locus runs over those generated artifacts produced:

| Claim/operator | Postimplementation result | Disposition |
| --- | --- | --- |
| `verification.safety-reachability` | `UNADMITTED_EFFECT` is unreachable; result `res-e90ff62f70169697c81e851f44fc4f423a0c26ca2d589f69b59b663200f4913e` | accepted finite-model result; advisory claim |
| `verification.guard-essentiality` | `exact-admission` is essential; removing it admits `DORMANT --publication.execute--> UNADMITTED_EFFECT`; result `res-b095833a4f708aedec42f62f3c10ab9634ae102e88f31522ca4aa97f1d6c2481` | accepted finite-model result; advisory claim |
| `control.nonblockingness` | all eight reachable stable phases are coreachable; no blocking states; result `res-f8ca50706925277190a6c0fba49a3ac44665d0a894203ea88c321434a7a4e549` | accepted finite-model result; advisory claim |
| event-completeness obligation | source inventory, sole reducer, generated-model parity, and repository-contract refusing tests were accepted as complete | closes the declared event alphabet obligation |

Safety/guard derivation
`drv-bfc66012cb01e28ea3e1ef7407acf433d2340a166a14a4120a09a27fc77f1859`
and liveness derivation
`drv-a9534324fdc7e93fcfba3fd6919cbe9513a7dff9e97428f66f46b5489c39c9a4`
remain advisory because the applicable Locus operators are currently candidate
capabilities and because the model deliberately names real-system unknowns.
The verified liveness frontier returned `current_claim: advisory`,
`termination: blocked`, and no `EvidenceAction`; there is no honest additional
action to invent inside V2.

This closes the finite stable-phase abstraction, not the whole live system.
The source-phase by target-phase expansion is conservative. Exact 17-facet
predicates, reducer branches, operating-system interruption behavior, and
external-provider truth are separately closed by executable unit, fault,
integration, historical, repository, and platform tests.

## 18. Complete V2 replacement work order

This is one atomic branch and one final PR. The order controls build safety, not
rollout compatibility.

1. Freeze this specification against base `c5b5e10...` and record historical
   fixtures.
2. Add slice 1 model, catalog, supervisor, protocol, ports, engine, generated
   graph, and formal/property tests.
3. Add one read-only plant observer and explicit invocation identity.
4. Add journaled local and external effects, independent verification, receipts,
   recovery, and fault injection.
5. Inventory every V1 reader, decision, renderer, surface, and writer; route each
   valuable operation through the catalog or classify a read-only exclusion.
6. Add slice 2 facade, CLI, hooks, host assets, SDK/MCP protocol, shell rendering,
   passive events, and consumer parity tests.
7. Port historical incidents, including every PR #172-#185 class, into the
   registry-driven scenario corpus.
8. Delete V1 decision authorities, unmanaged writers, migration/coexistence code,
   shadow controller, duplicate graph/digests, and obsolete docs.
9. Regenerate catalog artifacts and run static closure, full Go/repository tests,
   race tests, platform builds, formal properties, and Locus verified frontier.
10. Update public docs and one release note, verify the exact pushed head, and
    open one concise PR. Do not merge.

Both logical slices must be present before any V2 runtime is publishable. No
partial package rollout, feature flag, fallback, shadow execution, or second PR
is permitted.

## 19. Explicit deletion list for old authorities

The final tree must delete, not retain “just in case”:

- `boatstack/internal/deliverycontrol/**` as a shadow/non-authoritative graph,
  after any useful pure algorithms are made catalog-driven;
- V1 machine-state migration and grading authorities in `migrate.go`,
  `delivery_migrate.go`, `detached_migration.go`, and `migrate_effect_grade.go`;
- static duplicate-control digests such as `lifecycle_event_registry_test.go`,
  `deliverycontrol_parity_test.go`, and engagement/surface inventories once
  replaced by source/catalog completeness checks;
- lifecycle and completion decisions currently owned independently by
  `lifecycle.go`, `engagement.go`, `workspace.go`, `workspace_sync.go`,
  `delivery_terminal.go`, `pr_phase.go`, `next.go`, `run.go`, and `decision.go`;
- effect authority or direct managed writers in activation, planning, plan,
  delivery, mutation, configuration, runtime, publication, update, safety,
  recovery, attach/detach, init/provision, visual publication, and helper command
  paths; pure algorithms may survive only behind V2 ports;
- direct workflow dispatch in `cmd/boatstack-helper`;
- handwritten host/shell prescriptions that duplicate registry knowledge;
- path-only effect APIs, first-match alias resolution, ambient engagement, saved-
  plan activation, ancestry-as-publication, presence-as-validity, cleanup-as-
  completion, boolean uncertainty collapse, repairing status reads, host-specific
  state machines, raw state writers, and success before postcondition proof;
- documentation or public claims describing deleted V1 authority.

No compatibility wrapper may preserve an old internal API. If a preserved
product operation needs an adapter, it targets the new facade/protocol directly.

## 20. Completion criteria

V2 is complete only when all criteria are evidenced at the exact final head.

Architecture: one runtime kernel, catalog, observer, explicit identity,
admission path, receipt model, recovery model, and goal model own their respective
laws. The package graph is acyclic and the facade owns no independent durable
state.

Static closure: zero lifecycle decisions outside the kernel; zero ambiguous
effect identity; zero managed writers outside registered effects; zero
unclassified controlling fields/sites; zero unregistered surfaces; zero host-
specific transition decisions; zero duplicate graphs; zero path-only effect APIs.

Dynamic closure: zero reachable managed deadlocks; zero nonterminal states
without progress/recovery/frontier/safe terminal; zero consumer prescription
disagreements; zero accepted failed postconditions or mixed epochs; zero default
cleanup of unpublished/unresolved work; zero stale prescription admissions.

Behavior: valuable workflows remain possible through V2; safety is equal or
stronger; the historical corpus passes; adapted full Go and repository contract
tests pass; race tests pass; Windows/macOS/Linux compile/check jobs pass; POSIX
and PowerShell are semantically equivalent; all hosts consume kernel decisions.

Formal closure: the executable catalog is the checked model; event, writer, and
consumer inventories are complete; Locus safety and live coreachability results
are supported by observed code evidence; the explicit `verified` frontier is
`target-met` or any remaining action is proved outside the declared V2 target.

Documentation and delivery: this specification matches code; diagrams are
generated from the registry; public claims bind to tests; one release note
describes V2; one final PR has exact-head green CI; the PR is not automatically
merged.

## Appendix A. Historical control-law episodes and regression corpus

Each fixture contains initial plant facts, canonical observation, goal, event,
expected admitted transition, expected postcondition, forbidden transition,
source provenance, and failure class. Rows may share a stronger class fixture,
but every cited PR has an explicit provenance edge.

| Episode/provenance | Symptom and missing distinction | Split/mis-owned authority | V2 structural repair | Required fixture / removed accident |
| --- | --- | --- | --- | --- |
| Initialization and repair, PRs #35-#37 | Partial initialization and repair could leave mixed or misleading state | Filesystem writes vs installed binding/runtime | Journaled staged initialization with binding last and verified receipt | Fail every write boundary; remove repair-by-presence |
| Run/recovery, PRs #38-#39 | Interrupted commands could strand progress | Command success vs recovery state | Recovery is catalog state with bounded resume/rollback | Restart at each interruption; remove exception-path recovery |
| Hooks and malformed host events, PRs #42-#46 | Host-specific inputs diverged or bypassed policy | Hooks/hosts vs native controller | Typed surface request and one admission path | Malformed and replayed host request; remove host decisions |
| Workspace/config foundation, PRs #51-#52 | Workspace and config projections lost topology/authority distinctions | Workspace lifecycle vs config writer | Composite facts with evidence and one observer | Detached/embedded/hybrid configuration fixtures |
| Approval/grounding/worktrees, PRs #56-#59 | Authority or worktree identity was inferred from insufficient context | Approval artifacts and path lookup | Exact authority and `InvocationContext` binding | Ambiguous worktree/approval fingerprint fixtures |
| Deterministic plan and multi-delivery, PRs #61-#64 | One local slice or artifact could choose the wrong delivery | Plan/safety/workflow resolvers | Goal-scoped snapshot and deterministic supervisor | Two deliveries sharing artifacts; remove first-match selection |
| Publication/config corrections, PRs #68-#78 | Publication, mutation, update, or correction could invalidate its own proof | External provider/config writers vs lifecycle | Preview/admit/execute/observe/reconcile and postcondition receipts | Unknown publication; post-publication correction; remove accepted unverified success |
| Dual layout and state ledger, PRs #79, #89-#100 | Embedded/detached layouts and stale ledgers produced incompatible answers | Layout/path state vs delivery authority | Topology facts plus authoritative observation/canonicalization | Same logical plant in all topologies; remove path-as-authority |
| Shadow flow model, PRs #101-#106 | Useful graph/oracle/trajectory existed but was not runtime authority | `internal/deliverycontrol` vs production functions | Executable catalog is runtime and formal model | Generated reachability parity; delete shadow graph |
| Concurrency/worktree runtime, PRs #111-#123 | Stale runtime/worktree selection and destructive guards raced | Runtime launcher, worktree, cleanup, safety | Exact identity, source fingerprint, scoped lock, preservation on uncertainty | Stale runtime, shared aliases, branch/worktree combinations |
| Recovery/denial/owners, PRs #124-#138 | Recovery or denial could be overridden or fail to name a path | Local status slices vs repair/ownership policy | Recovery precedence and typed denial with registered correction | Budget exhaustion and contradictory owner evidence |
| PR state and terminal, PRs #145-#150 | Open/closed/merged and ancestry were collapsed | GitHub projection vs Git graph vs goal | Multi-state publication and goal-specific terminal verifier | Open, closed-unmerged, merged, unavailable, published-not-landed |
| Retro/readiness/visuals/insights, PRs #151-#159 | Ancillary evidence could leak into authority or lack freshness | Evidence services vs lifecycle | Separate services; managed writes cross effects, facts retain freshness | Stale evidence and privacy allowlist; remove evidence-presence authority |
| Update recovery and explicit goal, PRs #161-#163 | Update postconditions or local lifecycle ignored the requested terminal | Update writer/local phase vs goal | Independent verification and goal-first supervisor precedence | Configured PR vs merged terminals; self-invalidating update |
| Detached controller/privacy/cloud, PRs #164-#167 | Shared controller paths and external config/cloud facts were non-injective or sensitive | Detached registry/adapters vs repository identity | Explicit invocation plus evidence-source and privacy classifications | Two repos sharing controller alias; unknown external config |
| Operation/shell/readiness, PRs #168-#170 | Operation drivers and shell guidance could encode different control decisions | Native code vs POSIX/PowerShell/host text | Typed prescription rendered per environment | Semantic shell/host parity; remove hand-authored workflow logic |
| PR #172, deterministic worktree runtime launcher | Active worktree could select stale/wrong runtime | Launcher lookup vs worktree identity | Bind runtime source/version to explicit invocation | `stale-worktree-runtime-selection` |
| PR #173, detached launcher hydration | Detached bootstrap lacked a verified runtime and could dead-end | Bootstrap vs detached runtime owner | `runtime.hydrate` recovery before managed execution | `detached-bootstrap-hydration` |
| PR #174, workspace transition deadlock | Valid workspace states had no next transition | Workspace slice vs delivery resolver | Catalog coreachability and explicit recovery | `workspace-transition-deadlock` |
| PR #175-#176, cleanup/public lifecycle | Cleanup could act on weak completion/publication signals | Cleanup policy vs publication evidence | Cleanup requires goal/lifecycle predicate and explicit authority | `cleanup-before-publication`; remove cleanup-as-proof |
| PR #177, saved plans are not active authority | Mere plan presence activated ambient restrictions | Filesystem presence vs engagement | Engagement fact/lease and command scope | `saved-plan-ambient-restriction` |
| PR #178, detached command admission | Native and detached surfaces disagreed on command permission | Detached launcher vs controller admission | One surface request and admission protocol | `detached-command-admission-mismatch` |
| PR #179, planning bootstrap authority | Bootstrap commands independently reconstructed planning authority | Helper command vs lifecycle | Map command to catalog ID; kernel resolves | `split-bootstrap-command-authority` |
| PR #180, detached configuration authority | Detached config projection drifted from repository/external source | Config readers/writers vs topology | Evidence-backed config authority and reconcile transition | `configuration-projection-drift` |
| PR #181, composite lifecycle authority | Slice status collapsed states needing different actions | Lifecycle vs plan/workspace/publication | One composite snapshot and reachable constraints | `partial-delivery-vs-merged-projection` |
| PR #182, explicit engagement | Dormant repositories were affected by ambient Boatstack state | Repository presence/plan vs engagement | Dormant/command/active/conflict facet | `dormant-repository-interference` |
| PR #183, verified configuration mutation | Successful mutation could invalidate verification | Config writer vs verifier/runtime binding | Binding last, re-observe, independent target check | `configuration-mutation-self-invalidation` |
| PR #184, test sharding | Large test topology exposed implicit shared assumptions | Test partitions vs hidden global state | Isolated catalog/plant/effect fixtures and deterministic seeds | Cross-shard/race parity; remove test-order authority |
| PR #185, preserve active workspaces | Branch equal to main or incomplete publication could be read as landed and cleaned | Git ancestry, publication, workspace, active delivery, configured goal | Durable publication evidence, active-delivery precedence, preserve on ambiguity | `unpublished-equal-main-not-landed`, `closed-unmerged-not-cleanup-eligible`, `configured-merged-terminal`, `active-workspace-preserved` |

Additional class fixtures required even when covered by stronger rows are:
activation from the wrong worktree identity; ambiguous detached controller alias;
runtime publication before lock release; amendment deadlock; invalid-plan recovery;
partial multi-slice delivery overridden by merged provider state; CI/provider
unknown; external request settled before receipt; and every transactional
interruption point.
