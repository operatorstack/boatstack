### One worktree no longer blocks Boatstack work in another worktree

Boatstack keeps an operation ledger. The ledger records each mutation, such as a version
update or a PR publish. The ledger used to live in the shared Git common directory. Every
worktree in the clone wrote to the same ledger.

This caused false blocks across worktrees. An update that succeeded in one worktree left a
receipt. Running the same-version update from another worktree found that receipt, compared a
different worktree scope, and denied the command with "existing operation identity does not
match the prepared package". The shared ledger also grew without bound, because it collected
every worktree's receipts.

The operation ledger is now per-worktree. It lives under each worktree's own Git directory,
next to the delivery state that is already per-worktree. One worktree no longer sees or
blocks another worktree's operations. Two worktrees can run the same-version update at the
same time; each keeps its own receipt. Repair backups and their receipt move per-worktree
for the same reason, so two worktrees repairing the same version cannot overwrite each
other's restore point.

The shared runtime binary and the staged update package stay shared and version-namespaced.
They are immutable content, so sharing them across worktrees is safe and saves disk. Git
already forbids the same branch in two worktrees, which keeps shared per-version state and
remote PRs free of contention.

This is a deliberate break from the previous shared-and-serialized ledger. The ledger path
moves to a new version, so the old shared ledger is orphaned cleanly for every worktree,
including the main worktree. Doctor removes the orphaned ledger as best-effort hygiene. Old
Boatstack versions keep using their own installed binary and are not affected. A removed
worktree now takes its operation state with it.
