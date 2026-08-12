// Package delivery defines the software-delivery domain's authoring and
// compilation contracts. The domain is Boatstack's first production use of
// the general kernel, not part of the kernel itself.
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Transition = catalog.Transition
type TransitionID = catalog.TransitionID
type EventClass = catalog.EventClass
type AuthorityClass = catalog.AuthorityClass
type Capability = catalog.Capability
type FacetCondition = catalog.FacetCondition
type SelectionClass = catalog.SelectionClass
type ObjectiveContract = catalog.ObjectiveContract
type ObjectiveScope = catalog.ObjectiveScope
type EffectID = catalog.EffectID
type Prescription = catalog.Prescription
type ParameterSpec = catalog.ParameterSpec
type InterruptionContract = catalog.InterruptionContract
type PolicyContract = catalog.PolicyContract
type Reversibility = catalog.Reversibility
type ObjectiveKind = model.ObjectiveKind
type ProtocolPhase = model.ProtocolPhase
type FactStatus = model.FactStatus
type FacetName = model.FacetName

const (
	EventOwnedLocal       = catalog.EventOwnedLocal
	EventOwnedExternal    = catalog.EventOwnedExternal
	EventAuthority        = catalog.EventAuthority
	EventObservedExternal = catalog.EventObservedExternal
	EventRecovery         = catalog.EventRecovery

	AuthorityNone       = catalog.AuthorityNone
	AuthorityRepository = catalog.AuthorityRepository
	AuthorityHuman      = catalog.AuthorityHuman
	AuthorityAutonomy   = catalog.AuthorityAutonomy
	AuthorityProvider   = catalog.AuthorityProvider

	CapabilityRepositoryWrite    = catalog.CapabilityRepositoryWrite
	CapabilityCommandExecute     = catalog.CapabilityCommandExecute
	CapabilityProductMutate      = catalog.CapabilityProductMutate
	CapabilityPublicationPrepare = catalog.CapabilityPublicationPrepare
	CapabilityPublicationPublish = catalog.CapabilityPublicationPublish
	CapabilityHumanApprove       = catalog.CapabilityHumanApprove

	SelectionSystemRecovery    = catalog.SelectionSystemRecovery
	SelectionProgramRecovery   = catalog.SelectionProgramRecovery
	SelectionExtensionRecovery = catalog.SelectionExtensionRecovery
	SelectionObjectiveRequired = catalog.SelectionObjectiveRequired
	SelectionProgramProgress   = catalog.SelectionProgramProgress
	SelectionExplicitOnly      = catalog.SelectionExplicitOnly
	SelectionObservedExternal  = catalog.SelectionObservedExternal

	ObjectiveScopeNone             = catalog.ObjectiveScopeNone
	ObjectiveScopeOptionalPreserve = catalog.ObjectiveScopeOptionalPreserve
	ObjectiveScopeBoundExact       = catalog.ObjectiveScopeBoundExact

	ObjectiveApprovedPlan = model.ObjectiveApprovedPlan
	ObjectiveVerified     = model.ObjectiveVerified
	ObjectiveOpenPR       = model.ObjectiveOpenPR
	ObjectiveMerged       = model.ObjectiveMerged
	ObjectiveAbandoned    = model.ObjectiveAbandoned

	PhaseDormant    = model.PhaseDormant
	PhaseObserved   = model.PhaseObserved
	PhaseActive     = model.PhaseActive
	PhaseRecovery   = model.PhaseRecovery
	PhaseFrontier   = model.PhaseFrontier
	PhaseTerminal   = model.PhaseTerminal
	PhaseUnresolved = model.PhaseUnresolved
	PhaseAbandoned  = model.PhaseAbandoned

	FactKnown       = model.FactKnown
	FactAbsent      = model.FactAbsent
	FactUnknown     = model.FactUnknown
	FactStale       = model.FactStale
	FactConflicting = model.FactConflicting

	Reversible      = catalog.Reversible
	Compensatable   = catalog.Compensatable
	Irreversible    = catalog.Irreversible
	ObservationOnly = catalog.ObservationOnly

	FacetPhase               = model.FacetPhase
	FacetProgram             = model.FacetProgram
	FacetTopology            = model.FacetTopology
	FacetEngagement          = model.FacetEngagement
	FacetDelivery            = model.FacetDelivery
	FacetWorkspace           = model.FacetWorkspace
	FacetPlan                = model.FacetPlan
	FacetConfiguration       = model.FacetConfiguration
	FacetConfigurationPolicy = model.FacetConfigurationPolicy
	FacetRuntime             = model.FacetRuntime
	FacetPublication         = model.FacetPublication
	FacetVerification        = model.FacetVerification
	FacetRecovery            = model.FacetRecovery
	FacetTransaction         = model.FacetTransaction
	FacetRecoveryInfo        = model.FacetRecoveryInfo
	FacetTransactionInfo     = model.FacetTransactionInfo
	FacetTerminal            = model.FacetTerminal
	FacetObjective           = model.FacetObjective
)

const ProgramSchemaVersion = 3

func KernelEffectCapabilities(transition Transition) []Capability {
	return catalog.KernelEffectCapabilities(transition)
}

func UnionCapabilities(groups ...[]Capability) []Capability {
	return catalog.UnionCapabilities(groups...)
}

func KnownCondition(facet FacetName, values ...string) FacetCondition {
	return FacetCondition{Facet: facet, Statuses: []FactStatus{FactKnown}, Values: append([]string(nil), values...)}
}

func StatusCondition(facet FacetName, statuses ...FactStatus) FacetCondition {
	return FacetCondition{Facet: facet, Statuses: append([]FactStatus(nil), statuses...)}
}

// CoreSystemDefinition supplies Boatstack's operational capabilities. Trust is
// assigned by the application calling Compile, not by the implementation.
type CoreSystemDefinition interface {
	CoreManifest(context.Context) (CoreSystemManifest, error)
}

// ProgramRuntimeDefinition supplies one trusted in-process execution binding.
type ProgramRuntimeDefinition interface {
	RuntimeManifest(context.Context) (ProgramRuntimeManifest, error)
}

