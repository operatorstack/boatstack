# Boatstack safety

## Evidence status

Known, absent, unknown, stale, ambiguous, and conflicting are separate fact
states. Missing or uncertain evidence never grants permission to delete,
publish, overwrite, approve, or advance.

## Exact admission

Each managed effect binds the snapshot fingerprint, invocation, objective,
transition, parameters, authority receipts, configuration evidence, and expiry.
The engine re-observes under the effect lock. Drift rejects the admission before
mutation.

## Transaction boundary

Local writes stage exact prior and target bytes. The authoritative state installs
last. A fresh observation must satisfy the transition's executable target
predicate before a receipt is committed. Failure restores prior bytes when
reversible or exposes a typed recovery transaction after restart.

External effects use preview, authority, execute, observe, and reconcile.
Unknown settlement is preserved as unknown and is never blindly retried.

## Hook guard

Every host submits raw command text to the same `guard` query. One classifier
reduces it to an allowlisted intent fingerprint; raw text is not recorded.

- high-confidence destructive commands are denied;
- direct push, PR mutation, and worktree removal are routed to their registered
  transition while Boatstack is engaged;
- ordinary repository commands remain allowed;
- non-destructive direct work remains inert when Boatstack is dormant.

The guard is defense in depth, not a sandbox. Keep least-privilege credentials
and provider-side protections.

## Cleanup and merge

Git ancestry does not prove publication. Cleanup needs durable merged evidence
and a landed workspace, or explicit abandonment. It validates the preserved
source checkout before deleting the destination and verifies the resulting
terminal from that source.

Boatstack never grants merge authority.
