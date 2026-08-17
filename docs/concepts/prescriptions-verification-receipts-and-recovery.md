# Prescriptions, verification, receipts, and recovery

## Definitions

A **prescription** is a content-bound proposal to apply one selected transition
under exact state, program, observation, objective, authority, and invocation
context. A prescription carries no authority.

**Admission** is the final pre-effect recheck. **Verification** is the fresh
post-effect check. A **receipt** is created only after verified atomic commit.
**Recovery** records that an effect may have happened while safe settlement is
unknown. **Reconciliation** is a controlled operation that resolves known
drift or uncertain external state.

## Control boundary

```text
candidate effect != committed state
```

Apply locks the instance, re-observes, compares every freshness binding, and
re-runs the canonical relation. Stale prescriptions fail before effects.
Verification then decides whether the candidate postcondition may commit.

## Invariants

- Receipts are emitted only with committed state.
- Local reversible effects roll back on failed settlement.
- Possibly completed external effects are not blindly retried.
- Recovery uses a new admission and does not inherit stronger authority.
- Replay returns a prior committed result without repeating the effect only
  when exact identity and idempotency conditions hold.

## Lifecycle

An unresolved attempt is persisted before execution. A successful effect is
freshly observed and verified, then state and receipt commit atomically. An
interruption before safe settlement records recovery against the unchanged
pre-commit mode; a declared recovery or reconciliation transition resolves it.

## Current implementation anchors

- [Kernel apply protocol](../../boatstack/kernel/runtime.go)
- [Prescription transaction guide](../architecture/prescription-transactions.md)
- [Recovery tests](../../boatstack/internal/softwaredelivery/effects/recovery_test.go)
