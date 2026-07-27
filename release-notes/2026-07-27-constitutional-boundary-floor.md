### The destruction boundary is guaranteed to survive its own optimization

The guard now decides destruction by effect: it asks which executor runs a command, not whether a file
or a message spells a keyword. That change removed false positives — committing a migration, staging a
schema file, editing a note. This release records and enforces the limit of that optimization.

The rule that decides destruction is constitutional. It defines the real boundary — destroying a live
resource — and is never traded for convenience. No project setting disables it; a setting can only add
scope. The executor check that narrows when the rule is observed is an optimization: it exists to cut
false positives, and it may never let a live destructive command through. When the executor is live, the
rule still fires.

A conformance test now holds this floor. A live database client running a drop, a client running a
migration file, a recursive delete, an infrastructure destroy, a database reset, and a force push are
all still denied, while the same SQL sitting inert in a staged or committed file passes. The benefit of
the optimization does not become a leak, and the boundary cannot be optimized away.
