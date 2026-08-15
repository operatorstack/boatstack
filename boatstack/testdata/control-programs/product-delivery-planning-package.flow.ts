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
  schemaAsset,
  workArtifact,
} from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  repositoryDefaultBranch,
  deliveryBranch,
  managedWorktreeDestination,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedDelegation,
  trustedOperators,
  trustedTransition,
} from "@operatorstack/boatstack-software-delivery";

const planning = foregroundWork({
  id: "planning-package",
  instructions: instructionAsset("boatstack/testdata/control-programs/assets/planning-package.md"),
  inputs: [entryInput("plan")],
  outputs: [
    workArtifact({ id: "plan", path: "plan.md", media_type: "text/markdown", required: true, max_bytes: 262144 }),
    workArtifact({ id: "feature-spec", path: "feature-spec.md", media_type: "text/markdown", required: true, max_bytes: 262144 }),
    workArtifact({ id: "questions", path: "questions.md", media_type: "text/markdown", required: true, max_bytes: 131072 }),
    workArtifact({ id: "test-plan", path: "test-plan.md", media_type: "text/markdown", required: true, max_bytes: 262144 }),
    workArtifact({ id: "gaps", path: "gaps.md", media_type: "text/markdown", required: false, max_bytes: 131072 }),
    workArtifact({ id: "autonomy", path: "autonomy.md", media_type: "text/markdown", required: true, max_bytes: 131072 }),
    workArtifact({ id: "tasks", path: "compiled/tasks.json", media_type: "application/json", required: true, max_bytes: 262144, schema: schemaAsset("boatstack/testdata/control-programs/assets/planning-list.schema.json") }),
    workArtifact({ id: "test-matrix", path: "compiled/test-matrix.json", media_type: "application/json", required: true, max_bytes: 262144, schema: schemaAsset("boatstack/testdata/control-programs/assets/planning-list.schema.json") }),
    workArtifact({ id: "journey-oracles", path: "compiled/journey-oracles.json", media_type: "application/json", required: true, max_bytes: 262144, schema: schemaAsset("boatstack/testdata/control-programs/assets/planning-list.schema.json") }),
    workArtifact({ id: "evidence", path: "compiled/evidence.md", media_type: "text/markdown", required: true, max_bytes: 131072 }),
  ],
});

const lifecycle = [
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  { id: "plan.activate", priority: 50 },
  { id: "workspace.cut", priority: 52 },
  { id: "workspace.activate", priority: 53 },
  { id: "workspace.sync", priority: 58 },
  { id: "gate.build.record", priority: 61 },
  { id: "gate.test.record", priority: 62 },
  { id: "gate.review.record", priority: 63 },
  { id: "gate.change.record", priority: 64 },
  { id: "gate.journey.record", priority: 64 },
  { id: "evidence.visual.attach", priority: 66 },
  { id: "delivery.slice.advance", priority: 68 },
  { id: "publication.preview", priority: 72 },
  { id: "workspace.publish", priority: 75 },
  { id: "publication.execute", priority: 76 },
  { id: "publication.observe", priority: 77 },
  { id: "publication.correct", priority: 80 },
  { id: "workspace.reconcile", priority: 2 },
  { id: "publication.reconcile", priority: 1 },
];

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
    trustedTransition(planningPackageApprove, { parameters: {
      package_fingerprint: hostParameter({ id: "package-fingerprint", description: "package-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition(planningPackagePromote),
    trustedTransition({ id: "plan.activate", priority: 50 }),
    trustedTransition({ id: "workspace.cut", priority: 52 }, { parameters: {
      branch: deliveryBranch(), base_ref: repositoryDefaultBranch(), destination: managedWorktreeDestination(),
    }}),
    trustedTransition({ id: "workspace.activate", priority: 53 }, { parameters: { branch: hostParameter({ id: "branch", description: "branch", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "workspace.sync", priority: 58 }, { parameters: { branch: hostParameter({ id: "branch", description: "branch", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "gate.build.record", priority: 61 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
      evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }),
      evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.test.record", priority: 62 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }), evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }), evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.review.record", priority: 63 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }), evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }), evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.change.record", priority: 64 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }), evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }), evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.journey.record", priority: 64 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }), evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }), evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "evidence.visual.attach", priority: 66 }, { parameters: {
      manifest_path: hostParameter({ id: "manifest-path", description: "manifest-path", authorities: ["human"], scope: "transition" }),
      privacy_receipt: hostParameter({ id: "privacy-receipt", description: "privacy-receipt", authorities: ["human"], scope: "transition" }),
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "delivery.slice.advance", priority: 68 }, { parameters: {
      slice_id: hostParameter({ id: "slice-id", description: "slice-id", authorities: ["human"], scope: "transition" }),
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "publication.preview", priority: 72 }, { parameters: {
      base_ref: repositoryDefaultBranch(),
      head_ref: hostParameter({ id: "head-ref", description: "head-ref", authorities: ["human"], scope: "transition" }),
      body_path: hostParameter({ id: "body-path", description: "body-path", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "workspace.publish", priority: 75 }, { parameters: { branch: hostParameter({ id: "branch", description: "branch", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "publication.execute", priority: 76 }, { parameters: { preview_fingerprint: hostParameter({ id: "preview-fingerprint", description: "preview-fingerprint", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "publication.observe", priority: 77 }, { parameters: { publication_id: hostParameter({ id: "publication-id", description: "publication-id", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "publication.correct", priority: 80 }, { parameters: {
      publication_id: hostParameter({ id: "publication-id", description: "publication-id", authorities: ["human"], scope: "transition" }),
      body_path: hostParameter({ id: "body-path", description: "body-path", authorities: ["human"], scope: "transition" }),
      body_sha256: hostParameter({ id: "body-sha256", description: "body-sha256", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "workspace.reconcile", priority: 2 }, { parameters: { transaction_id: hostParameter({ id: "transaction-id", description: "transaction-id", authorities: ["human"], scope: "transition" }) }}),
    trustedTransition({ id: "publication.reconcile", priority: 1 }, { parameters: {
      publication_id: hostParameter({ id: "publication-id", description: "publication-id", authorities: ["human"], scope: "transition" }),
      transaction_id: hostParameter({ id: "transaction-id", description: "transaction-id", authorities: ["human"], scope: "transition" }),
    }}),
  ],
  targets: [marked("published-pr", all(
    fact("verification", ["current"]),
    fact("configuration", ["verified"]),
    fact("runtime", ["verified"]),
    fact("publication", ["open"]),
  ))],
  entries: [entry({
    id: "run",
    target: "published-pr",
    inputs: [inbox(".boatstack/plans/inbox")],
    delegation: trustedDelegation("autonomy"),
  })],
});