// Extension supplies an additive manifest. Runtime behavior is optional for a
// declaration-only extension and is assigned by the assembling application.
type Extension interface {
	ExtensionManifest(context.Context) (ExtensionManifest, error)
}

type CoreSystemManifest struct {
	ID           string       `json:"id"`
	Version      string       `json:"version"`
	Capabilities []Capability `json:"capabilities"`
	Transitions  []Transition `json:"transitions"`
}

type ProgramRuntimeManifest struct {
	ID                      string              `json:"id"`
	Version                 string              `json:"version"`
	ProtocolVersion         int                 `json:"protocol_version"`
	RuntimeMode             ProgramRuntimeMode  `json:"runtime_mode"`
	SupportedObjectives     []ObjectiveKind     `json:"supported_objectives"`
	ObjectiveContracts      []ObjectiveContract `json:"objective_contracts"`
	Transitions             []Transition        `json:"transitions"`
	Facts                   []string            `json:"facts,omitempty"`
	OwnedResources          []string            `json:"owned_resources"`
	Effects                 []string            `json:"effects"`
	Verifiers               []string            `json:"verifiers"`
	Capabilities            []Capability        `json:"capabilities"`
	RecoveryTransitions     []TransitionID      `json:"recovery_transitions"`
	Settings                json.RawMessage     `json:"settings,omitempty"`
	ConfigurationSchema     json.RawMessage     `json:"configuration_schema,omitempty"`
	PrivacyClassification   string              `json:"privacy_classification"`
	TelemetryClassification string              `json:"telemetry_classification"`
}

type ObjectiveConstraint struct {
	ObjectiveKind ObjectiveKind    `json:"objective_kind"`
	Conditions    []FacetCondition `json:"conditions"`
}

type ExtensionManifest struct {
	ID                      string                `json:"id"`
	Version                 string                `json:"version"`
	ProtocolVersion         int                   `json:"protocol_version"`
	ExecutableSHA256        string                `json:"executable_sha256,omitempty"`
	Settings                json.RawMessage       `json:"settings,omitempty"`
	SettingsSchema          json.RawMessage       `json:"settings_schema"`
	Facts                   []string              `json:"facts,omitempty"`
	Transitions             []Transition          `json:"transitions,omitempty"`
	ObjectiveConstraints    []ObjectiveConstraint `json:"objective_constraints,omitempty"`
	OwnedResources          []string              `json:"owned_resources,omitempty"`
	Effects                 []string              `json:"effects,omitempty"`
	Verifiers               []string              `json:"verifiers,omitempty"`
	Capabilities            []Capability          `json:"capabilities"`
	RecoveryTransitions     []TransitionID        `json:"recovery_transitions,omitempty"`
	PrivacyClassification   string                `json:"privacy_classification"`
	TelemetryClassification string                `json:"telemetry_classification"`
	Dependencies            []string              `json:"dependencies,omitempty"`
}

type ComponentIdentity struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

type ProgramSummary struct {
	SchemaVersion            int                 `json:"schema_version"`
	KernelVersion            string              `json:"kernel_version"`
	ProgramID                string              `json:"program_id"`
	ProgramVersion           string              `json:"program_version"`
	RequiresRuntime          string              `json:"requires_runtime,omitempty"`
	Core                     ComponentIdentity   `json:"core"`
	Runtime                  ComponentIdentity   `json:"program_runtime"`
	Extensions               []ComponentIdentity `json:"extensions,omitempty"`
	CoreTransitionCount      int                 `json:"core_transition_count"`
	RuntimeTransitionCount   int                 `json:"runtime_transition_count"`
	ExtensionTransitionCount int                 `json:"extension_transition_count"`
	TotalTransitionCount     int                 `json:"total_transition_count"`
	ProgramFingerprint       string              `json:"program_fingerprint"`
}

// ControlProgram is immutable after Compile. Its accessors always return
// copies; the runtime registry remains the one executable graph.
type ControlProgram struct {
	summary             ProgramSummary
	supervisoryProgram  general.Program
	registry            catalog.Registry
	objectiveContracts  catalog.ObjectiveContracts
	resourceOwnership   map[string]string
	settingsFingerprint string
	extensions          []compiledExtension
	programRuntime      compiledProgramRuntime
}

type compiledProgramRuntime struct {
	manifest ProgramRuntimeManifest
	identity ComponentIdentity
	runtime  ProgramRuntime
}

type CompiledProgramRuntime struct {
	Manifest ProgramRuntimeManifest
	Identity ComponentIdentity
	Runtime  ProgramRuntime
}

type compiledExtension struct {
	manifest ExtensionManifest
	identity ComponentIdentity
	runtime  ExtensionRuntime
}

type CompiledExtension struct {
	Manifest ExtensionManifest
	Identity ComponentIdentity
	Runtime  ExtensionRuntime
}

func (p ControlProgram) Summary() ProgramSummary {
	result := p.summary
	result.Extensions = append([]ComponentIdentity(nil), result.Extensions...)
	return result
}

func (p ControlProgram) Fingerprint() string       { return p.summary.ProgramFingerprint }
func (p ControlProgram) TransitionCount() int      { return p.registry.Len() }
func (p ControlProgram) Transitions() []Transition { return p.registry.All() }
func (p ControlProgram) ResourceOwnership() map[string]string {
	result := make(map[string]string, len(p.resourceOwnership))
	for resource, owner := range p.resourceOwnership {
		result[resource] = owner
	}
	return result
}

func (p ControlProgram) Extensions() []CompiledExtension {
	result := make([]CompiledExtension, 0, len(p.extensions))
	for _, extension := range p.extensions {
		result = append(result, CompiledExtension{Manifest: cloneExtensionManifest(extension.manifest), Identity: extension.identity, Runtime: extension.runtime})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Identity.ID < result[j].Identity.ID })
	return result
}

func (p ControlProgram) ExtensionByID(id string) (CompiledExtension, bool) {
	for _, extension := range p.extensions {
		if extension.identity.ID == id {
			return CompiledExtension{Manifest: cloneExtensionManifest(extension.manifest), Identity: extension.identity, Runtime: extension.runtime}, true
		}
	}
	return CompiledExtension{}, false
}

