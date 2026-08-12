package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Observation struct {
	Fingerprint string          `json:"fingerprint"`
	Value       json.RawMessage `json:"value"`
}

func NewObservation(value any) (Observation, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return Observation{}, err
	}
	fingerprint, err := contentHash(json.RawMessage(encoded))
	return Observation{Fingerprint: fingerprint, Value: encoded}, err
}

func (o Observation) Validate() error {
	if len(o.Fingerprint) != 64 || len(o.Value) == 0 || !json.Valid(o.Value) {
		return fmt.Errorf("domain observation requires canonical value and fingerprint")
	}
	fingerprint, err := contentHash(o.Value)
	if err != nil || fingerprint != o.Fingerprint {
		return fmt.Errorf("domain observation fingerprint mismatch")
	}
	return nil
}

type Evaluation struct {
	State       ControlState
	Observation Observation
	Objective   *Objective
	Transition  Transition
}

type Domain interface {
	Observe(context.Context, string) (Observation, error)
	Admissible(context.Context, Evaluation) (bool, string, error)
	Verify(context.Context, Evaluation, Effect, Observation) error
}

type Operation struct {
	InstanceID   string
	Transition   Transition
	Observation  Observation
	Objective    *Objective
	Capabilities []Capability
}

type Operator interface {
	Execute(context.Context, Operation) (Effect, error)
}

// CapabilityClassifier is trusted mechanism configuration. Program
// declarations may strengthen its answer but can never weaken it.
type CapabilityClassifier interface {
	RequiredCapabilities(Transition) ([]Capability, error)
}

// Store owns the durable transaction boundary. CommitTransition must atomically
// persist the target control state and its receipt: either both become visible
// or neither does. EnterRecovery is a separate compare-and-swap used only when
// an operator may already have changed its domain but the transaction did not
// commit.
type Store interface {
	Load(context.Context, string) (ControlState, error)
	CommitTransition(context.Context, uint64, ControlState, Receipt) error
	EnterRecovery(context.Context, uint64, ControlState) error
}

type Lock interface{ Unlock() error }

type Locker interface {
	Acquire(context.Context, string) (Lock, error)
}

type Clock interface{ Now() time.Time }

type DecisionKind string

const (
	Prescribed DecisionKind = "PRESCRIBED"
	Marked     DecisionKind = "MARKED"
	Frontier   DecisionKind = "FRONTIER"
	Blocked    DecisionKind = "BLOCKED"
	Refused    DecisionKind = "REFUSED"
	Unresolved DecisionKind = "UNRESOLVED"
)

type Decision struct {
	Kind       DecisionKind `json:"kind"`
	Transition string       `json:"transition,omitempty"`
	Candidates []string     `json:"candidates,omitempty"`
	Reason     string       `json:"reason"`
}

type Prescription struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	TransitionID  string `json:"transition_id"`
	Freshness
	ExpectedObjectiveBinding  *ObjectiveBinding `json:"expected_objective_binding,omitempty"`
	RequestedObjectiveBinding *ObjectiveBinding `json:"requested_objective_binding,omitempty"`
	RequiredCapabilities      []Capability      `json:"required_capabilities"`
}

type Resolution struct {
	State        ControlState  `json:"state"`
	Observation  Observation   `json:"observation"`
	Decision     Decision      `json:"decision"`
	Prescription *Prescription `json:"prescription,omitempty"`
}

type ResolveRequest struct {
	InstanceID string
	Objective  *Objective
	Authority  Authority
	Requested  string
}

type ApplyRequest struct {
	ResolveRequest
	Prescription Prescription
}

