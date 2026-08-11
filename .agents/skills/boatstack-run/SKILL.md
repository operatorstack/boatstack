---
name: boatstack-run
description: Drive delivery to an open or updated pull request. Use only when the user explicitly selects this Boatstack operation.
---

# Boatstack Run

Select the `open-or-updated-pr` terminal. This trigger never grants merge authority. Provider authority remains a separate verified receipt.

Run `boatstack status --repo . --format json` once for observation. An authority-free
`FRONTIER` from status is diagnostic only and cannot terminate this selected operation.

Bind one command-scoped context containing the exact goal, delivery, repository,
worktree, flow, actor, and supplied authority receipts. Preserve that context
through every `next`, `apply`, `recover`, and re-resolution. Never synthesize missing
authority or infer it from authentication, files, branches, or prior conversation.
Within that context, track requested authority sources separately from currently
materialized authority receipts.

For this operation, request human and repository-policy authority sources. The
repository-policy source remains requested when configuration is
stale, uninitialized, or under recovery; do not pass `--repository-authority`
until the current configuration has exact verified fingerprint evidence.

After a complete `installation.initialize`, `configuration.initialize`, or recovery
receipt, re-observe the post-receipt state. If configuration is now verified,
make one bounded attempt for that receipt to materialize the retained source by
adding `--repository-authority` to the next resolution. The kernel must derive the
receipt from that exact verified fingerprint. Never derive repository authority
from file presence, authentication, or prior conversation. If the source remains
unverifiable, record it as conclusively rejected and fail closed; do not retry it
again for the same receipt.

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
