### `boatstack flow tasks` — the ordered sub-actions inside a building slice

While a delivery slice is building, the next move was only ever named "build" — an opaque instruction
that left the operator to reconstruct the order of the work by hand. `flow tasks` now reads the
compiled plan's task graph, scopes it to the active slice, and prints the slice's sub-actions in
dependency order with the one to start pointed at. The same first sub-action is surfaced as a hint on
`flow next` whenever the slice is building, so "build" is no longer a dead end.

The ordering is a topological sort of the slice's own `depends_on` edges: a dependency on an earlier
slice's task is treated as already done, and a dependency outside the slice never pulls a foreign task
into the list. The reader adds no state and tracks no completion — it orders and points; the operator
decides when a sub-action is done. When there is no compiled task graph or the flow position cannot be
placed, it resolves to nothing with a reason rather than inventing a sub-action.

Coding work is still never a modeled transition — this is a read-only pointer over the plan, not a new
part of the delivery state machine.
