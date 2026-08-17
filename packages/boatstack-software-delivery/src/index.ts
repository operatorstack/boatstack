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
  fact,
  fromState,
  fromStateOrReceipt,
  hostParameter,
  operator,
  trustedParameterResolver,
  transition,
  type EntryInputDefinition,
  type EntryDefinition,
  type EvidenceDefinition,
  type FacetDefinition,
  type FlowDefinition,
  type OperatorDefinition,
  type ParameterProducer,
  type TargetDefinition,
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
  "preview_fingerprint",
  "publication_id",
  "recovery_transaction_id",
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
 * Repository-owned policy composed with canonical software-delivery wiring.
 *
 * Lifecycle membership, priorities, work, targets, and entries remain explicit.
 * This input contains data only and does not grant authority or execute code.
 */
export interface SoftwareDeliveryFlowDefinition {
  /** Stable repository-selected Control Program identity. */
  id: string;
  /** Repository-selected Control Program version. */
  version: string;
  /** Optional human-readable description passed through unchanged. */
  description?: string;
  /** Explicit trusted lifecycle membership and repository-selected priorities. */
  lifecycle: TrustedStep[];
  /** Foreground work bound specifically to `planning.package.admit`. */
  planningPackageWork?: WorkContract;
  /** Additional explicit work contracts, in repository-selected order. */
  work?: WorkContract[];
  /** Explicit repository completion predicates. */
  targets: TargetDefinition[];
  /** Explicit repository invocation surfaces. */
  entries: EntryDefinition[];
}

function validateSoftwareDeliveryDefinition(
  definition: SoftwareDeliveryFlowDefinition,
): void {
  const lifecycleIDs = new Set<string>();
  for (const step of definition.lifecycle) {
    if (step.id.trim().length === 0) {
      throw new Error(
        "SOFTWARE_DELIVERY_LIFECYCLE_EMPTY: lifecycle IDs must be non-empty",
      );
    }
    if (lifecycleIDs.has(step.id)) {
      throw new Error(
        `SOFTWARE_DELIVERY_LIFECYCLE_DUPLICATE: lifecycle ID ${JSON.stringify(step.id)} appears more than once`,
      );
    }
    lifecycleIDs.add(step.id);
    if (!Number.isSafeInteger(step.priority)) {
      throw new Error(
        `SOFTWARE_DELIVERY_PRIORITY_INVALID: priority for ${JSON.stringify(step.id)} must be a finite safe integer`,
      );
    }
  }

  const workIDs = new Set<string>();
  const work = definition.planningPackageWork
    ? [definition.planningPackageWork, ...(definition.work ?? [])]
    : definition.work ?? [];
  for (const contract of work) {
    if (workIDs.has(contract.id)) {
      throw new Error(
        `SOFTWARE_DELIVERY_WORK_DUPLICATE: work ID ${JSON.stringify(contract.id)} appears more than once`,
      );
    }
    workIDs.add(contract.id);
  }

  if (
    definition.planningPackageWork &&
    !lifecycleIDs.has(planningPackageAdmit.id)
  ) {
    throw new Error(
      "SOFTWARE_DELIVERY_PLANNING_WORK_UNUSED: planningPackageWork requires exactly one planning.package.admit lifecycle step",
    );
  }
}

function referencedInputResolvers(entries: EntryDefinition[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const entry of entries) {
    for (const input of entry.inputs ?? []) {
      if (!input.resolver || seen.has(input.resolver)) {
        continue;
      }
      seen.add(input.resolver);
      result.push(input.resolver);
    }
  }
  return result;
}

/**
 * Composes explicit repository policy with canonical software-delivery wiring.
 *
 * The returned value is a regular {@link FlowDefinition}; callers still pass it
 * to `defineFlow`, the sole raw-IR lowering boundary. This helper selects no
 * lifecycle members, priorities, targets, entries, authority, or delegation.
 */
