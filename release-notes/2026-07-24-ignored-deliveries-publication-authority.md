### Publication authority honors ignored deliveries

Publication-authority resolution now excludes `workflow.ignored_deliveries` when deciding whether a push or PR mutation is ambiguous, matching the behavior `next` and `run` already had. Previously a stale-but-active ignored delivery — for example an approved plan lock whose code shipped out of band, leaving zero delivery progress — still counted toward ambiguity and denied every unrelated push with `relation=ambiguous`, even though the slug was explicitly listed as ignored.

The safety interlock is unchanged for genuinely ambiguous work: a new, un-ignored active delivery that does not match the current branch still blocks publication. A configuration that fails to load leaves the delivery set unfiltered, preserving the prior conservative behavior.
