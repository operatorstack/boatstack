import {
  defineFlow,
  entry,
  evidence,
  fact,
  facet,
  fromEntryInput,
  marked,
  operator,
  transition,
} from "@operatorstack/boatstack";

export default defineFlow({
  id: "incident-response-invocation-missing",
  version: "1",
  declarations: { capabilities: ["service.restart"], authorities: ["human", "incident-commander"], effects: ["service.restart"], verifiers: ["healthcheck"], input_resolvers: ["incident-input"] },
  facets: [facet("incident", "enum", ["open", "mitigated"]), facet("service", "enum", ["degraded", "healthy"])],
  evidence: [evidence("healthcheck", "service", "observation")],
  operators: [operator("restart", {
    capabilities: ["service.restart"], authority: { any_of: ["incident-commander"] }, effects: ["service.restart"], verifier: "healthcheck", recovery: "restart", execution_context: "preserve",
    parameters: [
      { id: "incident", type: { kind: "string" }, required: true, secret: false, allowed_sources: ["entry-input"], authority: {} },
      { id: "channel", type: { kind: "string" }, required: true, secret: false, allowed_sources: ["host-input"], authority: { any_of: ["human"] } },
    ],
    state_effect: { kind: "assignments", assignments: [{ facet: "incident", value: "mitigated" }] },
  })],
  transitions: [transition("restart", "restart", {
    guard: fact("incident", ["open"]), target: fact("incident", ["mitigated"]), priority: 10,
    parameters: [{ parameter: "incident", producer: fromEntryInput("incident") }],
  })],
  targets: [marked("mitigated", fact("incident", ["mitigated"]))],
  entries: [entry({ id: "respond", target: "mitigated", inputs: [{ id: "incident", type: "text", required: true, resolver: "incident-input" }] })],
});
