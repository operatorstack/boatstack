### One prompt clears merged worktrees at the merge checkpoint

Boatstack cuts a fresh worktree and branch for each managed delivery. After a pull request
merged, that worktree and branch used to stay behind, so a fast loop left a growing pile of
finished workspaces to prune by hand — and the safety guard denied the raw deletion commands
used to prune them.

When a delivery's pull request is confirmed merged, Boatstack now offers to sweep the whole
backlog at once. It reclaims every terminal Boatstack workspace — each one whose branch is
confirmed merged (through the GitHub CLI, with a local-ancestry fallback) or explicitly
abandoned by listing its feature in `workflow.ignored_deliveries`. It never reclaims a
workspace with an open or unknown-state pull request, a worktree Boatstack did not create,
the base branch, the worktree you are standing in, or one holding uncommitted or unmerged
work unless you force it. It reclaims only the local worktree and branch; it never deletes a
remote branch or merges anything.

A new setting, `workspace.reap`, tunes the prompt: `confirm` (the default) asks once when
reclaimable workspaces exist, `auto` reclaims them without asking, and `off` disables the
sweep. The removal runs through Boatstack's own `workspace-reap` operation, so it is no
longer denied as filesystem or Git destruction; a denied raw deletion of a Boatstack
worktree now points you at `workspace-reap` instead.

Reaping keys only on merge or abandonment and inspects worktrees through Git, never through
another worktree's private delivery state, so per-worktree isolation is preserved. Nothing
changes for a repository that leaves `workspace.reap` at its default until a delivery merges.
