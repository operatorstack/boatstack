# Configuration schema

Boatstack accepts only `.boatstack/project.json` schema version 5. The
normative Go decoder is
`internal/softwaredelivery/protocol.DecodeProjectConfig`; the public example is
`project.example.json`.

Top-level keys are `schema_version`, `identity`, `project`, `policy`, `hosts`,
`projections`, and optional `extensions`.
Unknown keys and trailing JSON fail. Hosts are selected from `claude`, `cli`,
`codex`, `cursor`, `gemini`, `mcp`, and `sdk`; `cli` is mandatory.

Each `extensions` item selects only an additive subprocess extension and
requires a semantic ID, exact version, symlink-free absolute executable path,
exact SHA-256, optional strict JSON settings, and optional bounded deadline,
stdout, and stderr limits. Repository configuration cannot replace the primary
flow. A subprocess extension is a trusted executable boundary, not an OS
sandbox.

`identity.default` and nonempty `identity.roles` are required. Role IDs match
`^[a-z][a-z0-9._-]*$` and are at most 128 bytes. Each descriptor is either a
bounded `literal` actor or a structured `command` plus exact `args`. Boatstack
fingerprints and exposes the descriptor but never executes it. Identity
resolution proposes an actor; a role or actor does not grant human or
external-provider authority.

Configuration changes use `configuration.mutate` with `config_path` and
`config_sha256`. That fingerprint is the SHA-256 of the strict decoded schema-5
value in canonical JSON form, not the source file's raw bytes; the CLI derives it
when omitted. Never hand-edit controller state or reuse a prior schema.
