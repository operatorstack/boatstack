# Configure Boatstack

<!--
boatstack-config-field:schema_version
boatstack-config-field:project
boatstack-config-field:project.name
boatstack-config-field:project.default_branch
boatstack-config-field:project.context
boatstack-config-field:project.commands
boatstack-config-field:project.high_risk_paths
boatstack-config-field:workflow
boatstack-config-field:workflow.human_plan_approval
boatstack-config-field:workflow.independent_review_for_high_risk
boatstack-config-field:workflow.allow_pass_with_gaps
boatstack-config-field:workflow.maintain_changelog
boatstack-config-field:workflow.boundary_analysis
boatstack-config-field:workflow.pr_visual_evidence
boatstack-config-field:workspace
boatstack-config-field:workspace.enabled
boatstack-config-field:workspace.mode
boatstack-config-field:workspace.cleanup
boatstack-config-field:workspace.cleanup_after
boatstack-config-field:adapters
boatstack-config-field:integrations
boatstack-config-field:integrations.*.requested
boatstack-config-field:integrations.*.status
boatstack-config-field:integrations.*.version
boatstack-config-field:integrations.*.detail
-->

Boatstack keeps delivery policy in `.boatstack-project.json` so the same project rules apply when the coding agent, model, session, or worktree changes. Start with the outcome you want, then set only the policies your repository needs.

## Choose the outcome

| If you want to… | Configure… | What changes |
|---|---|---|
| Run the right project checks | `project.commands` | Boatstack uses repository-owned commands instead of inventing validation. `test` is required. |
| Give planning durable project context | `project.context` | Planning can find the named documents and directories without scanning the whole repository. |
| Treat selected files as higher risk | `project.high_risk_paths` and `workflow.independent_review_for_high_risk` | Changes matching those globs require the configured independent review boundary. |
| Require a person to approve plans | `workflow.human_plan_approval` | Build waits for an explicit approval receipt. |
| Allow a gate to pass with recorded gaps | `workflow.allow_pass_with_gaps` | A gate may report a pass with visible, retained gaps instead of requiring a gap-free result. |
| Keep reader-facing release history | `workflow.maintain_changelog` | Every managed delivery slice and Boatstack-prepared ad-hoc PR must update `CHANGELOG.md`. |
| Look for a missing systemic boundary | `workflow.boundary_analysis` | Planning checks whether the request is a local symptom and asks before expanding it into boundary work. |
| Attach fresh screenshots to frontend PRs | `workflow.pr_visual_evidence` | Boatstack structures visual review evidence without adding media or frontend tooling to Git. |
| Start features in fresh Git workspaces | `workspace` | Boatstack can create a branch or linked worktree and manage local cleanup under the selected policy. |
| Limit generated host adapters | `adapters` | Only the named Cursor, Claude Code, Codex, Gemini CLI, or GitHub surfaces are exported. |
| Add supported specialist workflows | `integrations` | The installer records whether gstack or Spec Kit was requested and its installed state. |

Changing configuration is an infrastructure change. Regenerate the Boatstack export and review the resulting diff through the repository's normal change process.

## Complete example

JSON does not support comments, so the explanations follow the example.

```json
{
  "schema_version": 1,
  "project": {
    "name": "example-product",
    "default_branch": "main",
    "context": [
      "README.md",
      "AGENTS.md",
      "docs/architecture/",
      "docs/decisions/"
    ],
    "commands": {
      "build": "npm run build",
      "lint": "npm run lint",
      "test": "npm test",
      "typecheck": "npm run typecheck"
    },
    "high_risk_paths": [
      "migrations/**",
      "auth/**",
      "billing/**"
    ]
  },
  "workflow": {
    "human_plan_approval": true,
    "independent_review_for_high_risk": true,
    "allow_pass_with_gaps": true,
    "maintain_changelog": false,
    "boundary_analysis": false,
    "pr_visual_evidence": "off"
  },
  "workspace": {
    "enabled": true,
    "mode": "worktree",
    "cleanup": "confirm",
    "cleanup_after": "merge"
  },
  "adapters": ["cursor", "claude", "codex", "gemini", "github"],
  "integrations": {
    "gstack": {
      "requested": false,
      "version": "<installer-managed-version>"
    },
    "spec-kit": {
      "requested": false,
      "version": "<installer-managed-version>"
    }
  }
}
```

Use the versions written by the installer; the placeholders above describe ownership and are not literal version values to copy.

## Field reference

### Root fields

| Field | Required | Values and default | Effect |
|---|---:|---|---|
| `schema_version` | Yes | Integer; currently `1` | Selects the configuration contract. A newer value requires a newer Boatstack; an older supported value is migrated during update. |
| `project` | Yes | Object | Names the project and supplies repository context and commands. |
| `workflow` | Yes | Object; booleans use `false` when omitted | Controls approval, review, gap, changelog, and boundary-analysis behavior. |
| `workspace` | No | Object; disabled when absent | Controls optional per-feature branch or worktree management. |
| `adapters` | No | Array of supported adapter names; empty or absent enables all supported adapters | Selects generated host surfaces. Duplicate and blank entries are removed during export. |
| `integrations` | No | Object keyed by supported integration name | Records requested specialist integrations and installer-maintained state. |

### `project`

