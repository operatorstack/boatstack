# Troubleshooting Boatstack V2

## Doctor reports configuration drift

Run:

```sh
boatstack doctor --repo . --format json
```

Do not edit machine state. Compare `.boatstack/project.json` with the intended
schema-2 file and request `configuration.mutate` with its exact SHA-256.
Malformed or unsupported configuration is not treated as verified.

## Runtime is absent, stale, or wrong

Use the checksum-verifying installer in update mode. It durably installs the
exact version-and-digest runtime candidate, requests `installation.update`, and
changes the repository runtime pin only after the kernel verifies the
candidate.

```sh
BOATSTACK_MODE=update BOATSTACK_VERSION=<tag> \
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
```

Do not copy a helper from another worktree or select a “latest” cache slot. A
missing pinned runtime fails closed; reinstall that exact release and checksum.

## A transition is FRONTIER

The transition is legal but its required authority is missing. Supply a current
human, autonomy, repository-policy, or provider receipt as named. Provider
authority is mandatory in addition to human/autonomy for external publication.

## A transition is REFUSED

The requested transition does not match the current source predicate or goal.
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
