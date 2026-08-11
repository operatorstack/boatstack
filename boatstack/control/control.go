// Package control defines the stable authoring and compilation contracts for
// Boatstack control programs. It contains no default flow or product surface.
package control

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

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Transition = catalog.Transition
type TransitionID = catalog.TransitionID
type EventClass = catalog.EventClass
type AuthorityClass = catalog.AuthorityClass
type FacetCondition = catalog.FacetCondition
type SelectionClass = catalog.SelectionClass
type GoalContract = catalog.GoalContract
type GoalScope = catalog.GoalScope
type EffectID = catalog.EffectID
type Prescription = catalog.Prescription
type ParameterSpec = catalog.ParameterSpec
type InterruptionContract = catalog.InterruptionContract
type Reversibility = catalog.Reversibility
type GoalKind = model.GoalKind
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

	SelectionSystemRecovery    = catalog.SelectionSystemRecovery
	SelectionFlowRecovery      = catalog.SelectionFlowRecovery
	SelectionExtensionRecovery = catalog.SelectionExtensionRecovery
	SelectionGoalRequired      = catalog.SelectionGoalRequired
	SelectionFlowProgress      = catalog.SelectionFlowProgress
	SelectionExplicitOnly      = catalog.SelectionExplicitOnly
	SelectionObservedExternal  = catalog.SelectionObservedExternal

	GoalScopeOptionalPreserve = catalog.GoalScopeOptionalPreserve

	GoalApprovedPlan = model.GoalApprovedPlan
	GoalVerified     = model.GoalVerified
	GoalOpenPR       = model.GoalOpenPR
	GoalMerged       = model.GoalMerged
	GoalAbandoned    = model.GoalAbandoned

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
	FacetGoal                = model.FacetGoal
)

const ProgramSchemaVersion = 1

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

// FlowDefinition supplies exactly one trusted in-process primary delivery law.
type FlowDefinition interface {
	FlowManifest(context.Context) (PrimaryFlowManifest, error)
}

// Extension supplies an additive manifest. Runtime behavior is optional for a
// declaration-only extension and is assigned by the assembling application.
type Extension interface {
	ExtensionManifest(context.Context) (ExtensionManifest, error)
}

type CoreSystemManifest struct {
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	Transitions []Transition `json:"transitions"`
}

type PrimaryFlowManifest struct {
	ID                      string          `json:"id"`
	Version                 string          `json:"version"`
	ProtocolVersion         int             `json:"protocol_version"`
	RuntimeMode             FlowRuntimeMode `json:"runtime_mode"`
	SupportedGoals          []GoalKind      `json:"supported_goals"`
	GoalContracts           []GoalContract  `json:"goal_contracts"`
	Transitions             []Transition    `json:"transitions"`
	Facts                   []string        `json:"facts,omitempty"`
	OwnedResources          []string        `json:"owned_resources"`
	Effects                 []string        `json:"effects"`
	Verifiers               []string        `json:"verifiers"`
	RecoveryTransitions     []TransitionID  `json:"recovery_transitions"`
	Settings                json.RawMessage `json:"settings,omitempty"`
	ConfigurationSchema     json.RawMessage `json:"configuration_schema,omitempty"`
	PrivacyClassification   string          `json:"privacy_classification"`
	TelemetryClassification string          `json:"telemetry_classification"`
}

type GoalConstraint struct {
	GoalKind   GoalKind         `json:"goal_kind"`
	Conditions []FacetCondition `json:"conditions"`
}

type ExtensionManifest struct {
	ID                      string           `json:"id"`
	Version                 string           `json:"version"`
	ProtocolVersion         int              `json:"protocol_version"`
	ExecutableSHA256        string           `json:"executable_sha256,omitempty"`
	Settings                json.RawMessage  `json:"settings,omitempty"`
	SettingsSchema          json.RawMessage  `json:"settings_schema"`
	Facts                   []string         `json:"facts,omitempty"`
	Transitions             []Transition     `json:"transitions,omitempty"`
	GoalConstraints         []GoalConstraint `json:"goal_constraints,omitempty"`
	OwnedResources          []string         `json:"owned_resources,omitempty"`
	Effects                 []string         `json:"effects,omitempty"`
	Verifiers               []string         `json:"verifiers,omitempty"`
	RecoveryTransitions     []TransitionID   `json:"recovery_transitions,omitempty"`
	PrivacyClassification   string           `json:"privacy_classification"`
	TelemetryClassification string           `json:"telemetry_classification"`
	Dependencies            []string         `json:"dependencies,omitempty"`
}