export function softwareDelivery(
  definition: SoftwareDeliveryFlowDefinition,
): FlowDefinition {
  validateSoftwareDeliveryDefinition(definition);

  const inputResolvers = referencedInputResolvers(definition.entries);
  const work = definition.planningPackageWork
    ? [definition.planningPackageWork, ...(definition.work ?? [])]
    : [...(definition.work ?? [])];

  return {
    id: definition.id,
    version: definition.version,
    ...(definition.description === undefined
      ? {}
      : { description: definition.description }),
    ...(inputResolvers.length === 0
      ? {}
      : { declarations: { input_resolvers: inputResolvers } }),
    facets: softwareDeliveryFacets.map((facetDefinition) => ({
      ...facetDefinition,
      ...(facetDefinition.values ? { values: [...facetDefinition.values] } : {}),
    })),
    evidence: softwareDeliveryEvidence.map((evidenceDefinition) => ({
      ...evidenceDefinition,
    })),
    work,
    operators: trustedOperators(definition.lifecycle),
    transitions: trustedSoftwareDeliveryTransitions(definition.lifecycle, {
      planningPackageWork: definition.planningPackageWork,
    }),
    targets: [...definition.targets],
    entries: [...definition.entries],
  };
}

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
  /** One repository-selected producer for each trusted operator parameter. */
  parameters?: Record<string, ParameterProducer>;
}

/** Reads the exact verified repository default branch. */
export function repositoryDefaultBranch(): ParameterProducer {
  return trustedParameterResolver(
    "software-delivery/repository-default-branch",
    "1",
  );
}

/** Derives a non-conflicting managed branch from the exact delivery context. */
export function deliveryBranch(): ParameterProducer {
  return trustedParameterResolver("software-delivery/delivery-branch", "1");
}

/** Derives the managed destination within Boatstack's trusted worktree root. */
export function managedWorktreeDestination(): ParameterProducer {
  return trustedParameterResolver(
    "software-delivery/managed-worktree-destination",
    "1",
  );
}

/** Reads the exact fingerprint of the planning-package manifest admitted for this delivery. */
export function admittedPlanningPackageFingerprint(): ParameterProducer {
  return trustedParameterResolver(
    "software-delivery/admitted-planning-package-fingerprint",
    "1",
  );
}

/** Reads the exact committed revision of the invoking worktree. */
export function currentSourceRevision(): ParameterProducer {
  return trustedParameterResolver("software-delivery/current-source-revision", "1");
}

/** Reads one canonical gate-evidence input path prepared for this delivery. */
export function gateEvidencePath(gate: "build" | "test" | "review" | "change" | "journey"): ParameterProducer {
  return trustedParameterResolver(`software-delivery/gate-evidence-path/${gate}`, "1");
}

/** Hashes the exact canonical gate-evidence input prepared for this delivery. */
export function gateEvidenceFingerprint(gate: "build" | "test" | "review" | "change" | "journey"): ParameterProducer {
  return trustedParameterResolver(`software-delivery/gate-evidence-fingerprint/${gate}`, "1");
}

/** Reads the canonical visual-evidence manifest path for this delivery. */
export function visualEvidenceManifestPath(): ParameterProducer {
  return trustedParameterResolver("software-delivery/visual-evidence-manifest-path", "1");
}

/** Hashes the canonical visual-evidence manifest for its privacy receipt. */
export function visualEvidencePrivacyReceipt(): ParameterProducer {
  return trustedParameterResolver("software-delivery/visual-evidence-privacy-receipt", "1");
}

/** Reads the canonical pull-request body path for this delivery. */
export function publicationBodyPath(): ParameterProducer {
  return trustedParameterResolver("software-delivery/publication-body-path", "1");
}

/** Hashes the canonical pull-request body for a correction. */
export function publicationBodyFingerprint(): ParameterProducer {
  return trustedParameterResolver("software-delivery/publication-body-sha256", "1");
}

function durableValue(facet: string): ParameterProducer {
  return fromState({ facet, availableWhen: fact(facet) });
}

/** Resolves the exact transaction identity from current recovery observation. */
export function observedRecoveryTransaction(): ParameterProducer {
  return fromState({
    facet: "recovery_transaction_id",
    availableWhen: fact("recovery_transaction_id"),
  });
}