type Receipt struct {
	SchemaVersion        int               `json:"schema_version"`
	ID                   string            `json:"id"`
	PrescriptionID       string            `json:"prescription_id"`
	Program              ProgramIdentity   `json:"program"`
	TransitionID         string            `json:"transition_id"`
	PriorStateRevision   uint64            `json:"prior_state_revision"`
	ResultStateRevision  uint64            `json:"result_state_revision"`
	ObjectiveBinding     *ObjectiveBinding `json:"objective_binding,omitempty"`
	AuthorityFingerprint string            `json:"authority_fingerprint"`
	Capabilities         []Capability      `json:"capabilities"`
	Effects              []EffectFact      `json:"effects"`
	PriorObservation     string            `json:"prior_observation"`
	ResultObservation    string            `json:"result_observation"`
	Verification         string            `json:"verification"`
	CommittedAt          time.Time         `json:"committed_at"`
}

type Runtime struct {
	program    Program
	domain     Domain
	operator   Operator
	classifier CapabilityClassifier
	store      Store
	locker     Locker
	clock      Clock
}

func NewRuntime(program Program, domain Domain, operator Operator, classifier CapabilityClassifier, store Store, locker Locker, clock Clock) (Runtime, error) {
	if err := program.Validate(); err != nil {
		return Runtime{}, err
	}
	if domain == nil || operator == nil || classifier == nil || store == nil || locker == nil || clock == nil {
		return Runtime{}, fmt.Errorf("kernel runtime requires domain, operator, capability classifier, transactional store, lock, and clock ports")
	}
	for _, transition := range program.Transitions {
		if _, err := requiredCapabilities(classifier, transition); err != nil {
			return Runtime{}, err
		}
	}
	return Runtime{program: program, domain: domain, operator: operator, classifier: classifier, store: store, locker: locker, clock: clock}, nil
}

func (r Runtime) Resolve(ctx context.Context, request ResolveRequest) (Resolution, error) {
	state, err := r.store.Load(ctx, request.InstanceID)
	if err != nil {
		return Resolution{}, err
	}
	observation, err := r.domain.Observe(ctx, request.InstanceID)
	if err != nil {
		return Resolution{}, err
	}
	return r.resolve(ctx, state, observation, request)
}

func (r Runtime) resolve(ctx context.Context, state ControlState, observation Observation, request ResolveRequest) (Resolution, error) {
	base := Resolution{State: state, Observation: observation}
	if err := state.Validate(); err != nil || observation.Validate() != nil || state.InstanceID != request.InstanceID || state.Program != r.program.Identity() {
		base.Decision = Decision{Kind: Unresolved, Reason: "control state, domain observation, instance, or program identity is invalid"}
		return base, nil
	}
	if r.program.Marked(state.Mode) && state.Recovery == nil && request.Requested == "" {
		base.Decision = Decision{Kind: Marked, Reason: "program-defined marked state is established"}
		return base, nil
	}
	authority, err := request.Authority.projection(r.clock.Now())
	if err != nil {
		base.Decision = Decision{Kind: Refused, Reason: err.Error()}
		return base, nil
	}
	transitions := map[string]Transition{}
	var candidates []RelationCandidate
	for _, transition := range r.program.Transitions {
		if request.Requested != "" && transition.ID != request.Requested {
			continue
		}
		if !contains(transition.SourceModes, state.Mode) {
			continue
		}
		if state.Recovery != nil && !contains(transition.Recovers, state.Recovery.TransitionID) {
			continue
		}
		if state.Recovery == nil && len(transition.Recovers) != 0 {
			continue
		}
		objective, reason := objectiveFor(state, request.Objective, transition)
		if reason != "" {
			continue
		}
		allowed, _, evaluationErr := r.domain.Admissible(ctx, Evaluation{State: state, Observation: observation, Objective: objective, Transition: transition})
		if evaluationErr != nil {
			return Resolution{}, evaluationErr
		}
		if allowed {
			required, capabilityErr := requiredCapabilities(r.classifier, transition)
			if capabilityErr != nil {
				return Resolution{}, capabilityErr
			}
			transitions[transition.ID] = transition
			candidates = append(candidates, RelationCandidate{ID: transition.ID, Priority: transition.Priority, Selectable: true, RequiredAll: required})
		}
	}
	base.Decision = Relate(RelationInput{Requested: request.Requested, Candidates: candidates, Available: authority.Capabilities})
	if base.Decision.Kind != Prescribed {
		return base, nil
	}
	top := transitions[base.Decision.Transition]
	required, err := requiredCapabilities(r.classifier, top)
	if err != nil {
		return Resolution{}, err
	}
	objective, _ := objectiveFor(state, request.Objective, top)
	prescription, err := newPrescription(state, observation, top, objective, authority, required)
	if err != nil {
		return Resolution{}, err
	}
	base.Prescription = &prescription
	return base, nil
}

