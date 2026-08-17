/**
 * Domain-neutral primitives for authoring Boatstack Control Programs.
 *
 * The TypeScript API creates declarative IR. It does not execute operators or
 * grant authority; the Boatstack runtime validates and executes the compiled
 * program.
 *
 * @packageDocumentation
 */

/** Canonical schema name emitted by {@link defineFlow}. */
export const CONTROL_PROGRAM_SCHEMA = "control-program" as const;
/** Current revision of the canonical Control Program schema. */
export const CONTROL_PROGRAM_SCHEMA_REVISION = 5 as const;

/**
 * A declarative condition over runtime state facts.
 *
 * Predicates influence executable transition and target semantics. A frontend
 * author may combine facts, but only the runtime decides whether they hold.
 */
export type Predicate =
  | { true: boolean }
  | { fact: { facet: string; statuses?: string[]; values?: string[] } }
  | { all: Predicate[] }
  | { any: Predicate[] }
  | { not: Predicate };

/** Declares one typed state facet owned by a Control Program. */
export interface FacetDefinition {
  id: string;
  kind: "enum" | "string" | "boolean";
  values?: string[];
  description?: string;
}

/** Declares a kind of evidence that a program may reference. */
export interface EvidenceDefinition {
  id: string;
  subject: string;
  kind: string;
  description?: string;
}

/** Requires a facet to hold one of the listed values before an effect runs. */
export interface StatePrecondition {
  facet: string;
  values: string[];
}

/** Selects a state value from trusted runtime input instead of a literal. */
export interface StateValueReference {
  parameter?: string;
  admission?: string;
  invocation?: string;
}

/** Assigns one declared facet from a literal or trusted runtime value. */
export interface StateAssignment {
  facet: string;
  value?: string;
  value_from?: StateValueReference;
}

/**
 * Declares the state mutation associated with an operator.
 *
 * Native handlers are references resolved by the trusted runtime. Repository
 * source cannot provide executable handler code through this type.
 */
export type StateEffectDefinition =
  | {
      kind: "assignments";
      preconditions?: StatePrecondition[];
      assignments: StateAssignment[];
    }
  | {
      kind: "native";
      preconditions?: StatePrecondition[];
      native_handler: string;
    };

/**
 * Declares an operator and its trusted execution contract.
 *
 * Capabilities, authority, effects, verification, recovery, and execution
 * context are executable control semantics. Domain adapters should normally
 * supply trusted bindings instead of asking repository Flows to repeat them.
 */
export interface OperatorDefinition {
  id: string;
  binding?: { reference: string; version: string };
  capabilities?: string[];
  authority?: { any_of?: string[]; all_of?: string[] };
  effects?: string[];
  verifier?: string;
  recovery?: string;
  state_effect?: StateEffectDefinition;
  execution_context?: "preserve" | "advance";
  parameters?: OperatorParameterDefinition[];
  /** Trusted binding-owned committed transition-receipt fields. */
  outputs?: { id: string; type: ValueTypeDefinition }[];
  description?: string;
}

/** Closed producer vocabulary accepted by invocation-completeness analysis. */
export type ParameterSourceKind =
  | "entry-input"
  | "state"
  | "receipt"
  | "state-or-receipt"
  | "work-output"
  | "trusted-resolver"
  | "host-input";

/** Immutable trusted validator reference resolved by the compiler. */
export interface TrustedValidatorBinding {
  reference: string;
  version: string;
  fingerprint: string;
}

/** Canonical value contract for one operator parameter. */
export type ValueTypeDefinition =
  | { kind: "string"; validator?: TrustedValidatorBinding }
  | { kind: "boolean" }
  | { kind: "integer"; minimum?: number; maximum?: number }
  | { kind: "json"; schema?: TrustedValidatorBinding };

/** Trusted operator-owned requirements for one invocation parameter. */
export interface OperatorParameterDefinition {
  id: string;
  type: ValueTypeDefinition;
  required: boolean;
  secret: boolean;
  allowed_sources: ParameterSourceKind[];
  authority: { any_of?: string[]; all_of?: string[] };
}

/** Exactly one repository-selected source for a transition parameter. */
export type ParameterProducer =
  | { kind: "entry-input"; input: string }
  | { kind: "state"; facet: string; available_when: Predicate }
  | { kind: "receipt"; transition: string; field: string }
  | {
      kind: "state-or-receipt";
      facet: string;
      available_when: Predicate;
      transition: string;
      field: string;
    }
  | { kind: "work-output"; work: string; output: string }
  | {
      kind: "trusted-resolver";
      binding: { reference: string; version: string; fingerprint?: string };
    }
  | {
      kind: "host-input";
      request: {
        id: string;
        description: string;
        authorities: string[];
        scope: "transition";
      };
    };

/** Binds one trusted operator parameter to its declared producer. */
export interface TransitionParameterBinding {
  parameter: string;
  producer: ParameterProducer;
}

