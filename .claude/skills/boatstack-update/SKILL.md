---
name: boatstack-update
description: Apply a checksum-verified Boatstack update. Use only when the user explicitly selects this Boatstack operation.
---

# Boatstack Update

Select `installation.update` or, after exact human acceptance of program drift, `installation.reconcile-update`. This trigger does not reclassify or advance a product delivery.

Run `boatstack status --repo . --format json` once for observation. An authority-free
`FRONTIER` from status is diagnostic only and cannot terminate this selected operation.

Bind one command-scoped context containing the exact goal, delivery, repository,
worktree, flow, actor, and supplied authority receipts. Preserve that context
through every `next`, `apply`, `recover`, and re-resolution. Never synthesize missing
authority or infer it from authentication, files, branches, or prior conversation.
Within that context, track requested authority sources separately from currently
materialized authority receipts.

For this operation, request only checksum-verified installation authority. Do not
request or materialize repository, provider, publication, product-delivery, or
merge authority. Installation receipts cannot be reused to broaden this scope.

If the candidate reports exact compiled-program drift, preserve the healthy old
launcher and present the prior program fingerprint, candidate program
fingerprint, and program-delta fingerprint. Do not accept the delta implicitly.
After explicit human acceptance, rerun the same checksum-bound update with
`--accept-program-change` so the Kernel uses the single atomic
`installation.reconcile-update` boundary. If the update has an interrupted local
transaction and `recovery.rollback` is permitted, carry the same human authority
through that rollback, preserve its complete receipt, and retry once from the
restored healthy old state. Never acquire repository authority to escape an
update recovery frontier.

Begin each cycle with an untargeted authority-bearing `next`. A `CANDIDATE`
identifies the next transition but is not permission to apply it: bind only its
declared parameters and re-resolve that exact transition. Apply only the stable
transition ID from the immediately preceding `PRESCRIBED` result and only its
declared parameters. Preserve the complete apply response and stderr, including
admission, receipt, postcondition, error, recovery, and transaction fields.
Re-resolve with the same context after every complete receipt.

Evaluate a frontier only after every requested authority source is materialized
or conclusively rejected against the post-receipt state. Stop only on an
authority-bearing `FRONTIER`, `BLOCKED`, `REFUSED`, or
`UNRESOLVED` result for this operation. Treat `TERMINAL` as exact goal evidence.
If recovery is active, use only a transition in `recovery_info.permitted` and
the exact transaction ID. Never choose maintenance, correction, abandonment,
merge, provider, or destructive authority as an escape from a frontier.
