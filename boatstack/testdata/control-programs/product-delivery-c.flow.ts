import { all, defineFlow, entry, fact, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedOperators,
  trustedSoftwareDeliveryTransitions,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "gate.test.record", priority: 62 },
  { id: "gate.review.record", priority: 63 },
  { id: "gate.change.record", priority: 64 },
  { id: "publication.preview", priority: 72 },
  { id: "publication.execute", priority: 76 },
  { id: "publication.observe", priority: 77 },
] satisfies TrustedStep[];

export default defineFlow({
  id: "product-delivery-c",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  operators: trustedOperators(lifecycle),
  transitions: trustedSoftwareDeliveryTransitions(lifecycle),
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
    diagnostics: { explain_on_suspend: true },
  })],
});
