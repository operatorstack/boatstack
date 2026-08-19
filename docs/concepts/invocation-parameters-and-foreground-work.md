# Invocation, parameters, foreground work, and suspension

## Definitions

An invocation materializes one program entry, target, input set, repository,
and run lineage. Every required reachable operator parameter must have exactly
one admissible producer: entry input, trusted resolver, durable state, receipt,
foreground-work output, or bounded host input.

**Foreground work** produces candidate artifacts under a declared input,
instruction, and output contract. A **typed suspension** records missing
actor-owned input, authority, or work without guessing.

## Control boundary

Invocation completeness is checked before an executable artifact is accepted.
At runtime, missing actor-owned values suspend the same run. Answers are
evidence for parameter materialization, not authority to perform an effect.

## Invariants

- Each required parameter has exactly one compatible producer.
- Trusted resolvers are immutable references and do not grant authority.
- Foreground-work completion does not independently advance Flow state.
- Same-transition parameters consume the candidate result being admitted.
- Later transitions and Work contracts consume only the applicable committed result.
- A cross-transition Work producer remains source-admissible at its own target so current-program evidence can be refreshed.
- A later result commits the exact producer receipt, result, contract, output, and byte identities.
- Requests and answers are correlated to the exact run and generation.
- Restart and resume preserve the same run and request lineage.

## Lifecycle

Compilation proves producer completeness for every reachable transition.
Runtime creates immutable input or work requests when materialization cannot
continue. A correlated answer or work result resumes the same run; rejected
values are superseded with a linked generation instead of being overwritten.

## Current implementation anchors

- [Invocation model](../../boatstack/invocation/invocation.go)
- [Invocation compiler](../../boatstack/controlprogram/invocation_compile.go)
- [Foreground-work manager](../../boatstack/internal/softwaredelivery/foregroundwork/manager.go)
