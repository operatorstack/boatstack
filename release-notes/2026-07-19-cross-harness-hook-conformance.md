### Enforce safety hooks consistently across supported hosts

Boatstack now applies explicit Cursor, Claude Code, and Codex hook contracts over
the shared irreversible-operation policy. Cursor MCP calls are decoded without
mistaking transport fields for shell commands, enforcing runtime failures block
with each host's documented exit semantics, and Codex linked-worktree trust and
hook-review steps are surfaced during installation and diagnosis. Deterministic
contract and process tests cover configuration merging, runtime hydration,
decision schemas, and host-specific settle behavior.