func (r Runtime) Apply(ctx context.Context, request ApplyRequest) (Receipt, error) {
	lock, err := r.locker.Acquire(ctx, request.InstanceID)
	if err != nil {
		return Receipt{}, err
	}
	defer lock.Unlock()
	state, err := r.store.Load(ctx, request.InstanceID)
	if err != nil {
		return Receipt{}, err
	}
	observation, err := r.domain.Observe(ctx, request.InstanceID)
	if err != nil {
		return Receipt{}, err
	}
	authority, err := request.Authority.projection(r.clock.Now())
	if err != nil {
		return Receipt{}, err
	}
	if err := request.Prescription.validateCurrent(state, observation, authority); err != nil {
		return Receipt{}, err
	}
	resolution, err := r.resolve(ctx, state, observation, request.ResolveRequest)
	if err != nil {
		return Receipt{}, err
	}
	if resolution.Decision.Kind != Prescribed || resolution.Prescription == nil {
		return Receipt{}, fmt.Errorf("apply refused: %s", resolution.Decision.Reason)
	}
	if request.Prescription.ID != resolution.Prescription.ID {
		return Receipt{}, StalePrescriptionError{Reason: "state, program, objective binding, observation, authority, or transition changed"}
	}
	transition, ok := r.program.Transition(request.Prescription.TransitionID)
	if !ok {
		return Receipt{}, StalePrescriptionError{Reason: "transition no longer belongs to the program"}
	}
	objective, _ := objectiveFor(state, request.Objective, transition)
	required, err := requiredCapabilities(r.classifier, transition)
	if err != nil {
		return Receipt{}, err
	}
	effect, err := r.operator.Execute(ctx, Operation{InstanceID: request.InstanceID, Transition: transition, Observation: observation, Objective: objective, Capabilities: required})
	if err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, fmt.Sprintf("operator outcome is unknown: %v", err))
	}
	if err := validateEffects(transition, effect); err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, err.Error())
	}
	sort.Slice(effect.Facts, func(i, j int) bool {
		if effect.Facts[i].Facet != effect.Facts[j].Facet {
			return effect.Facts[i].Facet < effect.Facts[j].Facet
		}
		if effect.Facts[i].Operation != effect.Facts[j].Operation {
			return effect.Facts[i].Operation < effect.Facts[j].Operation
		}
		return effect.Facts[i].Fingerprint < effect.Facts[j].Fingerprint
	})
	targetObservation, err := r.domain.Observe(ctx, request.InstanceID)
	if err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, fmt.Sprintf("target observation failed: %v", err))
	}
	if err := r.domain.Verify(ctx, Evaluation{State: state, Observation: observation, Objective: objective, Transition: transition}, effect, targetObservation); err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, fmt.Sprintf("verification failed: %v", err))
	}
	target := state
	target.Mode = transition.TargetMode
	target.Revision++
	switch transition.ObjectiveMutation {
	case BindObjectiveMutation:
		binding, bindErr := BindObjective(*objective)
		if bindErr != nil {
			return Receipt{}, bindErr
		}
		target.ObjectiveBinding = &binding
	case ClearObjectiveMutation:
		target.ObjectiveBinding = nil
	}
	if len(transition.Recovers) != 0 {
		target.Recovery = nil
	}
	receipt := Receipt{SchemaVersion: ReceiptSchemaVersion, PrescriptionID: request.Prescription.ID, Program: state.Program, TransitionID: transition.ID, PriorStateRevision: state.Revision, ResultStateRevision: target.Revision, ObjectiveBinding: cloneBinding(target.ObjectiveBinding), AuthorityFingerprint: authority.Fingerprint, Capabilities: required, Effects: append([]EffectFact(nil), effect.Facts...), PriorObservation: observation.Fingerprint, ResultObservation: targetObservation.Fingerprint, Verification: "satisfied", CommittedAt: r.clock.Now().UTC()}
	identity := receipt
	identity.ID = ""
	receipt.ID, err = contentHash(identity)
	if err != nil {
		return Receipt{}, err
	}
	receipt.ID = "rcp-" + receipt.ID
	if err := receipt.Validate(); err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, "transition receipt is invalid: "+err.Error())
	}
	if err := r.store.CommitTransition(ctx, state.Revision, target, receipt); err != nil {
		return Receipt{}, r.requireRecovery(ctx, state, request.Prescription, "state and receipt transaction did not commit: "+err.Error())
	}
	return receipt, nil
}

