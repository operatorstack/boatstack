### Concurrent first use hydrates the shared runtime exactly once

When several tool calls hit an empty shared-runtime slot at the same time, one guard hydrates
the slot and the others wait. A guard tests whether the slot is missing, then takes a
clone-wide lock to hydrate it. A slow guard could reach the lock only after the winner had
already finished and released it. Its test result was stale, so it took the lock and ran the
installer a second time.

The guard now re-tests the slot after it takes the lock and runs the installer only if the
slot is still missing or incomplete. So exactly one hydration happens under contention. This
also narrows the window where a peer is writing the helper while another guard starts it,
which complements the retry that already handles that case on Linux.
