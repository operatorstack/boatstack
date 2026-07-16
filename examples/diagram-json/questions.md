# Question ledger: diagram JSON output

| ID | Question | Why it matters | Options | Recommendation | Answer | Source | Status/expiry |
|---|---|---|---|---|---|---|---|
| Q-1 | How should JSON enter the public API? | Changing `printFlowGraph` to return multiple shapes would weaken its existing string contract. | Add `serializeFlowGraph`; add a `format` option to `printFlowGraph`; replace the current return type. | Add the sibling `serializeFlowGraph` function. | Add the sibling function. | Simulated maintainer decision | Accepted for demo |
| Q-2 | Is the JSON an internal object dump or a supported contract? | Consumers need to know whether field changes are breaking. | Versioned minimal schema; undocumented internal dump. | Publish a minimal schema with `schemaVersion: 1`. | Use the versioned minimal schema. | Simulated maintainer decision | Accepted for demo |
| Q-3 | What run data belongs in v1? | Serializing the entire execution trace expands scope and can leak unrelated internals. | Edge signal overlay plus optional bottleneck; entire `FlowRun`; topology only. | Include the compact overlay and bottleneck already exposed by the text renderer. | Use the compact overlay, excluding the raw trace. | Simulated maintainer decision | Accepted for demo |

Discoverable facts were answered from `src/diagram.ts`, `src/index.ts`,
`examples/05-diagram-printer/`, `package.json`, and the TypeScript configs. No
other product choice is hidden as an assumption in this draft.
