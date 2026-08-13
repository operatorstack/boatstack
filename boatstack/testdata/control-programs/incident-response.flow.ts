import {
  defineFlow,
  entry,
  evidence,
  fact,
  facet,
  marked,
  operator,
  transition,
} from "@operatorstack/boatstack";

export default defineFlow({
  id: "incident-response",
  version: "1",
  declarations: {
    capabilities: ["service.restart"],
    authorities: ["incident-commander"],
    effects: ["service.restart"],
    verifiers: ["healthcheck"],
  },
  facets: [
    facet("incident", "enum", ["open", "mitigated"]),
    facet("service", "enum", ["degraded", "healthy"]),
  ],
  evidence: [evidence("healthcheck", "service", "observation")],
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
    }),
  ],
  targets: [marked("mitigated", fact("incident", ["mitigated"]))],
  entries: [entry({ id: "respond", target: "mitigated" })],
});
