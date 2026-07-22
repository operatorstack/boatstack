### Resume side effects without duplicate retries

Boatstack now supervises managed mutations and publication attempts with durable,
fingerprinted operation receipts shared across linked worktrees. Host pre/post tool
events create and complete single-use leases; interrupted or unknown external
effects require reconciliation before a bounded retry. PR and Boatstack-update
publication recover an already-created pull request instead of opening a duplicate,
and `boatstack-run` uses the delivery's persistent three-cycle repair budget across
chats, host restarts, and async notifications. Human plan and publication approvals
remain required under their existing defaults. Machine-parsed subprocess results now
use stdout only; bounded stderr diagnostics can never become paths, refs, URLs, or
workflow authority.