/**
 * A repository asset resolved and fingerprinted by the trusted compiler.
 *
 * Flow source supplies only {@link path}. Compiled artifacts also contain the
 * exact UTF-8 bytes and SHA-256 used by the runtime; live runs never reread a
 * mutable instruction or schema file.
 */
export interface WorkAssetDefinition {
  path: string;
  sha256?: string;
  content?: string;
}

/** Binds one foreground-work input to a declared entry input. */
export interface WorkInputDefinition {
  id: string;
  entry_input: string;
}

/** Declares one bounded staged output of foreground work. */
export interface WorkArtifactDefinition {
  id: string;
  path: string;
  media_type: string;
  required: boolean;
  max_bytes?: number;
  schema?: WorkAssetDefinition;
}

/**
 * A bounded foreground-work requirement used by one or more transitions.
 *
 * This contract contains data only. It cannot grant authority, execute code,
 * install a handler, or advance Flow state independently.
 */
export interface WorkContract {
  id: string;
  instructions: WorkAssetDefinition;
  inputs: WorkInputDefinition[];
  outputs: WorkArtifactDefinition[];
  description?: string;
}

/**
 * Selects an operator when its guard is true and records its intended target
 * relation. Higher priority controls deterministic selection among admissible
 * transitions.
 */
export interface TransitionDefinition {
  id: string;
  operator: string;
  guard: Predicate;
  target: Predicate;
  priority: number;
  requires?: { authorities?: string[] };
  work?: string;
  parameters?: TransitionParameterBinding[];
  description?: string;
}

/** A named marked condition that counts as completion for an entry. */
export interface TargetDefinition {
  id: string;
  predicate: Predicate;
  description?: string;
}

/** Declares one typed value that must be resolved before a run starts. */
export interface EntryInputDefinition {
  id: string;
  type: string;
  required: boolean;
  resolver?: string;
  config?: unknown;
}

/**
 * A named invocation surface that selects a target and its required inputs.
 *
 * Delegation requests a trusted runtime mechanism; it never grants authority.
 * Diagnostics affect generated-agent projection only and do not change the
 * executable Control Program fingerprint.
 */
export interface EntryDefinition {
  id: string;
  target: string;
  inputs?: EntryInputDefinition[];
  delegation?: DelegationBindingDefinition;
  diagnostics?: { explain_on_suspend?: boolean };
  description?: string;
}

/** References a trusted delegation implementation resolved by Boatstack. */
export interface DelegationBindingDefinition {
  reference: string;
  version: string;
}

/** Complete domain-neutral authoring input accepted by {@link defineFlow}. */
export interface FlowDefinition {
  id: string;
  version: string;
  human_identity?: string;
  description?: string;
  declarations?: {
    capabilities?: string[];
    authorities?: string[];
    effects?: string[];
    verifiers?: string[];
    input_resolvers?: string[];
  };
  facets: FacetDefinition[];
  evidence?: EvidenceDefinition[];
  work?: WorkContract[];
  operators: OperatorDefinition[];
  transitions: TransitionDefinition[];
  targets: TargetDefinition[];
  entries: EntryDefinition[];
}

/** Canonical raw IR emitted by the TypeScript authoring frontend. */
export interface ControlProgramIR {
  schema: typeof CONTROL_PROGRAM_SCHEMA;
  schema_revision: typeof CONTROL_PROGRAM_SCHEMA_REVISION;
  program: { id: string; version: string; human_identity?: string; description?: string };
  declarations: NonNullable<FlowDefinition["declarations"]>;
  facets: FacetDefinition[];
  evidence: EvidenceDefinition[];
  work: WorkContract[];
  operators: OperatorDefinition[];
  transitions: TransitionDefinition[];
  targets: TargetDefinition[];
  entries: EntryDefinition[];
  description?: string;
}

/**
 * Lowers a declarative Flow definition to raw Control Program IR.
 *
 * Boatstack still canonicalizes, validates, resolves trusted bindings, and
 * fingerprints the result. Calling this function does not execute the Flow.
 *
 * @example
 * ```ts
 * const flow = defineFlow({
 *   id: "example",
 *   version: "1",
 *   facets: [facet("status", "enum", ["open", "done"])],
 *   operators: [],
 *   transitions: [],
 *   targets: [marked("done", fact("status", ["done"]))],
 *   entries: [entry({ id: "run", target: "done" })],
 * });
 * ```
 */
export function defineFlow(definition: FlowDefinition): ControlProgramIR {
  return {
    schema: CONTROL_PROGRAM_SCHEMA,
    schema_revision: CONTROL_PROGRAM_SCHEMA_REVISION,
    program: {
      id: definition.id,
      version: definition.version,
      ...(definition.human_identity
        ? { human_identity: definition.human_identity }
        : {}),
      ...(definition.description
        ? { description: definition.description }
        : {}),
    },
    declarations: definition.declarations ?? {},
    facets: definition.facets,
    evidence: definition.evidence ?? [],
    work: definition.work ?? [],
    operators: definition.operators,
    transitions: definition.transitions,
    targets: definition.targets,
    entries: definition.entries,
    ...(definition.description ? { description: definition.description } : {}),
  };
}

