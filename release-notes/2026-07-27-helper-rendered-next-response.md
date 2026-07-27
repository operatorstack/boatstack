### The next-step response is now rendered by the helper, not re-derived from prose

Asking Boatstack "what's next" always had a deterministic answer, but the user-facing response — the outcome line and the single next action — was described in the skill instructions as prose, and every agent re-derived it from a written state table on every turn. That table was one more place the guidance could drift from the machine.

`next-status --format response` now renders the canonical response contract directly: the branded banner, one plain-language outcome line, and exactly one "### Next step" block carrying the exact runnable command whenever one is prescribable — the identical command line `flow next` prints, produced by the same prescription layer, never a copy. The skill instructions now point at the renderer instead of restating the table, and a conformance suite pins the parity, the single-next-step shape, and the rule that machine stage names never leak into status prose.

This is the first slice of compiling the instruction layer down into the helper: where a rule can be rendered deterministically, the prose now defers to the rendering.
