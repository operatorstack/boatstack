# Writing a Flow

A repository Flow chooses trusted operations, their priorities, terminal
targets, and named entries. It does not provide executable operator handlers.

```ts
import { all, defineFlow, entry, fact, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedDelegation,
  trustedOperators,
  trustedTransitions,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "publication.observe", priority: 77 },
] satisfies TrustedStep[];

export default defineFlow({
  id: "product-delivery",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  operators: trustedOperators(lifecycle),
  transitions: trustedTransitions(lifecycle),
  targets: [
    marked(
      "published-pr",
      all(
        fact("verification", ["current"]),
        fact("configuration", ["verified"]),
        fact("runtime", ["verified"]),
        fact("publication", ["open"]),
      ),
    ),
  ],
  entries: [
    entry({
      id: "run",
      target: "published-pr",
      inputs: [inbox(".boatstack/plans/inbox")],
      delegation: trustedDelegation("autonomy"),
      diagnostics: { explain_on_suspend: true },
    }),
  ],
});
```

The trusted package owns operator effects, minimum capabilities, trusted
authority alternatives, verification, and recovery. The repository owns which
trusted transitions are present, their priorities, targets, entries, additional
mandatory authority, and diagnostics UX.

Compilation lowers this source to raw IR. Boatstack then validates references,
resolves trusted bindings, canonicalizes executable semantics, and fingerprints
the program. Runtime commands load the committed IR artifact; they do not
execute this source file.
