### Concurrent first use no longer trips a false "unsafe runtime" denial

When several tool calls hit an empty shared-runtime slot at the same time, one guard
hydrates the slot and the others must wait. The installer copies the helper before the
manifest, so for a short moment the slot holds the helper but not the manifest. A guard that
looked during that moment judged the runtime "unsafe or incomplete" and denied the tool call
by mistake. This showed up as a flaky CI failure on Linux under contention.

The guard now closes both sides of the race. First, it treats the slot as ready only when
both the helper and the manifest are present. So a guard that arrives during the gap joins
the hydrate lock instead of denying a half-written slot. Second, a waiting guard now waits
for the hydrating peer to release the clone-wide lock. The peer releases the lock only after
its installer finishes, so a released lock means the slot is complete. The guard then runs
the same checksum and safety gates, which still fail closed if hydration was disabled, timed
out, or failed. The fix applies to both the bash and PowerShell guards.

Three bounded conformance tests cover the fix. One runs many guards at once and confirms
that exactly one hydrates and every guard proceeds. Two deterministic tests drive a slow,
non-atomic peer — one from an empty slot and one from a half-written slot — and confirm the
guard waits for the peer instead of judging an incomplete slot.