func (p ControlProgram) ProgramRuntime() CompiledProgramRuntime {
	return CompiledProgramRuntime{Manifest: cloneRuntimeManifest(p.programRuntime.manifest), Identity: p.programRuntime.identity, Runtime: p.programRuntime.runtime}
}

// RuntimeRegistry and RuntimeObjectiveContracts are for the Boatstack mechanism.
// External applications should use Transitions and Summary.
func (p ControlProgram) RuntimeRegistry() catalog.Registry { return p.registry }
func (p ControlProgram) RuntimeObjectiveContracts() catalog.ObjectiveContracts {
	return p.objectiveContracts.Clone()
}

// SupervisoryProgram returns the domain-neutral executable program consumed
// by the kernel. The software manifest fingerprint remains bound into this
// program identity.
func (p ControlProgram) SupervisoryProgram() general.Program {
	return p.supervisoryProgram.Clone()
}

type CompileRequest struct {
	KernelVersion string
	Core          CoreSystemDefinition
	Runtime       ProgramRuntimeDefinition
	Extensions    []Extension
	Settings      any
}

var componentID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)

func Compile(ctx context.Context, request CompileRequest) (ControlProgram, error) {
	if request.KernelVersion == "" || request.Core == nil || request.Runtime == nil {
		return ControlProgram{}, fmt.Errorf("control program requires kernel version, CoreSystem, and exactly one ProgramRuntime")
	}
	core, err := request.Core.CoreManifest(ctx)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("load CoreSystem manifest: %w", err)
	}
	core = cloneCoreManifest(core)
	flow, err := request.Runtime.RuntimeManifest(ctx)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("load ProgramRuntime manifest: %w", err)
	}
	flow = cloneRuntimeManifest(flow)
	if err := validateCore(core); err != nil {
		return ControlProgram{}, err
	}
	if err := validateProgramRuntime(flow); err != nil {
		return ControlProgram{}, err
	}
	coreFingerprint, err := fingerprint(core)
	if err != nil {
		return ControlProgram{}, err
	}
	flowFingerprint, err := fingerprint(flow)
	if err != nil {
		return ControlProgram{}, err
	}
	settingsFingerprint, err := fingerprint(request.Settings)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("fingerprint program settings: %w", err)
	}
	var flowRuntime ProgramRuntime
	if runtimeDefinition, ok := request.Runtime.(RuntimeProgramDefinition); ok {
		flowRuntime = runtimeDefinition.ProgramRuntime()
	}
	if flow.RuntimeMode == ProgramRuntimeProtocol && flowRuntime == nil {
		return ControlProgram{}, fmt.Errorf("ProgramRuntime %q selects protocol runtime without a ProgramRuntime", flow.ID)
	}

	transitions := make([]Transition, 0, len(core.Transitions)+len(flow.Transitions))
	resources := map[string]string{}
	appendComponent := func(items []Transition, origin catalog.TransitionOrigin, declared []Capability, runtimeExecution bool) error {
		capabilities, capabilityErr := catalog.NormalizeCapabilities(origin.ID+".capabilities", declared)
		if capabilityErr != nil || len(capabilities) == 0 {
			if capabilityErr != nil {
				return capabilityErr
			}
			return fmt.Errorf("component %q requires a non-empty capability surface", origin.ID)
		}
		for _, item := range items {
			item = cloneTransition(item)
			item.Origin = origin
			item.Owner = origin.ID
			item.RuntimeExecution = runtimeExecution
			item.DeclaredCapabilities = append([]Capability(nil), capabilities...)
			if item.Controllable() && !item.Policy.ReconcilesProgram && !hasFacet(item.SourceConditions, model.FacetProgram) {
				item.SourceConditions = append(item.SourceConditions, KnownCondition(model.FacetProgram, string(model.ProgramUnbound), string(model.ProgramCurrent)))
			}
			for _, resource := range item.OwnedResources {
				if prior, exists := resources[resource]; exists && prior != origin.ID {
					return fmt.Errorf("resource %q has overlapping owners %q and %q", resource, prior, origin.ID)
				}
				resources[resource] = origin.ID
			}
			transitions = append(transitions, item)
		}
		return nil
	}
	if err := appendComponent(core.Transitions, catalog.TransitionOrigin{Kind: catalog.OriginCoreSystem, ID: core.ID, Version: core.Version, ManifestFingerprint: coreFingerprint}, core.Capabilities, false); err != nil {
		return ControlProgram{}, err
	}
	if err := appendComponent(flow.Transitions, catalog.TransitionOrigin{Kind: catalog.OriginControlProgram, ID: flow.ID, Version: flow.Version, ManifestFingerprint: flowFingerprint}, flow.Capabilities, flow.RuntimeMode == ProgramRuntimeProtocol); err != nil {
		return ControlProgram{}, err
	}

	extensionConditions := map[model.ObjectiveKind][]catalog.FacetCondition{}
	compiledExtensions := make([]compiledExtension, 0, len(request.Extensions))
	extensionIdentities := make([]ComponentIdentity, 0, len(request.Extensions))
	extensionCount := 0
	seenExtensions := map[string]bool{}
	reservedComponents := map[string]bool{core.ID: true, flow.ID: true}
	claimedFacts := map[string]string{}
	for _, fact := range flow.Facts {
		claimedFacts[fact] = flow.ID
	}
	for _, definition := range request.Extensions {
		if definition == nil {
			return ControlProgram{}, fmt.Errorf("nil extension definition")
		}
		manifest, manifestErr := definition.ExtensionManifest(ctx)
		if manifestErr != nil {
			return ControlProgram{}, fmt.Errorf("load extension manifest: %w", manifestErr)
		}
		manifest = cloneExtensionManifest(manifest)
		if err := validateExtension(manifest, seenExtensions, reservedComponents); err != nil {
			return ControlProgram{}, err
		}
		seenExtensions[manifest.ID] = true
		for _, fact := range manifest.Facts {
			if prior := claimedFacts[fact]; prior != "" {
				return ControlProgram{}, fmt.Errorf("fact %q is declared by both %q and %q", fact, prior, manifest.ID)
			}
			claimedFacts[fact] = manifest.ID
		}
		manifestFingerprint, manifestErr := fingerprint(manifest)
		if manifestErr != nil {
			return ControlProgram{}, manifestErr
		}
		identity := ComponentIdentity{ID: manifest.ID, Version: manifest.Version, Fingerprint: manifestFingerprint}
		extensionIdentities = append(extensionIdentities, identity)
		var runtime ExtensionRuntime
		if runtimeDefinition, ok := definition.(RuntimeExtension); ok {
			runtime = runtimeDefinition.Runtime()
		}
		if (len(manifest.Facts) != 0 || len(manifest.Transitions) != 0) && runtime == nil {
			return ControlProgram{}, fmt.Errorf("extension %q declares runtime behavior without an ExtensionRuntime", manifest.ID)
		}
		compiledExtensions = append(compiledExtensions, compiledExtension{manifest: manifest, identity: identity, runtime: runtime})
		for _, constraint := range manifest.ObjectiveConstraints {
			extensionConditions[constraint.ObjectiveKind] = append(extensionConditions[constraint.ObjectiveKind], constraint.Conditions...)
		}
		for _, resource := range manifest.OwnedResources {
			if !strings.HasPrefix(resource, manifest.ID+".") {
				return ControlProgram{}, fmt.Errorf("extension %q resource %q is not namespaced", manifest.ID, resource)
			}
			if prior, exists := resources[resource]; exists && prior != manifest.ID {
				return ControlProgram{}, fmt.Errorf("resource %q has overlapping owners %q and %q", resource, prior, manifest.ID)
			}
			resources[resource] = manifest.ID
		}
		for index := range manifest.Transitions {
			item := cloneTransition(manifest.Transitions[index])
			item.RuntimeExecution = true
			item.DeclaredCapabilities = append([]Capability(nil), manifest.Capabilities...)
			if item.Controllable() && !item.Policy.ReconcilesProgram && !hasFacet(item.SourceConditions, model.FacetProgram) {
				item.SourceConditions = append(item.SourceConditions, KnownCondition(model.FacetProgram, string(model.ProgramUnbound), string(model.ProgramCurrent)))
			}
			if !strings.HasPrefix(string(item.ID), manifest.ID+".") {
				return ControlProgram{}, fmt.Errorf("extension %q transition %q is not namespaced", manifest.ID, item.ID)
			}
			if item.Priority != 0 {
				return ControlProgram{}, fmt.Errorf("extension %q transition %q cannot declare raw priority", manifest.ID, item.ID)
			}
			if item.SelectionClass == "" {
				if item.Class == catalog.EventRecovery {
					item.SelectionClass = catalog.SelectionExtensionRecovery
				} else {
					item.SelectionClass = catalog.SelectionExplicitOnly
				}
			}
			if item.SelectionClass != catalog.SelectionObjectiveRequired && item.SelectionClass != catalog.SelectionExtensionRecovery &&
				item.SelectionClass != catalog.SelectionExplicitOnly && item.SelectionClass != catalog.SelectionObservedExternal {
				return ControlProgram{}, fmt.Errorf("extension %q transition %q uses forbidden selection class %q", manifest.ID, item.ID, item.SelectionClass)
			}
			if item.Class == catalog.EventRecovery && (item.SelectionClass != catalog.SelectionExtensionRecovery || !containsTransition(manifest.RecoveryTransitions, item.ID)) {
				return ControlProgram{}, fmt.Errorf("extension recovery %q requires EXTENSION_RECOVERY selection and an explicit recovery declaration", item.ID)
			}
			if item.SelectionClass == catalog.SelectionExtensionRecovery && item.Class != catalog.EventRecovery {
				return ControlProgram{}, fmt.Errorf("extension transition %q cannot use EXTENSION_RECOVERY selection outside a recovery event", item.ID)
			}
			item.Priority = 1
			item.Origin = catalog.TransitionOrigin{Kind: catalog.OriginExtension, ID: manifest.ID, Version: manifest.Version, ManifestFingerprint: manifestFingerprint}
			item.Owner = manifest.ID
			if !containsString(manifest.Effects, string(item.Effect)) {
				return ControlProgram{}, fmt.Errorf("extension transition %q uses undeclared effect %q", item.ID, item.Effect)
			}
			if !containsString(manifest.Verifiers, item.Verifier) {
				return ControlProgram{}, fmt.Errorf("extension transition %q uses undeclared verifier %q", item.ID, item.Verifier)
			}
			if item.Interruption.Recovery != "" && item.Interruption.Recovery != "recovery.escalate" && !containsTransition(manifest.RecoveryTransitions, item.Interruption.Recovery) {
				return ControlProgram{}, fmt.Errorf("extension transition %q uses undeclared recovery %q", item.ID, item.Interruption.Recovery)
			}
			for _, resource := range item.OwnedResources {
				if owner := resources[resource]; owner != manifest.ID {
					return ControlProgram{}, fmt.Errorf("extension transition %q writes undeclared resource %q", item.ID, resource)
				}
			}
			transitions = append(transitions, item)
			extensionCount++
		}
	}
	if err := validateDependencies(compiledExtensions); err != nil {
		return ControlProgram{}, err
	}
	for objective, conditions := range extensionConditions {
		sort.SliceStable(conditions, func(i, j int) bool {
			left, _ := json.Marshal(conditions[i])
			right, _ := json.Marshal(conditions[j])
			return string(left) < string(right)
		})
		extensionConditions[objective] = conditions
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("compile transition registry: %w", err)
	}
	contracts, err := catalog.NewObjectiveContracts(flow.ObjectiveContracts, extensionConditions)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("compile objective contracts: %w", err)
	}
	sort.Slice(extensionIdentities, func(i, j int) bool { return extensionIdentities[i].ID < extensionIdentities[j].ID })
	programIdentity := struct {
		SchemaVersion       int
		KernelVersion       string
		Core                ComponentIdentity
		Runtime             ComponentIdentity
		Extensions          []ComponentIdentity
		SettingsFingerprint string
		Transitions         []Transition
		ObjectiveContracts  []catalog.ObjectiveContract
		Resources           map[string]string
	}{
		ProgramSchemaVersion, request.KernelVersion,
		ComponentIdentity{ID: core.ID, Version: core.Version, Fingerprint: coreFingerprint},
		ComponentIdentity{ID: flow.ID, Version: flow.Version, Fingerprint: flowFingerprint},
		extensionIdentities, settingsFingerprint, registry.All(), contracts.All(), resources,
	}
	domainContractFingerprint, err := fingerprint(programIdentity)
	if err != nil {
		return ControlProgram{}, err
	}
	supervisoryProgram, err := compileSupervisoryProgram(flow, request.KernelVersion, domainContractFingerprint, registry.All())
	if err != nil {
		return ControlProgram{}, fmt.Errorf("compile supervisory program: %w", err)
	}
	summary := ProgramSummary{
		SchemaVersion: ProgramSchemaVersion, KernelVersion: request.KernelVersion,
		ProgramID: flow.ID, ProgramVersion: flow.Version,
		Core: programIdentity.Core, Runtime: programIdentity.Runtime, Extensions: extensionIdentities,
		CoreTransitionCount: len(core.Transitions), RuntimeTransitionCount: len(flow.Transitions),
		ExtensionTransitionCount: extensionCount, TotalTransitionCount: registry.Len(), ProgramFingerprint: supervisoryProgram.Fingerprint,
	}
	return ControlProgram{
		summary: summary, supervisoryProgram: supervisoryProgram, registry: registry, objectiveContracts: contracts, resourceOwnership: resources,
		settingsFingerprint: settingsFingerprint, extensions: compiledExtensions,
		programRuntime: compiledProgramRuntime{manifest: cloneRuntimeManifest(flow), identity: programIdentity.Runtime, runtime: flowRuntime},
	}, nil
}

