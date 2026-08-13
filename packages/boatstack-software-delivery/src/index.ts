import {
  always,
  facet,
  operator,
  transition,
  type EntryInputDefinition,
  type EvidenceDefinition,
  type FacetDefinition,
  type OperatorDefinition,
  type TransitionDefinition,
  type DelegationBindingDefinition,
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

export const softwareDeliveryEvidence: EvidenceDefinition[] = [
  { id: "plan-evidence", subject: "plan", kind: "artifact" },
  {
    id: "publication-evidence",
    subject: "publication",
    kind: "provider-observation",
  },
];

export interface TrustedStep {
  id: string;
  priority: number;
}

export interface TrustedTransitionOptions {
  requires?: { authorities?: string[] };
}

export function trustedDelegation(
  authority: "autonomy",
): DelegationBindingDefinition {
  return {
    reference: `${bindingPrefix}delegation/${authority}`,
    version: "1",
  };
}

export function inbox(path: string): EntryInputDefinition {
  return {
    id: "plan",
    type: "markdown-file",
    required: true,
    resolver: planInboxResolver,
    config: { path, cardinality: "exactly-one" },
  };
}

export function trustedOperator(step: TrustedStep): OperatorDefinition {
  return operator(step.id, {
    binding: { reference: `${bindingPrefix}${step.id}`, version: "1" },
  });
}

export function trustedOperators(steps: TrustedStep[]): OperatorDefinition[] {
  return steps.map(trustedOperator);
}

export function trustedTransition(
  step: TrustedStep,
  options: TrustedTransitionOptions = {},
): TransitionDefinition {
  return transition(step.id, step.id, {
    guard: always,
    target: always,
    priority: step.priority,
    ...(options.requires ? { requires: options.requires } : {}),
  });
}

export function trustedTransitions(
  steps: TrustedStep[],
  options: TrustedTransitionOptions = {},
): TransitionDefinition[] {
  return steps.map((step) => trustedTransition(step, options));
}
