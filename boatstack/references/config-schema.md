# Boatstack Configuration Schema

<!--
boatstack-config-field:schema_version
boatstack-config-field:project
boatstack-config-field:project.name
boatstack-config-field:project.default_branch
boatstack-config-field:project.context
boatstack-config-field:project.commands
boatstack-config-field:project.high_risk_paths
boatstack-config-field:project.migration
boatstack-config-field:project.migration.apply_command
boatstack-config-field:project.migration.verify_command
boatstack-config-field:workflow
boatstack-config-field:workflow.human_plan_approval
boatstack-config-field:workflow.independent_review_for_high_risk
boatstack-config-field:workflow.allow_pass_with_gaps
boatstack-config-field:workflow.maintain_changelog
boatstack-config-field:workflow.boundary_analysis
boatstack-config-field:workflow.pr_visual_evidence
boatstack-config-field:workflow.visual_evidence_publish
boatstack-config-field:workflow.visual_evidence_publish.mode
boatstack-config-field:workflow.visual_evidence_publish.host
boatstack-config-field:workflow.visual_evidence_publish.expiry
boatstack-config-field:workflow.ignored_deliveries
boatstack-config-field:delivery
boatstack-config-field:delivery.terminal
boatstack-config-field:workspace
boatstack-config-field:workspace.enabled
boatstack-config-field:workspace.mode
boatstack-config-field:workspace.cleanup
boatstack-config-field:workspace.cleanup_after
boatstack-config-field:workspace.reap
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

This is the exhaustive serialization contract, not a list of recommended user edits. Fields are classified as **deterministic control**, **agent-mediated guidance**, **identity/compatibility metadata**, or **installer-owned state**. The public configuration guide contains only supported user controls.

### Root Fields

- `schema_version` (integer, required): Must be exactly `1`. Identity/compatibility metadata managed by Boatstack.
- `project` (object, required): General project definition.
- `workflow` (object, required): Flags controlling state machine transitions and safety gates.
- `workspace` (object, optional): Opt-in per-feature branch or worktree management.
- `delivery` (object, optional): The standing goal of the delivery flow.
- `adapters` (array of strings, optional): Enabled host environment adapters. If empty, defaults to enabling all.
- `integrations` (object, optional): Installer-owned state for third-party integrations.

### project Fields

- `name` (string, required): Identity metadata written into generated configuration.
- `default_branch` (string, optional): Deterministic base for freshness, PR, update, and workspace operations.
- `context` (array of strings, optional): Agent-mediated durable-context hints; the controller does not load every path automatically.
- `commands` (object, required): Agent-mediated repository commands:
  - `test` (string, required): The exact command to execute project-local tests.
  - `visual` / `screenshot` / `e2e` (string, optional): The repository-owned visual capture harness Boatstack runs automatically during ship. A surface-scoped key `visual:<surface>` (e.g. `visual:web`, `visual:ops`; lowercase kebab surface, registered with `capability-register --surface`) outranks the global key for scenarios that declare that `surface`; scenarios without one, or without a surface key, use the global command exactly as before.
  - Other command names (string, optional): Additional repository-owned commands such as `build`, `lint`, or `typecheck`.
- `high_risk_paths` (array of strings, optional): Glob patterns of files requiring independent reviewer sign-off before shipping.
- `migration` (object, optional): Declares how migrations are graded by EFFECT against a disposable database, so a committed migration stays a data artifact for the guard while its real effect is executed and observed by a conformance harness. Both commands run via `sh -c` with the disposable database coordinate in the environment as `BOATSTACK_MIGRATE_DB`; when `apply_command` is absent, grading is skipped.
  - `apply_command` (string, optional): The command that applies the migration set to the disposable database.
  - `verify_command` (string, optional): The command that asserts the post-migration invariant; a non-zero exit grades the migration FAIL.

### workflow Fields

