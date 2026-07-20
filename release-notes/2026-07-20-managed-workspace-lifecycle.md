### Start and finish features on a clean workspace automatically

Boatstack can now manage the working area for each feature so you never build on a
stale branch or leave old worktrees behind. When enabled, it cuts a fresh branch
(or worktree) from the up-to-date default branch as a feature begins, and after
the feature's pull request has merged it offers to reclaim that worktree and
branch — you reply `c` to clean up or `k` to keep. Cleanup only removes local
work that has already landed: it never touches a remote branch, never merges
anything, and never discards uncommitted or unmerged changes without an explicit
override. The behavior is configured under `workspace` in your project file
(`enabled`, `mode`, `cleanup`, `cleanup_after`) and is off for any project that
does not opt in.
