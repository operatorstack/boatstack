# Host and hook contract

All hosts call `boatstack rpc` with one schema-14 JSON object and read one
schema-14 response. The decoder rejects unknown fields and trailing JSON.

Example read request:

```json
{
  "schema_version": 14,
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

A repository-selected human identity command is untrusted data and is not a
Boatstack transition effect. A Flow request, identity presentation, or
delegation request does not grant permission to execute it. The host must submit
the exact structured argv to its own command permission boundary and use the
explicit actor fallback when that boundary does not independently permit it.

A host that claims it can complete external publication must expose a provider
receipt issuer before the delivery begins. If that capability is absent, the
host must declare that it can progress only to the authority-bearing
publication frontier. Authentication state is not a receipt issuer.
