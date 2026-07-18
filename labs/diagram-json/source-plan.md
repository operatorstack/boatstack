# Source plan from host Plan mode

## Intent

Add machine-readable JSON output to the diagram printer without changing the
existing text output.

## Initial approach

- Inspect the current diagram representation, printer, and public exports.
- Prefer an additive serializer over changing the text printer's contract.
- Define an explicit versioned JSON shape rather than exposing internal objects.
- Add contract checks for parseability, determinism, public exports, and existing
  ASCII compatibility.

## Unknowns for Boatstack to resolve

- Whether the serializer is a sibling public API or a printer mode.
- Which schema stability guarantee is appropriate.
- How much optional run data belongs in the public JSON document.

This is an exploratory plan, not approval to implement.
