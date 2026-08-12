package catalog

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

type TransitionID string
type EffectID string

type OriginKind string

const (
	OriginCoreSystem     OriginKind = "core-system"
	OriginControlProgram OriginKind = "control-program"
	OriginExtension      OriginKind = "extension"
)

func (k OriginKind) Valid() bool {
	switch k {
	case OriginCoreSystem, OriginControlProgram, OriginExtension:
		return true
	default:
		return false
	}
}

type TransitionOrigin struct {
	Kind                OriginKind `json:"kind"`
	ID                  string     `json:"id"`
	Version             string     `json:"version"`
	ManifestFingerprint string     `json:"manifest_fingerprint"`
}

type SelectionClass string

const (
	SelectionSystemRecovery    SelectionClass = "SYSTEM_RECOVERY"
	SelectionProgramRecovery   SelectionClass = "PROGRAM_RECOVERY"
	SelectionExtensionRecovery SelectionClass = "EXTENSION_RECOVERY"
	SelectionObjectiveRequired SelectionClass = "OBJECTIVE_REQUIRED"
	SelectionProgramProgress   SelectionClass = "PROGRAM_PROGRESS"
	SelectionExplicitOnly      SelectionClass = "EXPLICIT_ONLY"
	SelectionObservedExternal  SelectionClass = "OBSERVED_EXTERNAL"
)

func (c SelectionClass) Valid() bool {
	switch c {
	case SelectionSystemRecovery, SelectionProgramRecovery, SelectionExtensionRecovery, SelectionObjectiveRequired,
		SelectionProgramProgress, SelectionExplicitOnly, SelectionObservedExternal:
		return true
	default:
		return false
	}
}

func (c SelectionClass) Rank() int {
	switch c {
	case SelectionSystemRecovery:
		return 1
	case SelectionProgramRecovery:
		return 2
	case SelectionExtensionRecovery:
		return 3
	case SelectionObjectiveRequired:
		return 4
	case SelectionProgramProgress:
		return 5
	case SelectionExplicitOnly:
		return 6
	case SelectionObservedExternal:
		return 7
	default:
		return 99
	}
}

type EventClass string

const (
	EventOwnedLocal       EventClass = "owned-local"
	EventOwnedExternal    EventClass = "owned-external"
	EventAuthority        EventClass = "authority"
	EventObservedExternal EventClass = "observed-external"
	EventRecovery         EventClass = "recovery"
)

func (c EventClass) Controllable() bool { return c != EventObservedExternal }

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

// StateEffectKind selects the software-delivery domain's durable-state
// interpreter. It is domain ABI data; the general kernel continues to own
// transition selection, objective scope, capabilities, and facet ownership.
type StateEffectKind string

const (
	StateEffectAssignments StateEffectKind = "assignments"
	StateEffectNative      StateEffectKind = "native"
)

// StateValueReference binds an assignment to one admitted input. Exactly one
// reference field or Assignment.Value must be present.
type StateValueReference struct {
	Parameter  string `json:"parameter,omitempty"`
	Admission  string `json:"admission,omitempty"`
	Invocation string `json:"invocation,omitempty"`
}

// StateAssignment assigns one closed durable-state field from a literal or
// an admitted input. Value is a pointer so the empty string remains an
// explicit literal rather than an omitted field.
type StateAssignment struct {
	Facet     string              `json:"facet"`
	Value     *string             `json:"value,omitempty"`
	ValueFrom StateValueReference `json:"value_from,omitempty"`
}

// StatePrecondition is an ANDed guard over the current durable snapshot. The
// listed values are ORed.
type StatePrecondition struct {
	Facet  string   `json:"facet"`
	Values []string `json:"values"`
}

