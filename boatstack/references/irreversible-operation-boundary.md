# Irreversible-operation boundary

The shared guard denies high-confidence destructive shell operations. Managed
Git push, PR mutation, and worktree removal are routed through registered
transitions while Boatstack is engaged.

External effects require preview, exact authority, idempotency, execution,
observation, and reconciliation. A request accepted by a provider is not proof
that the effect settled. Unknown settlement remains a recovery fact.

There is no in-session bypass. Use a registered cleanup, rollback, reconcile,
compensate, escalation, or abandonment transition with its declared authority.