func compileSupervisoryProgram(runtime ProgramRuntimeManifest, compatibility, domainContractFingerprint string, transitions []Transition) (general.Program, error) {
	recoveredBy := make(map[TransitionID][]string)
	for _, transition := range transitions {
		if transition.Controllable() && transition.Interruption.Recovery != "" {
			recoveredBy[transition.Interruption.Recovery] = append(recoveredBy[transition.Interruption.Recovery], string(transition.ID))
		}
	}
	projected := make([]general.Transition, 0, len(transitions))
	for _, transition := range transitions {
		if !transition.Controllable() {
			continue
		}
		capabilities := make([]general.Capability, 0, len(transition.RequiredCapabilities)+1)
		for _, capability := range transition.RequiredCapabilities {
			capabilities = append(capabilities, general.Capability(capability))
		}
		facets := append([]string(nil), transition.OwnedResources...)
		mutation := general.PreserveObjective
		if transition.Policy.BindsRequestedObjective {
			mutation = general.BindObjectiveMutation
			capabilities = append(capabilities, general.Capability("objective.bind"))
			facets = append(facets, "supervisor.objective")
		}
		projected = append(projected, general.Transition{
			ID: string(transition.ID), SourceModes: []string{"software-delivery"}, TargetMode: "software-delivery",
			ObjectiveScope: transition.Policy.ObjectiveScope, ObjectiveMutation: mutation,
			RequiredCapabilities: capabilities, OwnedFacets: facets,
			Operation: string(transition.ID), Priority: transition.SelectionClass.Rank()*1000 + transition.Priority,
			Recovers: recoveredBy[transition.ID],
		})
	}
	return general.CompileDomainProgram(runtime.ID, runtime.Version, compatibility, domainContractFingerprint, "software-delivery", []string{"software-delivery-marked"}, projected)
}

