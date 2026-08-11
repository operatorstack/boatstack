---
name: boatstack
description: Use when the user explicitly asks for Boatstack or when a V2 status query proves that the current exact worktree has an active managed delivery. Do not infer engagement from repository files, a saved plan, a branch name, or prior conversation.
---

# Boatstack V2

Boatstack owns delivery control. The coding agent owns implementation.

## Start with one read

Run:

```sh
boatstack status --repo . --format json
```

Use only the returned canonical snapshot and decision. Do not inspect or edit
machine state. If no goal is configured, ask for the target terminal:
`approved-plan`, `verified-implementation`, `open-or-updated-pr`,
`merged-delivery`, or `safely-abandoned`.

## Follow the prescription

- Request the stable transition ID returned by `next`.
- Carry the same goal ID, delivery ID, repository path, worktree, flow ID, and
  authority receipts.
- Execute only the typed parameters declared by `catalog`.
- Re-resolve after every receipt.
- Stop on `FRONTIER`, `BLOCKED`, `REFUSED`, or `UNRESOLVED`.
- Treat `TERMINAL` as exact goal evidence, not an agent completion claim.

Friendly CLI verbs are aliases only. The registry ID is authoritative. Use
`apply --transition <id>` or `recover --transition <id>` when an alias does
not exist.

## Authority

Human, autonomy, repository-policy, and provider authority are separate.
Repository authority comes from the current V2 configuration fingerprint.
External publication requires human or autonomy authority **and** an unexpired
provider receipt whose fingerprint binds the exact preview or correction body.
Never infer authority from authentication, path presence, prior approval, or a
successful command.

## Workspaces

`workspace.cut` transfers controller authority to the returned destination and
parks the source checkout. Continue only from that exact destination.
`workspace.cleanup` is legal only for proved landing or explicit abandonment;
it verifies completion from the preserved source checkout.

## Safety and recovery

Send shell commands to `boatstack guard --repo . --host <host> --command
<command>` when the host integration requests a guard decision. Never recreate
the destructive-command policy in prompt text.

If observation reports `RECOVERY`, use only a transition listed in
`recovery_info.permitted` and pass the exact transaction ID. Do not retry an
unknown external effect.

## Product evidence

Plans, approvals, gate evidence, visual manifests, and publication previews are
repository-owned inputs. Presence is never authority. Visual capture enters the
delivery model only through `evidence.visual.attach`. Retrospective analysis is
passive and cannot advance a delivery.

## Boundaries

Read [workflow.md](references/workflow.md) for the event families,
[artifacts.md](references/artifacts.md) for ownership, and
[host-hook-contracts.md](references/host-hook-contracts.md) for RPC integration.

Boatstack never authorizes merge. Do not merge unless the user separately and
explicitly asks.
