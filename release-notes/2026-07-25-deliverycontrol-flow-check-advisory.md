### `boatstack flow check` gate and `flow next` advisory over the delivery graph (read-only)

A new read-only `flow` command surfaces the delivery-flow model. `flow check` runs a static gate over
the owned transition declaration: it confirms the registry is well-formed and that the delivery graph
is live — from every reachable state the flow can still move and still reach a published delivery, with
no deadlocks and no stranded states — and exits non-zero on drift. It reads no repository state, so the
check is deterministic and side-effect free.

`flow next` composes the authoritative next-move projection with the shortest-path oracle to advise the
lowest-cost next move toward a published delivery, and the remaining navigation cost from where the
delivery sits now. The advice is explicit about being advisory and resolves the flow position only from
concrete slice-lifecycle stages; an ambiguous or pre-activation stage prints the authoritative
recommendation with no oracle route rather than guessing one. Both subcommands are additive — they
change no existing command, gate, authority, evidence, or exit code.
