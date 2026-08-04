# Configure Boatstack outcomes

<!--
boatstack-user-config-field:project.default_branch
boatstack-user-config-field:project.context
boatstack-user-config-field:project.commands
boatstack-user-config-field:project.high_risk_paths
boatstack-user-config-field:workflow.human_plan_approval
boatstack-user-config-field:workflow.independent_review_for_high_risk
boatstack-user-config-field:workflow.allow_pass_with_gaps
boatstack-user-config-field:workflow.maintain_changelog
boatstack-user-config-field:workflow.boundary_analysis
boatstack-user-config-field:workflow.pr_visual_evidence
boatstack-user-config-field:workflow.visual_evidence_publish.mode
boatstack-user-config-field:workflow.visual_evidence_publish.host
boatstack-user-config-field:workflow.visual_evidence_publish.expiry
boatstack-user-config-field:workflow.external_authority.mode
boatstack-user-config-field:workflow.external_authority.trust_store
boatstack-user-config-field:workflow.ignored_deliveries
boatstack-user-config-field:delivery.terminal
boatstack-user-config-field:insights.enabled
boatstack-user-config-field:insights.capture_mode
boatstack-user-config-field:insights.value_map
boatstack-user-config-field:insights.suggest_features
boatstack-user-config-field:insights.evaluate_on_pr
boatstack-user-config-field:insights.pending_frontier
boatstack-user-config-field:insights.completion_mode
boatstack-user-config-field:workspace.enabled
boatstack-user-config-field:workspace.mode
boatstack-user-config-field:workspace.cleanup
boatstack-user-config-field:workspace.cleanup_after
boatstack-user-config-field:workspace.reap
boatstack-user-config-field:adapters
-->

Boatstack's installer owns the complete `.boatstack-project.json` shape. Edit only the controls below, then regenerate the export and review the infrastructure diff. Fields not listed here are identity, compatibility, or installer state rather than product policy.

## Delivery readiness and journey evidence

New Boatstack plans use schema v3. Before approval is shown, Boatstack fetches
`origin` and verifies the current feature worktree, base/head commits, branch,
upstream relation, and journey-oracle manifest. Activation repeats the check and
stores the same readiness fingerprint in the immutable plan lock.

Each plan declares `journey_evidence`. Use `not_relevant` with a reason when no
user or operator journey can regress. Use `relevant` with typed runnable oracles
mapped to acceptance criteria when a journey matters. Relevant results are
recorded with `record-journey-results`; test and review gates reject missing,
failed, or stale results.

## Choose the outcome

