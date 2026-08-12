package kernel

import (
	"fmt"
	"sort"
)

type Transition struct {
	ID                   string            `json:"id"`
	SourceModes          []string          `json:"source_modes"`
	TargetMode           string            `json:"target_mode"`
	ObjectiveScope       ObjectiveScope    `json:"objective_scope"`
	ObjectiveMutation    ObjectiveMutation `json:"objective_mutation"`
	RequiredCapabilities []Capability      `json:"required_capabilities"`
	OwnedFacets          []string          `json:"owned_facets"`
	Operation            string            `json:"operation"`
	Priority             int               `json:"priority"`
	Recovers             []string          `json:"recovers,omitempty"`
}

type ObjectiveMutation string

const (
	PreserveObjective      ObjectiveMutation = "preserve"
	BindObjectiveMutation  ObjectiveMutation = "bind"
	ClearObjectiveMutation ObjectiveMutation = "clear"
)

func (m ObjectiveMutation) valid() bool {
	return m == PreserveObjective || m == BindObjectiveMutation || m == ClearObjectiveMutation
}

func (t Transition) validate() error {
	if !semanticID.MatchString(t.ID) || len(t.SourceModes) == 0 || t.TargetMode == "" || !t.ObjectiveScope.Valid() || !t.ObjectiveMutation.valid() || !semanticID.MatchString(t.Operation) || t.Priority < 1 {
		return fmt.Errorf("transition %q has incomplete identity, modes, objective scope, operation, or priority", t.ID)
	}
	if len(t.RequiredCapabilities) == 0 || len(t.OwnedFacets) == 0 {
		return fmt.Errorf("transition %q requires explicit capabilities and owned facets", t.ID)
	}
	if _, err := normalizeCapabilities(t.RequiredCapabilities); err != nil {
		return fmt.Errorf("transition %q: %w", t.ID, err)
	}
	seen := map[string]bool{}
	for _, facet := range t.OwnedFacets {
		if !semanticID.MatchString(facet) || seen[facet] {
			return fmt.Errorf("transition %q has invalid or duplicate owned facet %q", t.ID, facet)
		}
		seen[facet] = true
	}
	if t.ObjectiveMutation != PreserveObjective && (t.ObjectiveScope != ObjectiveNone || !contains(t.OwnedFacets, "supervisor.objective") || !containsCapability(t.RequiredCapabilities, "objective.bind")) {
		return fmt.Errorf("transition %q objective mutation requires NONE scope, supervisor.objective ownership, and objective.bind capability", t.ID)
	}
	for _, recovered := range t.Recovers {
		if !semanticID.MatchString(recovered) {
			return fmt.Errorf("transition %q has invalid recovery target %q", t.ID, recovered)
		}
	}
	return nil
}

func (t Transition) canonical() (Transition, error) {
	copy := t
	var err error
	if copy.SourceModes, err = canonicalIDs(copy.SourceModes, "source mode"); err != nil {
		return Transition{}, fmt.Errorf("transition %q: %w", copy.ID, err)
	}
	if copy.RequiredCapabilities, err = normalizeCapabilities(copy.RequiredCapabilities); err != nil {
		return Transition{}, fmt.Errorf("transition %q: %w", copy.ID, err)
	}
	if copy.OwnedFacets, err = canonicalIDs(copy.OwnedFacets, "owned facet"); err != nil {
		return Transition{}, fmt.Errorf("transition %q: %w", copy.ID, err)
	}
	if copy.Recovers, err = canonicalIDs(copy.Recovers, "recovery target"); err != nil {
		return Transition{}, fmt.Errorf("transition %q: %w", copy.ID, err)
	}
	return copy, nil
}

type Program struct {
	SchemaVersion             int          `json:"schema_version"`
	ID                        string       `json:"id"`
	Version                   string       `json:"version"`
	RuntimeCompatibility      string       `json:"runtime_compatibility"`
	DomainContractFingerprint string       `json:"domain_contract_fingerprint,omitempty"`
	InitialMode               string       `json:"initial_mode"`
	MarkedModes               []string     `json:"marked_modes"`
	Transitions               []Transition `json:"transitions"`
	Fingerprint               string       `json:"fingerprint"`
	byID                      map[string]Transition
}

func CompileProgram(id, version, runtimeCompatibility, initialMode string, markedModes []string, transitions []Transition) (Program, error) {
	return CompileDomainProgram(id, version, runtimeCompatibility, "", initialMode, markedModes, transitions)
}

// CompileDomainProgram binds the complete domain contract fingerprint into the
// same executable Program identity used by the kernel. Domain-specific ABI
// fields remain outside the kernel, but changing any of them changes the
// program fingerprint and invalidates prior prescriptions.
func CompileDomainProgram(id, version, runtimeCompatibility, domainContractFingerprint, initialMode string, markedModes []string, transitions []Transition) (Program, error) {
	program := Program{SchemaVersion: ProgramSchemaVersion, ID: id, Version: version, RuntimeCompatibility: runtimeCompatibility, DomainContractFingerprint: domainContractFingerprint, InitialMode: initialMode, MarkedModes: append([]string(nil), markedModes...), Transitions: append([]Transition(nil), transitions...)}
	if err := program.prepare(); err != nil {
		return Program{}, err
	}
	identity := program
	identity.Fingerprint = ""
	identity.byID = nil
	fingerprint, err := contentHash(identity)
	if err != nil {
		return Program{}, err
	}
	program.Fingerprint = fingerprint
	return program, nil
}