- `human_plan_approval` (boolean, optional): Deterministic activation control. `true` requires a current human receipt; `false` creates a fingerprinted policy lock.
- `independent_review_for_high_risk` (boolean, optional): Deterministic review control. Matching diffs require reviewer identity and method `human_peer` or `separate_agent`.
- `allow_pass_with_gaps` (boolean, optional): Deterministic gate control. `false` rejects `PASS_WITH_GAPS`; `true` preserves explicit gaps.
- `maintain_changelog` (boolean, optional): Whether a reader-visible `CHANGELOG.md` entry is required for each delivery slice.
- `boundary_analysis` (boolean, optional): Agent-mediated planning guidance that presents local repair versus programmatic enforcement as a material product decision.
- `pr_visual_evidence` (string, optional): `off`, `suggest`, or `require`. Omission is `off`. Relevant PRs use machine-local PNG evidence without committing media to Git; `suggest` records missing evidence as a visible gap and `require` blocks completed publication. When the approved plan declares `relevance: relevant` with scenarios, `suggest` ships with require semantics for that feature (a plan that promises pixels cannot ship without them) — even when no capture capability is registered yet. The two escapes are `off` (global) and a `not_relevant` plan decision with a reason (per feature, for genuinely nonvisual changes). Boatstack runs a registered capture command (`project.commands.visual`) automatically during ship, so under normal provisioning the escalation is invisible.
- `visual_evidence_publish` (object, optional): Agent-mediated publish control for how captured PNG bytes reach the pull-request comment. Omission keeps the default: commit the bytes to a public Boatstack-owned evidence branch and render them inline, but only for a **public** GitHub origin (a private origin falls back to manual attachment). Fields:
  - `mode` (string, optional): `external-host` opts the repository — including a **private** one — into uploading the exact PNG bytes to an anonymous expiring host so the comment renders inline anywhere. It is **never auto-selected** because it publishes screenshot bytes to a third party; only this explicit value turns it on. Empty keeps the default public-branch behavior.
  - `host` (string, optional): `litterbox` (default) or `catbox`. Only meaningful when `mode` is `external-host`. `litterbox` auto-expires uploads; `catbox` is permanent.
  - `expiry` (string, optional): `1h`, `12h`, `24h`, or `72h` (default `72h`). Only meaningful for an expiring host; the PR comment reminds reviewers of the host and this window.
- `ignored_deliveries` (array of strings, optional): Deterministic ambiguity control. Feature slugs of past deliveries to exclude from delivery-ambiguity resolution so historical work no longer blocks new work. New, unlisted ambiguous deliveries still pause the workflow.

### workspace Fields

- `enabled` (boolean, optional): Enables managed per-feature workspaces. Defaults to `false`.
- `mode` (string, optional): `worktree` or `branch`. Defaults to `worktree` when workspace management is enabled.
- `cleanup` (string, optional): `confirm`, `auto`, or `off`. Defaults to `confirm`. Governs single-feature cleanup of the named workspace.
- `cleanup_after` (string, optional): `merge` or `ship`. Defaults to `merge`.
- `reap` (string, optional): `confirm`, `auto`, or `off`. Defaults to `confirm`. Governs the post-merge sweep that reclaims all terminal (merged or abandoned) Boatstack workspaces at once. `confirm` prompts the operator once when reclaimable workspaces exist; `auto` reclaims them without prompting; `off` disables the sweep and its prompt.

### delivery Fields

- `terminal` (string, optional): `published` or `merged`. Defaults to `published`. Deterministic goal control: the state a delivery pursues before the flow reports nothing left to do. `published` ends the flow when the slice's pull request is open (the prior behavior, unchanged). `merged` keeps the read-only flow advisors naming post-publish steps until the pull request is observed merged. The goal a delivery is activated under is snapshotted on its state, so changing this value mid-flight never changes an in-progress delivery's goal; every invalid or unreadable value resolves to `published`.

### adapters Values

Supported values are `cursor`, `claude`, `codex`, `gemini`, and `github`. An empty or omitted array enables all supported adapters.

### integrations Fields

Supported integration keys are `gstack` and `spec-kit`. The installer owns these records; hand edits do not select or pin an installation. Each state can contain:

- `requested` (boolean, required when the integration is present): Whether installation was requested.
- `status` (string, optional): Installer-maintained installation status.
- `version` (string, optional): Installer-maintained pinned version or revision.
- `detail` (string, optional): Installer-maintained diagnostic detail.

## Version Changelog

### Version 1

- Initial schema with `project`, `workflow`, `workspace`, `adapters`, and `integrations`.
