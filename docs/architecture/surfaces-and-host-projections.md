# Surfaces and host projections

CLI, RPC, MCP, the Go SDK, and generated coding-agent files are invocation or
presentation surfaces over the same controller. They do not own separate
lifecycle, authority, verification, or recovery state machines.

**Hosts** are runtime surfaces enabled by project configuration. **Projections**
are generated host-native files selected independently, subject to the matching
host being enabled. The current canonical projection vocabulary is:

| Projection | Flow entry files |
| --- | --- |
| Codex | `.agents/skills/<slug>/.gitattributes`, `SKILL.md`, `agents/openai.yaml` |
| Claude | `.claude/skills/<slug>/.gitattributes`, `SKILL.md` |
| Cursor | `.cursor/commands/<slug>.md` plus shared `.gitattributes` |
| Gemini | `.gemini/skills/<slug>/SKILL.md` plus shared `.gitattributes` |

The Go host-projection registry owns this vocabulary and path mapping.
Boatstack owns the exact files recorded in its projection manifests, never the
host directory. Shared checkout attributes are reference-counted separately.

Low-level surfaces forward complete prescriptions and admission context. They
must not reconstruct a decision from partial fields. Generated projections
explain how a host resumes the same run, answers typed suspension, presents
authority, and invokes the controller; they do not grant authority.

Exact maintenance and Flow paths are listed in
[Generated files](../generated-files.md).

## Current implementation anchors

- [Canonical projection registry](../../boatstack/internal/hostprojection/projection.go)
- [Flow projection renderer](../../boatstack/flow/softwaredelivery/projections.go)
- [Runtime projection effects](../../boatstack/internal/softwaredelivery/effects/host_projections.go)
- [Surface protocol tests](../../boatstack/internal/softwaredelivery/surfaces/protocol_test.go)
