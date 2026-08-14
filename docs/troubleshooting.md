# Troubleshooting Boatstack

## Doctor reports configuration drift

Run:

```sh
boatstack doctor --repo . --format json
```

Do not edit machine state. Compare `.boatstack/project.json` with the intended
schema-2 file and request `configuration.mutate` with its exact SHA-256.
Malformed or unsupported configuration is not treated as verified.

## Runtime is absent, stale, or wrong

The launcher reports pre-runtime failures before it loads a Flow or creates
managed state. With `--format json`, the diagnostic uses the versioned
`boatstack-bootstrap-diagnostic` envelope. Text mode prints the same stable
code, exact pinned version, and SHA-256.

Use the checksum-verifying installer in `hydrate` mode. It durably restores the
exact version-and-digest runtime and launcher without changing repository or
controller state.

```sh
BOATSTACK_MODE=hydrate BOATSTACK_VERSION=<exact-runtime-tag> \
  BOATSTACK_EXPECTED_RUNTIME_SHA256=<exact-pinned-sha256> \
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/<installer-tag>/install.sh)"
```

The launcher supplies all three exact values. The installer tag identifies the
current launcher release that supports hydration; it may restore an older
pinned runtime tag. The explicit SHA-256 must still match the repository pin.

Display this command and obtain explicit approval before running it. Generated
Flow skills never run or authorize installation. If the `boatstack` launcher
itself is absent, they read the committed `.boatstack/runtime.json`, report
`BOATSTACK_LAUNCHER_NOT_FOUND`, and stop. An absent or invalid pin requires a
maintainer; never substitute `latest`.

Do not copy a helper from another worktree or select a “latest” cache slot. A
missing pinned runtime fails closed; reinstall that exact release and checksum.

## A transition is FRONTIER

The transition is legal but its required authority is missing. Supply a current
human, autonomy, repository-policy, or provider receipt as named. Provider
authority is mandatory in addition to human/autonomy for external publication.

## A transition is REFUSED

The requested transition does not match the current source predicate or objective.
Run `status --format json` and `next --format json`. Do not edit state files or
retry through another host.

## The snapshot is in RECOVERY

Run `status --format json` to read the exact transaction ID and permitted
recovery transitions. Use `recover --transition <id> --param
transaction_id=<id>` with the required authority. Generic compensation refuses
unknown external effects; use publication reconciliation or escalation.

## A workspace cut is refused

The destination must not exist. Its parent must resolve canonically. The branch
must be valid, and `base_ref` must contain the exact verified
`.boatstack/project.json`. This prevents a new worktree from starting with
stale or absent policy.

## Cleanup is refused

Boatstack preserves active, unpublished, closed-unmerged, ambiguous, and
unresolved workspaces. Observe the PR, prove merged landing, or explicitly
abandon the delivery. The preserved source checkout must still have its original
worktree identity and ref.

## A host disagrees with the CLI

Send the same schema-2 request to `boatstack rpc`. Hosts may change rendering,
but they may not change transition IDs, authority, source predicates, or target
postconditions. A parity failure is a product defect.
