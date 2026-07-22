# Boatstack Configuration Schema

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

This reference document defines the schema and version history of `.boatstack-project.json`.

## Current Schema Version

- **schema_version**: `1`

## Field Reference

### Root Fields

- `schema_version` (integer, required): Must be exactly `1`.
- `project` (object, required): General project definition.
- `workflow` (object, required): Flags controlling state machine transitions and safety gates.
- `workspace` (object, optional): Opt-in per-feature branch or worktree management.
- `adapters` (array of strings, optional): Enabled host environment adapters. If empty, defaults to enabling all.
- `integrations` (object, optional): Explicit configurations for individual third-party integrations.

### project Fields

- `name` (string, required): The human-readable name of the project.
- `default_branch` (string, optional): The canonical development/default branch (e.g. `main` or `master`).
- `context` (array of strings, optional): Paths to persistent project directories or contextual documents.
- `commands` (object, required): Custom development commands:
  - `test` (string, required): The exact command to execute project-local tests.
  - Other command names (string, optional): Additional repository-owned commands such as `build`, `lint`, or `typecheck`.
- `high_risk_paths` (array of strings, optional): Glob patterns of files requiring independent reviewer sign-off before shipping.

### workflow Fields

- `human_plan_approval` (boolean, optional): Whether a parent plan requires explicit human approval before building.
- `independent_review_for_high_risk` (boolean, optional): Whether modifications to high-risk files require a distinct peer review gate.
- `allow_pass_with_gaps` (boolean, optional): Whether the delivery verification allows outstanding questions or gaps.
- `maintain_changelog` (boolean, optional): Whether a reader-visible `CHANGELOG.md` entry is required for each delivery slice.
- `boundary_analysis` (boolean, optional): Whether planning checks for a missing systemic boundary and presents local repair versus programmatic enforcement as a material product decision.
- `pr_visual_evidence` (string, optional): `off`, `suggest`, or `require`. Omission is `off`. Relevant PRs use machine-local PNG evidence without committing media to Git; `suggest` records missing evidence as a visible gap and `require` blocks completed publication.

### workspace Fields

- `enabled` (boolean, optional): Enables managed per-feature workspaces. Defaults to `false`.
- `mode` (string, optional): `worktree` or `branch`. Defaults to `worktree` when workspace management is enabled.
- `cleanup` (string, optional): `confirm`, `auto`, or `off`. Defaults to `confirm`.
- `cleanup_after` (string, optional): `merge` or `ship`. Defaults to `merge`.

### adapters Values

Supported values are `cursor`, `claude`, `codex`, `gemini`, and `github`. An empty or omitted array enables all supported adapters.

### integrations Fields

Supported integration keys are `gstack` and `spec-kit`. Each integration state can contain:

- `requested` (boolean, required when the integration is present): Whether installation was requested.
- `status` (string, optional): Installer-maintained installation status.
- `version` (string, optional): Installer-maintained pinned version or revision.
- `detail` (string, optional): Installer-maintained diagnostic detail.

## Version Changelog

### Version 1

- Initial schema with `project`, `workflow`, `workspace`, `adapters`, and `integrations`.
