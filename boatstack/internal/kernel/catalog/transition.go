package catalog

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type TransitionID string
type EffectID string

type EventClass string

const (
	EventOwnedLocal       EventClass = "owned-local"
	EventOwnedExternal    EventClass = "owned-external"
	EventAuthority        EventClass = "authority"
	EventObservedExternal EventClass = "observed-external"
	EventRecovery         EventClass = "recovery"
)

func (c EventClass) Controllable() bool { return c != EventObservedExternal }

func GateName(id TransitionID) (string, bool) {
	value := string(id)
	if !strings.HasPrefix(value, "gate.") || !strings.HasSuffix(value, ".record") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "gate."), ".record")
	return name, name != ""
}

func (c EventClass) Valid() bool {
	switch c {
	case EventOwnedLocal, EventOwnedExternal, EventAuthority, EventObservedExternal, EventRecovery:
		return true
	default:
		return false
	}
}

type AuthorityClass string

const (
	AuthorityNone       AuthorityClass = "none"
	AuthorityRepository AuthorityClass = "repository-policy"
	AuthorityHuman      AuthorityClass = "human"
	AuthorityAutonomy   AuthorityClass = "autonomy"
	AuthorityProvider   AuthorityClass = "external-provider"
)

// AuthoritySet is the supervisor's minimal projection of verified authority.
// Receipts and fingerprints remain in protocol.Admission.
type AuthoritySet map[AuthorityClass]bool

func (c AuthorityClass) Valid() bool {
	switch c {
	case AuthorityNone, AuthorityRepository, AuthorityHuman, AuthorityAutonomy, AuthorityProvider:
		return true
	default:
		return false
	}
}

// Satisfies applies an OR clause followed by mandatory AND clauses. This keeps
// provider authority distinct from human/autonomy authority for external
// effects instead of treating all receipts as substitutes.
func (s AuthoritySet) Satisfies(alternatives, required []AuthorityClass) bool {
	for _, authority := range required {
		if authority != AuthorityNone && !s[authority] {
			return false
		}
	}
	if len(alternatives) == 0 {
		return true
	}
	for _, authority := range alternatives {
		if authority == AuthorityNone || s[authority] {
			return true
		}
	}
	return false
}

type Reversibility string

const (
	Reversible      Reversibility = "reversible"
	Compensatable   Reversibility = "compensatable"
	Irreversible    Reversibility = "irreversible"
	ObservationOnly Reversibility = "observation-only"
)

type Prescription struct {
	Operation             string   `json:"operation"`
	Arguments             []string `json:"arguments,omitempty"`
	AuthorityPrompt       string   `json:"authority_prompt,omitempty"`
	ExpectedPostcondition string   `json:"expected_postcondition"`
}

type ParameterSpec struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

type InterruptionContract struct {
	Points               []string     `json:"points"`
	PartialState         []string     `json:"partial_state"`
	Detection            string       `json:"detection"`
	ResumeContract       string       `json:"resume_contract"`
	RollbackContract     string       `json:"rollback_contract"`
	CompensationContract string       `json:"compensation_contract"`
	Recovery             TransitionID `json:"recovery,omitempty"`
	RecoveryAuthority    string       `json:"recovery_authority"`
	ResumptionPredicate  string       `json:"resumption_predicate"`
}

// FacetCondition is an executable, serializable predicate over one canonical
// control facet. Values are ORed; conditions on a transition are ANDed.
type FacetCondition struct {
	Facet    model.FacetName    `json:"facet"`
	Statuses []model.FactStatus `json:"statuses"`
	Values   []string           `json:"values,omitempty"`
}

func (c FacetCondition) Matches(snapshot model.Snapshot) bool {
	status, value, ok := snapshot.Facet(c.Facet)
	if !ok {
		return false
	}
	statusMatch := false
	for _, candidate := range c.Statuses {
		if candidate == status {
			statusMatch = true
			break
		}
	}
	if !statusMatch {
		return false
	}
	if len(c.Values) == 0 {
		return true
	}
	for _, candidate := range c.Values {
		if candidate == value {
			return true
		}
	}
	return false
}

