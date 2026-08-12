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
  "objective": {
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
objective, source predicate, authority clauses, parameters, or expected
postcondition. CLI, Cursor, Codex, Claude, Gemini, and MCP are capability labels,
not controllers.

A host that claims it can complete external publication must expose a provider
receipt issuer before the delivery begins. If that capability is absent, the
host must declare that it can progress only to the authority-bearing
publication frontier. Authentication state is not a receipt issuer.
