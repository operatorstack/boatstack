import { all, always, defineFlow, entry, fact, fromState, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedOperators,
  trustedDelegation,
  trustedTransition,
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
  transitions: [trustedTransition({ id: "publication.observe", priority: 77 }, {
    parameters: { publication_id: fromState({ facet: "publication_id", availableWhen: always }) },
  })],
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
