# Boatstack V2 replacement closure

> Historical replacement evidence for PR #186. The normative current
> architecture and executable counts are defined by
> [Boatstack programmable delivery control architecture](boatstack-v2-kernel.md).

Base revision: `c5b5e10cdcf4d97b645d705cb164e762acf93ff1`
Replacement mode: flag day; no V1 compatibility or state migration

This report binds the V2 implementation to the frozen V1 inventory. It is not a
claim that old APIs remain available.

## ZCA translation and value

The rewrite ships two logical slices together. Slice 1 is one authoritative
kernel over one canonical snapshot and transition catalog. Slice 2 is the CLI,
hook, SDK/MCP, shell, and host projection of that same kernel. The immediate
value is the removal of independently reconstructed lifecycle and effect
authority while keeping the product workflows available through V2 semantics.

## Deleted authority

The [frozen V1 inventory](boatstack-v1-authority-inventory.md) identified and the
rewrite deletes:

- 84 direct lifecycle/completion decision declarations across nine independent
  owner files;
- 388 supporting control-authority declarations across 22 additional files;
- 106 direct filesystem mutation sites;
- 14 external-effect dispatch or mutation-intent sites;
- the entire `internal/deliverycontrol` shadow graph and all V1 migration,
  coexistence, state-repair, host-state-machine, and fallback code.

The conservative removed V1 managed-effect surface is therefore 120 sites.
V2's static source inventory fails if an `os` writer exists outside
`internal/softwaredelivery/effects`, if a command boundary exists outside the exact plant/effect
allowlist, if a production file is unclassified, or if the deleted shadow
controller is imported or recreated.

## Executable replacement

The runtime has 17 controlling facets and 61 semantic transitions:

| Class | Count |
| --- | ---: |
| authority | 9 |
| owned-local | 30 |
| owned-external | 2 |
| recovery | 7 |
| observed-external | 13 |

The [generated table](boatstack-v2-transition-catalog.md),
[generated Mermaid graph](boatstack-v2-transition-catalog.mmd),
[Locus safety model](boatstack-v2-locus-safety.json), and
[Locus liveness model](boatstack-v2-locus-liveness.json) come from the same
registry used by the supervisor and engine. Golden tests reject byte drift and
require both formal alphabets to equal all 61 executable transitions.

Every effect follows observe, resolve, admit, lock, journal, execute,
re-observe, verify, and receipt. Runtime identity includes the exact executing
binary path and fingerprint. Workspace transfer stages both source and
destination controller states. Clone-family journals and receipts use repository
plus Git-common identity, while worktree state remains separately partitioned.
Repository policy is part of the canonical control projection: high-risk changes
are derived from the default-branch diff and live working tree, require human
review when configured, refuse visual attachment when disabled, and prevent a
required-visual terminal until revision-bound evidence exists.

## Historical and live evidence

The historical corpus contains 22 typed fixtures. It covers every PR from #172
through #185 and the additional ambiguity, interruption, stale-runtime,
publication, workspace, configuration, and objective-terminal failure classes named
in the V2 specification.

Live integration tests exercise embedded and detached installation, attach and
detach, two-clone identity separation, exact runtime update, linked-worktree
authority transfer and cleanup, strict configuration, shared guard behavior,
journal restart recovery, receipt/event generation, idempotency, and
postcondition failure. Repository tests execute an offline checksum-bound
install and update through the shipped binary.

The generated Locus phase graph is a conservative source-phase by target-phase
expansion. Formal checks found the forbidden effect state unreachable, proved
the exact-admission guard essential, found all eight reachable stable phases
coreachable, and accepted the event-completeness discharge. The claim remains
advisory: the 17-facet predicates, reducer branches, operating-system behavior,
and external-provider truth remain bound to executable source, fault,
integration, repository, and platform tests. The exact result IDs and blocked
verified frontier are recorded in the technical specification.