| Field | Required | Values and default | Effect |
|---|---:|---|---|
| `name` | Yes | Non-empty string | Human-readable project name used in generated configuration. |
| `default_branch` | No | Branch name; PR operations fall back to `origin/HEAD`, then `main` | Sets the canonical base branch for freshness checks, PRs, updates, and managed workspace cuts. Boatstack updates require it to be explicit. |
| `context` | No | Array of repository-relative file or directory paths; empty by default | Identifies durable context that planning should consult when relevant. |
| `commands` | Yes | Object of command-name to shell-command strings | Declares repository-owned validation commands. |
| `commands.test` | Yes | Non-empty command string | Supplies the minimum test boundary; configuration validation fails if it is absent or blank. |
| Other `commands.*` entries | No | Command strings such as `build`, `lint`, or `typecheck` | Make additional project checks available under their chosen names. Only `test` has a required name. |
| `high_risk_paths` | No | Array of Git-style glob patterns; empty by default | Marks paths for safety scanning and, when enabled, independent high-risk review. |

Context paths guide bounded discovery; they are not a request to load every listed file for every feature. Commands run from the repository and should be deterministic enough to act as evidence.

### `workflow`

The defaults below describe an omitted JSON field. A fresh installer-generated configuration writes its recommended policies explicitly, including human approval, independent high-risk review, and pass-with-gaps behavior, so review the actual file rather than assuming omission.

| Field | Default | Effect |
|---|---:|---|
| `human_plan_approval` | `false` | When `true`, requires explicit human plan approval before Build can activate the plan. |
| `independent_review_for_high_risk` | `false` | When `true`, changes matching `project.high_risk_paths` require the independent review boundary before shipping. Configure both fields for this policy to have a target. |
| `allow_pass_with_gaps` | `false` | When `true`, verification may pass with explicitly recorded outstanding gaps. It does not hide or discard them. |
| `maintain_changelog` | `false` | When `true`, requires a reader-visible `CHANGELOG.md` entry for every managed delivery slice and Boatstack-prepared ad-hoc PR. |
| `boundary_analysis` | `false` | When `true`, planning checks whether a request indicates a missing systemic boundary. Scope expansion remains a material human decision; choosing programmatic enforcement produces a boundary slice followed by the feature slice. |
| `pr_visual_evidence` | `off` | `suggest` records missing relevant screenshots as a visible PR gap; `require` blocks completed PR publication until current screenshot evidence is available. Media remains machine-local until PR attachment. |

### `workspace`

Workspace management is off unless `workspace.enabled` is `true`. Empty policy fields receive defaults only after it is enabled.

| Field | Values and default | Effect |
|---|---|---|
| `enabled` | Boolean; `false` | Master switch. When `false`, Boatstack does not create or remove branches or worktrees. |
| `mode` | `worktree` (default) or `branch` | Creates a linked worktree or switches to a fresh in-place feature branch. |
| `cleanup` | `confirm` (default), `auto`, or `off` | Asks before eligible cleanup, performs it automatically, or disables managed cleanup. |
| `cleanup_after` | `merge` (default) or `ship` | Makes cleanup eligible after the PR is confirmed merged or after the feature is published. Safety checks still prevent discarding uncommitted or unmerged local work without an explicit operator override. |

Managed workspaces are cut from the current remote default branch. Boatstack does not rewrite history, reuse an existing branch, delete remote branches, merge pull requests, or silently discard local work.

### `adapters`

Supported values are `cursor`, `claude`, `codex`, `gemini`, and `github`. An empty or omitted array enables all five. Use a subset only when the repository intentionally does not support the other host surfaces.

### `integrations`

Supported keys are `gstack` and `spec-kit`. Installation normally owns this object; prefer selecting integrations through the installer instead of hand-editing its result.

| Field | Ownership | Effect |
|---|---|---|
| `requested` | User choice recorded by installer | Whether the integration was requested. |
| `status` | Installer-maintained, optional | Current installation result, such as installed or partial. |
| `version` | Installer-maintained, optional | Pinned integration version or revision. |
| `detail` | Installer-maintained, optional | Human-readable installation or diagnostic detail. |

## Common policies

### Require a repository changelog

```json
{
  "workflow": {
    "maintain_changelog": true
  }
}
```

Add a categorized entry under `CHANGELOG.md`'s current `Unreleased` heading. See [the format and first-entry example](getting-started.md#keep-a-repository-changelog).

### Analyze systemic boundaries during planning

```json
{
  "workflow": {
    "boundary_analysis": true
  }
}
```

This adds a product decision when repository evidence suggests that a local request is a symptom of a broader missing boundary. It does not silently turn every feature into a refactor.

### Add screenshots to relevant pull requests

```json
{
  "workflow": {
    "pr_visual_evidence": "suggest"
  }
}
```

`suggest` keeps delivery nonblocking when capture or attachment is unavailable and exposes the missing evidence as a PR gap. Use `require` only when every relevant frontend PR must publish current screenshots. Boatstack stores PNG bytes in Git-common machine state, never in the product tree.

### Require independent review for high-risk paths

```json
{
  "project": {
    "high_risk_paths": ["migrations/**", "auth/**", "billing/**"]
  },
  "workflow": {
    "independent_review_for_high_risk": true
  }
}
```

Choose paths where a distinct reviewer is meaningful. Broad patterns increase review cost and should reflect actual repository risk boundaries.

### Manage a fresh worktree for each feature

```json
{
  "workspace": {
    "enabled": true,
    "mode": "worktree",
    "cleanup": "confirm",
    "cleanup_after": "merge"
  }
}
```

This is the conservative managed-workspace policy: start from a fresh remote base, use a linked worktree, and ask before reclaiming local state after merge.
