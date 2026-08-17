// Package controlprogram defines Boatstack's domain-neutral Control Program IR.
// Domain packages bind the generic declarations to their own observations and
// operators; this package does not know about software delivery.
package controlprogram

import "encoding/json"

const (
	SchemaName     = "control-program"
	SchemaRevision = 5
)

type Document struct {
	Schema         string         `json:"schema"`
	SchemaRevision int            `json:"schema_revision"`
	Program        Program        `json:"program"`
	Declarations   Declarations   `json:"declarations"`
	Facets         []Facet        `json:"facets"`
	Evidence       []Evidence     `json:"evidence,omitempty"`
	Work           []WorkContract `json:"work,omitempty"`
	Operators      []Operator     `json:"operators"`
	Transitions    []Transition   `json:"transitions"`
	Targets        []Target       `json:"targets"`
	Entries        []Entry        `json:"entries"`
	Description    string         `json:"description,omitempty"`
}

type Program struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	HumanIdentity string `json:"human_identity,omitempty"`
	Description   string `json:"description,omitempty"`
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

// WorkAsset is a repository asset declaration in raw IR and an exact
// content-bound asset in compiled IR. The trusted compiler supplies SHA256 and
// Content; repository Flow source may supply only Path.
type WorkAsset struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Content string `json:"content,omitempty"`
}

type WorkInput struct {
	ID         string `json:"id"`
	EntryInput string `json:"entry_input"`
}

type WorkOutput struct {
	ID        string     `json:"id"`
	Path      string     `json:"path"`
	MediaType string     `json:"media_type"`
	Required  bool       `json:"required"`
	MaxBytes  int64      `json:"max_bytes,omitempty"`
	Schema    *WorkAsset `json:"schema,omitempty"`
}

