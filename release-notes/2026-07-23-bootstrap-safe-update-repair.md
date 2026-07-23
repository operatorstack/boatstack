### Repair an update without trusting the broken installation

Boatstack updates now download and verify the target helper before diagnosing the installed runtime. Exact stale hook and generated-state migrations repair automatically. Recoverable Boatstack-owned drift receives a fingerprinted `--repair` preview, a Git-common backup, and remains visible in the same update PR. User-owned state is preserved, and downgrades require separate `--allow-downgrade` authority.
