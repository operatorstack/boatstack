# Product request

Add machine-readable JSON output to the diagram printer while preserving the
current text output.

## Initial projection

- Domain: diagram serialization
- Actor: a library integrator consuming harness diagrams
- Input: an existing `DiagramGraph` and optional `FlowRun`
- Output: a stable JSON document
- User-visible goal: consume the same diagram data in scripts and tools
- Next operator: an application parses and stores or renders the JSON
- Verification boundary: public contract tests plus byte-compatible ASCII output