func (r Receipt) Validate() error {
	identity := r
	want := identity.ID
	identity.ID = ""
	got, err := contentHash(identity)
	if err != nil || want != "rcp-"+got {
		return fmt.Errorf("receipt content identity is invalid")
	}
	if r.SchemaVersion != ReceiptSchemaVersion || r.PrescriptionID == "" || !semanticID.MatchString(r.TransitionID) || r.PriorStateRevision == 0 || r.ResultStateRevision != r.PriorStateRevision+1 || r.AuthorityFingerprint == "" || len(r.PriorObservation) != 64 || len(r.ResultObservation) != 64 || r.Verification != "satisfied" || r.CommittedAt.IsZero() {
		return fmt.Errorf("receipt is missing exact transition, revision, observation, authority, or verification facts")
	}
	if err := r.Program.Validate(); err != nil {
		return err
	}
	if r.ObjectiveBinding != nil {
		if err := r.ObjectiveBinding.Validate(); err != nil {
			return err
		}
	}
	if _, err := normalizeCapabilities(r.Capabilities); err != nil {
		return err
	}
	if len(r.Effects) == 0 {
		return fmt.Errorf("receipt contains no committed effect facts")
	}
	return nil
}

type StalePrescriptionError struct{ Reason string }

func (e StalePrescriptionError) Error() string { return "stale prescription: " + e.Reason }
func IsStale(err error) bool                   { var target StalePrescriptionError; return errors.As(err, &target) }

type RecoveryRequiredError struct{ Reason string }

func (e RecoveryRequiredError) Error() string { return "recovery required: " + e.Reason }
func IsRecoveryRequired(err error) bool {
	var target RecoveryRequiredError
	return errors.As(err, &target)
}

func (r Runtime) requireRecovery(ctx context.Context, state ControlState, prescription Prescription, reason string) error {
	target := state
	target.Revision++
	target.Recovery = &RecoveryState{PrescriptionID: prescription.ID, TransitionID: prescription.TransitionID, Reason: reason}
	if err := r.store.EnterRecovery(ctx, state.Revision, target); err != nil {
		return RecoveryRequiredError{Reason: reason + "; recovery state commit failed: " + err.Error()}
	}
	return RecoveryRequiredError{Reason: reason}
}

func newPrescription(state ControlState, observation Observation, transition Transition, objective *Objective, authority authorityProjection, required []Capability) (Prescription, error) {
	bindingFingerprint, err := Fingerprint(state.ObjectiveBinding)
	if err != nil {
		return Prescription{}, err
	}
	freshness, err := NewFreshness(state.Revision, state.Program.Fingerprint, observation.Fingerprint, bindingFingerprint, authority.Fingerprint)
	if err != nil {
		return Prescription{}, err
	}
	p := Prescription{SchemaVersion: PrescriptionSchemaVersion, TransitionID: transition.ID, Freshness: freshness, RequiredCapabilities: append([]Capability(nil), required...)}
	p.ExpectedObjectiveBinding = cloneBinding(state.ObjectiveBinding)
	if transition.ObjectiveMutation == BindObjectiveMutation {
		binding, err := BindObjective(*objective)
		if err != nil {
			return Prescription{}, err
		}
		p.RequestedObjectiveBinding = &binding
	}
	identity := p
	identity.ID = ""
	id, err := contentHash(identity)
	if err != nil {
		return Prescription{}, err
	}
	p.ID = "prx-" + id
	return p, nil
}

