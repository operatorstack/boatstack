# Domain-neutral SDK

`@operatorstack/boatstack` declares complete Control Program IR. The main
composition boundary is `defineFlow`; it validates and canonicalizes authoring
data into raw IR but does not execute a Flow.

## Program structure

- `defineFlow` lowers a complete `FlowDefinition`.
- `facet`, `evidence`, `operator`, and `transition` declare the relation.
- `marked` declares a target; `entry` selects a target and normalizes inputs.
- `fact`, `all`, and `always` build predicates.

## Invocation and parameters

`hostParameter`, `fromEntryInput`, `fromState`, `fromReceipt`,
`fromStateOrReceipt`, `fromWorkOutput`, and `trustedParameterResolver` describe
exact producers. They do not resolve values while TypeScript runs. The compiler
requires one compatible producer for each required reachable parameter.

## Foreground work

`foregroundWork` declares bounded candidate work. `instructionAsset` and
`schemaAsset` name repository assets that the compiler later resolves and
fingerprints. `entryInput` binds an input; `workArtifact` declares an output.
Work completion does not independently advance Flow state.

## Authority

`AuthorityRequirements` appears on transitions and entries. On an entry it
declares activation authority; on a transition it adds mandatory admission
authority. Neither form creates a receipt or chooses an actor.

See [Flow anatomy](flow-anatomy.md) and the generated module page for exact
types, categories, and signatures.
