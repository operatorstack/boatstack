# Irreversible-operation boundary

Boatstack removes high-confidence irreversible external side effects from the coding agent's reachable action space. It does not restrict ordinary implementation choices.

## Immutable policy

The guard always denies:

- database or schema drops, truncation, resets, flushes, destructive downgrades, clean restores, and unbounded deletes or updates;
- recursive removal of repository, home, root, parent, or wildcard targets;
- destructive Git cleanup, hard resets, and forced remote-history replacement;
- cloud, project, database, cluster, namespace, or volume destruction;
- Supabase branch deletion and lifecycle weakening that makes a protected branch deletable;
- public or unauthenticated service exposure through supported cloud and cluster control planes;
- disabling recovery or deleting backups and snapshots.

There is no break-glass token or in-session override. Intentional destructive recovery belongs to a separately controlled operator surface outside Boatstack. Agents may edit source that describes a dangerous operation for review, but may not execute it; an operational diff containing that capability blocks build activation and subsequent gates until it is removed or transferred to the operator boundary.

Recoverable repository alignment is not an exception to this policy. Raw `git reset --hard`, `git clean`, and forced history replacement remain denied. The project-local `workspace-sync` helper may align one exact local branch to one freshly fetched remote branch only after it creates and verifies Git recovery refs for the original branch and any staged, unstaged, or untracked work. It blocks active managed-delivery branches and reports the retained recovery refs.

## Failure response

After an external-write failure:

1. preserve the partial state;
2. use read-only inspection to establish the exact target and failure;
3. stop rather than widen credentials, targets, or authority;
4. retry only when the operation is transactional and retry-safe, otherwise fix forward;
5. record the failure and recovery evidence.

Planning declares each external side effect with its kind, immutable target identity, reversibility, failure policy, and `destructive: false`. Test evidence must independently prove target selection and transactional or fix-forward behavior.

## Defense in depth

Project hooks are deterministic interception, not a complete security sandbox. Host APIs can change, some tool surfaces may not expose hooks, and an agent can possess credentials broader than the repository intends. [Codex requires project-local hooks and their exact definitions to be trusted](https://learn.chatgpt.com/docs/hooks); [Claude documents that command hooks run with the user's full permissions](https://code.claude.com/docs/en/hooks); Cursor documents pre-shell and pre-MCP interception but host enablement remains a separate trust boundary, and a current fast-exit race can drop hook output. Protected services still require least-privilege credentials, scoped roles, backups, and service-side approval for destructive administration. `doctor` verifies generated contracts, launchers, helper version, and fail-closed smoke behavior, then reports host activation as an operator verification step rather than claiming that repository structure proves the host actually loaded the hook.

Managed-run preflight reports this distinction directly. `HOOK_GUARDED` means the deterministic hook blocks recognized unsafe effects, but ambient cloud authority is not proven absent. `CREDENTIAL_ENFORCED` requires `workflow.external_authority.mode: "credential-enforced"`, an operator-provisioned trust store outside the managed principal's writable boundary, and a short-lived Ed25519-signed receipt from service IAM, a credential broker, or an isolated host. The receipt binds the repository, worktree, host session, principal, issuer, enforcement mechanism, and expiry, and must attest `repository-only` authority with no cloud control-plane capability. Missing, stale, mismatched, overprivileged, self-authored, or invalidly signed receipts block the run before delivery mutation.

The external attestor obtains the expected repository and worktree fingerprints from `.product-loop/boatstack authority-context --repo .`. The managed host supplies the absolute receipt path in `BOATSTACK_AUTHORITY_RECEIPT`, the session binding in `BOATSTACK_HOST_SESSION`, and the attested principal fingerprint in `BOATSTACK_PRINCIPAL_FINGERPRINT`. These coordinates are bindings, not credentials, and must contain no secret material.

The receipt is strict JSON with `schema_version: 1`, the two context fingerprints, `host_session`, `principal_fingerprint`, `authority_class: "repository-only"`, `cloud_control_plane_authority: false`, `enforced_by` (`service-iam`, `credential-broker`, or `isolated-host`), `issuer`, RFC 3339 `issued_at` and `expires_at`, and a base64 Ed25519 `signature`. Its maximum lifetime is 15 minutes. The signing payload is the compact JSON returned by `AuthorityReceiptSigningBytes` with `signature` set to the empty string; unknown or duplicate fields are rejected.

## Evaluation status

This guard is a **PROPOSED** Move. Existing benchmark evidence supports deterministic protocol enforcement over stronger prompting, and a sanitized database incident establishes the target mechanism: failed external operation -> scope drift -> invented destructive recovery. The exact guard is not promoted until paired evaluation demonstrates zero destructive executions, retained safe diagnostics and transactional operations, bounded latency, no secret-bearing denial logs, and no workflow regression.