func (p Prescription) validateCurrent(state ControlState, observation Observation, authority authorityProjection) error {
	identity := p
	want := identity.ID
	identity.ID = ""
	got, err := contentHash(identity)
	if err != nil || want != "prx-"+got {
		return StalePrescriptionError{Reason: "prescription content identity is invalid"}
	}
	bindingFingerprint, bindingErr := Fingerprint(state.ObjectiveBinding)
	current, freshnessErr := NewFreshness(state.Revision, state.Program.Fingerprint, observation.Fingerprint, bindingFingerprint, authority.Fingerprint)
	if p.SchemaVersion != PrescriptionSchemaVersion || bindingErr != nil || freshnessErr != nil || p.Freshness.Check(current) != nil || !equalBinding(p.ExpectedObjectiveBinding, state.ObjectiveBinding) {
		return StalePrescriptionError{Reason: "state, program, objective binding, observation, or authority changed"}
	}
	return nil
}

func objectiveFor(state ControlState, supplied *Objective, transition Transition) (*Objective, string) {
	if transition.ObjectiveMutation == BindObjectiveMutation {
		if supplied == nil || supplied.Validate() != nil {
			return nil, "objective binding requires one exact objective revision"
		}
		if state.ObjectiveBinding != nil && state.ObjectiveBinding.Matches(*supplied) {
			return nil, "objective revision is already bound"
		}
		copy := *supplied
		return &copy, ""
	}
	if transition.ObjectiveMutation == ClearObjectiveMutation && state.ObjectiveBinding == nil {
		return nil, "objective binding is already absent"
	}
	switch transition.ObjectiveScope {
	case ObjectiveNone:
		return nil, ""
	case ObjectiveOptionalPreserve:
		if state.ObjectiveBinding != nil && supplied != nil && state.ObjectiveBinding.Matches(*supplied) {
			copy := *supplied
			return &copy, ""
		}
		// A command-scoped objective is not allowed to reinterpret durable
		// supervisory state. Maintenance runs without an objective projection
		// while its prescription remains bound to the exact existing binding.
		return nil, ""
	case ObjectiveBoundExact:
		if state.ObjectiveBinding == nil || supplied == nil || !state.ObjectiveBinding.Matches(*supplied) {
			return nil, "transition requires the exact bound objective revision"
		}
		copy := *supplied
		return &copy, ""
	default:
		return nil, "invalid objective scope"
	}
}

func validateEffects(transition Transition, effect Effect) error {
	if len(effect.Facts) == 0 {
		return fmt.Errorf("operator returned no committed effect facts")
	}
	owned := map[string]bool{}
	for _, facet := range transition.OwnedFacets {
		owned[facet] = true
	}
	for _, fact := range effect.Facts {
		if !owned[fact.Facet] || !semanticID.MatchString(fact.Operation) || fact.Fingerprint == "" {
			return fmt.Errorf("effect fact escapes transition-owned facets or is incomplete")
		}
	}
	return nil
}

func missingCapabilities(required, available []Capability) []Capability {
	set := map[Capability]bool{}
	for _, value := range available {
		set[value] = true
	}
	var result []Capability
	for _, value := range required {
		if !set[value] {
			result = append(result, value)
		}
	}
	return result
}

func requiredCapabilities(classifier CapabilityClassifier, transition Transition) ([]Capability, error) {
	minimum, err := classifier.RequiredCapabilities(transition)
	if err != nil {
		return nil, err
	}
	return normalizeCapabilities(append(append([]Capability(nil), transition.RequiredCapabilities...), minimum...))
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func containsCapability(values []Capability, wanted Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func cloneBinding(value *ObjectiveBinding) *ObjectiveBinding {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func equalBinding(left, right *ObjectiveBinding) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
