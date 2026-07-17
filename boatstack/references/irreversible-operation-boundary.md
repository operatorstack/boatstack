# Irreversible-operation boundary

Boatstack removes high-confidence irreversible external side effects from the coding agent's reachable action space. It does not restrict ordinary implementation choices.

## Immutable policy

The guard always denies:

- database or schema drops, truncation, resets, flushes, destructive downgrades, clean restores, and unbounded deletes or updates;
- recursive removal of repository, home, root, parent, or wildcard targets;
- destructive Git cleanup, hard resets, and forced remote-history replacement;
- cloud, project, database, cluster, namespace, or volume destruction;
- disabling recovery or deleting backups and snapshots.

There is no break-glass token or in-session override. Intentional destructive recovery belongs to a separately controlled operator surface outside Boatstack. Agents may edit source that describes a dangerous operation for review, but may not execute it; an operational diff containing that capability blocks build activation and subsequent gates until it is removed or transferred to the operator boundary.

## Failure response

After an external-write failure:

1. preserve the partial state;
2. use read-only inspection to establish the exact target and failure;
3. stop rather than widen credentials, targets, or authority;
4. retry only when the operation is transactional and retry-safe, otherwise fix forward;
5. record the failure and recovery evidence.

Planning declares each external side effect with its kind, immutable target identity, reversibility, failure policy, and `destructive: false`. Test evidence must independently prove target selection and transactional or fix-forward behavior.

## Defense in depth

Project hooks are deterministic interception, not a complete security sandbox. Host APIs can change, some tool surfaces may not expose hooks, and an agent can possess credentials broader than the repository intends. [Codex documents that current `PreToolUse` shell interception is incomplete](https://learn.chatgpt.com/docs/hooks); [Claude documents that command hooks run with the user's full permissions](https://code.claude.com/docs/en/hooks); Cursor documents pre-shell and pre-MCP interception but host enablement remains a separate trust boundary. Protected services still require least-privilege credentials, scoped roles, backups, and service-side approval for destructive administration. `doctor` verifies the generated fragments, launchers, helper version, and fail-closed smoke behavior; host trust or enablement remains an operator-visible assumption when the host does not expose that state.

## Evaluation status

This guard is a **PROPOSED** Move. Existing benchmark evidence supports deterministic protocol enforcement over stronger prompting, and a sanitized database incident establishes the target mechanism: failed external operation -> scope drift -> invented destructive recovery. The exact guard is not promoted until paired evaluation demonstrates zero destructive executions, retained safe diagnostics and transactional operations, bounded latency, no secret-bearing denial logs, and no workflow regression.
