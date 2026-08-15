import {
  defineFlow,
  entry,
  entryInput,
  fact,
  facet,
  foregroundWork,
  instructionAsset,
  marked,
  operator,
  schemaAsset,
  transition,
  workArtifact,
} from "@operatorstack/boatstack";

const diagnosis = foregroundWork({
  id: "diagnose",
  instructions: instructionAsset("boatstack/testdata/control-programs/assets/diagnose.md"),
  inputs: [entryInput("incident")],
  outputs: [
    workArtifact({
      id: "diagnosis",
      path: "diagnosis.json",
      media_type: "application/json",
      required: true,
      max_bytes: 65536,
      schema: schemaAsset("boatstack/testdata/control-programs/assets/diagnosis.schema.json"),
    }),
  ],
});

export default defineFlow({
  id: "incident-response-work",
  version: "1",
  declarations: {
    capabilities: ["service.restart"],
    authorities: ["incident-commander"],
    effects: ["service.restart"],
    verifiers: ["healthcheck"],
    input_resolvers: ["incident.input"],
  },
  facets: [facet("incident", "enum", ["open", "mitigated"])],
  work: [diagnosis],
  operators: [
    operator("restart", {
      capabilities: ["service.restart"],
      authority: { any_of: ["incident-commander"] },
      effects: ["service.restart"],
      verifier: "healthcheck",
      recovery: "restart",
      execution_context: "preserve",
      state_effect: {
        kind: "assignments",
        assignments: [{ facet: "incident", value: "mitigated" }],
      },
    }),
  ],
  transitions: [
    transition("restart", "restart", {
      guard: fact("incident", ["open"]),
      target: fact("incident", ["mitigated"]),
      priority: 10,
      work: "diagnose",
    }),
  ],
  targets: [marked("mitigated", fact("incident", ["mitigated"]))],
  entries: [
    entry({
      id: "respond",
      target: "mitigated",
      inputs: [
        {
          id: "incident",
          type: "json",
          required: true,
          resolver: "incident.input",
          config: {},
        },
      ],
    }),
  ],
});
