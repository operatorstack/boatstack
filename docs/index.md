# Boatstack documentation

Boatstack documentation is divided by authority and rate of change. Current
executable code is the first source of truth, followed by deterministic tests,
generated artifacts, exact reference documents, and historical records.

| Plane | Purpose | Start here |
| --- | --- | --- |
| Concepts | Stable terms, relationships, and invariants | [Concepts](concepts/index.md) |
| Current architecture | How those concepts map to the implementation now | [Architecture](architecture/index.md) |
| Domain guides | Software-delivery behavior and Flow authoring | [Product Delivery](product-delivery/index.md) |
| Reference | Exact commands, schemas, paths, and configuration | [Getting started](getting-started.md) |
| Generated and historical | Machine-owned evidence and superseded design context | [Generated files](generated-files.md), [History](history/index.md) |

The [glossary](glossary.md) is the terminology authority. The
[TypeScript guide](typescript/index.md) connects authoring APIs to the same
Control Program model.

## Reference documents

- [Getting started](getting-started.md)
- [Configuration](configuration.md)
- [Control Program IR](control-program-ir.md)
- [Generated files and ownership](generated-files.md)
- [Runtime selection](runtime-selection.md)
- [Safety boundaries](safety.md)
- [Self-review loop](self-review.md)
- [Troubleshooting](troubleshooting.md)
- [Public-surface contract](public-surface.md)

## Generated evidence

The transition catalog, Mermaid graphs, and Locus inputs under
`docs/architecture/boatstack-*` are produced from executable registries. Their
generators, commands, and verifiers are listed in
[Generated files and ownership](generated-files.md). Do not edit them by hand.

## Documentation control law

**Boundary:** executable behavior becomes a public documentation claim.

**Control law:** a current claim must be supported by current code, tests,
generated evidence, or an exact reference contract; generated evidence remains
generator-owned and historical prose cannot become current authority.

**Required evidence:** resolving links, deterministic documentation contracts,
TypeDoc with strict validation, generated-artifact comparison, and relevant
runtime tests. A failed check blocks publication rather than weakening the
claim or changing runtime behavior to match old prose.
