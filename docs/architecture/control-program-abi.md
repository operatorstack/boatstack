# Control Program ABI

A Flow is the product-facing name for one complete Control Program. The
runtime validates that complete program before it constructs a registry or
resolves a transition.

```text
repository source
  -> strict parse
  -> structural and semantic validation
  -> typed normalization
  -> canonical executable representation
  -> SHA-256 program fingerprint
  -> runtime compatibility
  -> Kernel
```

## Manifest

| Field | Class | Canonical rule |
| --- | --- | --- |
| `schema_version` | compatibility | Must equal the supported positive ABI version; excluded from the executable fingerprint. |
| `program_id` | identity | Lowercase semantic ID without `/`; included because it qualifies every transition ID. |
| `program_version` | descriptive author identity | Non-empty deterministic token; excluded because changing it alone does not change executable semantics. |
| `requires_runtime` | compatibility | Exact `>=MAJOR.MINOR.PATCH` minimum; checked before registry construction and excluded from the executable fingerprint. |
| `capabilities` | executable semantics | Exact, duplicate-free sets of used effect and verifier IDs; sorted canonically. Declaration does not grant authority. |
| `owned_resources` | executable semantics | Exact, duplicate-free set of resources written by transitions; sorted canonically. |
| `goal_contracts` | executable semantics | Sorted by goal; conjunctive conditions and their set-valued members are sorted. |
| `transitions` | executable semantics | Local declarations are normalized, program-qualified, validated, and sorted by complete ID for hashing. |

`goal_contracts` and `owned_resources` are required beyond the tentative six
fields because terminal resolution and effect ownership consume them directly.
No repository state, runtime path, agent session, or granted authority belongs
to this ABI.

## Ordering

Explicit `selection_class` and `priority` carry selection semantics. Source
declaration order does not. Phase lists, goals, identities, authorities,
evidence, resources, parameters, conditions, interruption points, managed
operations, capabilities, and goal contracts are sets or name-keyed
declarations and are normalized into canonical order. Prescription arguments
retain source order because argument order is executable.

Unknown fields, duplicate JSON keys, duplicate declarations, ambiguous IDs,
and implicit aliases fail closed. The parser does not hash raw JSON, preserve
whitespace, or depend on JSON object key order.

## Identity and compatibility

The canonical internal transition identity is:

```text
<program_id>/<local_transition_id>
```

Both components reject `/`, so the mapping is injective. Recovery references
are qualified through the same rule. Renaming `program_id` intentionally
creates a different program fingerprint and different transition identities.

The runtime returns `PROGRAM_SCHEMA_UNSUPPORTED` for a newer schema,
`RUNTIME_TOO_OLD` when the verified runtime is below the minimum, and
`PROGRAM_INVALID` for malformed or semantically incomplete input. None of
those failures constructs a registry or reaches effects.

The executable fingerprint excludes `program_version` and runtime
compatibility because those are separate identities. It includes the complete
normalized transition graph, exact goal contracts, capability bindings,
resource ownership, and program-qualified identity. Thus representation-only
changes remain stable while every kernel-observable control-law change changes
the fingerprint.