func (p *Program) prepare() error {
	if p.SchemaVersion != ProgramSchemaVersion || !semanticID.MatchString(p.ID) || p.Version == "" || p.RuntimeCompatibility == "" || p.InitialMode == "" || len(p.MarkedModes) == 0 || len(p.Transitions) == 0 {
		return fmt.Errorf("program requires schema, identity, compatibility, initial mode, marked modes, and transitions")
	}
	if p.DomainContractFingerprint != "" && len(p.DomainContractFingerprint) != 64 {
		return fmt.Errorf("program domain contract fingerprint must be an exact sha256 identity")
	}
	markedModes, err := canonicalIDs(p.MarkedModes, "marked mode")
	if err != nil {
		return err
	}
	p.MarkedModes = markedModes
	p.Transitions = append([]Transition(nil), p.Transitions...)
	for index, transition := range p.Transitions {
		canonical, err := transition.canonical()
		if err != nil {
			return err
		}
		p.Transitions[index] = canonical
	}
	sort.Slice(p.Transitions, func(i, j int) bool {
		if p.Transitions[i].Priority != p.Transitions[j].Priority {
			return p.Transitions[i].Priority < p.Transitions[j].Priority
		}
		return p.Transitions[i].ID < p.Transitions[j].ID
	})
	p.byID = make(map[string]Transition, len(p.Transitions))
	for _, transition := range p.Transitions {
		if err := transition.validate(); err != nil {
			return err
		}
		if _, exists := p.byID[transition.ID]; exists {
			return fmt.Errorf("program duplicates transition %q", transition.ID)
		}
		p.byID[transition.ID] = transition
	}
	for _, transition := range p.Transitions {
		if len(transition.Recovers) != 0 && (transition.ObjectiveScope == ObjectiveBoundExact || transition.ObjectiveMutation != PreserveObjective) {
			return fmt.Errorf("recovery transition %q must preserve objective state without requiring an exact objective", transition.ID)
		}
		for _, recovered := range transition.Recovers {
			recoveredTransition, exists := p.byID[recovered]
			if !exists {
				return fmt.Errorf("transition %q recovers unknown transition %q", transition.ID, recovered)
			}
			for _, sourceMode := range recoveredTransition.SourceModes {
				if !contains(transition.SourceModes, sourceMode) {
					return fmt.Errorf("transition %q cannot recover %q from source mode %q", transition.ID, recovered, sourceMode)
				}
			}
		}
	}
	return nil
}

func canonicalIDs(values []string, label string) ([]string, error) {
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if !semanticID.MatchString(value) {
			return nil, fmt.Errorf("%s %q is not a semantic identifier", label, value)
		}
		if index > 0 && value == result[index-1] {
			return nil, fmt.Errorf("%s %q is duplicated", label, value)
		}
	}
	return result, nil
}

func (p Program) Validate() error {
	copy := p
	want := copy.Fingerprint
	copy.Fingerprint = ""
	copy.byID = nil
	if err := copy.prepare(); err != nil {
		return err
	}
	got, err := contentHash(copy)
	if err != nil || want == "" || got != want {
		return fmt.Errorf("program fingerprint does not identify its canonical executable representation")
	}
	return nil
}

func (p Program) Identity() ProgramIdentity {
	return ProgramIdentity{ID: p.ID, Version: p.Version, Fingerprint: p.Fingerprint}
}

func (p Program) Transition(id string) (Transition, bool) {
	if p.byID == nil {
		copy := p
		if copy.prepare() != nil {
			return Transition{}, false
		}
		return copy.Transition(id)
	}
	transition, ok := p.byID[id]
	return transition, ok
}

func (p Program) Marked(mode string) bool {
	for _, candidate := range p.MarkedModes {
		if candidate == mode {
			return true
		}
	}
	return false
}

func (p Program) Clone() Program {
	copy := p
	copy.MarkedModes = append([]string(nil), p.MarkedModes...)
	copy.Transitions = append([]Transition(nil), p.Transitions...)
	copy.byID = make(map[string]Transition, len(copy.Transitions))
	for index, transition := range copy.Transitions {
		transition.SourceModes = append([]string(nil), transition.SourceModes...)
		transition.RequiredCapabilities = append([]Capability(nil), transition.RequiredCapabilities...)
		transition.OwnedFacets = append([]string(nil), transition.OwnedFacets...)
		transition.Recovers = append([]string(nil), transition.Recovers...)
		copy.Transitions[index] = transition
		copy.byID[transition.ID] = transition
	}
	return copy
}