func validateCore(manifest CoreSystemManifest) error {
	if !componentID.MatchString(manifest.ID) || manifest.Version == "" || len(manifest.Transitions) == 0 || len(manifest.Capabilities) == 0 {
		return fmt.Errorf("CoreSystem requires semantic id, version, and transitions")
	}
	if _, err := catalog.NormalizeCapabilities("CoreSystem "+manifest.ID+" capabilities", manifest.Capabilities); err != nil {
		return err
	}
	return nil
}

func validateProgramRuntime(manifest ProgramRuntimeManifest) error {
	if !componentID.MatchString(manifest.ID) || manifest.Version == "" || manifest.ProtocolVersion != ProgramRuntimeProtocolVersion ||
		(manifest.RuntimeMode != ProgramRuntimeNative && manifest.RuntimeMode != ProgramRuntimeProtocol) ||
		len(manifest.Transitions) == 0 || len(manifest.SupportedObjectives) == 0 ||
		len(manifest.Capabilities) == 0 ||
		!validJSONObject(manifest.ConfigurationSchema) ||
		manifest.PrivacyClassification == "" || manifest.TelemetryClassification == "" {
		return fmt.Errorf("ProgramRuntime requires semantic id, version, configuration schema, objectives, and transitions")
	}
	declaredCapabilities, err := catalog.NormalizeCapabilities("ProgramRuntime "+manifest.ID+" capabilities", manifest.Capabilities)
	if err != nil {
		return err
	}
	if err := validateDeclaredSchema(manifest.ConfigurationSchema, manifest.Settings, "ProgramRuntime "+manifest.ID+" configuration"); err != nil {
		return err
	}
	supported := map[ObjectiveKind]bool{}
	for _, objective := range manifest.SupportedObjectives {
		if !objective.Valid() || supported[objective] {
			return fmt.Errorf("ProgramRuntime has invalid or duplicate objective %q", objective)
		}
		supported[objective] = true
	}
	for _, contract := range manifest.ObjectiveContracts {
		if !supported[contract.ObjectiveKind] {
			return fmt.Errorf("ProgramRuntime objective contract %q is not supported", contract.ObjectiveKind)
		}
		delete(supported, contract.ObjectiveKind)
	}
	if len(supported) != 0 {
		return fmt.Errorf("ProgramRuntime does not define every supported objective contract")
	}
	for _, values := range [][]string{manifest.Facts, manifest.OwnedResources, manifest.Effects, manifest.Verifiers} {
		if duplicate := duplicateString(values); duplicate != "" {
			return fmt.Errorf("ProgramRuntime %q duplicates declaration %q", manifest.ID, duplicate)
		}
	}
	if manifest.RuntimeMode == ProgramRuntimeProtocol {
		for _, value := range append(append(append([]string(nil), manifest.Facts...), manifest.OwnedResources...), append(manifest.Effects, manifest.Verifiers...)...) {
			if !strings.HasPrefix(value, manifest.ID+".") {
				return fmt.Errorf("protocol ProgramRuntime %q declaration %q is not namespaced", manifest.ID, value)
			}
		}
	}
	resources := stringSet(manifest.OwnedResources)
	facts := stringSet(manifest.Facts)
	effects := stringSet(manifest.Effects)
	verifiers := stringSet(manifest.Verifiers)
	recoveryDeclarations := transitionSet(manifest.RecoveryTransitions)
	if len(recoveryDeclarations) != len(manifest.RecoveryTransitions) {
		return fmt.Errorf("ProgramRuntime %q duplicates a recovery declaration", manifest.ID)
	}
	transitions := make(map[TransitionID]Transition, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		transition.RuntimeExecution = manifest.RuntimeMode == ProgramRuntimeProtocol
		transition.DeclaredCapabilities = declaredCapabilities
		if transition.Controllable() && len(transition.RequiredCapabilities) == 0 {
			return fmt.Errorf("ProgramRuntime transition %q requires explicit capabilities", transition.ID)
		}
		if missing := catalog.MissingCapability(catalog.RequiredCapabilities(transition), catalog.NewCapabilitySet(declaredCapabilities...)); missing != "" {
			return fmt.Errorf("ProgramRuntime transition %q: CAPABILITY_NOT_DECLARED %q", transition.ID, missing)
		}
		transitions[transition.ID] = transition
		if manifest.RuntimeMode == ProgramRuntimeProtocol && !strings.HasPrefix(string(transition.ID), manifest.ID+".") {
			return fmt.Errorf("protocol ProgramRuntime %q transition %q is not namespaced", manifest.ID, transition.ID)
		}
		if transition.Controllable() {
			if !effects[string(transition.Effect)] || !verifiers[transition.Verifier] {
				return fmt.Errorf("ProgramRuntime transition %q uses an undeclared effect or verifier", transition.ID)
			}
			for _, resource := range transition.OwnedResources {
				if !resources[resource] {
					return fmt.Errorf("ProgramRuntime transition %q writes undeclared resource %q", transition.ID, resource)
				}
				if manifest.RuntimeMode == ProgramRuntimeProtocol && !strings.HasPrefix(resource, manifest.ID+".") {
					return fmt.Errorf("protocol ProgramRuntime %q resource %q is not namespaced", manifest.ID, resource)
				}
			}
			if manifest.RuntimeMode == ProgramRuntimeProtocol {
				for _, condition := range transition.TargetConditions {
					if !strings.HasPrefix(string(condition.Facet), manifest.ID+".") {
						return fmt.Errorf("protocol ProgramRuntime transition %q targets non-owned fact %q", transition.ID, condition.Facet)
					}
					if !facts[string(condition.Facet)] {
						return fmt.Errorf("protocol ProgramRuntime transition %q targets undeclared fact %q", transition.ID, condition.Facet)
					}
				}
			}
		}
	}
	for recovery := range recoveryDeclarations {
		transition, ok := transitions[recovery]
		if !ok || transition.Class != EventRecovery {
			return fmt.Errorf("ProgramRuntime %q recovery %q is not a declared recovery transition", manifest.ID, recovery)
		}
	}
	return nil
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = true
		}
	}
	return result
}

