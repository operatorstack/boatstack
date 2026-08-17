import {
  defineFlow,
  entry,
  fact,
  marked,
} from "@operatorstack/boatstack";
import {
  softwareDelivery,
  trustedDelegation,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "plan.activate", priority: 50 },
];

export default defineFlow(softwareDelivery({
  id: "example-product",
  version: "1",
  humanIdentity: "developer",
  lifecycle: lifecycle,
  targets: [
    marked("active-plan", fact("plan", ["active"])),
  ],
  entries: [
    entry({
      id: "run",
      target: "active-plan",
      requires: { authorities: ["human"] },
      delegation: trustedDelegation("autonomy"),
    }),
  ],
}));
