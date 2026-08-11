# Host and hook contract

All hosts call `boatstack rpc` with one schema-2 JSON object and read one
schema-2 response. The decoder rejects unknown fields and trailing JSON.

Example read request:

```json
{
  "schema_version": 2,
  "operation": "resolve",
  "repository": "/absolute/worktree",
  "host": "codex",
  "correlation_id": "host-123",
  "goal": {
    "id": "search-timeout",
    "kind": "verified-implementation",
    "delivery_id": "search-timeout"
  },
  "authority": {"receipts": []}
}
```

For a pre-execution hook, use operation `guard` and add `command`. The helper
classifies raw text once, returns `guard.allowed`, and never writes the command
to receipts or events.

Hosts may render commands differently. They may not change the transition ID,
goal, source predicate, authority clauses, parameters, or expected
postcondition. CLI, Cursor, Codex, Claude, Gemini, and MCP are capability labels,
not controllers.
