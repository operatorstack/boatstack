import {
  defineFlow,
  entry,
  evidence,
  fact,
  facet,
  fromEntryInput,
  hostParameter,
  always,
  marked,
  operator,
  transition,
} from "@operatorstack/boatstack";

export default defineFlow({
  id: "incident-response-invocation",
  version: "1",
  human_identity: "developer",
  declarations: {
    authorities: ["human"],
    verifiers: ["state-effect"],
    input_resolvers: ["incident-input"],
  },
  facets: [
    facet("incident", "enum", ["open", "mitigated"]),
    facet("service", "enum", ["degraded", "healthy"]),
  ],
  evidence: [evidence("state-effect", "incident", "state-observation")],
  operators: [operator("restart", {
    capabilities: [],
    authority: { any_of: ["human"] },
    effects: [],
    verifier: "state-effect",
    execution_context: "preserve",
    parameters: [
      { id: "incident", type: { kind: "string" }, required: true, secret: false, allowed_sources: ["entry-input"], authority: {} },
      { id: "channel", type: { kind: "string" }, required: true, secret: false, allowed_sources: ["host-input"], authority: { any_of: ["human"] } },
    ],
    state_effect: { kind: "assignments", assignments: [{ facet: "incident", value: "mitigated" }] },
  })],
  transitions: [transition("restart", "restart", {
    guard: always,
    target: fact("incident", ["mitigated"]),
    priority: 10,
    parameters: [
      { parameter: "incident", producer: fromEntryInput("incident") },
      { parameter: "channel", producer: hostParameter({ id: "channel", description: "Select the response channel.", authorities: ["human"], scope: "transition" }) },
    ],
  })],
  targets: [marked("mitigated", fact("incident", ["mitigated"]))],
  entries: [entry({ id: "respond", target: "mitigated", inputs: [{ id: "incident", type: "text", required: true, resolver: "incident-input" }] })],
});
