import {
  all,
  defineFlow,
  entry,
  entryInput,
  fact,
  foregroundWork,
  instructionAsset,
  marked,
  schemaAsset,
  workArtifact,
} from "@operatorstack/boatstack";
import {
  inbox,
  planningPackageAdmit,
  planningPackageApprove,
  planningPackagePromote,
  softwareDelivery,
  trustedDelegation,
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
  { id: "plan.abandon", priority: 31 },
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

export default defineFlow(softwareDelivery({
  id: "product-delivery",
  version: "1",
  humanIdentity: "developer",
  lifecycle: lifecycle,
  planningPackageWork: planning,
  targets: [
    marked("published-pr", all(
      fact("verification", ["current"]),
      fact("configuration", ["verified"]),
      fact("runtime", ["verified"]),
      fact("publication", ["open"]),
    )),
    marked("safely-abandoned", all(
      fact("delivery", ["discarded"]),
      fact("workspace", ["abandoned", "absent"]),
    )),
  ],
  entries: [
    entry({
      id: "run",
      target: "published-pr",
      inputs: [inbox(".boatstack/plans/inbox")],
      delegation: trustedDelegation("autonomy"),
    }),
    entry({
      id: "abandon",
      target: "safely-abandoned",
      inputs: [inbox(".boatstack/plans/inbox")],
    }),
  ],
}));