type ComponentIdentity struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

type ProgramSummary struct {
	SchemaVersion            int                 `json:"schema_version"`
	KernelVersion            string              `json:"kernel_version"`
	Core                     ComponentIdentity   `json:"core"`
	Flow                     ComponentIdentity   `json:"flow"`
	Extensions               []ComponentIdentity `json:"extensions,omitempty"`
	CoreTransitionCount      int                 `json:"core_transition_count"`
	FlowTransitionCount      int                 `json:"flow_transition_count"`
	ExtensionTransitionCount int                 `json:"extension_transition_count"`
	TotalTransitionCount     int                 `json:"total_transition_count"`
	ProgramFingerprint       string              `json:"program_fingerprint"`
}

// ControlProgram is immutable after Compile. Its accessors always return
// copies; the runtime registry remains the one executable graph.
type ControlProgram struct {
	summary             ProgramSummary
	registry            catalog.Registry
	goalContracts       catalog.GoalContracts
	resourceOwnership   map[string]string
	settingsFingerprint string
	extensions          []compiledExtension
	flow                compiledFlow
}

type compiledFlow struct {
	manifest PrimaryFlowManifest
	identity ComponentIdentity
	runtime  FlowRuntime
}

type CompiledFlow struct {
	Manifest PrimaryFlowManifest
	Identity ComponentIdentity
	Runtime  FlowRuntime
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

func (p ControlProgram) Flow() CompiledFlow {
	return CompiledFlow{Manifest: cloneFlowManifest(p.flow.manifest), Identity: p.flow.identity, Runtime: p.flow.runtime}
}

// RuntimeRegistry and RuntimeGoalContracts are for the Boatstack mechanism.
// External applications should use Transitions and Summary.
func (p ControlProgram) RuntimeRegistry() catalog.Registry           { return p.registry }
func (p ControlProgram) RuntimeGoalContracts() catalog.GoalContracts { return p.goalContracts.Clone() }

type CompileRequest struct {
	KernelVersion string
	Core          CoreSystemDefinition
	Flow          FlowDefinition
	Extensions    []Extension
	Settings      any
}

var componentID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)

