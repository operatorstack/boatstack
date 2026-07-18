# Feature spec: versioned JSON diagram output

## Outcome boundary

- Domain: diagram serialization
- Actor: a library integrator consuming harness diagrams
- Input: `DiagramGraph`, optional `FlowRun`, and diagram options
- Output: a deterministic JSON string with `schemaVersion: 1`
- User-visible goal: use harness diagrams in scripts and non-terminal tools
- Next operator: a JSON parser, store, or renderer
- Verification boundary: public contract fixture and unchanged ASCII fixture

## Problem and outcome

`printFlowGraph` makes the harness structure inspectable in a terminal, but a
consumer cannot safely parse that presentation text. Add an independent public
serializer so machines can consume the topology and the same compact run
overlay without changing the text-rendering contract.

## Non-goals

- Replacing or redesigning the ASCII renderer.
- Serializing the complete `FlowRun` or private implementation state.
- Adding a command-line interface, network endpoint, or UI.
- Supporting arbitrary schema versions in this feature.

## Scenarios

1. A consumer serializes a static graph and parses its nodes and edges.
2. A consumer provides a run and receives edge signals and a bottleneck summary.
3. An existing caller continues to receive identical text from `printFlowGraph`.

## Acceptance criteria

- **AC-1:** `serializeFlowGraph(graph)` returns parseable JSON containing
  `schemaVersion: 1`, graph identity, nodes, and edges.
- **AC-2:** repeated calls with the same inputs produce byte-identical output,
  preserve node/edge order, and omit absent optional fields.
- **AC-3:** with a run, the document includes the compact edge-signal overlay
  and optional bottleneck summary, respects the existing cost visibility option,
  and does not expose the raw trace.
- **AC-4:** all existing `printFlowGraph` outputs remain byte-compatible with
  `labs/05-diagram-printer/expected-output.txt`.
- **AC-5:** the serializer and its public schema types are exported from
  `src/index.ts` and demonstrated in the diagram example documentation.

## Interfaces and data

Add the following sibling API rather than changing `printFlowGraph`:

```ts
serializeFlowGraph(
  graph: DiagramGraph,
  run?: FlowRun,
  options?: DiagramOptions,
): string
```

The v1 document has a top-level `schemaVersion`, graph identity, ordered `nodes`
and `edges`, plus optional `signals` and `bottleneck` fields. Public TypeScript
types define the exact schema. Serialization uses a documented, fixed JSON
format so fixture comparison is meaningful.

## Invariants and trust boundaries

- `printFlowGraph` retains its current signature and behavior.
- Graph and run inputs are not mutated.
- Only documented diagram fields and compact derived measurements cross the
  serializer boundary.
- The output contains no raw prompts, model responses, environment values, or
  execution trace.

## Failure and recovery behavior

Invalid graph references keep the current diagram-layer behavior; this feature
does not introduce a second graph validator. JSON serialization errors propagate
to the caller. A schema change requires a new schema version rather than a
silent v1 mutation.

## Observability

No telemetry is added. Contract fixtures, TypeScript checks, and executable
examples provide evidence at the library boundary.

## Rollout and rollback

This is an additive API with no migration. Rollback removes the serializer,
schema types, fixture, and documentation as one commit while leaving the text
renderer untouched.

## Linked questions, ADRs, and gaps

- Decisions: [Q-1 through Q-3](questions.md).
- ADR: not required; the change is additive and local to the existing diagram
  boundary.
- Known gap: schema evolution beyond v1 is deliberately deferred until a real
  consumer requires it.
