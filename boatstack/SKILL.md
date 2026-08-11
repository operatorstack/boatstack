---
name: boatstack
description: Use when the user explicitly asks for Boatstack or when a V2 status query proves that the current exact worktree has an active managed delivery. Do not infer engagement from repository files, a saved plan, a branch name, or prior conversation.
---

# Boatstack V2

Boatstack owns delivery control. The coding agent owns implementation.

## Select one Codex mode

Recognize these mode names case-insensitively. The legacy spelling
`auto-plan` remains an alias for `Autoplan`.

- `$boatstack Autoplan` selects the `approved-plan` terminal.
- `$boatstack Run` selects the `open-or-updated-pr` terminal. It never selects
  merge authority.
- `$boatstack Update` selects the checksum-verified `installation.update`
  path. It does not reclassify a product delivery.

For bare `$boatstack`, present exactly three choices: `Autoplan`, `Run`, and
`Update`. Selecting a choice is identical to invoking that mode directly.
Mode selection chooses intent and a target only. It does not approve unseen
plan bytes, supply external-provider authority, authorize merge, or broaden
repository policy.

## Observe once

Run:

```sh
boatstack status --repo . --format json
```

Use only the returned canonical snapshot and decision. Do not inspect or edit
machine state. `status` is observation only: an authority-free `FRONTIER` is a
diagnostic result, not the delivery verdict for an explicitly selected mode.
If no goal is configured, bind `Autoplan` or `Run` to a safe goal ID, delivery
ID, and the terminal selected above. `Update` preserves any configured product
goal and requests only `installation.update`.

## Bind authority and follow the prescription

After mode selection, create one command-scoped authority context containing
the exact goal ID, delivery ID, repository path, worktree, flow ID, actor, and
authority receipt paths actually supplied by the user or host. Do not synthesize
missing authority. Carry this same context through every `next`, `apply`,
`recover`, and re-resolution; never fall back to the authority-free status
decision.

- Request the stable transition ID returned by `next`.
- Execute only the typed parameters declared by `catalog`.
- Preserve the complete `apply` response and stderr before interpreting it,
  including admission, receipt, postcondition evidence, error, recovery, and
  transaction fields. Do not pipe away or truncate those fields.
- Re-resolve with the same authority context after every complete receipt.
- Stop on `FRONTIER`, `BLOCKED`, `REFUSED`, or `UNRESOLVED` only when that
  decision was produced by an authority-bearing resolution for the selected
  mode.
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
