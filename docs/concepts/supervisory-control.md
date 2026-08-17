# Supervisory control

## Definition

Boatstack is a controller over discrete, named state transitions. It combines
an external objective, durable supervisory state, a current domain observation,
and authority with one canonical transition relation.

```text
objective + state + observation + authority
                    ↓
              relation → decision → prescription
                    ↓
              fresh admission
                    ↓
              operator → effect
                    ↓
              fresh verification
                    ↓
                state + receipt
```

## Control boundary

A proposal never authorizes or performs an effect. A model, human, service, or
workflow may propose; the controller owns admissibility. The operator receives
one admitted operation, and verification decides whether the candidate result
may become trusted state.

## Invariants

- Resolve and apply use the same legality relation.
- State, program, observation, objective, and authority drift invalidate the
  prescription before the effect.
- Candidate effects are not committed state.
- Rejected or uncertain candidates never silently become trusted state.
- Completion and recovery are declared by the Control Program.

Boatstack uses supervisory-control language for these implemented relations.
It does not claim supervisor synthesis, maximal permissiveness, formal
controllability or observability, theorem-proved whole-system nonblockingness,
global optimality, or whole-system model checking.

## Lifecycle

The controller may return a prescription, a marked result, a frontier,
refusal, blocker, or unresolved state. Successful application produces a fresh
observation, verified state transition, and receipt. Interruption or uncertain
settlement enters recovery.

## Current implementation anchors

- [Kernel runtime](../../boatstack/kernel/runtime.go)
- [Canonical relation](../../boatstack/kernel/relation.go)
- [Runtime conformance tests](../../boatstack/kernel/runtime_test.go)
