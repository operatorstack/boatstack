import {
  all,
  always,
  entry,
  fact,
  facet,
  marked,
  operator,
  transition,
  type EntryDefinition,
  type EntryInputDefinition,
  type FacetDefinition,
  type FlowDefinition,
  type OperatorDefinition,
  type TargetDefinition,
  type TransitionDefinition,
} from "@operatorstack/boatstack";

const bindingPrefix = "software-delivery/";
export const planInboxResolver = "software-delivery.plan-inbox";

export const softwareDeliveryFacets: FacetDefinition[] = [
  "phase",
  "program",
  "engagement",
  "objective",
  "delivery",
  "workspace",
  "plan",
  "configuration",
  "configuration-policy",
  "runtime",
  "publication",
  "verification",
  "recovery",
  "recovery-info",
  "transaction",
  "terminal",
  "recovery_budget",
  "recovery_cause",
  "recovery_resumption",
  "recovery_source_phase",
  "source_revision",
  "transaction_id",
  "transaction_transition",
  "workspace_base_ref",
  "workspace_branch",
  "workspace_path",
  "workspace_source_id",
  "workspace_source_path",
  "workspace_source_ref",
  "worktree_fingerprint",
].map((id) => facet(id, "string"));

export const publishedPR: TargetDefinition = marked(
  "published-pr",
  all(
    fact("verification", ["current"]),
    fact("configuration", ["verified"]),
    fact("runtime", ["verified"]),
    fact("publication", ["open"]),
  ),
  "A provider-observed open or updated pull request",
);

export interface TrustedStep {
  id: string;
  priority: number;
}

export const runToPublishedPR: TrustedStep[] = [
  { id: "plan.create", priority: 35 },
  { id: "plan.validate", priority: 40 },
  { id: "plan.invalidate", priority: 41 },
  { id: "plan.amend", priority: 42 },
  { id: "plan.approve", priority: 45 },
  { id: "plan.approve-amendment", priority: 46 },
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

export function inbox(path: string): EntryInputDefinition {
  return {
    id: "plan",
    type: "markdown-file",
    required: true,
    resolver: planInboxResolver,
    config: { path, cardinality: "exactly-one" },
  };
}

export function trustedOperators(steps: TrustedStep[]): OperatorDefinition[] {
  return steps.map((step) =>
    operator(step.id, {
      binding: { reference: `${bindingPrefix}${step.id}`, version: "1" },
    }),
  );
}

export function trustedTransitions(
  steps: TrustedStep[],
): TransitionDefinition[] {
  return steps.map((step) =>
    transition(step.id, step.id, {
      guard: always,
      target: always,
      priority: step.priority,
    }),
  );
}

export function productDeliveryFlow(input: {
  id: string;
  version: string;
  steps?: TrustedStep[];
  entries: EntryDefinition[];
  description?: string;
}): FlowDefinition {
  const steps = input.steps ?? runToPublishedPR;
  return {
    id: input.id,
    version: input.version,
    description: input.description,
    declarations: { input_resolvers: [planInboxResolver] },
    facets: softwareDeliveryFacets,
    evidence: [
      { id: "plan-evidence", subject: "plan", kind: "artifact" },
      {
        id: "publication-evidence",
        subject: "publication",
        kind: "provider-observation",
      },
    ],
    operators: trustedOperators(steps),
    transitions: trustedTransitions(steps),
    targets: [publishedPR],
    entries: input.entries,
  };
}

export function runEntry(path = ".boatstack/plans/inbox"): EntryDefinition {
  return entry(
    "run",
    "published-pr",
    [inbox(path)],
    "Implement one approved repository plan and publish a pull request",
  );
}
