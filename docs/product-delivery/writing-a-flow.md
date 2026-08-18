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
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  softwareDelivery,
  trustedDelegation,
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
      id: "implementation-plan",
      path: "plan.md",
      media_type: "text/markdown",
      required: true,
      max_bytes: 262144,
      guidance: instructionAsset(".boatstack/flows/assets/implementation-plan.md"),
    }),
  ],
});

export default defineFlow(softwareDelivery({
  id: "product-delivery",
  version: "1",
  humanIdentity: "developer",
  lifecycle: lifecycle,
  planningPackage: {
    work: planning,
    planOutput: "implementation-plan",
  },
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
      requires: { authorities: ["human"] },
      inputs: [inbox(".boatstack/plans/inbox")],
      delegation: trustedDelegation("autonomy"),
      diagnostics: { explain_on_suspend: true },
    }),
  ],
}));
```

`defineFlow` remains the canonical raw-IR lowering boundary.
`softwareDelivery` is a pure composition helper, not a framework, runtime, or
second Flow language. It executes no repository code and grants no authority.
It adds no lifecycle steps, priorities, targets, entries, or delegation; those
control decisions remain visible in repository source. The low-level exports
remain available for custom facets, evidence, bindings, parameter producers,
authority strengthening, and domain composition.

| Input | Derived output |
| --- | --- |
| `lifecycle` | trusted operators and transitions, including explicit additional-work bindings |
| `planningPackage` | planning work registration and explicit canonical plan-output binding |
| `work` | additional work registration in caller order; every contract ID must be referenced by a lifecycle step's `work` field |
| entry input `resolver` | `declarations.input_resolvers` |
| software-delivery domain | canonical facets and evidence |
| `targets` | unchanged |
| `entries` | unchanged |

The resulting committed IR artifact remains inspectable. The helper only
removes coordinated mechanical wiring from the authoring source.

Additional work remains explicit at both ends. Declare each contract in `work`
and name its ID on every lifecycle step that requires it, for example
`{ id: "plan.activate", priority: 50, work: "implementation" }`. The helper
rejects unknown or unreferenced work IDs; it never chooses a transition for
caller-owned work.

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
suspension. Its immutable request and answer receipt bind the current verified
control bundle and human-identity authority context. Record its answer with
`boatstack flow input answer`, then resume the same run. Identity rotation
therefore produces a fresh suspension instead of consuming an old answer. If
the value is semantically rejected, use
`boatstack flow input supersede` to issue a linked request generation. Do not
edit or delete the old request or receipt.

A declarative transition that requires human authority returns
`AUTHORITY_REQUIRED` with an exact `authority_fingerprint`. After the operator
approves the displayed transition and proposed actor, resume with both
`--authority-fingerprint <fingerprint>` and `--human <actor>`. A changed
authority or identity fingerprint requires a fresh approval.

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
