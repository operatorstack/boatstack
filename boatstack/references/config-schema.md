# Configuration schema

Boatstack V2 accepts only `.boatstack/project.json` schema version 2. The
normative Go decoder is
`internal/kernel/protocol.DecodeProjectConfig`; the public example is
`project.example.json`.

Top-level keys are `schema_version`, `project`, `policy`, and `hosts`.
Unknown keys and trailing JSON fail. Hosts are selected from `claude`, `cli`,
`codex`, `cursor`, `gemini`, and `mcp`; `cli` is mandatory.

Configuration changes use `configuration.mutate` with `config_path` and
`config_sha256`. Never hand-edit controller state or reuse a V1 schema.
