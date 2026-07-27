### Every managed path now has a declared owner

Boatstack keeps state in several places — committed feature evidence under `.product-loop/`, per-worktree control state under the worktree's Git directory, clone-shared runtime slots and receipts under the Git common directory, and an external root for detached supervision. Until now that layout lived implicitly in path resolvers, guard patterns, and prose, and each partitioning defect (a clone-shared ledger, one worktree's state blocking another, a stale binary next to a fresh pin) was discovered by hitting it.

The layout is now a single declared registry: every managed tree names its class, its partition, the verbs that own it, and whether the guard protects it from raw mutation. A conformance suite holds the registry, the path resolvers, the guard's classifiers, and the exported ownership table in `artifacts.md` to each other — none of them can drift silently. A frozen inventory also pins which source files may hand-join managed paths, so new code is steered through the shared resolvers instead of new string literals.

The documentation was corrected along the way: operation receipts live under the current worktree's Git directory (`operations/v2`), not the old clone-shared location the docs still described.
