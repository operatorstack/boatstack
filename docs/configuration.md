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
boatstack-user-config-field:workflow.ignored_deliveries
boatstack-user-config-field:workspace.enabled
boatstack-user-config-field:workspace.mode
boatstack-user-config-field:workspace.cleanup
boatstack-user-config-field:workspace.cleanup_after
boatstack-user-config-field:adapters
-->

Boatstack's installer owns the complete `.boatstack-project.json` shape. Edit only the controls below, then regenerate the export and review the infrastructure diff. Fields not listed here are identity, compatibility, or installer state rather than product policy.

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
| Add frontend PR screenshots | `workflow.pr_visual_evidence` | `suggest` exposes missing screenshots as a gap; `require` blocks completed publication. |
| Render screenshots inline on a private PR | `workflow.visual_evidence_publish.*` | `mode: external-host` uploads the captured PNGs to an anonymous expiring host so the comment renders inline even on a private repo; opt-in, never automatic. |
| Ignore old ambiguous deliveries | `workflow.ignored_deliveries` | Listed feature slugs are excluded from delivery-ambiguity resolution so past work stops blocking new work; new, unlisted ambiguous deliveries still pause. |
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
    "cleanup_after": "merge"
  },
  "adapters": ["cursor", "claude", "codex", "github"]
}
```

Workspace `mode` is `worktree` or `branch`; cleanup is `confirm`, `auto`, or `off`; and cleanup eligibility begins after `merge` or `ship`. Supported adapters are `cursor`, `claude`, `codex`, `gemini`, and `github`. Empty or omitted adapters enable all supported surfaces.

## Installer-owned fields

The installer maintains `schema_version`, `project.name`, and integration records. Select gstack or Spec Kit through installation and update flows. Their `requested`, `status`, `version`, and `detail` values are receipts and provenance, not hand-edited workflow switches.

For serialization, defaults, migration, and installer compatibility details, see the generated internal configuration schema in `.product-loop/config-schema.md`.
