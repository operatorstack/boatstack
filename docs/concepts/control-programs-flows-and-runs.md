# Control Programs, Flows, and runs

## Definitions

A **Control Program** is the complete executable control law. It declares
transitions, targets, entries, invocation requirements, authority constraints,
verification, and recovery. A **Flow** is the product-facing name for a complete
Control Program authored for a domain.

An **entry** is a named invocation surface selecting a target and inputs. A
**target** is a marked predicate defining accepted completion for that entry. A
**run** binds one exact program, entry, target, input set, repository, and
execution lineage.

## Control boundary

Program source is not runtime authority. The compiler validates and
canonicalizes the complete program before runtime construction. Entry names are
semantic identifiers; names such as `run` have no built-in lifecycle meaning.

## Invariants

- Program identity is a fingerprint of canonical executable semantics.
- Changing executable semantics invalidates earlier prescriptions and requires
  explicit reconciliation of existing state.
- Every entry selects one declared target.
- Entry activation authority and later run-scoped delegation remain separate.
- A run never borrows inputs, receipts, authority, or answers from another run.

## Lifecycle

Authoring source lowers to raw IR, trusted bindings and assets are resolved,
invocation completeness is checked, and a canonical artifact receives its
program fingerprint. Runtime loads that checked artifact and materializes an
entry as a run.

## Current implementation anchors

- [Control Program model](../../boatstack/controlprogram/ir.go)
- [Canonicalization](../../boatstack/controlprogram/canonical.go)
- [Program and invocation tests](../../boatstack/controlprogram/canonical_test.go)
