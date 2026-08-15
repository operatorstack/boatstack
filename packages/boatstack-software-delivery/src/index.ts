/**
 * Trusted software-delivery bindings for repository-owned Boatstack Flows.
 *
 * Repositories choose lifecycle membership, priorities, targets, entries, and
 * additional mandatory authority. Boatstack's trusted binding registry owns
 * effects, handlers, minimum capability, authority, verification, and recovery
 * semantics.
 *
 * @packageDocumentation
 */

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
  type WorkContract,
} from "@operatorstack/boatstack";

const bindingPrefix = "software-delivery/";
/** Trusted resolver reference used by {@link inbox}. */
export const planInboxResolver = "software-delivery.plan-inbox";

/** State facets declared by the trusted software-delivery domain adapter. */
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

/** Evidence relations declared by the software-delivery domain adapter. */
export const softwareDeliveryEvidence: EvidenceDefinition[] = [
  { id: "plan-evidence", subject: "plan", kind: "artifact" },
  {
    id: "publication-evidence",
    subject: "publication",
    kind: "provider-observation",
  },
];

/** Selects a trusted software-delivery operation and its repository priority. */
export interface TrustedStep {
  id: string;
  priority: number;
}

/** Admits a completed planning package into the delivery lifecycle. */
export const planningPackageAdmit: TrustedStep = {
  id: "planning.package.admit",
  priority: 43,
};
/** Records approval of the exact admitted planning package. */
export const planningPackageApprove: TrustedStep = {
  id: "planning.package.approve",
  priority: 44,
};
/** Promotes an approved planning package into the active delivery plan. */
export const planningPackagePromote: TrustedStep = {
  id: "planning.package.promote",
  priority: 45,
};

/**
 * Repository-owned strengthening applied to a trusted transition.
 *
 * Authorities listed here are additional mandatory requirements. They cannot
 * replace trusted alternatives, weaken provider requirements, or grant
 * authority.
 */
export interface TrustedTransitionOptions {
  requires?: { authorities?: string[] };
  /** Foreground work that must complete before this transition is admitted. */
  work?: WorkContract;
}

/**
 * Requests trusted run-scoped autonomy delegation for an entry.
 *
 * This declaration does not grant authority. Boatstack materializes authority
 * only from a runtime-owned authorization bound to the exact run.
 *
 * @example
 * ```ts
 * entry({
 *   id: "run",
 *   target: "published-pr",
 *   delegation: trustedDelegation("autonomy"),
 * })
 * ```
 */
export function trustedDelegation(
  authority: "autonomy",
): DelegationBindingDefinition {
  return {
    reference: `${bindingPrefix}delegation/${authority}`,
    version: "1",
  };
}

/**
 * Requires exactly one regular Markdown plan from a repository inbox.
 *
 * Input selection happens before managed run state is created. Zero or several
 * eligible plans produce a typed blocker rather than an arbitrary choice.
 */
export function inbox(path: string): EntryInputDefinition {
  return {
    id: "plan",
    type: "markdown-file",
    required: true,
    resolver: planInboxResolver,
    config: { path, cardinality: "exactly-one" },
  };
}

/** Resolves one lifecycle step through the trusted operator registry. */
export function trustedOperator(step: TrustedStep): OperatorDefinition {
  return operator(step.id, {
    binding: { reference: `${bindingPrefix}${step.id}`, version: "1" },
  });
}

/** Resolves a lifecycle list through the trusted operator registry. */
export function trustedOperators(steps: TrustedStep[]): OperatorDefinition[] {
  return steps.map(trustedOperator);
}

/**
 * Declares one repository-selected transition backed by a trusted operation.
 *
 * The repository may set priority and add mandatory authority through
 * `options.requires`. It cannot override the trusted effect or handler.
 */
export function trustedTransition(
  step: TrustedStep,
  options: TrustedTransitionOptions = {},
): TransitionDefinition {
  return transition(step.id, step.id, {
    guard: always,
    target: always,
    priority: step.priority,
    ...(options.requires ? { requires: options.requires } : {}),
    ...(options.work ? { work: options.work.id } : {}),
  });
}

/**
 * Declares trusted transitions for a lifecycle list.
 *
 * Pass the same lifecycle used by {@link trustedOperators}; compilation rejects
 * unresolved operator references, but authors should keep this pairing explicit.
 */
export function trustedTransitions(
  steps: TrustedStep[],
  options: TrustedTransitionOptions = {},
): TransitionDefinition[] {
  return steps.map((step) => trustedTransition(step, options));
}
