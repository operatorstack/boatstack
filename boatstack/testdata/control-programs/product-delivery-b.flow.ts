import { all, always, defineFlow, entry, fact, fromState, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedOperators,
  trustedTransition,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "publication.observe", priority: 77 },
  { id: "plan.abandon", priority: 31 },
] satisfies TrustedStep[];

export default defineFlow({
  id: "product-delivery-b",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  operators: trustedOperators(lifecycle),
  transitions: [
    trustedTransition({ id: "publication.observe", priority: 77 }, {
      parameters: { publication_id: fromState({ facet: "publication_id", availableWhen: always }) },
    }),
    trustedTransition({ id: "plan.abandon", priority: 31 }),
  ],
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
    entry({ id: "deliver", target: "published-pr", inputs: [inbox(".boatstack/plans/inbox")] }),
    entry({ id: "cancel", target: "safely-abandoned", inputs: [inbox(".boatstack/plans/inbox")] }),
  ],
});