// Transition is both the executable runtime declaration and the source for the
// generated finite-state model. No second graph is maintained.
type Transition struct {
	ID                      TransitionID          `json:"id"`
	Version                 int                   `json:"version"`
	Class                   EventClass            `json:"class"`
	SourcePhases            []model.ProtocolPhase `json:"source_phases"`
	TargetPhases            []model.ProtocolPhase `json:"target_phases"`
	GoalKinds               []model.GoalKind      `json:"goal_kinds,omitempty"`
	RequiredIdentity        []string              `json:"required_identity"`
	Authority               []AuthorityClass      `json:"authority"`
	AuthorityAll            []AuthorityClass      `json:"authority_all,omitempty"`
	RequiredEvidence        []string              `json:"required_evidence"`
	OwnedResources          []string              `json:"owned_resources,omitempty"`
	Effect                  EffectID              `json:"effect,omitempty"`
	LocalEffects            []EffectID            `json:"local_effects,omitempty"`
	ExternalEffects         []EffectID            `json:"external_effects,omitempty"`
	Idempotent              bool                  `json:"idempotent"`
	Parameters              []ParameterSpec       `json:"parameters,omitempty"`
	Prescription            Prescription          `json:"prescription"`
	SourcePredicate         string                `json:"source_predicate"`
	SourceConditions        []FacetCondition      `json:"source_conditions"`
	AdmissionPredicate      string                `json:"admission_predicate"`
	TargetPredicate         string                `json:"target_predicate"`
	TargetConditions        []FacetCondition      `json:"target_conditions"`
	Verifier                string                `json:"verifier"`
	Interruption            InterruptionContract  `json:"interruption"`
	Reversibility           Reversibility         `json:"reversibility"`
	TerminalEffect          string                `json:"terminal_effect,omitempty"`
	PrivacyClassification   string                `json:"privacy_classification"`
	TelemetryClassification string                `json:"telemetry_classification"`
	CostClass               string                `json:"cost_class"`
	Priority                int                   `json:"priority"`
	AllowsIdentityRebind    bool                  `json:"allows_identity_rebind,omitempty"`
	AllowsWorktreeTransfer  bool                  `json:"allows_worktree_transfer,omitempty"`
}

func (t Transition) Controllable() bool { return t.Class.Controllable() }

func (t Transition) SupportsGoal(goal model.Goal) bool {
	if len(t.GoalKinds) == 0 {
		return true
	}
	for _, kind := range t.GoalKinds {
		if kind == goal.Kind {
			return true
		}
	}
	return false
}

func containsPhase(phases []model.ProtocolPhase, phase model.ProtocolPhase) bool {
	for _, candidate := range phases {
		if candidate == phase {
			return true
		}
	}
	return false
}

func (t Transition) SourceMatches(snapshot model.Snapshot) bool {
	if snapshot.Phase.Status != model.FactKnown || !containsPhase(t.SourcePhases, snapshot.Phase.Value) {
		return false
	}
	for _, condition := range t.SourceConditions {
		if !condition.Matches(snapshot) {
			return false
		}
	}
	return true
}

func (t Transition) TargetMatches(snapshot model.Snapshot) bool {
	if snapshot.Phase.Status != model.FactKnown || !containsPhase(t.TargetPhases, snapshot.Phase.Value) {
		return false
	}
	for _, condition := range t.TargetConditions {
		if !condition.Matches(snapshot) {
			return false
		}
	}
	return true
}

func (t Transition) DeclaresTargetPhase(phase model.ProtocolPhase) bool {
	return containsPhase(t.TargetPhases, phase)
}

type Registry struct {
	ordered []Transition
	byID    map[TransitionID]Transition
}

var semanticID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

func New(transitions []Transition) (Registry, error) {
	registry := Registry{ordered: append([]Transition(nil), transitions...), byID: make(map[TransitionID]Transition, len(transitions))}
	for index, transition := range registry.ordered {
		if err := validateTransition(transition); err != nil {
			return Registry{}, fmt.Errorf("transition %d: %w", index, err)
		}
		if _, exists := registry.byID[transition.ID]; exists {
			return Registry{}, fmt.Errorf("duplicate transition id %q", transition.ID)
		}
		registry.byID[transition.ID] = transition
	}
	for _, transition := range registry.ordered {
		if recovery := transition.Interruption.Recovery; recovery != "" {
			candidate, ok := registry.byID[recovery]
			if !ok || candidate.Class != EventRecovery {
				return Registry{}, fmt.Errorf("transition %q references non-recovery transition %q", transition.ID, recovery)
			}
		}
	}
	sort.SliceStable(registry.ordered, func(i, j int) bool {
		if registry.ordered[i].Priority != registry.ordered[j].Priority {
			return registry.ordered[i].Priority < registry.ordered[j].Priority
		}
		return registry.ordered[i].ID < registry.ordered[j].ID
	})
	return registry, nil
}

