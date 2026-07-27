### `flow next` lists every legal move, not just the best one

A law states what is admissible; a smaller model cannot always derive a compliant command from that statement. Boatstack now compiles the position's law into its solution set: `flow next --json` and the status response carry the full list of admissible next commands — the prescribed primary plus `alternatives` — each an exact runnable line with its owed human inputs marked, ordered most-productive first. The agent picks a legal move instead of deriving one.

The set is computed, never hand-written: it enumerates the delivery registry's legal out-edges, the guard's own stage-admission tables, and the planning prescription layer. Because the guard and the enumerator now read the same declarations, a new conformance sweep can hold the whole loop closed: every command the tool presents is one the guard admits at the exact position that presented it — the tool never prescribes what it would deny.

Text renderings stay calm: one primary `Run:` line as before, plus a single `Also legal from here:` line naming up to three other moves. The full set rides in the structured output.
