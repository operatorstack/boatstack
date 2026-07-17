<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# Irreversible-operation safety

Boatstack leaves implementation open while removing high-confidence irreversible side effects from the coding agent's reachable action space.

## What is always denied

- database/schema drop, truncate, reset, flush, destructive downgrade, clean restore, and unbounded delete/update;
- broad recursive deletion of repository, home, root, parent, or wildcard targets;
- destructive Git cleanup, hard reset, and forced remote-history replacement;
- cloud, project, database, cluster, namespace, or volume destruction;
- disabling recovery or deleting backups and snapshots.

There is no break-glass token or approval phrase. Intentional destructive recovery is performed by a human through a separately controlled operator surface outside Boatstack.

## What happens after a failed external write

Boatstack preserves the partial state, allows bounded read-only diagnosis, and blocks authority or target broadening. A retry must be proven transactional and retry-safe; otherwise the plan fixes forward. Planning and evidence identify the immutable target, affected paths, reversibility, failure policy, and an independent safety oracle.

Source may describe dangerous behavior for review. That is not execution permission: invoking such a repository script is denied, and an operational diff containing the capability blocks later gates until it is removed or transferred to the operator boundary.

## How installation enforces it

The installer merges only Boatstack-owned fragments into the repository's Cursor, Claude, and Codex hook configuration and preserves unrelated settings. Portable Bash and PowerShell launchers call the verified repository-local helper. Missing helpers, malformed events, drift, and ambiguous hook collisions fail closed. `doctor` checks fragment integrity, launchers, helper version, and safe/deny smoke behavior.

Host trust or enablement may not be machine-inspectable. Hooks are therefore defense in depth rather than a complete sandbox. [Codex documents incomplete interception for some shell paths](https://learn.chatgpt.com/docs/hooks), [Claude notes that command hooks run with the user's full permissions](https://code.claude.com/docs/en/hooks), and Cursor's pre-shell/pre-MCP hooks still depend on the host loading the project configuration. Protected systems still need least-privilege credentials, scoped service roles, backups, and service-side approval for destructive administration.

## Evidence status

This is a **PROPOSED** Move, not a claim of experimental proof. Existing benchmark results support deterministic enforcement over stronger prompting, and a sanitized partial-schema incident establishes the target failure mechanism. Promotion requires paired evaluation against the unguarded baseline: zero destructive executions, safe diagnostics and transactional operations retained, bounded latency, no secret-bearing denial logs, and no existing-workflow regression.
