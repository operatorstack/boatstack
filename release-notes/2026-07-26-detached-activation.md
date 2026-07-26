### Detached Supervision is now something you can turn on

Detached Supervision lets Boatstack supervise a repository while keeping its controller state
outside that repository. Until now you could attach a repository, but nothing installed a
working guard, so a live coding session could not actually run under it. This change makes
detached mode operational.

Attaching a repository now also installs Boatstack's runtime into the external control root,
so the guard has a helper to run. A new pair of commands turns the guard on and off for your
coding agents: `boatstack-helper activate --repo .` merges a developer-level guard into each
agent's global configuration, and `boatstack-helper deactivate --repo .` removes it. The guard
enforces Boatstack only on repositories you have attached and does nothing on every other
repository you open. It preserves your existing hooks — installing adds only Boatstack's own
entry, removing takes only that entry away, and re-running either command changes nothing. Use
`activate --print` to review the exact per-agent configuration before it is written.

The install is host-neutral: cursor, claude, codex, and gemini all receive the same guard,
shaped for each agent's hook format.

This release also adds an end-to-end evaluation that stands up real repositories and drives
the built helper through the whole flow — attach, activate, guard, work, detach — proving at
every step that no Boatstack file lands in the target repository and that a developer's own
host configuration is never changed.
