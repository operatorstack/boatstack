// Package controlprogram defines Boatstack's domain-neutral Control Program IR.
// Domain packages bind the generic declarations to their own observations and
// operators; this package does not know about software delivery.
package controlprogram

import "encoding/json"

const (
	SchemaName     = "control-program"
	SchemaRevision = 1
)

type Document struct {
	Schema         string       `json:"schema"`
	SchemaRevision int          `json:"schema_revision"`
	Program        Program      `json:"program"`
	Declarations   Declarations `json:"declarations"`
	Facets         []Facet      `json:"facets"`
	Evidence       []Evidence   `json:"evidence,omitempty"`
	Operators      []Operator   `json:"operators"`
	Transitions    []Transition `json:"transitions"`
	Targets        []Target     `json:"targets"`
	Entries        []Entry      `json:"entries"`
	Description    string       `json:"description,omitempty"`
}

type Program struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Declarations struct {
	Capabilities   []string `json:"capabilities,omitempty"`
	Authorities    []string `json:"authorities,omitempty"`
	Effects        []string `json:"effects,omitempty"`
	Verifiers      []string `json:"verifiers,omitempty"`
	InputResolvers []string `json:"input_resolvers,omitempty"`
}

type Facet struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Values      []string `json:"values,omitempty"`
	Description string   `json:"description,omitempty"`
}

type Evidence struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

// Predicate is a closed AST. Exactly one node variant must be present.
type Predicate struct {
	All  []Predicate    `json:"all,omitempty"`
	Any  []Predicate    `json:"any,omitempty"`
	Not  *Predicate     `json:"not,omitempty"`
	Fact *FactPredicate `json:"fact,omitempty"`
	True *bool          `json:"true,omitempty"`
}

type FactPredicate struct {
	Facet    string   `json:"facet"`
	Statuses []string `json:"statuses,omitempty"`
	Values   []string `json:"values,omitempty"`
}

type OperatorBinding struct {
	Reference   string `json:"reference"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type AuthorityRequirement struct {
	AnyOf []string `json:"any_of,omitempty"`
	AllOf []string `json:"all_of,omitempty"`
}

type Operator struct {
	ID               string               `json:"id"`
	Binding          *OperatorBinding     `json:"binding,omitempty"`
	Capabilities     []string             `json:"capabilities,omitempty"`
	Authority        AuthorityRequirement `json:"authority"`
	Effects          []string             `json:"effects,omitempty"`
	Verifier         string               `json:"verifier,omitempty"`
	Recovery         string               `json:"recovery,omitempty"`
	StateEffect      *StateEffect         `json:"state_effect,omitempty"`
	ExecutionContext string               `json:"execution_context"`
	Description      string               `json:"description,omitempty"`
}

type StateEffect struct {
	Kind          string              `json:"kind"`
	Preconditions []StatePrecondition `json:"preconditions,omitempty"`
	Assignments   []StateAssignment   `json:"assignments,omitempty"`
	NativeHandler string              `json:"native_handler,omitempty"`
}

type StatePrecondition struct {
	Facet  string   `json:"facet"`
	Values []string `json:"values"`
}

type StateAssignment struct {
	Facet     string          `json:"facet"`
	Value     *string         `json:"value,omitempty"`
	ValueFrom *ValueReference `json:"value_from,omitempty"`
}

type ValueReference struct {
	Parameter  string `json:"parameter,omitempty"`
	Admission  string `json:"admission,omitempty"`
	Invocation string `json:"invocation,omitempty"`
}

type Transition struct {
	ID          string                 `json:"id"`
	Operator    string                 `json:"operator"`
	Guard       Predicate              `json:"guard"`
	Target      Predicate              `json:"target"`
	Priority    int                    `json:"priority"`
	Requires    TransitionRequirements `json:"requires,omitempty"`
	Description string                 `json:"description,omitempty"`
}

type TransitionRequirements struct {
	Authorities []string `json:"authorities,omitempty"`
}

type Target struct {
	ID          string    `json:"id"`
	Predicate   Predicate `json:"predicate"`
	Description string    `json:"description,omitempty"`
}

type Entry struct {
	ID          string             `json:"id"`
	Target      string             `json:"target"`
	Inputs      []EntryInput       `json:"inputs,omitempty"`
	Delegation  *DelegationBinding `json:"delegation,omitempty"`
	Description string             `json:"description,omitempty"`
}

type DelegationBinding struct {
	Reference   string   `json:"reference"`
	Version     string   `json:"version"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Authorities []string `json:"authorities,omitempty"`
}

type EntryInput struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Resolver string          `json:"resolver,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// BindingResolver is the only way a domain can add trusted operator
// semantics. The compiler copies the resolved semantics into the canonical IR
// and binds their exact fingerprint.
type BindingResolver interface {
	ResolveOperator(reference, version string) (ResolvedOperator, error)
	ResolveDelegation(reference, version string) (ResolvedDelegation, error)
}

type ResolvedOperator struct {
	Fingerprint      string
	Capabilities     []string
	Authority        AuthorityRequirement
	Effects          []string
	Verifier         string
	Recovery         string
	StateEffect      StateEffect
	ExecutionContext string
}

type ResolvedDelegation struct {
	Fingerprint string
	Authorities []string
	Delegable   bool
}
