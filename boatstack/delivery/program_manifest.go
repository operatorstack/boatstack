package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
)

type ProgramErrorCode string

const (
	ProgramInvalid              ProgramErrorCode = "PROGRAM_INVALID"
	ProgramSchemaUnsupported    ProgramErrorCode = "PROGRAM_SCHEMA_UNSUPPORTED"
	RuntimeTooOld               ProgramErrorCode = "RUNTIME_TOO_OLD"
	manifestProgramVersion                       = "manifest"
	manifestKernelCompatibility                  = "general-kernel"
)

type ProgramError struct {
	Code   ProgramErrorCode
	Field  string
	Detail string
}

func (e ProgramError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Detail)
}

type ProgramCapabilities struct {
	Effects           []string     `json:"effects"`
	Verifiers         []string     `json:"verifiers"`
	CapabilitySurface []Capability `json:"capability_surface"`
}

// ProgramTransition contains only author-controlled executable semantics.
// Origin, owner, and the program-qualified ID are derived by validation.
type ProgramTransition struct {
	ID                            TransitionID         `json:"id"`
	Version                       int                  `json:"version"`
	SelectionClass                SelectionClass       `json:"selection_class"`
	Class                         EventClass           `json:"class"`
	SourcePhases                  []ProtocolPhase      `json:"source_phases"`
	TargetPhases                  []ProtocolPhase      `json:"target_phases"`
	TargetIDs                     []TargetID           `json:"target_ids,omitempty"`
	RequiredIdentity              []string             `json:"required_identity"`
	Authority                     []AuthorityClass     `json:"authority"`
	AuthorityAll                  []AuthorityClass     `json:"authority_all,omitempty"`
	RequiredCapabilities          []Capability         `json:"required_capabilities"`
	RequiredEvidence              []string             `json:"required_evidence"`
	OwnedResources                []string             `json:"owned_resources,omitempty"`
	OwnedFacets                   []StateFacet         `json:"owned_facets"`
	StateEffect                   StateEffect          `json:"state_effect"`
	Effect                        EffectID             `json:"effect,omitempty"`
	LocalEffects                  []EffectID           `json:"local_effects,omitempty"`
	ExternalEffects               []EffectID           `json:"external_effects,omitempty"`
	Idempotent                    bool                 `json:"idempotent"`
	Parameters                    []ParameterSpec      `json:"parameters,omitempty"`
	Prescription                  Prescription         `json:"prescription"`
	SourcePredicate               string               `json:"source_predicate"`
	SourceConditions              []FacetCondition     `json:"source_conditions"`
	AdmissionPredicate            string               `json:"admission_predicate"`
	TargetPredicate               string               `json:"target_predicate"`
	TargetConditions              []FacetCondition     `json:"target_conditions"`
	Verifier                      string               `json:"verifier"`
	Interruption                  InterruptionContract `json:"interruption"`
	Reversibility                 Reversibility        `json:"reversibility"`
	TerminalEffect                string               `json:"terminal_effect,omitempty"`
	PrivacyClassification         string               `json:"privacy_classification"`
	TelemetryClassification       string               `json:"telemetry_classification"`
	CostClass                     string               `json:"cost_class"`
	Policy                        PolicyContract       `json:"policy,omitempty"`
	Priority                      int                  `json:"priority"`
	AllowsIdentityRebind          bool                 `json:"allows_identity_rebind,omitempty"`
	AllowsWorktreeTransfer        bool                 `json:"allows_worktree_transfer,omitempty"`
	ExecutionContext              string               `json:"execution_context,omitempty"`
	BindsSourceRevision           bool                 `json:"binds_source_revision,omitempty"`
	AuthorityFingerprintParameter string               `json:"authority_fingerprint_parameter,omitempty"`
}

// ProgramManifest is the complete source representation of one Control
// Program. Product surfaces may call the complete program a Flow.
type ProgramManifest struct {
	SchemaVersion      int                 `json:"schema_version"`
	ProgramID          string              `json:"program_id"`
	ProgramVersion     string              `json:"program_version"`
	RequiresRuntime    string              `json:"requires_runtime"`
	Capabilities       ProgramCapabilities `json:"capabilities"`
	OwnedResources     []string            `json:"owned_resources"`
	ObjectiveContracts []ObjectiveContract `json:"objective_contracts"`
	Transitions        []ProgramTransition `json:"transitions"`
}

