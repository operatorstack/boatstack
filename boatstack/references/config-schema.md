# Boatstack Configuration Schema

<!--
boatstack-config-field:schema_version
boatstack-config-field:project
boatstack-config-field:project.name
boatstack-config-field:project.default_branch
boatstack-config-field:project.context
boatstack-config-field:project.commands
boatstack-config-field:project.high_risk_paths
boatstack-config-field:project.visual_surfaces
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
boatstack-config-field:workflow.external_authority
boatstack-config-field:workflow.external_authority.mode
boatstack-config-field:workflow.external_authority.trust_store
boatstack-config-field:workflow.ignored_deliveries
boatstack-config-field:delivery
boatstack-config-field:delivery.terminal
boatstack-config-field:insights
boatstack-config-field:insights.enabled
boatstack-config-field:insights.capture_mode
boatstack-config-field:insights.value_map
boatstack-config-field:insights.suggest_features
boatstack-config-field:insights.evaluate_on_pr
boatstack-config-field:insights.pending_frontier
boatstack-config-field:insights.completion_mode
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
- `insights` (object, optional): Opt-in controls for independent, reviewable repository insight captures.
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
- `visual_surfaces` (array of objects, optional): Registered product surfaces. Each object has a lowercase-kebab `id` and repository-relative `paths`; changes below these paths are screenshot candidates and cannot use `not_relevant`.
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
- `visual_evidence_publish` (object, optional): Publish control for externally hosted screenshots. Omission defaults to Litterbox with a 72-hour expiry. Upload is refused until every PNG has explicit human privacy review. Fields:
  - `mode` (string, optional): Compatibility value `external-host`; external hosting is always used.
  - `host` (string, optional): `litterbox` (default) or `catbox`. Only meaningful when `mode` is `external-host`. `litterbox` auto-expires uploads; `catbox` is permanent.
  - `expiry` (string, optional): `1h`, `12h`, `24h`, or `72h` (default `72h`). Only meaningful for an expiring host; the PR comment reminds reviewers of the host and this window.
- `external_authority` (object, optional): Declares the credential boundary for managed runs. Omission or `mode: "hook-only"` reports `HOOK_GUARDED`; it never claims cloud credentials are constrained. `mode: "credential-enforced"` requires a short-lived external receipt signed by an issuer in a protected external trust store.
  - `mode` (string): `hook-only` or `credential-enforced`.
  - `trust_store` (string): Absolute path to an operator-provisioned JSON file mapping issuer IDs to base64 Ed25519 public keys. Boatstack rejects a trust store or parent directory owned or writable by the managed principal and never holds a signing key.
- `ignored_deliveries` (array of strings, optional): Deterministic ambiguity control. Feature slugs of past deliveries to exclude from delivery-ambiguity resolution so historical work no longer blocks new work. New, unlisted ambiguous deliveries still pause the workflow.

### workspace Fields

- `enabled` (boolean, optional): Enables managed per-feature workspaces. Defaults to `false`.
- `mode` (string, optional): `worktree` or `branch`. Defaults to `worktree` when workspace management is enabled.
- `cleanup` (string, optional): `confirm`, `auto`, or `off`. Defaults to `confirm`. Governs single-feature cleanup of the named workspace.
- `cleanup_after` (string, optional): `merge` or `ship`. Defaults to `merge`.
- `reap` (string, optional): `confirm`, `auto`, or `off`. Defaults to `confirm`. Governs the post-merge sweep that reclaims all terminal (merged or abandoned) Boatstack workspaces at once. `confirm` prompts the operator once when reclaimable workspaces exist; `auto` reclaims them without prompting; `off` disables the sweep and its prompt.

### delivery Fields

- `terminal` (string, optional): `published` or `merged`. Defaults to `published`. Deterministic goal control: the state a delivery pursues before the flow reports nothing left to do. `published` ends the flow when the slice's pull request is open (the prior behavior, unchanged). `merged` keeps the read-only flow advisors naming post-publish steps until the pull request is observed merged. The goal a delivery is activated under is snapshotted on its state, so changing this value mid-flight never changes an in-progress delivery's goal; every invalid or unreadable value resolves to `published`.

### insights Fields

This block is opt-in. When `enabled` is false or the block is absent, Boatstack preserves existing behavior. Every confirmed capture and every later insight event is written below `docs/insights/<id>/` so the handoff is a reviewable repository diff. Boatstack never stores insight content in detached state or the Git control directory.

- `enabled` (boolean, optional): Enables independent insight capture and evaluation.
- `capture_mode` (string, optional): `manual`. Boatstack previews each capture and requires a separate state-scoped save confirmation.
- `value_map` (string, optional): `required`. The confirmed capture must contain the canonical Product Value Map lineage and exact source binding.
- `suggest_features` (boolean, optional): Allows the host adapter to propose one primary topic and related topics without creating a delivery.
- `evaluate_on_pr` (boolean, optional): Appends an evaluation event when Boatstack publishes or observes a terminal PR for a bound managed feature. Evaluation never completes a capture.
- `pending_frontier` (boolean, optional): Enables the separate read-only insight frontier. It never replaces the delivery frontier.
- `completion_mode` (string, optional): `human_confirmed`. A human records final disposition; completing before readiness requires a non-empty reason.

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
