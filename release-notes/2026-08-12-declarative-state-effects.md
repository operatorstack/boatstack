### Declare software-delivery state effects

Software-delivery transitions now declare their durable state assignments and owned facets as data. Repository-authored control programs can add state-only transitions without adding a Go reducer case, while cross-field operations remain behind explicitly named native handlers.

The control-program schema is now version 4, and the program-runtime and extension protocols are version 3. Manifests must declare each controllable transition's owned facets and state effect.

Native handlers now compile only against their registered component, effect, facet, and objective-policy contract. Declarative assignments must close durable-state invariants, and ordering-only declaration changes preserve the same program fingerprint.
