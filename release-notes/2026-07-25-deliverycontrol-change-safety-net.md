### Change-safety net — the delivery model can grow without breaking what ships today

The delivery machine is now described in one authoritative registry, and two safety nets keep that
description honest as new features land:

- **Coverage conformance.** A test parses the CLI's real delivery dispatch and asserts every delivery
  verb it handles is classified — either mirrored by a registry transition or on an explicit
  non-delivery allowlist. A new delivery verb that forgets to declare itself in the registry now fails
  the build instead of silently drifting out of the model the driver navigates.

- **Schema-migration hook.** Managed delivery state is routed through a versioned migration step before
  it is loaded. At the current schema version this is a byte-for-byte pass-through, so today's behavior
  is unchanged; when the schema next changes, old state has a defined, tested upgrade path, and state
  written by a newer Boatstack fails closed with "update Boatstack" rather than as generic corruption.

Both are covered by control-law conformance suites, so the guarantees are enforced, not aspirational.
Together they mean the delivery-flow surface can gain moves and fields without stranding an in-flight
delivery or letting a new verb escape the model.
