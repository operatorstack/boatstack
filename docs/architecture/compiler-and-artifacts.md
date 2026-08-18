# Compiler and artifacts

This document maps [Control Programs and Flows](../concepts/control-programs-flows-and-runs.md)
to the current compiler, canonical artifact, and projection pipeline.

A Flow is the product-facing name for one complete Control Program. The
runtime validates that complete program before it constructs a registry or
resolves a transition.

```text
Flow TypeScript
  -> restricted trusted frontend
  -> raw Control Program IR
  -> strict Go decode and canonicalization
  -> trusted binding and asset resolution
  -> invocation completeness analysis
  -> program fingerprint and checked artifact
  -> host projections and atomic publication
  -> runtime check and load
```

## Control Program document

The current document identifies `schema: "control-program"` and
`schema_revision: 7`. Its top-level sections are:

| Section | Purpose |
| --- | --- |
| `program` | program ID, version, optional human identity role, and description |
| `declarations` | the complete capability, authority, effect, verifier, and input-resolver vocabulary |
| `facets` and `evidence` | typed observable/control facts and their relations |
| `work` | bounded foreground-work assets, inputs, and output contracts |
| `operators` | trusted bindings, authority algebra, capabilities, effects, verification, recovery, state effects, and parameter contracts |
| `transitions` | guards, targets, priorities, additional mandatory authority, work, and parameter producers |
| `targets` and `entries` | marked predicates and repository-owned invocation surfaces |

The Go decoder rejects unknown fields, duplicate JSON keys, trailing JSON,
invalid or duplicate declarations, unresolved references, and incomplete
reachable invocation parameters. A repository cannot use inline data to
replace semantics owned by a trusted operator, delegation, resolver, or value
validator binding.

The TypeScript source is authoring input, not runtime code. The frontend accepts
only named imports from trusted SDK packages and a declarative default export.
It rejects local modules and executable repository code. Frontend selection is
explicit; the runtime never discovers a repository binary as compiler
authority.

Source bytes, dependency lock, project configuration, referenced assets,
trusted bindings, and projected files are checked before publication. The
canonical artifact publishes last. Runtime loading rechecks its hashes and
compatibility before constructing the controller.

Generated ownership is per exact file. Obsolete projections are removed only
when a verified ownership record proves that Boatstack owns them; host
directories are never claimed wholesale.

## Artifact envelope

The committed artifact identifies `schema: "control-program-artifact"` and
`schema_revision: 7`. It contains the compiler version; source and dependency
lock paths and hashes; the program fingerprint; the canonical projection
selection and its fingerprint; hashes of every generated projection; hashes
of referenced work assets; and the compiled Control Program document.

Checking an artifact re-reads the source, lock, and assets; recompiles trusted
bindings; compares the program and projection-selection fingerprints;
re-renders the selected projections; and compares both their hashes and their
repository bytes. Any mismatch makes the artifact stale.

## Canonical ordering and identity

Declaration sets and name-keyed collections are normalized into canonical
order. Transition `priority` carries selection semantics; source declaration
order does not. Parameter bindings retain their declared meaning and are
validated against the trusted operator contract.

Unknown fields, duplicate JSON keys, duplicate declarations, ambiguous IDs,
and implicit aliases fail closed. The parser does not hash raw JSON, preserve
whitespace, or depend on JSON object key order.

The executable fingerprint hashes the normalized document after descriptions
and entry diagnostic presentation preferences are removed. Program identity,
version, human identity role, declarations, trusted binding fingerprints,
facets, work contracts, operators, transitions, targets, and entries remain
semantic. Representation-only ordering and prose changes preserve the
fingerprint; control-law changes do not.

## Capability boundary

Each controllable transition declares `required_capabilities`. Validation
requires that set, plus the kernel-owned minimum for its concrete effect, to be
inside the program capability surface. Admission then requires all of those
capabilities from external authority. Missing or unknown capabilities fail
closed; intersection never produces a partially admitted transition.

Prescriptions bind the authority source identity and exact required/effective
capability set. Admissions also retain the broader granted set. Effects receive
only the exact effective set and recheck the kernel minimum before execution.
See [Capability and authority boundary](capability-authority-boundary.md).

## Current implementation anchors

- [Raw IR and artifact](../../boatstack/controlprogram/ir.go)
- [Canonicalization](../../boatstack/controlprogram/canonical.go)
- [Flow compile boundary](../../boatstack/cmd/boatstack-helper/flow_command.go)
- [Frontend conformance tests](../../boatstack/controlprogram/frontend_conformance_test.go)
