package kernel

// DecisionTrace is a read-only projection of the exact evaluation that fed
// the canonical relation. It contains identities and bounded reasons, never
// domain observation payloads or objective references.
type DecisionTrace struct {
	SchemaVersion          int                `json:"schema_version"`
	InstanceID             string             `json:"instance_id"`
	StateRevision          uint64             `json:"state_revision"`
	CurrentMode            string             `json:"current_mode"`
	Program                ProgramIdentity    `json:"program"`
	StateProgram           *ProgramIdentity   `json:"state_program,omitempty"`
	ObservationFingerprint string             `json:"observation_fingerprint"`
	Objective              ObjectiveTrace     `json:"objective"`
	AuthorityFingerprint   string             `json:"authority_fingerprint,omitempty"`
	RequestedTransition    string             `json:"requested_transition,omitempty"`
	Marked                 bool               `json:"marked"`
	Recovery               *RecoveryTrace     `json:"recovery,omitempty"`
	Decision               DecisionTraceValue `json:"decision"`
	Candidates             []CandidateTrace   `json:"candidates,omitempty"`
}

const DecisionTraceSchemaVersion = 1

type DecisionTraceValue struct {
	Kind       string   `json:"kind"`
	Transition string   `json:"transition,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Reason     string   `json:"reason"`
}

func TraceDecision(decision Decision) DecisionTraceValue {
	return DecisionTraceValue{
		Kind: string(decision.Kind), Transition: decision.Transition,
		Candidates: append([]string(nil), decision.Candidates...), Reason: decision.Reason,
	}
}

type ObjectiveTrace struct {
	Binding   *ObjectiveIdentity `json:"binding,omitempty"`
	Requested *ObjectiveIdentity `json:"requested,omitempty"`
}

type ObjectiveIdentity struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

type RecoveryTrace struct {
	Active         bool   `json:"active"`
	PrescriptionID string `json:"prescription_id,omitempty"`
	TransitionID   string `json:"transition_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type EvaluationTrace struct {
	Evaluated bool   `json:"evaluated"`
	Satisfied bool   `json:"satisfied"`
	Reason    string `json:"reason,omitempty"`
}

type CandidateDisposition string

const (
	DispositionSelected            CandidateDisposition = "selected"
	DispositionSourceModeRejected  CandidateDisposition = "source-mode-rejected"
	DispositionRecoveryRejected    CandidateDisposition = "recovery-rejected"
	DispositionObjectiveRejected   CandidateDisposition = "objective-rejected"
	DispositionDomainRejected      CandidateDisposition = "domain-rejected"
	DispositionAuthorityFrontier   CandidateDisposition = "authority-frontier"
	DispositionShadowed            CandidateDisposition = "shadowed"
	DispositionAmbiguous           CandidateDisposition = "ambiguous"
	DispositionExplicitlyRefused   CandidateDisposition = "explicitly-refused"
	DispositionIrrelevantToRequest CandidateDisposition = "irrelevant-to-request"
)

type CandidateTrace struct {
	TransitionID       string               `json:"transition_id"`
	SourceMode         EvaluationTrace      `json:"source_mode"`
	RecoveryCompatible EvaluationTrace      `json:"recovery_compatible"`
	ObjectiveScope     EvaluationTrace      `json:"objective_scope"`
	ObjectiveMutation  EvaluationTrace      `json:"objective_mutation"`
	DomainAdmissible   EvaluationTrace      `json:"domain_admissible"`
	Selection          EvaluationTrace      `json:"selection"`
	Selectable         bool                 `json:"selectable"`
	SelectionClass     string               `json:"selection_class,omitempty"`
	Rank               int                  `json:"rank"`
	Priority           int                  `json:"priority"`
	Authority          AuthorityTrace       `json:"authority"`
	Survived           bool                 `json:"survived"`
	Disposition        CandidateDisposition `json:"disposition"`
}

type AuthorityTrace struct {
	Available    []Capability `json:"available,omitempty"`
	RequiredAll  []Capability `json:"required_all,omitempty"`
	RequiredAny  []Capability `json:"required_any,omitempty"`
	MissingAll   []Capability `json:"missing_all,omitempty"`
	MissingAny   []Capability `json:"missing_any,omitempty"`
	AllSatisfied bool         `json:"all_satisfied"`
	AnySatisfied bool         `json:"any_satisfied"`
	Satisfied    bool         `json:"satisfied"`
}
