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
  hostParameter,
  instructionAsset,
  marked,
  workArtifact,
} from "@operatorstack/boatstack";
import {
  deliveryBranch,
  inbox,
  managedWorktreeDestination,
  planInboxResolver,
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  repositoryDefaultBranch,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedDelegation,
  trustedOperators,
  trustedTransition,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  { id: "workspace.cut", priority: 52 },
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
    trustedTransition(planningPackageApprove, {
      parameters: {
        package_fingerprint: hostParameter({
          id: "package-fingerprint",
          description: "Enter the exact admitted planning-package fingerprint.",
          authorities: ["human"],
          scope: "transition",
        }),
      },
    }),
    trustedTransition(planningPackagePromote),
    trustedTransition(
      { id: "workspace.cut", priority: 52 },
      {
        parameters: {
          base_ref: repositoryDefaultBranch(),
          branch: deliveryBranch(),
          destination: managedWorktreeDestination(),
        },
      },
    ),
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

Every required trusted operator parameter has exactly one producer in the
repository source. Compilation rejects missing, duplicate, incompatible, or
authority-weakening producers before it projects an artifact or generated
skill. Trusted resolvers are read-only. A missing `hostParameter` returns a
typed `TRANSITION_INPUT_REQUIRED` suspension; record its answer with
`boatstack flow input answer`, then resume the same run.

The planning-package operations are optional trusted mechanisms. A repository
that includes them owns the planning instruction, output contract, transition
order, approval policy, target, and entry. `planning.package.admit` atomically
stores the verified package, `planning.package.approve` binds approval to its
exact manifest, and `planning.package.promote` publishes the approved canonical
plan. Repositories that omit these operations retain the existing plan path;
StandardFlow does not add them automatically.