// RuntimeCompatibility is verified runtime evidence. Declaring a capability
// does not grant transition authority.
type RuntimeCompatibility struct {
	Version      string
	Effects      []string
	Verifiers    []string
	Capabilities []Capability
}

var (
	programSemanticID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	programVersionID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	runtimeConstraint = regexp.MustCompile(`^>=(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	runtimeVersion    = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)
)

// LoadProgram strictly parses, validates, normalizes, compatibility-checks,
// and fingerprints one complete Control Program before registry construction.
func LoadProgram(source io.Reader, runtime RuntimeCompatibility) (ControlProgram, error) {
	raw, err := io.ReadAll(io.LimitReader(source, 16<<20))
	if err != nil {
		return ControlProgram{}, invalidProgram("", "read program source: "+err.Error())
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return ControlProgram{}, invalidProgram("", err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest ProgramManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ControlProgram{}, invalidProgram("", "parse program source: "+err.Error())
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ControlProgram{}, invalidProgram("", "program source contains trailing JSON")
	}
	return ValidateProgram(manifest, runtime)
}

// ValidateProgram is the typed form of LoadProgram and enforces the same
// boundary for programmatic callers.
func ValidateProgram(manifest ProgramManifest, runtime RuntimeCompatibility) (ControlProgram, error) {
	if manifest.SchemaVersion < 1 {
		return ControlProgram{}, invalidProgram("schema_version", "must be a positive integer")
	}
	if manifest.SchemaVersion != ProgramSchemaVersion {
		return ControlProgram{}, ProgramError{Code: ProgramSchemaUnsupported, Field: "schema_version", Detail: fmt.Sprintf("schema %d is unsupported", manifest.SchemaVersion)}
	}
	if !programSemanticID.MatchString(manifest.ProgramID) || strings.Contains(manifest.ProgramID, "/") {
		return ControlProgram{}, invalidProgram("program_id", "must be a non-empty semantic identifier without '/'")
	}
	if !programVersionID.MatchString(manifest.ProgramVersion) {
		return ControlProgram{}, invalidProgram("program_version", "must be a non-empty deterministic identity")
	}
	minimum, err := parseMinimumRuntime(manifest.RequiresRuntime)
	if err != nil {
		return ControlProgram{}, invalidProgram("requires_runtime", err.Error())
	}
	actual, err := parseRuntimeVersion(runtime.Version)
	if err != nil {
		return ControlProgram{}, invalidProgram("runtime.version", err.Error())
	}
	if actual.less(minimum) {
		return ControlProgram{}, ProgramError{Code: RuntimeTooOld, Field: "requires_runtime", Detail: fmt.Sprintf("runtime %s does not satisfy %s", runtime.Version, manifest.RequiresRuntime)}
	}
	if len(manifest.Transitions) == 0 {
		return ControlProgram{}, invalidProgram("transitions", "at least one transition is required")
	}

	effects, err := normalizedDeclarations("capabilities.effects", manifest.Capabilities.Effects)
	if err != nil {
		return ControlProgram{}, err
	}
	verifiers, err := normalizedDeclarations("capabilities.verifiers", manifest.Capabilities.Verifiers)
	if err != nil {
		return ControlProgram{}, err
	}
	resources, err := normalizedDeclarations("owned_resources", manifest.OwnedResources)
	if err != nil {
		return ControlProgram{}, err
	}
	if missing := missingCapability(effects, runtime.Effects); missing != "" {
		return ControlProgram{}, invalidProgram("capabilities.effects", fmt.Sprintf("runtime does not provide %q", missing))
	}
	if missing := missingCapability(verifiers, runtime.Verifiers); missing != "" {
		return ControlProgram{}, invalidProgram("capabilities.verifiers", fmt.Sprintf("runtime does not provide %q", missing))
	}
	declaredCapabilities, err := catalog.NormalizeCapabilities("capabilities.capability_surface", manifest.Capabilities.CapabilitySurface)
	if err != nil || len(declaredCapabilities) == 0 {
		if err != nil {
			return ControlProgram{}, invalidProgram("capabilities.capability_surface", err.Error())
		}
		return ControlProgram{}, invalidProgram("capabilities.capability_surface", "must declare a non-empty capability surface")
	}
	runtimeCapabilities, err := catalog.NormalizeCapabilities("runtime.capabilities", runtime.Capabilities)
	if err != nil {
		return ControlProgram{}, invalidProgram("runtime.capabilities", err.Error())
	}
	if missing := catalog.MissingCapability(declaredCapabilities, catalog.NewCapabilitySet(runtimeCapabilities...)); missing != "" {
		return ControlProgram{}, invalidProgram("capabilities.capability_surface", fmt.Sprintf("runtime does not provide %q", missing))
	}

	normalized := make([]Transition, len(manifest.Transitions))
	seen := map[TransitionID]bool{}
	usedEffects := map[string]bool{}
	usedVerifiers := map[string]bool{}
	usedResources := map[string]bool{}
	for index, declared := range manifest.Transitions {
		source := declared.runtimeTransition()
		// Repository-authored programs execute through the protocol runtime.
		// This is compiler-owned and cannot be disabled by program input.
		source.RuntimeExecution = true
		source.DeclaredCapabilities = append([]Capability(nil), declaredCapabilities...)
		if source.Controllable() && len(source.RequiredCapabilities) == 0 {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].required_capabilities", index), "must not be empty")
		}
		local := source.ID
		if !programSemanticID.MatchString(string(local)) || strings.Contains(string(local), "/") {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].id", index), "must be a local semantic identifier without '/'")
		}
		if seen[local] {
			return ControlProgram{}, invalidProgram("transitions", fmt.Sprintf("duplicate transition id %q", local))
		}
		seen[local] = true
		source.ID = TransitionID(manifest.ProgramID + "/" + string(local))
		source.Owner = manifest.ProgramID
		source.Origin = catalog.TransitionOrigin{Kind: catalog.OriginControlProgram, ID: manifest.ProgramID, Version: manifest.ProgramVersion, ManifestFingerprint: "pending"}
		if source.Interruption.Recovery != "" {
			if !programSemanticID.MatchString(string(source.Interruption.Recovery)) || strings.Contains(string(source.Interruption.Recovery), "/") {
				return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].interruption.recovery", index), "must be a local semantic identifier without '/'")
			}
			source.Interruption.Recovery = TransitionID(manifest.ProgramID + "/" + string(source.Interruption.Recovery))
		}
		if source.Policy.BindsRequestedObjective || source.Policy.ReconcilesProgram {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].policy", index), "runtime-reserved program mutation policy is not repository-declarable")
		}
		if source.Controllable() && !containsDeclaration(effects, string(source.Effect)) {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].effect", index), "effect is not declared by the program")
		}
		if missing := catalog.MissingCapability(catalog.RequiredCapabilities(source), catalog.NewCapabilitySet(declaredCapabilities...)); missing != "" {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].required_capabilities", index), fmt.Sprintf("CAPABILITY_NOT_DECLARED %q", missing))
		}
		if source.Controllable() {
			usedEffects[string(source.Effect)] = true
		}
		if !containsDeclaration(verifiers, source.Verifier) {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].verifier", index), "verifier is not declared by the program")
		}
		usedVerifiers[source.Verifier] = true
		for _, resource := range source.OwnedResources {
			if !containsDeclaration(resources, resource) {
				return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d].owned_resources", index), fmt.Sprintf("resource %q is not declared by the program", resource))
			}
			usedResources[resource] = true
		}
		normalized[index], err = normalizeProgramTransition(source)
		if err != nil {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("transitions[%d]", index), err.Error())
		}
	}
	if extra := firstUnusedDeclaration(effects, usedEffects); extra != "" {
		return ControlProgram{}, invalidProgram("capabilities.effects", fmt.Sprintf("unused declaration %q", extra))
	}
	if extra := firstUnusedDeclaration(verifiers, usedVerifiers); extra != "" {
		return ControlProgram{}, invalidProgram("capabilities.verifiers", fmt.Sprintf("unused declaration %q", extra))
	}
	if extra := firstUnusedDeclaration(resources, usedResources); extra != "" {
		return ControlProgram{}, invalidProgram("owned_resources", fmt.Sprintf("unused declaration %q", extra))
	}

	contracts := append([]ObjectiveContract(nil), manifest.ObjectiveContracts...)
	for index := range contracts {
		contracts[index].Conditions, err = normalizeConditionSet(contracts[index].Conditions)
		if err != nil {
			return ControlProgram{}, invalidProgram(fmt.Sprintf("objective_contracts[%d].conditions", index), err.Error())
		}
	}
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].TargetID < contracts[j].TargetID })
	objectiveContracts, err := catalog.NewObjectiveContracts(contracts, nil)
	if err != nil {
		return ControlProgram{}, invalidProgram("objective_contracts", err.Error())
	}

	canonicalTransitions := cloneTransitionsForProgram(normalized)
	for index := range canonicalTransitions {
		canonicalTransitions[index].Origin.Version = ""
		canonicalTransitions[index].Origin.ManifestFingerprint = ""
	}
	sort.Slice(canonicalTransitions, func(i, j int) bool { return canonicalTransitions[i].ID < canonicalTransitions[j].ID })
	canonical := struct {
		ProgramID          string
		Capabilities       ProgramCapabilities
		OwnedResources     []string
		ObjectiveContracts []ObjectiveContract
		Transitions        []Transition
	}{manifest.ProgramID, ProgramCapabilities{Effects: effects, Verifiers: verifiers, CapabilitySurface: declaredCapabilities}, resources, objectiveContracts.All(), canonicalTransitions}
	domainContractFingerprint, err := fingerprint(canonical)
	if err != nil {
		return ControlProgram{}, invalidProgram("", "fingerprint canonical program: "+err.Error())
	}
	supervisoryProgram, err := compileSupervisoryProgram(
		ProgramRuntimeManifest{ID: manifest.ProgramID, Version: manifestProgramVersion},
		manifestKernelCompatibility,
		domainContractFingerprint,
		normalized,
	)
	if err != nil {
		return ControlProgram{}, invalidProgram("transitions", "compile supervisory program: "+err.Error())
	}
	programFingerprint := supervisoryProgram.Fingerprint
	for index := range normalized {
		normalized[index].Origin.ManifestFingerprint = programFingerprint
	}
	registry, err := catalog.New(normalized)
	if err != nil {
		return ControlProgram{}, invalidProgram("transitions", err.Error())
	}
	ownership := make(map[string]string, len(resources))
	for _, resource := range resources {
		ownership[resource] = manifest.ProgramID
	}
	identity := ComponentIdentity{ID: manifest.ProgramID, Version: manifest.ProgramVersion, Fingerprint: programFingerprint}
	return ControlProgram{
		summary: ProgramSummary{
			SchemaVersion: ProgramSchemaVersion, KernelVersion: runtime.Version,
			ProgramID: manifest.ProgramID, ProgramVersion: manifest.ProgramVersion, RequiresRuntime: manifest.RequiresRuntime,
			Runtime: identity, RuntimeTransitionCount: registry.Len(), TotalTransitionCount: registry.Len(), ProgramFingerprint: programFingerprint,
		},
		supervisoryProgram: supervisoryProgram, registry: registry, objectiveContracts: objectiveContracts, resourceOwnership: ownership,
		programRuntime: compiledProgramRuntime{identity: identity},
	}, nil
}

func (value ProgramTransition) runtimeTransition() Transition {
	return Transition{
		ID: value.ID, Version: value.Version, SelectionClass: value.SelectionClass, Class: value.Class,
		SourcePhases: value.SourcePhases, TargetPhases: value.TargetPhases, TargetIDs: value.TargetIDs,
		RequiredIdentity: value.RequiredIdentity, Authority: value.Authority, AuthorityAll: value.AuthorityAll,
		RequiredCapabilities: value.RequiredCapabilities,
		RequiredEvidence:     value.RequiredEvidence, OwnedResources: value.OwnedResources, Effect: value.Effect,
		OwnedFacets: value.OwnedFacets, StateEffect: value.StateEffect,
		LocalEffects: value.LocalEffects, ExternalEffects: value.ExternalEffects, Idempotent: value.Idempotent,
		Parameters: value.Parameters, Prescription: value.Prescription, SourcePredicate: value.SourcePredicate,
		SourceConditions: value.SourceConditions, AdmissionPredicate: value.AdmissionPredicate,
		TargetPredicate: value.TargetPredicate, TargetConditions: value.TargetConditions, Verifier: value.Verifier,
		Interruption: value.Interruption, Reversibility: value.Reversibility, TerminalEffect: value.TerminalEffect,
		PrivacyClassification: value.PrivacyClassification, TelemetryClassification: value.TelemetryClassification,
		CostClass: value.CostClass, Policy: value.Policy, Priority: value.Priority,
		AllowsIdentityRebind: value.AllowsIdentityRebind, AllowsWorktreeTransfer: value.AllowsWorktreeTransfer,
		ExecutionContext:    value.ExecutionContext,
		BindsSourceRevision: value.BindsSourceRevision, AuthorityFingerprintParameter: value.AuthorityFingerprintParameter,
	}
}

func invalidProgram(field, detail string) error {
	return ProgramError{Code: ProgramInvalid, Field: field, Detail: detail}
}

type semanticVersion struct {
	major, minor, patch int
	prerelease          bool
}

func parseMinimumRuntime(value string) (semanticVersion, error) {
	match := runtimeConstraint.FindStringSubmatch(value)
	if match == nil {
		return semanticVersion{}, fmt.Errorf("must use exact minimum syntax >=MAJOR.MINOR.PATCH")
	}
	return versionParts(match[1:4], false), nil
}

func parseRuntimeVersion(value string) (semanticVersion, error) {
	match := runtimeVersion.FindStringSubmatch(value)
	if match == nil {
		return semanticVersion{}, fmt.Errorf("must be vMAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH")
	}
	return versionParts(match[1:4], match[4] != ""), nil
}

func versionParts(parts []string, prerelease bool) semanticVersion {
	values := make([]int, 3)
	for index, part := range parts {
		values[index], _ = strconv.Atoi(part)
	}
	return semanticVersion{major: values[0], minor: values[1], patch: values[2], prerelease: prerelease}
}

func (v semanticVersion) less(other semanticVersion) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	if v.patch != other.patch {
		return v.patch < other.patch
	}
	return v.prerelease && !other.prerelease
}

func normalizedDeclarations(field string, values []string) ([]string, error) {
	result := append([]string(nil), values...)
	seen := map[string]bool{}
	for _, value := range result {
		if value == "" || !programSemanticID.MatchString(value) || seen[value] {
			return nil, invalidProgram(field, fmt.Sprintf("contains invalid or duplicate declaration %q", value))
		}
		seen[value] = true
	}
	sort.Strings(result)
	return result, nil
}

func missingCapability(required, available []string) string {
	set := map[string]bool{}
	for _, value := range available {
		set[value] = true
	}
	for _, value := range required {
		if !set[value] {
			return value
		}
	}
	return ""
}

func firstUnusedDeclaration(declared []string, used map[string]bool) string {
	for _, value := range declared {
		if !used[value] {
			return value
		}
	}
	return ""
}

func containsDeclaration(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func normalizeProgramTransition(value Transition) (Transition, error) {
	var err error
	value.SourcePhases, err = uniqueSorted(value.SourcePhases, func(v ProtocolPhase) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.TargetPhases, err = uniqueSorted(value.TargetPhases, func(v ProtocolPhase) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.TargetIDs, err = uniqueSorted(value.TargetIDs, func(v TargetID) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.RequiredIdentity, err = uniqueSorted(value.RequiredIdentity, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.Authority, err = uniqueSorted(value.Authority, func(v AuthorityClass) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.AuthorityAll, err = uniqueSorted(value.AuthorityAll, func(v AuthorityClass) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.RequiredEvidence, err = uniqueSorted(value.RequiredEvidence, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.OwnedResources, err = uniqueSorted(value.OwnedResources, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.OwnedFacets, err = uniqueSorted(value.OwnedFacets, func(v StateFacet) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	for index := range value.StateEffect.Preconditions {
		value.StateEffect.Preconditions[index].Values, err = uniqueSorted(value.StateEffect.Preconditions[index].Values, func(v string) string { return v })
		if err != nil {
			return Transition{}, err
		}
	}
	value.StateEffect.Preconditions, err = uniqueSorted(value.StateEffect.Preconditions, func(v StatePrecondition) string { return v.Facet })
	if err != nil {
		return Transition{}, err
	}
	value.StateEffect.Assignments, err = uniqueSorted(value.StateEffect.Assignments, func(v StateAssignment) string { return v.Facet })
	if err != nil {
		return Transition{}, err
	}
	value.LocalEffects, err = uniqueSorted(value.LocalEffects, func(v EffectID) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.ExternalEffects, err = uniqueSorted(value.ExternalEffects, func(v EffectID) string { return string(v) })
	if err != nil {
		return Transition{}, err
	}
	value.Parameters, err = uniqueSorted(value.Parameters, func(v ParameterSpec) string { return v.Name })
	if err != nil {
		return Transition{}, err
	}
	value.Interruption.Points, err = uniqueSorted(value.Interruption.Points, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.Interruption.PartialState, err = uniqueSorted(value.Interruption.PartialState, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.Policy.ManagedOperations, err = uniqueSorted(value.Policy.ManagedOperations, func(v string) string { return v })
	if err != nil {
		return Transition{}, err
	}
	value.SourceConditions, err = normalizeConditionSet(value.SourceConditions)
	if err != nil {
		return Transition{}, err
	}
	value.TargetConditions, err = normalizeConditionSet(value.TargetConditions)
	if err != nil {
		return Transition{}, err
	}
	return value, nil
}

func normalizeConditionSet(values []FacetCondition) ([]FacetCondition, error) {
	result := append([]FacetCondition(nil), values...)
	for index := range result {
		var err error
		result[index].Statuses, err = uniqueSorted(result[index].Statuses, func(value FactStatus) string { return string(value) })
		if err != nil {
			return nil, err
		}
		result[index].Values, err = uniqueSortedAllowEmpty(result[index].Values)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := json.Marshal(result[i])
		right, _ := json.Marshal(result[j])
		return string(left) < string(right)
	})
	for index := 1; index < len(result); index++ {
		left, _ := json.Marshal(result[index-1])
		right, _ := json.Marshal(result[index])
		if bytes.Equal(left, right) {
			return nil, fmt.Errorf("contains duplicate condition")
		}
	}
	return result, nil
}

func uniqueSortedAllowEmpty(values []string) ([]string, error) {
	result := append([]string(nil), values...)
	seen := map[string]bool{}
	for _, value := range result {
		if seen[value] {
			return nil, fmt.Errorf("contains duplicate declaration %q", value)
		}
		seen[value] = true
	}
	sort.Strings(result)
	return result, nil
}

func uniqueSorted[T any](values []T, key func(T) string) ([]T, error) {
	result := append([]T(nil), values...)
	seen := map[string]bool{}
	for _, value := range result {
		name := key(value)
		if name == "" || seen[name] {
			return nil, fmt.Errorf("contains empty or duplicate declaration %q", name)
		}
		seen[name] = true
	}
	sort.Slice(result, func(i, j int) bool { return key(result[i]) < key(result[j]) })
	return result, nil
}

func cloneTransitionsForProgram(values []Transition) []Transition {
	result := make([]Transition, len(values))
	for index, value := range values {
		result[index] = cloneTransition(value)
	}
	return result
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key := keyToken.(string)
				if seen[key] {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return nil
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
