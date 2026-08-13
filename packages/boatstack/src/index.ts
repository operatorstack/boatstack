export const CONTROL_PROGRAM_SCHEMA = "control-program" as const;
export const CONTROL_PROGRAM_SCHEMA_REVISION = 1 as const;

export type Predicate =
  | { true: boolean }
  | { fact: { facet: string; statuses?: string[]; values?: string[] } }
  | { all: Predicate[] }
  | { any: Predicate[] }
  | { not: Predicate };

export interface FacetDefinition {
  id: string;
  kind: "enum" | "string" | "boolean";
  values?: string[];
  description?: string;
}

export interface EvidenceDefinition {
  id: string;
  subject: string;
  kind: string;
  description?: string;
}

export interface StatePrecondition {
  facet: string;
  values: string[];
}

export interface StateValueReference {
  parameter?: string;
  admission?: string;
  invocation?: string;
}

export interface StateAssignment {
  facet: string;
  value?: string;
  value_from?: StateValueReference;
}

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
  description?: string;
}

export interface TransitionDefinition {
  id: string;
  operator: string;
  guard: Predicate;
  target: Predicate;
  priority: number;
  requires?: { authorities?: string[] };
  description?: string;
}

export interface TargetDefinition {
  id: string;
  predicate: Predicate;
  description?: string;
}

export interface EntryInputDefinition {
  id: string;
  type: string;
  required: boolean;
  resolver?: string;
  config?: unknown;
}

export interface EntryDefinition {
  id: string;
  target: string;
  inputs?: EntryInputDefinition[];
  delegation?: DelegationBindingDefinition;
  description?: string;
}

export interface DelegationBindingDefinition {
  reference: string;
  version: string;
}

export interface FlowDefinition {
  id: string;
  version: string;
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
  operators: OperatorDefinition[];
  transitions: TransitionDefinition[];
  targets: TargetDefinition[];
  entries: EntryDefinition[];
}

export interface ControlProgramIR {
  schema: typeof CONTROL_PROGRAM_SCHEMA;
  schema_revision: typeof CONTROL_PROGRAM_SCHEMA_REVISION;
  program: { id: string; version: string; description?: string };
  declarations: NonNullable<FlowDefinition["declarations"]>;
  facets: FacetDefinition[];
  evidence: EvidenceDefinition[];
  operators: OperatorDefinition[];
  transitions: TransitionDefinition[];
  targets: TargetDefinition[];
  entries: EntryDefinition[];
  description?: string;
}

export function defineFlow(definition: FlowDefinition): ControlProgramIR {
  return {
    schema: CONTROL_PROGRAM_SCHEMA,
    schema_revision: CONTROL_PROGRAM_SCHEMA_REVISION,
    program: {
      id: definition.id,
      version: definition.version,
      ...(definition.description
        ? { description: definition.description }
        : {}),
    },
    declarations: definition.declarations ?? {},
    facets: definition.facets,
    evidence: definition.evidence ?? [],
    operators: definition.operators,
    transitions: definition.transitions,
    targets: definition.targets,
    entries: definition.entries,
    ...(definition.description ? { description: definition.description } : {}),
  };
}

export function facet(
  id: string,
  kind: FacetDefinition["kind"],
  values?: string[],
): FacetDefinition {
  return { id, kind, ...(values ? { values } : {}) };
}

export function evidence(
  id: string,
  subject: string,
  kind: string,
): EvidenceDefinition {
  return { id, subject, kind };
}

export function operator(
  id: string,
  definition: Omit<OperatorDefinition, "id">,
): OperatorDefinition {
  return { id, ...definition };
}

export function transition(
  id: string,
  operatorID: string,
  definition: Omit<TransitionDefinition, "id" | "operator">,
): TransitionDefinition {
  return { id, operator: operatorID, ...definition };
}

export function marked(
  id: string,
  predicate: Predicate,
  description?: string,
): TargetDefinition {
  return { id, predicate, ...(description ? { description } : {}) };
}

export function entry(definition: EntryDefinition): EntryDefinition {
  return { ...definition, inputs: definition.inputs ?? [] };
}

export const always: Predicate = { true: true };

export function fact(
  facetID: string,
  values: string[] = [],
  statuses: string[] = ["known"],
): Predicate {
  return { fact: { facet: facetID, statuses, values } };
}

export function all(...predicates: Predicate[]): Predicate {
  return { all: predicates };
}