| Outcome | Control | Enforcement |
|---|---|---|
| Use the correct base branch | `project.default_branch` | Boatstack uses it for freshness, PR, update, and workspace boundaries. |
| Give planning bounded durable context | `project.context` | The coding agent consults these paths when relevant; Boatstack does not load all of them automatically. |
| Advertise repository-owned checks | `project.commands` | The coding agent receives these commands. `test` is required by configuration validation. |
| Mark sensitive paths | `project.high_risk_paths` | Matching changed paths participate in safety and PR-risk classification. |
| Require human plan authorization | `workflow.human_plan_approval` | `true` requires a current fingerprinted human receipt; `false` creates a fingerprinted policy-activation lock without claiming human approval. |
| Require independent high-risk review | `workflow.independent_review_for_high_risk` | Matching diffs require a typed review receipt naming the reviewer and `human_peer` or `separate_agent` method. |
| Permit visible verification gaps | `workflow.allow_pass_with_gaps` | `false` rejects `PASS_WITH_GAPS` at delivery and PR gates; `true` retains the gaps as evidence. |
| Maintain reader-facing history | `workflow.maintain_changelog` | Managed delivery and Boatstack-prepared PRs require a categorized `CHANGELOG.md` entry. |
| Check for a systemic boundary | `workflow.boundary_analysis` | Planning guidance asks whether the request is a local symptom before scope expands. |
| Add frontend PR screenshots | `workflow.pr_visual_evidence` | `suggest` exposes missing screenshots as a gap; `require` blocks completed publication. A plan that approves visual scenarios lifts `suggest` to require semantics for that feature; `off` and a per-feature `not_relevant` decision (with a reason) are the escapes. Boatstack captures registered scenarios automatically during ship; per-surface harnesses register as `project.commands["visual:<surface>"]` (`capability-register --surface`) and scenarios select them with a `surface` field. |
| Render screenshots inline on a private PR | `workflow.visual_evidence_publish.*` | `mode: external-host` uploads the captured PNGs to an anonymous expiring host so the comment renders inline even on a private repo; opt-in, never automatic. |
| Require repository-only credentials | `workflow.external_authority.*` | `credential-enforced` blocks managed execution without a current external receipt signed by a configured Ed25519 issuer. Omission stays explicitly `HOOK_GUARDED`. |
| Ignore old ambiguous deliveries | `workflow.ignored_deliveries` | Listed feature slugs are excluded from delivery-ambiguity resolution so past work stops blocking new work; new, unlisted ambiguous deliveries still pause. |
| Pursue the PR to merge, not just to open | `delivery.terminal` | `merged` keeps the read-only flow advisors naming post-publish steps (watch checks, route corrections) until the PR is observed merged; the default `published` ends the flow when the PR is open, exactly as before. |
| Preserve and evaluate product insights | `insights.*` | Manual, fingerprint-bound captures and events become tracked `docs/insights/` diffs; PR evidence can update readiness, but only a human completes an insight. |
| Use fresh feature workspaces | `workspace.*` | Boatstack creates and cleans branches or linked worktrees under the selected policy. |
| Limit generated host surfaces | `adapters` | Export generates only the selected supported adapters. |

The distinction in the Enforcement column matters: context, commands, and boundary analysis guide the coding agent; approval, gap, review, changelog, workspace, adapter, and visual-evidence policies also have deterministic Boatstack checks.

## Project controls

```json
{
  "project": {
    "default_branch": "main",
    "context": ["README.md", "AGENTS.md", "docs/decisions/"],
    "commands": {
      "test": "npm test",
      "lint": "npm run lint",
      "typecheck": "npm run typecheck"
    },
    "high_risk_paths": ["migrations/**", "auth/**", "billing/**"]
  }
}
```

`context` is a bounded discovery hint, not a request to scan every path. Command names other than `test` are optional and become available to the coding agent under their chosen names.

## Workflow controls

```json
{
  "workflow": {
    "human_plan_approval": true,
    "independent_review_for_high_risk": true,
    "allow_pass_with_gaps": false
  }
}
```

When human approval is disabled, Boatstack still locks the exact plan and inputs using `authorization_mode: policy`. For high-risk review, the review gate records reviewer provenance; this is an auditable claim, not cryptographic identity proof.

```json
{
  "workflow": {
    "maintain_changelog": true,
    "boundary_analysis": true
  }
}
```

Changelog enforcement is mechanical. Boundary analysis is model-mediated planning guidance and cannot silently expand approved scope.

```json
{
  "workflow": {
    "external_authority": {
      "mode": "credential-enforced",
      "trust_store": "/etc/boatstack/authority-issuers.json"
    }
  }
}
```

Strict mode requires an external service-IAM, credential-broker, or isolated-host attestor. The JSON trust store maps issuer IDs to base64 Ed25519 public keys and must be operator-owned outside the repository; Boatstack rejects a file or parent directory owned or writable by the managed principal. Obtain the expected binding with `boatstack-helper authority-context --repo .`; the attestor signs a receipt for that repository, worktree, host session, principal, and a maximum 15-minute lifetime. Set the absolute receipt path in `BOATSTACK_AUTHORITY_RECEIPT`, the attested session in `BOATSTACK_HOST_SESSION`, and the attested principal fingerprint in `BOATSTACK_PRINCIPAL_FINGERPRINT`. Boatstack never holds the signing key. Missing or invalid evidence blocks `run-preflight` and remains `HOOK_GUARDED`; only a valid external receipt reports `CREDENTIAL_ENFORCED`.

