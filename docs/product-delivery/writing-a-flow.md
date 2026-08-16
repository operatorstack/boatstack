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
  trustedSoftwareDeliveryTransitions,
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
  transitions: trustedSoftwareDeliveryTransitions(lifecycle, {
    planningPackageWork: planning,
  }),
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
skill. Trusted resolvers are read-only.

The standard producer ownership is:

| Value | Canonical producer |
| --- | --- |
| admitted planning-package fingerprint | trusted manifest resolver |
| workspace branch, preview fingerprint, recovery IDs | durable state |
| publication ID | `publication.execute` effect receipt |
| gate evidence path and fingerprint | trusted canonical-artifact resolver |
| source revision | trusted committed-HEAD resolver |
| publication body path and fingerprint | trusted canonical-artifact resolver |
| next slice ID | genuinely actor-owned host input |

Human or delegated approval remains an authority decision. The approving actor
does not type deterministic values such as the admitted package fingerprint.
Only `delivery.slice.advance.slice_id` is free-form in the standard lifecycle.
A missing free-form input returns a typed `TRANSITION_INPUT_REQUIRED`
suspension; record its answer with `boatstack flow input answer`, then resume
the same run. If the value is semantically rejected, use
`boatstack flow input supersede` to issue a linked request generation. Do not
edit or delete the old request or receipt.

Prepare gate evidence at
`.boatstack/evidence/<delivery-id>/<gate>.input.json`, visual evidence at
`.boatstack/evidence/<delivery-id>/visual-manifest.input.json`, and the pull
request body at `.boatstack/publication/<delivery-id>.body.md`. Boatstack binds
the exact path and content fingerprint when the transition is materialized.

The planning-package operations are optional trusted mechanisms. A repository
that includes them owns the planning instruction, output contract, transition
order, approval policy, target, and entry. `planning.package.admit` atomically
stores the verified package, `planning.package.approve` binds approval to its
exact manifest, and `planning.package.promote` publishes the approved canonical
plan. Repositories that omit these operations retain the existing plan path;
StandardFlow does not add them automatically.
