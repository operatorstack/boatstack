# Lifecycle, evidence, and publication

Product Delivery supplies trusted operations; a repository Flow selects which
ones form its lifecycle. Availability does not imply membership, and no single
lifecycle is mandatory for every repository.

A current Flow may compose planning admission and promotion, entry activation,
autonomy delegation, managed workspace cut/activate/sync/publish,
implementation work, gate evidence, visual or custom evidence, delivery
slices, publication preview/execute/observe/correct, reconciliation,
completion, and abandonment. Each included operation remains an explicit
transition or explicit foreground-work binding.

Gate evidence is admitted only through its canonical input path and exact
fingerprint for the current source revision. Visual and custom evidence are
ordinary declared Flow work or transitions where the selected lifecycle uses
them; they are not ambient model claims. Delivery slices keep change identity
and evidence scoped to one addressable unit.

Publication is split into preview, external execution, observation, and
correction. Preview binds the exact clean worktree and committed HEAD. Provider
authority is proven independently at the provider boundary and cannot be
replaced by human approval, repository configuration, or a command using
provider credentials. A changed source or worktree invalidates the preview.

Local managed effects use the transaction journal and deterministic reversal.
If an external publication may have occurred but settlement is unknown, the
run becomes recovery-required. Observation or reconciliation establishes the
external result before a retry or corrective transition. Marked completion is
defined by the entry target; abandonment is a separately declared accepted
target when the Flow includes it.

## Current implementation anchors

- [Trusted transition catalog](../../boatstack/internal/softwaredelivery/catalog/transition.go)
- [Effect driver](../../boatstack/internal/softwaredelivery/effects/driver.go)
- [Provider admission](../../boatstack/internal/softwaredelivery/protocol/admission.go)
- [Recovery tests](../../boatstack/internal/softwaredelivery/effects/recovery_test.go)
