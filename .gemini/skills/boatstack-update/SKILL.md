---
name: boatstack-update
description: Apply a checksum-verified Boatstack update. Use only when the user explicitly selects this Boatstack operation.
---

# Boatstack Update

Select the `installation.update` transition. This trigger does not reclassify or advance a product delivery.

Run `boatstack status --repo . --format json` once for observation. An authority-free
`FRONTIER` from status is diagnostic only and cannot terminate this selected operation.

Bind one command-scoped context containing the exact goal, delivery, repository,
worktree, flow, actor, and supplied authority receipts. Preserve that context
through every `next`, `apply`, `recover`, and re-resolution. Never synthesize missing
authority or infer it from authentication, files, branches, or prior conversation.

Begin each cycle with an untargeted authority-bearing `next`. Apply only the
stable transition ID from the immediately preceding prescription and only its
declared parameters. Preserve the complete apply response and stderr, including
admission, receipt, postcondition, error, recovery, and transaction fields.
Re-resolve with the same context after every complete receipt.

Stop only on an authority-bearing `FRONTIER`, `BLOCKED`, `REFUSED`, or
`UNRESOLVED` result for this operation. Treat `TERMINAL` as exact goal evidence.
If recovery is active, use only a transition in `recovery_info.permitted` and
the exact transaction ID. Never choose maintenance, correction, abandonment,
merge, provider, or destructive authority as an escape from a frontier.