func transitionSet(values []TransitionID) map[TransitionID]bool {
	result := make(map[TransitionID]bool, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = true
		}
	}
	return result
}

func validateExtension(manifest ExtensionManifest, seen, reserved map[string]bool) error {
	if !componentID.MatchString(manifest.ID) || manifest.Version == "" || manifest.ProtocolVersion != ExtensionProtocolVersion || seen[manifest.ID] || reserved[manifest.ID] {
		return fmt.Errorf("extension requires unique semantic id, version, and protocol version")
	}
	if manifest.PrivacyClassification == "" || manifest.TelemetryClassification == "" {
		return fmt.Errorf("extension %q requires privacy and telemetry classifications", manifest.ID)
	}
	declaredCapabilities, err := catalog.NormalizeCapabilities("extension "+manifest.ID+" capabilities", manifest.Capabilities)
	if err != nil || ((len(manifest.Facts) != 0 || len(manifest.Transitions) != 0) && len(declaredCapabilities) == 0) {
		if err != nil {
			return err
		}
		return fmt.Errorf("extension %q runtime behavior requires a non-empty capability surface", manifest.ID)
	}
	if (len(manifest.Facts) != 0 || len(manifest.Transitions) != 0) && !catalog.NewCapabilitySet(declaredCapabilities...)[catalog.CapabilityCommandExecute] {
		return fmt.Errorf("extension %q runtime behavior requires command.execute", manifest.ID)
	}
	if !validJSONObject(manifest.SettingsSchema) {
		return fmt.Errorf("extension %q requires a JSON-object settings schema", manifest.ID)
	}
	if err := validateDeclaredSchema(manifest.SettingsSchema, manifest.Settings, "extension "+manifest.ID+" settings"); err != nil {
		return err
	}
	if manifest.ExecutableSHA256 != "" {
		if len(manifest.ExecutableSHA256) != 64 {
			return fmt.Errorf("extension %q executable SHA-256 is invalid", manifest.ID)
		}
		if _, err := hex.DecodeString(manifest.ExecutableSHA256); err != nil {
			return fmt.Errorf("extension %q executable SHA-256 is invalid", manifest.ID)
		}
	}
	for _, fact := range manifest.Facts {
		if !strings.HasPrefix(fact, manifest.ID+".") {
			return fmt.Errorf("extension %q fact %q is not namespaced", manifest.ID, fact)
		}
	}
	declaredFacts := stringSet(manifest.Facts)
	if duplicate := duplicateString(manifest.Facts); duplicate != "" {
		return fmt.Errorf("extension %q duplicates fact %q", manifest.ID, duplicate)
	}
	for _, effect := range manifest.Effects {
		if !strings.HasPrefix(effect, manifest.ID+".") {
			return fmt.Errorf("extension %q effect %q is not namespaced", manifest.ID, effect)
		}
	}
	if duplicate := duplicateString(manifest.Effects); duplicate != "" {
		return fmt.Errorf("extension %q duplicates effect %q", manifest.ID, duplicate)
	}
	for _, verifier := range manifest.Verifiers {
		if !strings.HasPrefix(verifier, manifest.ID+".") {
			return fmt.Errorf("extension %q verifier %q is not namespaced", manifest.ID, verifier)
		}
	}
	if duplicate := duplicateString(manifest.Verifiers); duplicate != "" {
		return fmt.Errorf("extension %q duplicates verifier %q", manifest.ID, duplicate)
	}
	if duplicate := duplicateString(manifest.OwnedResources); duplicate != "" {
		return fmt.Errorf("extension %q duplicates resource %q", manifest.ID, duplicate)
	}
	if duplicate := duplicateString(manifest.Dependencies); duplicate != "" {
		return fmt.Errorf("extension %q duplicates dependency %q", manifest.ID, duplicate)
	}
	for _, dependency := range manifest.Dependencies {
		if dependency == manifest.ID {
			return fmt.Errorf("extension %q cannot depend on itself", manifest.ID)
		}
	}
	constrainedFacets := map[ObjectiveKind]map[FacetName][]FacetCondition{}
	for _, constraint := range manifest.ObjectiveConstraints {
		if !constraint.ObjectiveKind.Valid() || len(constraint.Conditions) == 0 {
			return fmt.Errorf("extension %q has invalid objective constraint", manifest.ID)
		}
		for _, condition := range constraint.Conditions {
			if !condition.Facet.Valid() || len(condition.Statuses) == 0 || condition.Facet == model.FacetTerminal {
				return fmt.Errorf("extension %q has invalid or terminal-reporting objective condition", manifest.ID)
			}
			for _, status := range condition.Statuses {
				if !status.Valid() {
					return fmt.Errorf("extension %q has invalid objective-condition status %q", manifest.ID, status)
				}
			}
			if constrainedFacets[constraint.ObjectiveKind] == nil {
				constrainedFacets[constraint.ObjectiveKind] = map[FacetName][]FacetCondition{}
			}
			constrainedFacets[constraint.ObjectiveKind][condition.Facet] = append(constrainedFacets[constraint.ObjectiveKind][condition.Facet], condition)
		}
	}
	declaredRecovery := map[TransitionID]bool{}
	for _, recovery := range manifest.RecoveryTransitions {
		if declaredRecovery[recovery] {
			return fmt.Errorf("extension %q duplicates recovery %q", manifest.ID, recovery)
		}
		declaredRecovery[recovery] = true
	}
	seenTransitions := make(map[TransitionID]bool, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		transition.RuntimeExecution = true
		transition.DeclaredCapabilities = declaredCapabilities
		if transition.Controllable() && len(transition.RequiredCapabilities) == 0 {
			return fmt.Errorf("extension transition %q requires explicit capabilities", transition.ID)
		}
		if missing := catalog.MissingCapability(catalog.RequiredCapabilities(transition), catalog.NewCapabilitySet(declaredCapabilities...)); missing != "" {
			return fmt.Errorf("extension transition %q: CAPABILITY_NOT_DECLARED %q", transition.ID, missing)
		}
		seenTransitions[transition.ID] = true
		for _, condition := range transition.TargetConditions {
			if !strings.HasPrefix(string(condition.Facet), manifest.ID+".") {
				return fmt.Errorf("extension transition %q targets non-owned fact %q", transition.ID, condition.Facet)
			}
			if !declaredFacts[string(condition.Facet)] {
				return fmt.Errorf("extension transition %q targets undeclared fact %q", transition.ID, condition.Facet)
			}
		}
		if transition.SelectionClass == SelectionObjectiveRequired {
			if len(transition.ObjectiveKinds) == 0 {
				return fmt.Errorf("extension transition %q is implicitly selectable without an explicit constrained objective", transition.ID)
			}
			for _, objective := range transition.ObjectiveKinds {
				discharges := false
				for _, target := range transition.TargetConditions {
					for _, obligation := range constrainedFacets[objective][target.Facet] {
						discharges = discharges || conditionImplies(target, obligation)
					}
				}
				if !discharges {
					return fmt.Errorf("extension transition %q is implicitly selectable for objective %q without discharging an active obligation", transition.ID, objective)
				}
			}
		}
		if declaredRecovery[transition.ID] && transition.Class != EventRecovery {
			return fmt.Errorf("extension recovery %q is not a recovery transition", transition.ID)
		}
	}
	for recovery := range declaredRecovery {
		if !seenTransitions[recovery] {
			return fmt.Errorf("extension %q recovery %q has no declared transition", manifest.ID, recovery)
		}
	}
	return nil
}