func validateTransition(t Transition) error {
	if t.ID == "" || !semanticID.MatchString(string(t.ID)) {
		return fmt.Errorf("invalid semantic id %q", t.ID)
	}
	if !t.Class.Valid() {
		return fmt.Errorf("%s: invalid event class %q", t.ID, t.Class)
	}
	if t.Version < 1 || len(t.SourcePhases) == 0 || len(t.TargetPhases) == 0 {
		return fmt.Errorf("%s: version, source phases, and target phases are required", t.ID)
	}
	for _, phase := range append(append([]model.ProtocolPhase(nil), t.SourcePhases...), t.TargetPhases...) {
		if !phase.Valid() {
			return fmt.Errorf("%s: invalid phase %q", t.ID, phase)
		}
	}
	if len(t.Authority) == 0 || len(t.RequiredEvidence) == 0 {
		return fmt.Errorf("%s: authority and evidence declarations are required", t.ID)
	}
	for _, authority := range append(append([]AuthorityClass(nil), t.Authority...), t.AuthorityAll...) {
		if !authority.Valid() {
			return fmt.Errorf("%s: invalid authority class %q", t.ID, authority)
		}
	}
	if t.Controllable() && t.Effect == "" {
		return fmt.Errorf("%s: controllable transition has no owned effect", t.ID)
	}
	if !t.Controllable() && (t.Effect != "" || len(t.OwnedResources) != 0) {
		return fmt.Errorf("%s: observed external transition cannot own effects", t.ID)
	}
	if t.Controllable() && len(t.OwnedResources) == 0 {
		return fmt.Errorf("%s: controllable transition has no owned resource", t.ID)
	}
	if len(t.RequiredIdentity) == 0 || t.SourcePredicate == "" || t.AdmissionPredicate == "" || t.TargetPredicate == "" || t.Verifier == "" || t.Prescription.Operation == "" || t.Prescription.ExpectedPostcondition == "" {
		return fmt.Errorf("%s: identity, predicates, verifier, and typed prescription are required", t.ID)
	}
	if len(t.SourceConditions) == 0 || len(t.TargetConditions) == 0 {
		return fmt.Errorf("%s: source and target facet conditions are required", t.ID)
	}
	for _, condition := range append(append([]FacetCondition(nil), t.SourceConditions...), t.TargetConditions...) {
		if !condition.Facet.Valid() || condition.Facet == model.FacetPhase || len(condition.Statuses) == 0 {
			return fmt.Errorf("%s: invalid or phase-duplicating facet condition", t.ID)
		}
		for _, status := range condition.Statuses {
			if !status.Valid() {
				return fmt.Errorf("%s: invalid fact status %q", t.ID, status)
			}
		}
	}
	if t.Class == EventOwnedExternal {
		if len(t.LocalEffects) != 0 || len(t.ExternalEffects) != 1 || t.ExternalEffects[0] != t.Effect {
			return fmt.Errorf("%s: owned external effect scope is incomplete", t.ID)
		}
	} else if t.Controllable() {
		if len(t.LocalEffects) != 1 || t.LocalEffects[0] != t.Effect || len(t.ExternalEffects) != 0 {
			return fmt.Errorf("%s: owned local effect scope is incomplete", t.ID)
		}
	} else if len(t.LocalEffects) != 0 || len(t.ExternalEffects) != 0 {
		return fmt.Errorf("%s: observed event declares an owned effect scope", t.ID)
	}
	contract := t.Interruption
	if len(contract.Points) == 0 || len(contract.PartialState) == 0 || contract.Detection == "" || contract.ResumeContract == "" ||
		contract.RollbackContract == "" || contract.CompensationContract == "" || contract.RecoveryAuthority == "" || contract.ResumptionPredicate == "" {
		return fmt.Errorf("%s: interruption and recovery contract is incomplete", t.ID)
	}
	if t.Controllable() && contract.Recovery == "" {
		return fmt.Errorf("%s: controllable transition has no interruption recovery transition", t.ID)
	}
	if !t.Controllable() && contract.Recovery != "" {
		return fmt.Errorf("%s: observed transition cannot own an interruption recovery transition", t.ID)
	}
	if t.TerminalEffect == "" {
		return fmt.Errorf("%s: terminal effect declaration is required", t.ID)
	}
	parameterNames := map[string]bool{}
	for _, parameter := range t.Parameters {
		if parameter.Name == "" || parameterNames[parameter.Name] {
			return fmt.Errorf("%s: parameter names must be non-empty and unique", t.ID)
		}
		parameterNames[parameter.Name] = true
	}
	if t.Reversibility == "" || t.PrivacyClassification == "" || t.TelemetryClassification == "" || t.CostClass == "" {
		return fmt.Errorf("%s: recovery/telemetry metadata are incomplete", t.ID)
	}
	if t.Priority < 1 {
		return fmt.Errorf("%s: priority must be positive", t.ID)
	}
	return nil
}

func (r Registry) Len() int { return len(r.ordered) }

func (r Registry) All() []Transition { return append([]Transition(nil), r.ordered...) }

func (r Registry) Lookup(id TransitionID) (Transition, bool) {
	transition, ok := r.byID[id]
	return transition, ok
}

func (r Registry) Admissible(snapshot model.Snapshot, goal model.Goal) []Transition {
	var result []Transition
	for _, transition := range r.ordered {
		if transition.Controllable() && transition.SourceMatches(snapshot) && transition.SupportsGoal(goal) {
			result = append(result, transition)
		}
	}
	return result
}
