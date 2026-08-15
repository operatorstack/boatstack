# Writing a Flow

A repository Flow chooses trusted operations, their priorities, terminal
targets, and named entries. It does not provide executable operator handlers.

```ts
import {
  all,
  defineFlow,
  entry,
  entryInput,
  fact,
  foregroundWork,
  instructionAsset,
  marked,
  workArtifact,
} from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedDelegation,
  trustedOperators,
  trustedTransition,
  trustedTransitions,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  // Add the repository's trusted execution, gate, and publication steps here.
] satisfies TrustedStep[];

const planning = foregroundWork({
  id: "planning-package",
  instructions: instructionAsset(".boatstack/flows/assets/planning.md"),
  inputs: [entryInput("plan")],
  outputs: [
    workArtifact({
      id: "plan",
      path: "plan.md",
      media_type: "text/markdown",
      required: true,
      max_bytes: 262144,
    }),
  ],
});

export default defineFlow({
  id: "product-delivery",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  work: [planning],
  operators: trustedOperators(lifecycle),
  transitions: [
    trustedTransition(planningPackageAdmit, { work: planning }),
    trustedTransition(planningPackageApprove),
    trustedTransition(planningPackagePromote),
    // Add trustedTransitions(...) for the remaining lifecycle here.
  ],
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

The planning-package operations are optional trusted mechanisms. A repository
that includes them owns the planning instruction, output contract, transition
order, approval policy, target, and entry. `planning.package.admit` atomically
stores the verified package, `planning.package.approve` binds approval to its
exact manifest, and `planning.package.promote` publishes the approved canonical
plan. Repositories that omit these operations retain the existing plan path;
StandardFlow does not add them automatically.
