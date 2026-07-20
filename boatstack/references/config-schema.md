# Boatstack Configuration Schema

This reference document defines the schema and version history of `.boatstack-project.json`.

## Current Schema Version

- **schema_version**: `1`

## Field Reference

### Root Fields

- `schema_version` (integer, required): Must be exactly `1`.
- `project` (object, required): General project definition.
- `workflow` (object, required): Flags controlling state machine transitions and safety gates.
- `adapters` (array of strings, optional): Enabled host environment adapters. If empty, defaults to enabling all.
- `integrations` (object, optional): Explicit configurations for individual third-party integrations.

### project Fields

- `name` (string, required): The human-readable name of the project.
- `default_branch` (string, optional): The canonical development/default branch (e.g. `main` or `master`).
- `context` (array of strings, optional): Paths to persistent project directories or contextual documents.
- `commands` (object, required): Custom development commands:
  - `test` (string, required): The exact command to execute project-local tests.
- `high_risk_paths` (array of strings, optional): Glob patterns of files requiring independent reviewer sign-off before shipping.

### workflow Fields

- `human_plan_approval` (boolean, optional): Whether a parent plan requires explicit human approval before building.
- `independent_review_for_high_risk` (boolean, optional): Whether modifications to high-risk files require a distinct peer review gate.
- `allow_pass_with_gaps` (boolean, optional): Whether the delivery verification allows outstanding questions or gaps.
- `maintain_changelog` (boolean, optional): Whether a release-notes fragment is required for each delivery slice.

## Version Changelog

### Version 1

- Initial schema with `project`, `workflow`, `adapters`, and `integrations`.