type rejectingSchemaLoader struct{}

func (rejectingSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external JSON Schema reference %q is not permitted", url)
}

func validateDeclaredSchema(schemaRaw, instanceRaw json.RawMessage, label string) error {
	decode := func(raw json.RawMessage, fallback string) (any, error) {
		if len(raw) == 0 {
			raw = json.RawMessage(fallback)
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("contains trailing JSON")
		}
		return value, nil
	}
	schemaValue, err := decode(schemaRaw, `{}`)
	if err != nil {
		return fmt.Errorf("%s schema is invalid JSON: %w", label, err)
	}
	instance, err := decode(instanceRaw, `{}`)
	if err != nil {
		return fmt.Errorf("%s value is invalid JSON: %w", label, err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(rejectingSchemaLoader{})
	const location = "urn:boatstack:component-schema"
	if err := compiler.AddResource(location, schemaValue); err != nil {
		return fmt.Errorf("%s schema could not be loaded: %w", label, err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return fmt.Errorf("%s schema is invalid: %w", label, err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("%s does not satisfy its declared schema: %w", label, err)
	}
	return nil
}

func conditionImplies(target, obligation FacetCondition) bool {
	if target.Facet != obligation.Facet {
		return false
	}
	for _, status := range target.Statuses {
		found := false
		for _, allowed := range obligation.Statuses {
			found = found || status == allowed
		}
		if !found {
			return false
		}
	}
	if len(obligation.Values) == 0 {
		return true
	}
	if len(target.Values) == 0 {
		return false
	}
	for _, value := range target.Values {
		found := false
		for _, allowed := range obligation.Values {
			found = found || value == allowed
		}
		if !found {
			return false
		}
	}
	return true
}

func duplicateString(values []string) string {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return value
		}
		seen[value] = true
	}
	return ""
}

func hasFacet(conditions []FacetCondition, facet FacetName) bool {
	for _, condition := range conditions {
		if condition.Facet == facet {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsTransition(values []TransitionID, wanted TransitionID) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateDependencies(extensions []compiledExtension) error {
	known := map[string]bool{}
	for _, extension := range extensions {
		known[extension.manifest.ID] = true
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	byID := map[string]ExtensionManifest{}
	for _, extension := range extensions {
		byID[extension.manifest.ID] = extension.manifest
	}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("extension dependency cycle includes %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range byID[id].Dependencies {
			if !known[dependency] {
				return fmt.Errorf("extension %q depends on unavailable extension %q", id, dependency)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[id], visited[id] = false, true
		return nil
	}
	for id := range known {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func cloneTransition(value Transition) Transition {
	value.SourcePhases = append([]model.ProtocolPhase(nil), value.SourcePhases...)
	value.TargetPhases = append([]model.ProtocolPhase(nil), value.TargetPhases...)
	value.ObjectiveKinds = append([]model.ObjectiveKind(nil), value.ObjectiveKinds...)
	value.RequiredIdentity = append([]string(nil), value.RequiredIdentity...)
	value.Authority = append([]catalog.AuthorityClass(nil), value.Authority...)
	value.AuthorityAll = append([]catalog.AuthorityClass(nil), value.AuthorityAll...)
	value.RequiredCapabilities = append([]Capability(nil), value.RequiredCapabilities...)
	value.DeclaredCapabilities = append([]Capability(nil), value.DeclaredCapabilities...)
	value.RequiredEvidence = append([]string(nil), value.RequiredEvidence...)
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.LocalEffects = append([]catalog.EffectID(nil), value.LocalEffects...)
	value.ExternalEffects = append([]catalog.EffectID(nil), value.ExternalEffects...)
	value.Parameters = append([]catalog.ParameterSpec(nil), value.Parameters...)
	value.SourceConditions = cloneConditions(value.SourceConditions)
	value.TargetConditions = cloneConditions(value.TargetConditions)
	value.Interruption.Points = append([]string(nil), value.Interruption.Points...)
	value.Interruption.PartialState = append([]string(nil), value.Interruption.PartialState...)
	value.Policy.ManagedOperations = append([]string(nil), value.Policy.ManagedOperations...)
	return value
}

func cloneConditions(values []catalog.FacetCondition) []catalog.FacetCondition {
	result := make([]catalog.FacetCondition, len(values))
	for index, value := range values {
		value.Statuses = append([]model.FactStatus(nil), value.Statuses...)
		value.Values = append([]string(nil), value.Values...)
		result[index] = value
	}
	return result
}

func cloneCoreManifest(value CoreSystemManifest) CoreSystemManifest {
	value.Capabilities = append([]Capability(nil), value.Capabilities...)
	value.Transitions = cloneTransitions(value.Transitions)
	return value
}

func cloneRuntimeManifest(value ProgramRuntimeManifest) ProgramRuntimeManifest {
	value.SupportedObjectives = append([]ObjectiveKind(nil), value.SupportedObjectives...)
	value.ObjectiveContracts = append([]ObjectiveContract(nil), value.ObjectiveContracts...)
	for index := range value.ObjectiveContracts {
		value.ObjectiveContracts[index].Conditions = cloneConditions(value.ObjectiveContracts[index].Conditions)
	}
	value.Transitions = cloneTransitions(value.Transitions)
	value.Facts = append([]string(nil), value.Facts...)
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.Effects = append([]string(nil), value.Effects...)
	value.Verifiers = append([]string(nil), value.Verifiers...)
	value.Capabilities = append([]Capability(nil), value.Capabilities...)
	value.RecoveryTransitions = append([]TransitionID(nil), value.RecoveryTransitions...)
	value.Settings = append(json.RawMessage(nil), value.Settings...)
	value.ConfigurationSchema = append(json.RawMessage(nil), value.ConfigurationSchema...)
	return value
}

func cloneExtensionManifest(value ExtensionManifest) ExtensionManifest {
	value.Settings = append(json.RawMessage(nil), value.Settings...)
	value.SettingsSchema = append(json.RawMessage(nil), value.SettingsSchema...)
	value.Facts = append([]string(nil), value.Facts...)
	value.Transitions = cloneTransitions(value.Transitions)
	value.ObjectiveConstraints = append([]ObjectiveConstraint(nil), value.ObjectiveConstraints...)
	for index := range value.ObjectiveConstraints {
		value.ObjectiveConstraints[index].Conditions = cloneConditions(value.ObjectiveConstraints[index].Conditions)
	}
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.Effects = append([]string(nil), value.Effects...)
	value.Verifiers = append([]string(nil), value.Verifiers...)
	value.Capabilities = append([]Capability(nil), value.Capabilities...)
	value.RecoveryTransitions = append([]TransitionID(nil), value.RecoveryTransitions...)
	value.Dependencies = append([]string(nil), value.Dependencies...)
	return value
}

func validJSONObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded == nil {
		return false
	}
	var trailing any
	return decoder.Decode(&trailing) == io.EOF
}

func cloneTransitions(values []Transition) []Transition {
	result := make([]Transition, len(values))
	for index, value := range values {
		result[index] = cloneTransition(value)
	}
	return result
}

func fingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode control identity: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