/** Declares a repository-owned UTF-8 instruction asset. */
export function instructionAsset(path: string): WorkAssetDefinition {
  return { path };
}

/** Declares a repository-owned strict JSON Schema asset. */
export function schemaAsset(path: string): WorkAssetDefinition {
  return { path };
}

/** Binds a foreground-work input to one entry input ID. */
export function entryInput(id: string): WorkInputDefinition {
  return { id, entry_input: id };
}

/** Declares one bounded foreground-work output artifact. */
export function workArtifact(
  definition: WorkArtifactDefinition,
): WorkArtifactDefinition {
  return { ...definition };
}

/**
 * Declares one foreground-work contract inside a Flow.
 *
 * {@link defineFlow} remains the complete controller. This helper only defines
 * bounded work that a selected transition may require before its trusted
 * operator can be admitted.
 */
export function foregroundWork(definition: WorkContract): WorkContract {
  return { ...definition };
}

/** Resolves a transition parameter from one declared entry input. */
export function fromEntryInput(input: string): ParameterProducer {
  return { kind: "entry-input", input };
}

/** Resolves a transition parameter from a state facet under an exact availability condition. */
export function fromState(definition: {
  facet: string;
  availableWhen: Predicate;
}): ParameterProducer {
  return {
    kind: "state",
    facet: definition.facet,
    available_when: definition.availableWhen,
  };
}

/** Resolves a transition parameter from an earlier transition receipt. */
export function fromReceipt(definition: {
  transition: string;
  field: string;
}): ParameterProducer {
  return { kind: "receipt", ...definition };
}

/** Resolves from current durable state, falling back to one exact committed receipt. */
export function fromStateOrReceipt(definition: {
  facet: string;
  availableWhen: Predicate;
  transition: string;
  field: string;
}): ParameterProducer {
  return {
    kind: "state-or-receipt",
    facet: definition.facet,
    available_when: definition.availableWhen,
    transition: definition.transition,
    field: definition.field,
  };
}

/** Resolves a transition parameter from exact run-scoped foreground-work output. */
export function fromWorkOutput(definition: {
  work: string;
  output: string;
}): ParameterProducer {
  return { kind: "work-output", ...definition };
}

/** Requests one trusted runtime-owned parameter resolver binding. */
export function trustedParameterResolver(
  reference: string,
  version: string,
): ParameterProducer {
  return { kind: "trusted-resolver", binding: { reference, version } };
}

/** Declares a typed, resumable host-input request for a transition parameter. */
export function hostParameter(definition: {
  id: string;
  description: string;
  authorities: string[];
  scope: "transition";
}): ParameterProducer {
  return { kind: "host-input", request: { ...definition } };
}

/**
 * Declares a typed state facet.
 *
 * Facet identifiers and allowed values are executable schema. The runtime
 * rejects effects and predicates that reference undeclared facets or values.
 */
export function facet(
  id: string,
  kind: FacetDefinition["kind"],
  values?: string[],
): FacetDefinition {
  return { id, kind, ...(values ? { values } : {}) };
}

/** Declares an evidence relation without creating evidence at authoring time. */
export function evidence(
  id: string,
  subject: string,
  kind: string,
): EvidenceDefinition {
  return { id, subject, kind };
}

/**
 * Declares a generic operator.
 *
 * Prefer a trusted domain adapter where one exists. This helper describes an
 * operator but cannot make an untrusted implementation executable.
 */
export function operator(
  id: string,
  definition: Omit<OperatorDefinition, "id">,
): OperatorDefinition {
  return { id, ...definition };
}

/** Declares a generic transition that refers to a declared operator. */
export function transition(
  id: string,
  operatorID: string,
  definition: Omit<TransitionDefinition, "id" | "operator">,
): TransitionDefinition {
  return { id, operator: operatorID, ...definition };
}

/**
 * Declares a named terminal target.
 *
 * Targets define what counts as done. Entries select which target a particular
 * invocation pursues.
 */
export function marked(
  id: string,
  predicate: Predicate,
  description?: string,
): TargetDefinition {
  return { id, predicate, ...(description ? { description } : {}) };
}

/**
 * Declares an invocation entry and normalizes omitted inputs to an empty list.
 *
 * @example
 * ```ts
 * entry({ id: "run", target: "published-pr" })
 * ```
 */
export function entry(definition: EntryDefinition): EntryDefinition {
  return { ...definition, inputs: definition.inputs ?? [] };
}

/** Predicate that is true in every state. */
export const always: Predicate = { true: true };

/**
 * Matches a facet value with optional evidence-status constraints.
 *
 * The default status is `known`; pass an explicit list when unknown or other
 * statuses are admissible.
 */
export function fact(
  facetID: string,
  values: string[] = [],
  statuses: string[] = ["known"],
): Predicate {
  return { fact: { facet: facetID, statuses, values } };
}

/** Requires every supplied predicate to hold. */
export function all(...predicates: Predicate[]): Predicate {
  return { all: predicates };
}
