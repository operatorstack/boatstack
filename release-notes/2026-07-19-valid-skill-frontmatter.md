### Validate generated skill metadata before installation

Boatstack now emits valid YAML frontmatter for its Codex and Claude skill adapters and rejects malformed or host-incompatible skill metadata before installation. Codex can discover `$boatstack` again, while export checks and doctor prevent future adapter-format regressions from being reported as healthy.
