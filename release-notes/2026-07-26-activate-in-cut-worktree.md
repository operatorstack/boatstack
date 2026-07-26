### A managed delivery activates inside its cut worktree, not on the base branch

In worktree mode, Boatstack cuts a fresh worktree for each feature and expects the build to
happen there. It suggested this but never enforced it: if you drove the flow from the
repository root — the main worktree, still on the base branch — after the worktree had been
cut, activation ran there anyway. That stranded the compiled task graph and plan lock on the
base branch and created a second, competing delivery record, split off from the one in the cut
worktree.

Activation now refuses to run in that situation. When workspace management is on in worktree
mode and a feature's workspace has already been cut, activating from the main worktree on the
base branch is blocked with a clear message: change into the cut worktree and run it there.
The guard is precise — it does nothing during the normal flow (which cuts the worktree first,
then activates inside it), when workspace management is off or in branch mode, or before a
workspace exists — so only the base-branch mistake is stopped. Delivery state stays
worktree-local as before; this simply keeps activation out of the wrong worktree.