// WorkContract declares bounded foreground work. It contains no executable
// code, capabilities, authority, effects, or native handlers.
type WorkContract struct {
	ID           string       `json:"id"`
	Instructions WorkAsset    `json:"instructions"`
	Inputs       []WorkInput  `json:"inputs"`
	Outputs      []WorkOutput `json:"outputs"`
	Description  string       `json:"description,omitempty"`
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

type ParameterSourceKind string

const (
	ParameterSourceEntryInput      ParameterSourceKind = "entry-input"
	ParameterSourceState           ParameterSourceKind = "state"
	ParameterSourceReceipt         ParameterSourceKind = "receipt"
	ParameterSourceStateOrReceipt  ParameterSourceKind = "state-or-receipt"
	ParameterSourceWorkOutput      ParameterSourceKind = "work-output"
	ParameterSourceTrustedResolver ParameterSourceKind = "trusted-resolver"
	ParameterSourceHostInput       ParameterSourceKind = "host-input"
)

type TrustedValidatorBinding struct {
	Reference   string `json:"reference"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// ValueTypeDefinition is the closed canonical parameter value-type model.
// Only fields owned by the selected kind may be populated.
type ValueTypeDefinition struct {
	Kind      string                   `json:"kind"`
	Validator *TrustedValidatorBinding `json:"validator,omitempty"`
	Minimum   *int64                   `json:"minimum,omitempty"`
	Maximum   *int64                   `json:"maximum,omitempty"`
	Schema    *TrustedValidatorBinding `json:"schema,omitempty"`
}

type OperatorParameter struct {
	ID             string                `json:"id"`
	Type           ValueTypeDefinition   `json:"type"`
	Required       bool                  `json:"required"`
	Secret         bool                  `json:"secret"`
	AllowedSources []ParameterSourceKind `json:"allowed_sources"`
	Authority      AuthorityRequirement  `json:"authority"`
}

type ParameterResolverBinding struct {
	Reference   string `json:"reference"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type HostInputRequest struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Authorities []string `json:"authorities"`
	Scope       string   `json:"scope"`
}

// ParameterProducer is a closed tagged union. The compiler rejects fields
// that do not belong to Kind and resolves trusted bindings before publication.
type ParameterProducer struct {
	Kind          ParameterSourceKind       `json:"kind"`
	Input         string                    `json:"input,omitempty"`
	Facet         string                    `json:"facet,omitempty"`
	AvailableWhen *Predicate                `json:"available_when,omitempty"`
	Transition    string                    `json:"transition,omitempty"`
	Field         string                    `json:"field,omitempty"`
	Work          string                    `json:"work,omitempty"`
	Output        string                    `json:"output,omitempty"`
	Binding       *ParameterResolverBinding `json:"binding,omitempty"`
	Request       *HostInputRequest         `json:"request,omitempty"`
}

type TransitionParameterBinding struct {
	Parameter string            `json:"parameter"`
	Producer  ParameterProducer `json:"producer"`
}

type Operator struct {
	ID               string                 `json:"id"`
	Binding          *OperatorBinding       `json:"binding,omitempty"`
	Capabilities     []string               `json:"capabilities,omitempty"`
	Authority        AuthorityRequirement   `json:"authority"`
	Effects          []string               `json:"effects,omitempty"`
	Verifier         string                 `json:"verifier,omitempty"`
	Recovery         string                 `json:"recovery,omitempty"`
	StateEffect      *StateEffect           `json:"state_effect,omitempty"`
	ExecutionContext string                 `json:"execution_context"`
	Parameters       []OperatorParameter    `json:"parameters,omitempty"`
	Outputs          []OperatorOutput       `json:"outputs,omitempty"`
	StateInputs      []OperatorStateInput   `json:"state_inputs,omitempty"`
	ReceiptInputs    []OperatorReceiptInput `json:"receipt_inputs,omitempty"`
	Description      string                 `json:"description,omitempty"`
}

// OperatorOutput declares a committed transition-receipt field that later
// transitions may consume. Trusted bindings own this metadata.
type OperatorOutput struct {
	ID   string              `json:"id"`
	Type ValueTypeDefinition `json:"type"`
}

// OperatorStateInput binds a state-sourced parameter to the exact canonical
// facet and availability predicate owned by a trusted operator adapter.
type OperatorStateInput struct {
	Parameter     string    `json:"parameter"`
	Facet         string    `json:"facet"`
	AvailableWhen Predicate `json:"available_when"`
}

// OperatorReceiptInput binds a receipt-capable parameter to one exact
// committed transition output owned by a trusted operator adapter. Guaranteed
// is required when that receipt is the parameter's only source.
type OperatorReceiptInput struct {
	Parameter  string `json:"parameter"`
	Transition string `json:"transition"`
	Field      string `json:"field"`
	Guaranteed bool   `json:"guaranteed,omitempty"`
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
	ID          string                       `json:"id"`
	Operator    string                       `json:"operator"`
	Guard       Predicate                    `json:"guard"`
	Target      Predicate                    `json:"target"`
	Priority    int                          `json:"priority"`
	Requires    TransitionRequirements       `json:"requires,omitempty"`
	Work        string                       `json:"work,omitempty"`
	Parameters  []TransitionParameterBinding `json:"parameters,omitempty"`
	Description string                       `json:"description,omitempty"`
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
	Diagnostics *EntryDiagnostics  `json:"diagnostics,omitempty"`
	Description string             `json:"description,omitempty"`
}

// EntryDiagnostics controls generated host UX only. It is excluded from the
// executable Program fingerprint but remains bound by the source, artifact,
// and generated-projection digests.
type EntryDiagnostics struct {
	ExplainOnSuspend bool `json:"explain_on_suspend,omitempty"`
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
	ResolveParameterResolver(reference, version string) (ResolvedParameterResolver, error)
	ResolveValueValidator(reference, version string) (ResolvedValueValidator, error)
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
	Parameters       []OperatorParameter
	Outputs          []OperatorOutput
	StateInputs      []OperatorStateInput
	ReceiptInputs    []OperatorReceiptInput
}

type ResolvedParameterResolver struct {
	Fingerprint    string
	OutputType     ValueTypeDefinition
	SourceKind     ParameterSourceKind
	Authority      AuthorityRequirement
	Dependencies   []string
	StabilityScope string
	MaySuspend     bool
}

type ResolvedValueValidator struct {
	Fingerprint string
	Type        ValueTypeDefinition
}

type ResolvedDelegation struct {
	Fingerprint string
	Authorities []string
	Delegable   bool
}
