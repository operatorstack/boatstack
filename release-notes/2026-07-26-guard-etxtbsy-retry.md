### Concurrent first use no longer fails with "Text file busy" on Linux

When several tool calls hit an empty shared-runtime slot at the same time, one guard
hydrates the slot and the others wait. On Linux the kernel refuses to run a file while
another process still holds it open for writing. It reports this as "Text file busy". A
waiting guard could reach the run step in that brief window, fail to start the helper, and
deny the tool call by mistake. This showed up as a flaky Linux CI failure under contention.
macOS and Windows do not enforce this rule, so only Linux saw the denial.

The guard now retries the helper a bounded number of times when the start fails with this
exact condition, then hands off as before. The retry is short and self-clearing: the peer
closes the file the moment its write finishes, so the next attempt starts the helper. A
helper that genuinely cannot run still returns the same status after the retries, so no real
failure is hidden. The runtime binary is still written atomically, so the fix only closes
the read-side race.