function canonicalGateParameters(gate: "build" | "test" | "review" | "change" | "journey"): Record<string, ParameterProducer> {
  return {
    source_revision: currentSourceRevision(),
    evidence_path: gateEvidencePath(gate),
    evidence_fingerprint: gateEvidenceFingerprint(gate),
  };
}

/**
 * Returns the standard explicit producer bindings for one trusted lifecycle step.
 * This helper declares data ownership only; it does not infer values at runtime.
 */
export function standardSoftwareDeliveryParameters(step: TrustedStep): Record<string, ParameterProducer> {
  switch (step.id) {
    case "planning.package.approve":
      return { package_fingerprint: admittedPlanningPackageFingerprint() };
    case "workspace.cut":
      return {
        branch: deliveryBranch(),
        base_ref: repositoryDefaultBranch(),
        destination: managedWorktreeDestination(),
      };
    case "workspace.activate":
    case "workspace.sync":
    case "workspace.publish":
      return { branch: durableValue("workspace_branch") };
    case "gate.build.record":
      return canonicalGateParameters("build");
    case "gate.test.record":
      return canonicalGateParameters("test");
    case "gate.review.record":
      return canonicalGateParameters("review");
    case "gate.change.record":
      return canonicalGateParameters("change");
    case "gate.journey.record":
      return canonicalGateParameters("journey");
    case "evidence.visual.attach":
      return {
        manifest_path: visualEvidenceManifestPath(),
        privacy_receipt: visualEvidencePrivacyReceipt(),
        source_revision: currentSourceRevision(),
      };
    case "delivery.slice.advance":
      return {
        slice_id: hostParameter({
          id: "delivery-slice",
          description: "Select the next bounded delivery slice.",
          authorities: ["human", "autonomy"],
          scope: "transition",
        }),
        source_revision: currentSourceRevision(),
      };
    case "publication.preview":
      return {
        base_ref: repositoryDefaultBranch(),
        head_ref: durableValue("workspace_branch"),
        body_path: publicationBodyPath(),
      };
    case "publication.execute":
      return { preview_fingerprint: durableValue("preview_fingerprint") };
    case "publication.observe":
      return {
        publication_id: fromStateOrReceipt({
          facet: "publication_id",
          availableWhen: fact("publication_id"),
          transition: "publication.execute",
          field: "publication_id",
        }),
      };
    case "publication.correct":
      return {
        publication_id: durableValue("publication_id"),
        body_path: publicationBodyPath(),
        body_sha256: publicationBodyFingerprint(),
      };
    case "workspace.reconcile":
      return { transaction_id: observedRecoveryTransaction() };
    case "publication.reconcile":
      return {
        transaction_id: observedRecoveryTransaction(),
      };
    default:
      return {};
  }
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
  const parameters = options.parameters ?? {};
  return transition(step.id, step.id, {
    guard: always,
    target: always,
    priority: step.priority,
    ...(options.requires ? { requires: options.requires } : {}),
    ...(options.work ? { work: options.work.id } : {}),
    ...(Object.keys(parameters).length !== 0
      ? {
          parameters: Object.entries(parameters).map(
            ([parameter, producer]) => ({ parameter, producer }),
          ),
        }
      : {}),
  });
}

/**
 * Declares one standard software-delivery transition with its canonical,
 * explicit producer bindings. Authority remains owned by the trusted operator.
 */
export function trustedSoftwareDeliveryTransition(
  step: TrustedStep,
  options: Omit<TrustedTransitionOptions, "parameters"> = {},
): TransitionDefinition {
  const parameters = standardSoftwareDeliveryParameters(step);
  return trustedTransition(step, {
    ...options,
    ...(Object.keys(parameters).length !== 0 ? { parameters } : {}),
  });
}

/** Expands a standard lifecycle into transitions with explicit canonical producers. */
export function trustedSoftwareDeliveryTransitions(
  steps: TrustedStep[],
  options: { planningPackageWork?: WorkContract } = {},
): TransitionDefinition[] {
  return steps.map((step) =>
    trustedSoftwareDeliveryTransition(step, {
      ...(step.id === planningPackageAdmit.id && options.planningPackageWork
        ? { work: options.planningPackageWork }
        : {}),
    }),
  );
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
