# Planning and foreground work

Planning is an optional repository-selected lifecycle, not a kernel primitive
or a mandatory phase for every Flow.

A foreground-work contract declares immutable package instructions, bounded
inputs, and typed output artifacts with optional artifact-local `guidance`.
Both instruction layers are compiler-resolved UTF-8 assets whose exact bytes
and hashes participate in program and request identity. Guidance describes how
to generate one artifact; it is not authority, verification, or executable
repository code. The runtime materializes one request for the exact
run and validates required outputs, media types, size limits, and schemas. Work
produces candidate artifacts only; completion does not advance Flow state or
create authority.

Accepted foreground work uses two plan-neutral trusted operations:

1. `work.package.admit` verifies and stores an immutable fingerprint-addressed snapshot;
2. `work.package.approve` records exact admitted authority and actor provenance.

The optional `planning.package.promote` specialization selects one
compiler-bound required output from an approved package and publishes its exact
bytes as the canonical plan. Generic admission and approval do not read or
change plan state. The promotion receipt alone gives that output plan meaning.

Repositories choose whether these operations belong to their lifecycle, their
priorities, and the target they serve. `workPackage.work` and
`planningPackage.work` bind foreground work only to generic admission.
`planningPackage.planOutput` binds only promotion. No output ID is implicitly special.
Additional work must be explicitly registered and named
by every lifecycle step that consumes it.

Missing actor-owned parameters create `TRANSITION_INPUT_REQUIRED` suspension.
The immutable request binds the run, control bundle, transition, parameter,
and identity context. `boatstack flow input answer` records a correlated answer
and resumes the same run. A rejected value is superseded with a linked request
generation; old requests and receipts are not edited or deleted.

The selected transition consumes verified work output through its declared
parameter producer. An answer or work receipt is evidence, not approval or
delegated authority.

## Immutable packages and verification

Admission writes `.boatstack/work-packages/<delivery>/<full-sha256>/` with
`manifest.json`, `contract.json`, `work-receipt.json`, and the exact output
bytes. `approval.json` is a one-time immutable sidecar. Historical verification
uses the embedded contract and never needs the old Flow or controller state.

```sh
boatstack flow work-package verify --repo . --all --format json
boatstack flow work-package verify --repo . --delivery <delivery> \
  --package <fingerprint> --require-approval --require-current-program --format json
```

The result reports integrity, contract, approval, and current-program status
separately. It always reports semantic correctness as `not-evaluated` and
origin authenticity as `not-proven`.

## Related API

- [`foregroundWork`, assets, and work outputs](../typescript/base-sdk.md#foreground-work)
- [Accepted-work and planning helpers](../typescript/software-delivery-sdk.md#accepted-work-and-planning)
- [Writing a Flow](writing-a-flow.md)

## Current implementation anchors

- [Accepted-work binding](../../boatstack/flow/softwaredelivery/work_package.go)
- [Foreground-work manager](../../boatstack/internal/softwaredelivery/foregroundwork/manager.go)
- [Journal/work conformance](../../boatstack/internal/softwaredelivery/effects/journal_work_test.go)
