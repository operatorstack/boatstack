### Use one delivery kernel on every supported host

Boatstack V2 replaces the V1 runtime in one flag-day change. Every managed delivery now uses one typed snapshot, one 61-transition catalog, one authority check, one transactional effect boundary, and one verified receipt path.

The CLI, hooks, SDK, MCP, Cursor, Codex, Claude Code, and Gemini CLI now consume the same protocol. Detached clones and linked worktrees keep exact identities. Runtime updates bind to the exact executing binary. Unknown or stale evidence does not grant permission to publish, delete, or advance.

V2 does not read or migrate V1 machine state. Reinstall or reattach each repository with a schema-2 configuration.

The executable registry also generates its readable catalog and Locus safety/liveness abstractions. Repository tests reject drift between those checked artifacts and all 61 runtime transitions; the formal claim remains advisory outside the finite stable-phase model.

Cross-platform authority is canonical at the boundary: tests construct native absolute paths, checked generated artifacts are LF-pinned, and configuration authority hashes strict decoded schema-2 semantics rather than checkout-specific JSON bytes. Normal Git line-ending conversion cannot invalidate a transferred workspace, while actual policy changes still rotate authority.
