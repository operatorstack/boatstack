### A mutation blocked by invalid delivery state now names the step that clears it

Invalid managed delivery state blocks all product mutation. This is deliberate: adding a delivery to
the ignore list quiets the status report, but it does not let corrupt state pass the mutation guard,
so bad state can never be laundered into a change. That protection stays.

What was missing was a way forward. The block reported an opaque verification error and did not say how
to unblock work, and the operator's ignore action did not help on this path. The step to run was not
named.

The block is now actionable. When invalid delivery state stops a mutation, the guard names
`discard-delivery` — the reachable step that archives the corrupt state reversibly and lets work
continue. It also separates a corrupt-state fault, which a step can clear, from a sensing fault such as
an unreadable store or a broken configuration, which is diagnosed by the read-only `doctor` because no
mutation step can repair a channel. The read-only status path keeps tolerating an ignored delivery; the
mutation path keeps failing closed — the same rule, each boundary in its own role.
