import { all, defineFlow, entry, fact, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedOperators,
  trustedTransitions,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "publication.observe", priority: 77 },
] satisfies TrustedStep[];

export default defineFlow({
  id: "product-delivery-a",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  operators: trustedOperators(lifecycle),
  transitions: trustedTransitions(lifecycle),
  targets: [marked("published-pr", all(
    fact("verification", ["current"]),
    fact("configuration", ["verified"]),
    fact("runtime", ["verified"]),
    fact("publication", ["open"]),
  ))],
  entries: [entry("run", "published-pr", [inbox(".boatstack/plans/inbox")])],
});