func Compile(ctx context.Context, request CompileRequest) (ControlProgram, error) {
	if request.KernelVersion == "" || request.Core == nil || request.Flow == nil {
		return ControlProgram{}, fmt.Errorf("control program requires kernel version, CoreSystem, and exactly one PrimaryFlow")
	}
	core, err := request.Core.CoreManifest(ctx)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("load CoreSystem manifest: %w", err)
	}
	core = cloneCoreManifest(core)
	flow, err := request.Flow.FlowManifest(ctx)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("load PrimaryFlow manifest: %w", err)
	}
	flow = cloneFlowManifest(flow)
	if err := validateCore(core); err != nil {
		return ControlProgram{}, err
	}
	if err := validateFlow(flow); err != nil {
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
	var flowRuntime FlowRuntime
	if runtimeDefinition, ok := request.Flow.(RuntimeFlowDefinition); ok {
		flowRuntime = runtimeDefinition.FlowRuntime()
	}
	if flow.RuntimeMode == FlowRuntimeProtocol && flowRuntime == nil {
		return ControlProgram{}, fmt.Errorf("PrimaryFlow %q selects protocol runtime without a FlowRuntime", flow.ID)
	}

	transitions := make([]Transition, 0, len(core.Transitions)+len(flow.Transitions))
	resources := map[string]string{}
	appendComponent := func(items []Transition, origin catalog.TransitionOrigin) error {
		for _, item := range items {
			item = cloneTransition(item)
			item.Origin = origin
			item.Owner = origin.ID
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
	if err := appendComponent(core.Transitions, catalog.TransitionOrigin{Kind: catalog.OriginCoreSystem, ID: core.ID, Version: core.Version, ManifestFingerprint: coreFingerprint}); err != nil {
		return ControlProgram{}, err
	}
	if err := appendComponent(flow.Transitions, catalog.TransitionOrigin{Kind: catalog.OriginPrimaryFlow, ID: flow.ID, Version: flow.Version, ManifestFingerprint: flowFingerprint}); err != nil {
		return ControlProgram{}, err
	}

	extensionConditions := map[model.GoalKind][]catalog.FacetCondition{}
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
		for _, constraint := range manifest.GoalConstraints {
			extensionConditions[constraint.GoalKind] = append(extensionConditions[constraint.GoalKind], constraint.Conditions...)
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
			if item.SelectionClass != catalog.SelectionGoalRequired && item.SelectionClass != catalog.SelectionExtensionRecovery &&
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
	for goal, conditions := range extensionConditions {
		sort.SliceStable(conditions, func(i, j int) bool {
			left, _ := json.Marshal(conditions[i])
			right, _ := json.Marshal(conditions[j])
			return string(left) < string(right)
		})
		extensionConditions[goal] = conditions
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("compile transition registry: %w", err)
	}
	contracts, err := catalog.NewGoalContracts(flow.GoalContracts, extensionConditions)
	if err != nil {
		return ControlProgram{}, fmt.Errorf("compile goal contracts: %w", err)
	}
	sort.Slice(extensionIdentities, func(i, j int) bool { return extensionIdentities[i].ID < extensionIdentities[j].ID })
	programIdentity := struct {
		SchemaVersion       int
		KernelVersion       string
		Core                ComponentIdentity
		Flow                ComponentIdentity
		Extensions          []ComponentIdentity
		SettingsFingerprint string
		Transitions         []Transition
		GoalContracts       []catalog.GoalContract
		Resources           map[string]string
	}{
		ProgramSchemaVersion, request.KernelVersion,
		ComponentIdentity{ID: core.ID, Version: core.Version, Fingerprint: coreFingerprint},
		ComponentIdentity{ID: flow.ID, Version: flow.Version, Fingerprint: flowFingerprint},
		extensionIdentities, settingsFingerprint, registry.All(), contracts.All(), resources,
	}
	programFingerprint, err := fingerprint(programIdentity)
	if err != nil {
		return ControlProgram{}, err
	}
	summary := ProgramSummary{
		SchemaVersion: ProgramSchemaVersion, KernelVersion: request.KernelVersion,
		Core: programIdentity.Core, Flow: programIdentity.Flow, Extensions: extensionIdentities,
		CoreTransitionCount: len(core.Transitions), FlowTransitionCount: len(flow.Transitions),
		ExtensionTransitionCount: extensionCount, TotalTransitionCount: registry.Len(), ProgramFingerprint: programFingerprint,
	}
	return ControlProgram{
		summary: summary, registry: registry, goalContracts: contracts, resourceOwnership: resources,
		settingsFingerprint: settingsFingerprint, extensions: compiledExtensions,
		flow: compiledFlow{manifest: cloneFlowManifest(flow), identity: programIdentity.Flow, runtime: flowRuntime},
	}, nil
}

func validateCore(manifest CoreSystemManifest) error {
	if !componentID.MatchString(manifest.ID) || manifest.Version == "" || len(manifest.Transitions) == 0 {
		return fmt.Errorf("CoreSystem requires semantic id, version, and transitions")
	}
	return nil
}

func validateFlow(manifest PrimaryFlowManifest) error {
	if !componentID.MatchString(manifest.ID) || manifest.Version == "" || manifest.ProtocolVersion != FlowProtocolVersion ||
		(manifest.RuntimeMode != FlowRuntimeNative && manifest.RuntimeMode != FlowRuntimeProtocol) ||
		len(manifest.Transitions) == 0 || len(manifest.SupportedGoals) == 0 ||
		!validJSONObject(manifest.ConfigurationSchema) ||
		manifest.PrivacyClassification == "" || manifest.TelemetryClassification == "" {
		return fmt.Errorf("PrimaryFlow requires semantic id, version, configuration schema, goals, and transitions")
	}
	if err := validateDeclaredSchema(manifest.ConfigurationSchema, manifest.Settings, "PrimaryFlow "+manifest.ID+" configuration"); err != nil {
		return err
	}
	supported := map[GoalKind]bool{}
	for _, goal := range manifest.SupportedGoals {
		if !goal.Valid() || supported[goal] {
			return fmt.Errorf("PrimaryFlow has invalid or duplicate goal %q", goal)
		}
		supported[goal] = true
	}
	for _, contract := range manifest.GoalContracts {
		if !supported[contract.GoalKind] {
			return fmt.Errorf("PrimaryFlow goal contract %q is not supported", contract.GoalKind)
		}
		delete(supported, contract.GoalKind)
	}
	if len(supported) != 0 {
		return fmt.Errorf("PrimaryFlow does not define every supported goal contract")
	}
	for _, values := range [][]string{manifest.Facts, manifest.OwnedResources, manifest.Effects, manifest.Verifiers} {
		if duplicate := duplicateString(values); duplicate != "" {
			return fmt.Errorf("PrimaryFlow %q duplicates declaration %q", manifest.ID, duplicate)
		}
	}
	if manifest.RuntimeMode == FlowRuntimeProtocol {
		for _, value := range append(append(append([]string(nil), manifest.Facts...), manifest.OwnedResources...), append(manifest.Effects, manifest.Verifiers...)...) {
			if !strings.HasPrefix(value, manifest.ID+".") {
				return fmt.Errorf("protocol PrimaryFlow %q declaration %q is not namespaced", manifest.ID, value)
			}
		}
	}
	resources := stringSet(manifest.OwnedResources)
	facts := stringSet(manifest.Facts)
	effects := stringSet(manifest.Effects)
	verifiers := stringSet(manifest.Verifiers)
	recoveryDeclarations := transitionSet(manifest.RecoveryTransitions)
	if len(recoveryDeclarations) != len(manifest.RecoveryTransitions) {
		return fmt.Errorf("PrimaryFlow %q duplicates a recovery declaration", manifest.ID)
	}
	transitions := make(map[TransitionID]Transition, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		transitions[transition.ID] = transition
		if manifest.RuntimeMode == FlowRuntimeProtocol && !strings.HasPrefix(string(transition.ID), manifest.ID+".") {
			return fmt.Errorf("protocol PrimaryFlow %q transition %q is not namespaced", manifest.ID, transition.ID)
		}
		if transition.Controllable() {
			if !effects[string(transition.Effect)] || !verifiers[transition.Verifier] {
				return fmt.Errorf("PrimaryFlow transition %q uses an undeclared effect or verifier", transition.ID)
			}
			for _, resource := range transition.OwnedResources {
				if !resources[resource] {
					return fmt.Errorf("PrimaryFlow transition %q writes undeclared resource %q", transition.ID, resource)
				}
				if manifest.RuntimeMode == FlowRuntimeProtocol && !strings.HasPrefix(resource, manifest.ID+".") {
					return fmt.Errorf("protocol PrimaryFlow %q resource %q is not namespaced", manifest.ID, resource)
				}
			}
			if manifest.RuntimeMode == FlowRuntimeProtocol {
				for _, condition := range transition.TargetConditions {
					if !strings.HasPrefix(string(condition.Facet), manifest.ID+".") {
						return fmt.Errorf("protocol PrimaryFlow transition %q targets non-owned fact %q", transition.ID, condition.Facet)
					}
					if !facts[string(condition.Facet)] {
						return fmt.Errorf("protocol PrimaryFlow transition %q targets undeclared fact %q", transition.ID, condition.Facet)
					}
				}
			}
		}
	}
	for recovery := range recoveryDeclarations {
		transition, ok := transitions[recovery]
		if !ok || transition.Class != EventRecovery {
			return fmt.Errorf("PrimaryFlow %q recovery %q is not a declared recovery transition", manifest.ID, recovery)
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
	constrainedFacets := map[GoalKind]map[FacetName][]FacetCondition{}
	for _, constraint := range manifest.GoalConstraints {
		if !constraint.GoalKind.Valid() || len(constraint.Conditions) == 0 {
			return fmt.Errorf("extension %q has invalid goal constraint", manifest.ID)
		}
		for _, condition := range constraint.Conditions {
			if !condition.Facet.Valid() || len(condition.Statuses) == 0 || condition.Facet == model.FacetTerminal {
				return fmt.Errorf("extension %q has invalid or terminal-reporting goal condition", manifest.ID)
			}
			for _, status := range condition.Statuses {
				if !status.Valid() {
					return fmt.Errorf("extension %q has invalid goal-condition status %q", manifest.ID, status)
				}
			}
			if constrainedFacets[constraint.GoalKind] == nil {
				constrainedFacets[constraint.GoalKind] = map[FacetName][]FacetCondition{}
			}
			constrainedFacets[constraint.GoalKind][condition.Facet] = append(constrainedFacets[constraint.GoalKind][condition.Facet], condition)
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
		seenTransitions[transition.ID] = true
		for _, condition := range transition.TargetConditions {
			if !strings.HasPrefix(string(condition.Facet), manifest.ID+".") {
				return fmt.Errorf("extension transition %q targets non-owned fact %q", transition.ID, condition.Facet)
			}
			if !declaredFacts[string(condition.Facet)] {
				return fmt.Errorf("extension transition %q targets undeclared fact %q", transition.ID, condition.Facet)
			}
		}
		if transition.SelectionClass == SelectionGoalRequired {
			if len(transition.GoalKinds) == 0 {
				return fmt.Errorf("extension transition %q is implicitly selectable without an explicit constrained goal", transition.ID)
			}
			for _, goal := range transition.GoalKinds {
				discharges := false
				for _, target := range transition.TargetConditions {
					for _, obligation := range constrainedFacets[goal][target.Facet] {
						discharges = discharges || conditionImplies(target, obligation)
					}
				}
				if !discharges {
					return fmt.Errorf("extension transition %q is implicitly selectable for goal %q without discharging an active obligation", transition.ID, goal)
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
	value.GoalKinds = append([]model.GoalKind(nil), value.GoalKinds...)
	value.RequiredIdentity = append([]string(nil), value.RequiredIdentity...)
	value.Authority = append([]catalog.AuthorityClass(nil), value.Authority...)
	value.AuthorityAll = append([]catalog.AuthorityClass(nil), value.AuthorityAll...)
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
	value.Transitions = cloneTransitions(value.Transitions)
	return value
}

func cloneFlowManifest(value PrimaryFlowManifest) PrimaryFlowManifest {
	value.SupportedGoals = append([]GoalKind(nil), value.SupportedGoals...)
	value.GoalContracts = append([]GoalContract(nil), value.GoalContracts...)
	for index := range value.GoalContracts {
		value.GoalContracts[index].Conditions = cloneConditions(value.GoalContracts[index].Conditions)
	}
	value.Transitions = cloneTransitions(value.Transitions)
	value.Facts = append([]string(nil), value.Facts...)
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.Effects = append([]string(nil), value.Effects...)
	value.Verifiers = append([]string(nil), value.Verifiers...)
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
	value.GoalConstraints = append([]GoalConstraint(nil), value.GoalConstraints...)
	for index := range value.GoalConstraints {
		value.GoalConstraints[index].Conditions = cloneConditions(value.GoalConstraints[index].Conditions)
	}
	value.OwnedResources = append([]string(nil), value.OwnedResources...)
	value.Effects = append([]string(nil), value.Effects...)
	value.Verifiers = append([]string(nil), value.Verifiers...)
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
