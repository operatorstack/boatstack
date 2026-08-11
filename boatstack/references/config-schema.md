# Configuration schema

Boatstack V2 accepts only `.boatstack/project.json` schema version 2. The
normative Go decoder is
`internal/kernel/protocol.DecodeProjectConfig`; the public example is
`project.example.json`.

Top-level keys are `schema_version`, `project`, `policy`, `hosts`, and optional
`extensions`.
Unknown keys and trailing JSON fail. Hosts are selected from `claude`, `cli`,
`codex`, `cursor`, `gemini`, `mcp`, and `sdk`; `cli` is mandatory.

Each `extensions` item selects only an additive subprocess extension and
requires a semantic ID, exact version, symlink-free absolute executable path,
exact SHA-256, optional strict JSON settings, and optional bounded deadline,
stdout, and stderr limits. Repository configuration cannot replace the primary
flow. A subprocess extension is a trusted executable boundary, not an OS
sandbox.

Configuration changes use `configuration.mutate` with `config_path` and
`config_sha256`. That fingerprint is the SHA-256 of the strict decoded schema-2
value in canonical JSON form, not the source file's raw bytes; the CLI derives it
when omitted. Never hand-edit controller state or reuse a V1 schema.
