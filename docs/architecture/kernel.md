# General kernel

This document maps the [supervisory-control concepts](../concepts/supervisory-control.md)
to the current domain-neutral Go implementation.

Boatstack has one domain-neutral supervisory mechanism and one production
domain: software delivery.

```text
external Objective
        │ exact bind
        ▼
ControlState + Program + Domain Observation + Authority
        │
        ▼
canonical transition relation
        │
        ▼
Operator → Effect Facts → fresh observation → verification → receipt
```

The general mechanism is in `boatstack/kernel`. It has no Git, repository,
branch, worktree, plan, coding-host, test, review, publication, or pull-request
types. The software-delivery domain is in `boatstack/delivery`,
`boatstack/flow/standard`, and `boatstack/internal/softwaredelivery`.

The dependency rule is:

```text
kernel
  ↑
domain contracts
  ↑
software delivery
```

The general kernel never imports the software-delivery implementation.

## Kernel-owned semantics

- exact program identity and fingerprint;
- control-instance identity and monotonic state revision;
- external `Objective` and durable exact `ObjectiveBinding`;
- objective scopes: `none`, `optional-preserve`, and `bound-exact`;
- explicit objective bind and clear mutations;
- one transition relation used by resolve and apply;
- state, program, objective, observation, and authority freshness;
- trusted minimum-capability classification;
- transition-owned effect facets;
- operator-neutral execution and fresh postcondition verification;
- program-defined marked modes;
- explicit recovery state and recovery transitions;
- domain-neutral committed receipts.

## Software-delivery-owned semantics

- Git and repository identity;
- repository policy and coding-host configuration;
- plans, worktrees, builds, tests, reviews, and evidence;
- delivery and publication state;
- provider-authorized pull-request effects;
- software-specific objective kinds and terminal contracts.

`DeliveryController` is the software-delivery facade. It retains the existing
transactional repository implementation as a domain executor. It projects the
compiled software Program ABI into one kernel `Program`, supplies admissible
domain candidates, and delegates ordering, targeted/untargeted selection,
marked-state recognition, ambiguity, and authority admission to
`kernel.Relate`. It does not own a second selector.

The complete software manifest is hashed as the kernel Program's domain
contract fingerprint. The resulting kernel Program fingerprint is the one
identity used by software snapshots, prescriptions, admissions, and receipts.
Changing either generic transition data or any software-domain contract makes
prior prescriptions stale.

## Objective law

An objective is external reference data. Supervisory state stores only an
exact binding:

```text
Objective        = id + revision + fingerprint + reference
ObjectiveBinding = objective id + revision + fingerprint
```

A command-scoped objective cannot reinterpret an existing binding. Changing
intent creates a new objective revision. Prescriptions that bind an earlier
revision become stale before any operator effect.

Maintenance transitions use `optional-preserve`: absent remains absent and a
known binding remains byte-for-byte exact. Product progress uses
`bound-exact`. Objective binding is an explicit, capability-gated transition.

The software domain retains its typed objective projection so it can evaluate
delivery-specific terminal contracts. The kernel freshness envelope binds the
exact status/value fingerprint of that projection. Refreshed evidence alone
does not change the binding; any semantic objective change does.

## Canonical relation

Resolve filters the program by mode, recovery state, objective law, the domain
predicate, and authority. Apply reloads the state and observation under the
instance lock, verifies prescription freshness, and calls the same relation.
It cannot use a separate deterministic legality rule.

Software delivery uses the same relation through its domain adapter. Its
advanced journal and reversible effect machinery remain domain-owned, while
the prescription uses the same `kernel.Freshness` CAS identity as the generic
runtime: state revision, Program fingerprint, snapshot fingerprint, objective
binding fingerprint, and authority fingerprint.

Programs declare capabilities, but a trusted capability classifier supplies
the minimum for each concrete operation. The operator receives only that
admitted set. Effect facts must stay inside transition-owned facets.

The generic `Store` is one durability boundary. `CommitTransition` atomically
persists the target control state and its verified receipt; neither may become
visible alone. If an operator may have changed domain state but that atomic
commit fails, `EnterRecovery` records recovery against the unchanged
pre-commit mode. Program compilation rejects any recovery mapping that cannot
run from every source mode of the transition it recovers.

## Non-software proof fixture

`boatstack/kernel/runtime_test.go` runs an integer control instance:

```text
objective.bind → increment → increment → marked(value = 2)
                         ↘ interruption → reset recovery
```

It binds `reach-two@1`, invokes deterministic functions, verifies fresh integer
observations, advances revisions, commits receipts, rejects stale objective and
observation bindings, denies missing capability authority, and recovers an
interrupted operator. The fixture imports no software-delivery package and
requires no Git executable or repository.

## Enforced properties

1. Program determinacy: executable law is bound to one program fingerprint.
2. Prescription soundness: resolve and apply share one relation.
3. Freshness: state, program, objective binding, observation, and authority are exact.
4. Authority non-escalation: program and operator declarations grant nothing.
5. Effect containment: trusted capabilities and owned facets bound effects.
6. Objective separation: only an exact binding enters control state.
7. Objective preservation: unowned transitions cannot change the binding.
8. Domain isolation: the kernel has no software-delivery dependency.
9. State ownership: control and effect mutations have explicit owners.
10. Fact fidelity: receipts contain committed effects and verified observations.
11. Recovery liveness: uncertain outcomes enter explicit recovery state.
12. Marked-state generality: the program defines accepted modes.
13. Operator neutrality: the fixture uses deterministic functions, not an agent.
14. Domain substitution: the integer domain runs without kernel changes.

## Current implementation anchors

- [Kernel types and ports](../../boatstack/kernel/types.go)
- [Resolve/apply runtime](../../boatstack/kernel/runtime.go)
- [Relation tests](../../boatstack/kernel/relation_test.go)
- [Domain-neutral conformance fixture](../../boatstack/kernel/conformance/integer.go)
