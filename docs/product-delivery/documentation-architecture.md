# Documentation architecture

This site has two deliberately separate future-facing inputs:

```text
TypeDoc
  documents APIs available to Flow authors

future Flow renderer
  documents a concrete control system built with those APIs
```

TypeDoc reads the authoritative TypeScript declarations and comments. Its JSON
reflection model is retained at `build/docs/api.json` as a possible input for
future cross-linking.

A later renderer may read canonical `.flow.ir.json` and describe entries,
targets, transitions, authority, delegation, and diagnostics for a concrete
repository Flow. It must consume canonical IR rather than interpreting
TypeScript source, and it must not become a second source of executable
semantics.
