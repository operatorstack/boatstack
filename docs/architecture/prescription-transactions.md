# Prescription transaction boundary

Resolution and application form one compare-and-swap transaction over the
durable logical state and the immutable executable Control Program.

```text
snapshot(state revision N, program fingerprint P)
  -> resolve
  -> prescription(transition, N, P, snapshot fingerprint)
  -> repository-scoped lock
  -> re-observe and compare the complete binding
  -> effect and durable state commit N+1
  -> receipt(N, N+1, P, prescription, admission)
```

The prescription is content-addressed and carries no authority. Apply and
recover require its transition ID, durable state revision, program fingerprint,
snapshot fingerprint, and correlation unchanged. Authority remains separately
typed, scoped, and validated by admission.

## Control law

An effect may commit only when the current durable state revision, executable
program fingerprint, and admission-relevant snapshot exactly equal the values
observed during resolution. The comparison occurs again under the
repository-scoped kernel lock before journaling or effects.

Every accepted logical transition, including recovery and extension-owned
effects, advances the durable revision exactly once from `N` to `N+1`. Read-only
resolution, refusal, failed preflight, rollback, and ordinary stale replay do
not advance it. Revision zero and overflow are invalid.

A mismatch returns `STALE_PRESCRIPTION`, produces no managed effect or receipt,
and requires a new resolution. Explicit idempotent replay is valid only when
the caller supplies the original idempotency key and the current state proves
that the recorded transaction settled.

## Interruption

Local effects stage reversible resource mutations in the transaction journal;
the logical state mutation installs last. A failed local effect rolls back
without advancing the committed revision. Recovery of an interrupted journal is
itself a prescribed transition and commits a new revision. An external effect
whose settlement cannot be proven remains recovery-required and is never
blindly retried.
