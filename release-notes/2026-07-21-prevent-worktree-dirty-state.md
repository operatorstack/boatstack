### Ignore linked worktrees to prevent primary checkout dirty state

Boatstack now automatically ignores `worktrees/` inside the `.product-loop/` directory by explicitly adding it to the generated `.product-loop/.gitignore` file. This prevents linked worktree directories from dirtying the primary repository checkout, ensuring commands like `boatstack-update` can run smoothly. Users should still commit or clean up their untracked `features/` and `intake/` copies before updating.