// StateEffect is the domain-owned executable state declaration. Generic
// assignments and native handlers are mutually exclusive.
type StateEffect struct {
	Kind          StateEffectKind     `json:"kind"`
	Preconditions []StatePrecondition `json:"preconditions,omitempty"`
	Assignments   []StateAssignment   `json:"assignments,omitempty"`
	NativeHandler string              `json:"native_handler,omitempty"`
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

// ObjectiveScope is the software-delivery projection of the kernel's general
// objective-scope law. The transition catalog must state the scope explicitly.
type ObjectiveScope = general.ObjectiveScope

const (
	ObjectiveScopeNone             = general.ObjectiveNone
	ObjectiveScopeOptionalPreserve = general.ObjectiveOptionalPreserve
	ObjectiveScopeBoundExact       = general.ObjectiveBoundExact
)

type PolicyContract struct {
	RequiredWhen            string         `json:"required_when,omitempty"`
	AuthorityRule           string         `json:"authority_rule,omitempty"`
	AvailabilityRule        string         `json:"availability_rule,omitempty"`
	CurrentEvidencePrefix   string         `json:"current_evidence_prefix,omitempty"`
	ManagedOperations       []string       `json:"managed_operations,omitempty"`
	BindsRequestedObjective bool           `json:"binds_requested_objective,omitempty"`
	ReconcilesProgram       bool           `json:"reconciles_program,omitempty"`
	RechecksExternalState   bool           `json:"rechecks_external_state,omitempty"`
	ObjectiveScope          ObjectiveScope `json:"objective_scope,omitempty"`
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
	ID                            TransitionID          `json:"id"`
	Version                       int                   `json:"version"`
	Origin                        TransitionOrigin      `json:"origin"`
	Owner                         string                `json:"owner"`
	SelectionClass                SelectionClass        `json:"selection_class"`
	Class                         EventClass            `json:"class"`
	SourcePhases                  []model.ProtocolPhase `json:"source_phases"`
	TargetPhases                  []model.ProtocolPhase `json:"target_phases"`
	ObjectiveKinds                []model.ObjectiveKind `json:"objective_kinds,omitempty"`
	RequiredIdentity              []string              `json:"required_identity"`
	Authority                     []AuthorityClass      `json:"authority"`
	AuthorityAll                  []AuthorityClass      `json:"authority_all,omitempty"`
	RequiredCapabilities          []Capability          `json:"required_capabilities,omitempty"`
	DeclaredCapabilities          []Capability          `json:"-"`
	RuntimeExecution              bool                  `json:"-"`
	RequiredEvidence              []string              `json:"required_evidence"`
	OwnedResources                []string              `json:"owned_resources,omitempty"`
	OwnedFacets                   []model.StateFacet    `json:"owned_facets,omitempty"`
	StateEffect                   StateEffect           `json:"state_effect,omitempty"`
	Effect                        EffectID              `json:"effect,omitempty"`
	LocalEffects                  []EffectID            `json:"local_effects,omitempty"`
	ExternalEffects               []EffectID            `json:"external_effects,omitempty"`
	Idempotent                    bool                  `json:"idempotent"`
	Parameters                    []ParameterSpec       `json:"parameters,omitempty"`
	Prescription                  Prescription          `json:"prescription"`
	SourcePredicate               string                `json:"source_predicate"`
	SourceConditions              []FacetCondition      `json:"source_conditions"`
	AdmissionPredicate            string                `json:"admission_predicate"`
	TargetPredicate               string                `json:"target_predicate"`
	TargetConditions              []FacetCondition      `json:"target_conditions"`
	Verifier                      string                `json:"verifier"`
	Interruption                  InterruptionContract  `json:"interruption"`
	Reversibility                 Reversibility         `json:"reversibility"`
	TerminalEffect                string                `json:"terminal_effect,omitempty"`
	PrivacyClassification         string                `json:"privacy_classification"`
	TelemetryClassification       string                `json:"telemetry_classification"`
	CostClass                     string                `json:"cost_class"`
	Policy                        PolicyContract        `json:"policy,omitempty"`
	Priority                      int                   `json:"priority"`
	AllowsIdentityRebind          bool                  `json:"allows_identity_rebind,omitempty"`
	AllowsWorktreeTransfer        bool                  `json:"allows_worktree_transfer,omitempty"`
	BindsSourceRevision           bool                  `json:"binds_source_revision,omitempty"`
	AuthorityFingerprintParameter string                `json:"authority_fingerprint_parameter,omitempty"`
}

func (t Transition) Controllable() bool { return t.Class.Controllable() }

// ImplicitlySelectable reports whether an untargeted resolution may choose the
// transition as delivery progress. Maintenance, correction, abandonment, and
// caller-defined markers remain available through an explicit requested
// transition, but cannot outrank the configured objective by merely being
// admissible from the same snapshot.
func (t Transition) ImplicitlySelectable() bool {
	return t.SelectionClass == SelectionSystemRecovery ||
		t.SelectionClass == SelectionProgramRecovery ||
		t.SelectionClass == SelectionExtensionRecovery ||
		t.SelectionClass == SelectionObjectiveRequired ||
		t.SelectionClass == SelectionProgramProgress
}

func (t Transition) SupportsObjective(objective model.Objective) bool {
	if t.Policy.ObjectiveScope == ObjectiveScopeOptionalPreserve {
		return true
	}
	if len(t.ObjectiveKinds) == 0 {
		return true
	}
	for _, kind := range t.ObjectiveKinds {
		if kind == objective.Kind {
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
	if t.Policy.ObjectiveScope == ObjectiveScopeOptionalPreserve && snapshot.Objective.Status != model.FactKnown && snapshot.Objective.Status != model.FactAbsent {
		return false
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
	ordered          []Transition
	byID             map[TransitionID]Transition
	managedOperation map[string]TransitionID
}

var semanticID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*(?:/[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*)?$`)

func New(transitions []Transition) (Registry, error) {
	registry := Registry{
		ordered: cloneTransitions(transitions), byID: make(map[TransitionID]Transition, len(transitions)),
		managedOperation: make(map[string]TransitionID),
	}
	for index, transition := range registry.ordered {
		if err := validateTransition(transition); err != nil {
			return Registry{}, fmt.Errorf("transition %d: %w", index, err)
		}
		if _, exists := registry.byID[transition.ID]; exists {
			return Registry{}, fmt.Errorf("duplicate transition id %q", transition.ID)
		}
		registry.byID[transition.ID] = cloneTransition(transition)
		for _, operation := range transition.Policy.ManagedOperations {
			if prior, exists := registry.managedOperation[operation]; exists {
				return Registry{}, fmt.Errorf("managed operation %q is owned by both %q and %q", operation, prior, transition.ID)
			}
			registry.managedOperation[operation] = transition.ID
		}
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
		if registry.ordered[i].SelectionClass.Rank() != registry.ordered[j].SelectionClass.Rank() {
			return registry.ordered[i].SelectionClass.Rank() < registry.ordered[j].SelectionClass.Rank()
		}
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
	if !t.Origin.Kind.Valid() || t.Origin.ID == "" || t.Origin.Version == "" || t.Origin.ManifestFingerprint == "" || t.Owner == "" {
		return fmt.Errorf("%s: transition origin, owner, version, and manifest fingerprint are required", t.ID)
	}
	if _, err := DurableStateFacetPolicy(t); err != nil {
		return err
	}
	if err := validateStateEffect(t); err != nil {
		return err
	}
	if !t.SelectionClass.Valid() {
		return fmt.Errorf("%s: invalid selection class %q", t.ID, t.SelectionClass)
	}
	if t.Class == EventObservedExternal && t.SelectionClass != SelectionObservedExternal {
		return fmt.Errorf("%s: observed external transition must use OBSERVED_EXTERNAL selection", t.ID)
	}
	if t.Class != EventObservedExternal && t.SelectionClass == SelectionObservedExternal {
		return fmt.Errorf("%s: controllable transition cannot use OBSERVED_EXTERNAL selection", t.ID)
	}
	if err := validateSelectionOwnership(t); err != nil {
		return err
	}
	if t.Version < 1 || len(t.SourcePhases) == 0 || len(t.TargetPhases) == 0 {
		return fmt.Errorf("%s: version, source phases, and target phases are required", t.ID)
	}
	for _, phase := range append(append([]model.ProtocolPhase(nil), t.SourcePhases...), t.TargetPhases...) {
		if !phase.Valid() {
			return fmt.Errorf("%s: invalid phase %q", t.ID, phase)
		}
	}
	objectiveKinds := map[model.ObjectiveKind]bool{}
	for _, objective := range t.ObjectiveKinds {
		if !objective.Valid() || objectiveKinds[objective] {
			return fmt.Errorf("%s: objective kinds must be valid and unique", t.ID)
		}
		objectiveKinds[objective] = true
	}
	if len(t.Authority) == 0 || len(t.RequiredEvidence) == 0 {
		return fmt.Errorf("%s: authority and evidence declarations are required", t.ID)
	}
	for _, authority := range append(append([]AuthorityClass(nil), t.Authority...), t.AuthorityAll...) {
		if !authority.Valid() {
			return fmt.Errorf("%s: invalid authority class %q", t.ID, authority)
		}
	}
	required, err := NormalizeCapabilities(string(t.ID)+".required_capabilities", t.RequiredCapabilities)
	if err != nil {
		return err
	}
	declared, err := NormalizeCapabilities(string(t.ID)+".declared_capabilities", t.DeclaredCapabilities)
	if err != nil {
		return err
	}
	if t.Controllable() {
		allRequired := UnionCapabilities(required, KernelEffectCapabilities(t))
		if missing := MissingCapability(allRequired, NewCapabilitySet(declared...)); missing != "" {
			return fmt.Errorf("%s: CAPABILITY_NOT_DECLARED %q", t.ID, missing)
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
	managedOperations := map[string]bool{}
	for _, operation := range t.Policy.ManagedOperations {
		if !semanticID.MatchString(operation) || managedOperations[operation] {
			return fmt.Errorf("%s: managed operations must be semantic and unique", t.ID)
		}
		managedOperations[operation] = true
	}
	if !t.Policy.ObjectiveScope.Valid() {
		return fmt.Errorf("%s: invalid objective scope %q", t.ID, t.Policy.ObjectiveScope)
	}
	if t.Policy.ObjectiveScope == ObjectiveScopeOptionalPreserve && t.Policy.BindsRequestedObjective {
		return fmt.Errorf("%s: optional-preserve maintenance cannot bind a requested product objective", t.ID)
	}
	if t.Policy.BindsRequestedObjective && (t.Origin.Kind != OriginCoreSystem || !conditionNamesFacet(t.TargetConditions, model.FacetObjective)) {
		return fmt.Errorf("%s: requested-objective binding requires a CoreSystem objective target", t.ID)
	}
	if t.Policy.ReconcilesProgram && (t.Origin.Kind != OriginCoreSystem || !conditionNamesFacet(t.TargetConditions, model.FacetProgram)) {
		return fmt.Errorf("%s: program reconciliation requires a CoreSystem program target", t.ID)
	}
	providerDeclared := containsAuthorityClass(t.Authority, AuthorityProvider) || containsAuthorityClass(t.AuthorityAll, AuthorityProvider)
	if t.AuthorityFingerprintParameter != "" && (!parameterNames[t.AuthorityFingerprintParameter] || !providerDeclared) {
		return fmt.Errorf("%s: provider authority binding requires a declared parameter and mandatory provider authority", t.ID)
	}
	if t.Reversibility == "" || t.PrivacyClassification == "" || t.TelemetryClassification == "" || t.CostClass == "" {
		return fmt.Errorf("%s: recovery/telemetry metadata are incomplete", t.ID)
	}
	if t.Priority < 1 {
		return fmt.Errorf("%s: priority must be positive", t.ID)
	}
	return nil
}

func conditionNamesFacet(conditions []FacetCondition, facet model.FacetName) bool {
	for _, condition := range conditions {
		if condition.Facet == facet {
			return true
		}
	}
	return false
}

func validateSelectionOwnership(t Transition) error {
	switch t.SelectionClass {
	case SelectionSystemRecovery:
		if t.Origin.Kind != OriginCoreSystem || t.Class != EventRecovery {
			return fmt.Errorf("%s: SYSTEM_RECOVERY is reserved for CoreSystem recovery events", t.ID)
		}
	case SelectionProgramRecovery:
		if t.Origin.Kind != OriginControlProgram || t.Class != EventRecovery {
			return fmt.Errorf("%s: PROGRAM_RECOVERY is reserved for ProgramRuntime recovery events", t.ID)
		}
	case SelectionExtensionRecovery:
		if t.Origin.Kind != OriginExtension || t.Class != EventRecovery {
			return fmt.Errorf("%s: EXTENSION_RECOVERY is reserved for extension recovery events", t.ID)
		}
	default:
		if t.Class == EventRecovery {
			return fmt.Errorf("%s: recovery event requires its origin-specific recovery selection class", t.ID)
		}
	}
	return nil
}

func containsAuthorityClass(values []AuthorityClass, wanted AuthorityClass) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (r Registry) Len() int { return len(r.ordered) }

func (r Registry) All() []Transition { return cloneTransitions(r.ordered) }

func (r Registry) Lookup(id TransitionID) (Transition, bool) {
	transition, ok := r.byID[id]
	return cloneTransition(transition), ok
}

// ManagedTransition resolves a host-classified operation through the compiled
// program. Command classifiers never select flow transition IDs themselves.
func (r Registry) ManagedTransition(operation string) (Transition, bool) {
	id, ok := r.managedOperation[operation]
	if !ok {
		return Transition{}, false
	}
	return r.Lookup(id)
}

func (r Registry) Admissible(snapshot model.Snapshot, objective model.Objective) []Transition {
	var result []Transition
	for _, transition := range r.ordered {
		if transition.Controllable() && transition.SourceMatches(snapshot) && transition.SupportsObjective(objective) {
			result = append(result, cloneTransition(transition))
		}
	}
	return result
}

func cloneTransitions(values []Transition) []Transition {
	result := make([]Transition, len(values))
	for index, value := range values {
		result[index] = cloneTransition(value)
	}
	return result
}

func cloneTransition(value Transition) Transition {
	value.SourcePhases = append([]model.ProtocolPhase(nil), value.SourcePhases...)
	value.TargetPhases = append([]model.ProtocolPhase(nil), value.TargetPhases...)
	value.ObjectiveKinds = append([]model.ObjectiveKind(nil), value.ObjectiveKinds...)
	value.RequiredIdentity = append([]string(nil), value.RequiredIdentity...)
	value.Authority = append([]AuthorityClass(nil), value.Authority...)
	value.AuthorityAll = append([]AuthorityClass(nil), value.AuthorityAll...)
	value.RequiredCapabilities = append([]Capability(nil), value.RequiredCapabilities...)
	value.DeclaredCapabilities = append([]Capability(nil), value.DeclaredCapabilities...)
	value.RequiredEvidence = append([]string(nil), value.RequiredEvidence...)
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.OwnedFacets = append([]model.StateFacet(nil), value.OwnedFacets...)
	value.StateEffect.Preconditions = append([]StatePrecondition(nil), value.StateEffect.Preconditions...)
	for index := range value.StateEffect.Preconditions {
		value.StateEffect.Preconditions[index].Values = append([]string(nil), value.StateEffect.Preconditions[index].Values...)
	}
	value.StateEffect.Assignments = append([]StateAssignment(nil), value.StateEffect.Assignments...)
	for index := range value.StateEffect.Assignments {
		if literal := value.StateEffect.Assignments[index].Value; literal != nil {
			copy := *literal
			value.StateEffect.Assignments[index].Value = &copy
		}
	}
	value.LocalEffects = append([]EffectID(nil), value.LocalEffects...)
	value.ExternalEffects = append([]EffectID(nil), value.ExternalEffects...)
	value.Parameters = append([]ParameterSpec(nil), value.Parameters...)
	value.Prescription.Arguments = append([]string(nil), value.Prescription.Arguments...)
	value.SourceConditions = cloneConditions(value.SourceConditions)
	value.TargetConditions = cloneConditions(value.TargetConditions)
	value.Interruption.Points = append([]string(nil), value.Interruption.Points...)
	value.Interruption.PartialState = append([]string(nil), value.Interruption.PartialState...)
	value.Policy.ManagedOperations = append([]string(nil), value.Policy.ManagedOperations...)
	return value
}

func validateStateEffect(t Transition) error {
	if !t.Controllable() {
		if t.StateEffect.Kind != "" || len(t.OwnedFacets) != 0 {
			return fmt.Errorf("%s: observed external transition cannot declare a durable state effect", t.ID)
		}
		return nil
	}
	switch t.StateEffect.Kind {
	case StateEffectAssignments:
		if t.StateEffect.NativeHandler != "" {
			return fmt.Errorf("%s: assignment state effect cannot name a native handler", t.ID)
		}
	case StateEffectNative:
		if t.StateEffect.NativeHandler == "" || !semanticID.MatchString(t.StateEffect.NativeHandler) || len(t.StateEffect.Assignments) != 0 {
			return fmt.Errorf("%s: native state effect requires one semantic handler and no assignments", t.ID)
		}
		if t.RuntimeExecution {
			return fmt.Errorf("%s: component runtime transition cannot invoke a host-native state handler", t.ID)
		}
		if err := validateNativeStateHandler(t); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s: controllable transition requires a declared state effect", t.ID)
	}
	owned := map[model.StateFacet]bool{}
	for _, facet := range t.OwnedFacets {
		owned[facet] = true
	}
	parameters := map[string]ParameterSpec{}
	for _, parameter := range t.Parameters {
		parameters[parameter.Name] = parameter
	}
	preconditions := map[string]bool{}
	for _, condition := range t.StateEffect.Preconditions {
		_, supported := DeclaredStateFieldFacet(condition.Facet)
		if !supported || len(condition.Values) == 0 || preconditions[condition.Facet] {
			return fmt.Errorf("%s: state-effect preconditions require unique facets and values", t.ID)
		}
		preconditions[condition.Facet] = true
		values := map[string]bool{}
		for _, value := range condition.Values {
			if values[value] || !ValidDeclaredStateLiteral(condition.Facet, value) {
				return fmt.Errorf("%s: state-effect precondition %q has invalid or duplicate value %q", t.ID, condition.Facet, value)
			}
			values[value] = true
		}
		if !statePreconditionImpliedBySource(t, condition) {
			return fmt.Errorf("%s: state-effect precondition %q is not implied by resolver source conditions", t.ID, condition.Facet)
		}
	}
	assignments := map[string]bool{}
	for _, assignment := range t.StateEffect.Assignments {
		facet, supported := DeclaredStateFieldFacet(assignment.Facet)
		if !supported || assignments[assignment.Facet] {
			return fmt.Errorf("%s: state-effect assignments require unique facets", t.ID)
		}
		if !owned[facet] {
			return fmt.Errorf("%s: state-effect assignment %q exceeds owned facet %q", t.ID, assignment.Facet, facet)
		}
		assignments[assignment.Facet] = true
		sources := 0
		if assignment.Value != nil {
			sources++
		}
		for _, source := range []string{assignment.ValueFrom.Parameter, assignment.ValueFrom.Admission, assignment.ValueFrom.Invocation} {
			if source != "" {
				sources++
			}
		}
		if sources != 1 {
			return fmt.Errorf("%s: state-effect assignment %q requires exactly one value source", t.ID, assignment.Facet)
		}
		if assignment.Value != nil && !ValidDeclaredStateLiteral(assignment.Facet, *assignment.Value) {
			return fmt.Errorf("%s: state-effect assignment %q has invalid literal %q", t.ID, assignment.Facet, *assignment.Value)
		}
		if err := validateDeterministicAssignment(t, assignment); err != nil {
			return err
		}
		if !stateAssignmentMatchesTarget(t, assignment) {
			return fmt.Errorf("%s: state-effect assignment %q is not compatible with resolver target conditions", t.ID, assignment.Facet)
		}
		if parameter := assignment.ValueFrom.Parameter; parameter != "" {
			spec, declared := parameters[parameter]
			if !declared {
				return fmt.Errorf("%s: state-effect assignment %q references undeclared parameter %q", t.ID, assignment.Facet, parameter)
			}
			if !spec.Required {
				return fmt.Errorf("%s: state-effect assignment %q references optional parameter %q", t.ID, assignment.Facet, parameter)
			}
		}
		if source := assignment.ValueFrom.Admission; source != "" && source != "source_revision" && source != "worktree_fingerprint" && source != "expected_program_fingerprint" {
			return fmt.Errorf("%s: state-effect assignment %q references unknown admission value %q", t.ID, assignment.Facet, source)
		}
		if source := assignment.ValueFrom.Invocation; source != "" && source != "invoking_path" && source != "worktree_id" && source != "ref" {
			return fmt.Errorf("%s: state-effect assignment %q references unknown invocation value %q", t.ID, assignment.Facet, source)
		}
	}
	if err := validateDeclarativeStateClosure(t, assignments); err != nil {
		return err
	}
	return nil
}

func statePreconditionImpliedBySource(t Transition, condition StatePrecondition) bool {
	allowed := make(map[string]bool, len(condition.Values))
	for _, value := range condition.Values {
		allowed[value] = true
	}
	if condition.Facet == "phase" {
		for _, phase := range t.SourcePhases {
			if !allowed[string(phase)] {
				return false
			}
		}
		return true
	}
	facet, ok := DeclaredStateResolverFacet(condition.Facet)
	if !ok {
		return false
	}
	for _, source := range t.SourceConditions {
		if source.Facet != facet || len(source.Statuses) != 1 || source.Statuses[0] != model.FactKnown || len(source.Values) == 0 {
			continue
		}
		for _, value := range source.Values {
			if !allowed[value] {
				return false
			}
		}
		return true
	}
	return false
}

func stateAssignmentMatchesTarget(t Transition, assignment StateAssignment) bool {
	if assignment.Facet == "phase" {
		return assignment.Value != nil && t.DeclaresTargetPhase(model.ProtocolPhase(*assignment.Value))
	}
	facet, stateFacet := DeclaredStateResolverFacet(assignment.Facet)
	if !stateFacet {
		return true
	}
	if assignment.Value == nil {
		return false
	}
	for _, target := range t.TargetConditions {
		if target.Facet != facet {
			continue
		}
		if len(target.Statuses) != 1 || target.Statuses[0] != model.FactKnown {
			return false
		}
		if len(target.Values) == 0 {
			return true
		}
		for _, value := range target.Values {
			if value == *assignment.Value {
				return true
			}
		}
		return false
	}
	return true
}

func cloneConditions(values []FacetCondition) []FacetCondition {
	result := make([]FacetCondition, len(values))
	for index, value := range values {
		value.Statuses = append([]model.FactStatus(nil), value.Statuses...)
		value.Values = append([]string(nil), value.Values...)
		result[index] = value
	}
	return result
}