```json
{
  "workflow": {
    "pr_visual_evidence": "suggest"
  }
}
```

Visual-evidence values are `off`, `suggest`, and `require`. Screenshot bytes stay outside Git history until explicitly attached to the PR.

By default Boatstack can publish those screenshots inline only for a **public** repository (it commits the bytes to a Boatstack-owned public branch and renders them from an immutable raw URL). On a **private** repository GitHub cannot fetch those bytes for the comment, so it falls back to manual attachment. To render inline on a private repository, opt into the external-host mode:

```json
{
  "workflow": {
    "visual_evidence_publish": {
      "mode": "external-host",
      "host": "litterbox",
      "expiry": "72h"
    }
  }
}
```

`mode: external-host` uploads the exact captured PNG bytes to an anonymous host (`litterbox`, which auto-expires uploads after `expiry` — one of `1h`, `12h`, `24h`, `72h`; or `catbox`, permanent) and posts the returned URLs inline. It is **never automatic** — only this explicit value turns it on — because the bytes leave your repository to a third party. The PR comment carries a standing reminder naming the host and expiry, so do not use this mode for sensitive screenshots.

```json
{
  "workflow": {
    "ignored_deliveries": ["old-feature-slug", "another-past-feature"]
  }
}
```

List feature slugs here to drop past deliveries from the ambiguity check so historical work no longer blocks new work. Any new, unlisted ambiguous delivery still pauses the workflow.

## Workspace and adapter controls

```json
{
  "workspace": {
    "enabled": true,
    "mode": "worktree",
    "cleanup": "confirm",
    "cleanup_after": "merge",
    "reap": "confirm"
  },
  "adapters": ["cursor", "claude", "codex", "github"]
}
```

Workspace `mode` is `worktree` or `branch`; cleanup is `confirm`, `auto`, or `off`; and cleanup eligibility begins after `merge` or `ship`. `reap` is `confirm`, `auto`, or `off`: when a delivery's PR is confirmed merged, Boatstack sweeps every terminal (merged or abandoned) Boatstack workspace at once — `confirm` asks the operator once before reclaiming them, `auto` reclaims without asking, and `off` disables the sweep. Supported adapters are `cursor`, `claude`, `codex`, `gemini`, and `github`. Empty or omitted adapters enable all supported surfaces.

## Delivery goal

```json
{
  "delivery": {
    "terminal": "merged"
  }
}
```

`delivery.terminal` names the state a delivery pursues before the flow reports nothing left to do. The default `published` ends the flow when the slice's pull request is open, exactly as before. `merged` keeps the read-only flow advisors (`next-status`, `flow next`, `flow frontier`, `flow watch`) naming post-publish steps — watch the checks, route a correction, surface merge eligibility — until the pull request is observed merged. The goal a delivery starts under is snapshotted with the delivery, so changing this value never changes an in-progress delivery's goal. Boatstack itself never merges a pull request under any setting.

## Independent insight controls

```json
{
  "insights": {
    "enabled": true,
    "capture_mode": "manual",
    "value_map": "required",
    "suggest_features": true,
    "evaluate_on_pr": true,
    "pending_frontier": true,
    "completion_mode": "human_confirmed"
  }
}
```

Every save follows a complete Value Map preview, a warning that the exact content will enter Git history, and a separate confirmation bound to that preview. Captures, their human-readable projections, and append-only events live below `docs/insights/<id>/`. Each mutation is therefore a reviewable repository diff. Boatstack stores no insight content in detached or Git control state. Topic suggestions do not create deliveries. PR publication or terminal observation may append evaluation evidence, but never completes an insight. The insight frontier remains separate from Boatstack's authoritative delivery next action.

## Installer-owned fields

The installer maintains `schema_version`, `project.name`, and integration records. Select gstack or Spec Kit through installation and update flows. Their `requested`, `status`, `version`, and `detail` values are receipts and provenance, not hand-edited workflow switches.

For serialization, defaults, migration, and installer compatibility details, see the generated internal configuration schema in `.product-loop/config-schema.md`.
