# Current architecture

Concept documents define the model; this section maps that model to the
current implementation. Package boundaries are implementation anchors, not
definitions.

| Responsibility | Current owner | Representative implementation | Primary verifier |
| --- | --- | --- | --- |
| General supervisory mechanism | kernel | `boatstack/kernel` | kernel conformance and integer fixture |
| Control Program IR and compiler | controlprogram | `boatstack/controlprogram` | canonicalization and frontend conformance |
| Invocation materialization | invocation | `boatstack/invocation` | invocation and completeness tests |
| Software-delivery contracts | core, delivery, flow | `boatstack/core`, `boatstack/delivery`, `boatstack/flow` | program and relation tests |
| Software-delivery execution | internal software delivery | `boatstack/internal/softwaredelivery` | boundary, effect, recovery, and runtime tests |
| Runtime persistence and topology | runtime | `boatstack/internal/runtime` | control-bundle and flow-file tests |
| Host and API surfaces | surfaces, SDK, distribution | `boatstack/sdk`, `boatstack/distribution` | surface parity and repository contracts |
| Extensions | extension | `boatstack/extension` | in-process and subprocess conformance |
| Analysis and generated evidence | analysis, surfaces | `boatstack/analysis`, software-delivery renderers | generated-artifact byte comparison |

The dependency direction is from the domain-neutral kernel toward domain
contracts and then concrete domain/runtime adapters. The kernel never imports
the software-delivery implementation.

## Guides

- [General kernel](kernel.md)
- [Compiler and artifacts](compiler-and-artifacts.md)
- [Runtime, persistence, and control bundles](runtime-persistence-and-control-bundles.md)
- [Surfaces and host projections](surfaces-and-host-projections.md)
- [Software-delivery domain](software-delivery-domain.md)
- [Conformance and generated evidence](conformance-and-generated-evidence.md)

Focused boundaries:

- [Capability and authority](capability-authority-boundary.md)
- [Prescription transactions](prescription-transactions.md)

Machine-owned catalogs and models remain beside these guides and are indexed
by [generated-file ownership](../generated-files.md#generated-architecture-evidence).
