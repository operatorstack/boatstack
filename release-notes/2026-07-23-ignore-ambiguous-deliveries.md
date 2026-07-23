### Resolve delivery ambiguity for known past deliveries

Boatstack no longer hard-stops new work when historical published or active deliveries make the delivery state ambiguous. A new `workflow.ignored_deliveries` configuration field lists feature slugs to exclude from ambiguity resolution, and both `next` and `run` honor it deterministically.

New, un-ignored ambiguous deliveries still pause the workflow, so the safety interlock is preserved. A bounded `ignore-delivery --repo . --feature <slug>` helper appends a slug to the list after explicit user confirmation, persisting it through the standard configuration round-trip.
