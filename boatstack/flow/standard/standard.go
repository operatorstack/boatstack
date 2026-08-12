// Package standard owns Boatstack's first-party opinionated software-delivery
// flow. It has no CLI, SDK, host-rendering, journal, or receipt authority.
package standard

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const (
	ID      = "boatstack.standard"
	Version = "1.0.0"
)

type definition struct{}

//go:embed transitions.json
var transitionDeclarations []byte

func Definition() delivery.ProgramRuntimeDefinition { return definition{} }

func (definition) RuntimeManifest(context.Context) (delivery.ProgramRuntimeManifest, error) {
	transitions, err := decodeTransitions()
	if err != nil {
		return delivery.ProgramRuntimeManifest{}, err
	}
	resources, effects, verifiers, recoveries := declarations(transitions)
	capabilities := []delivery.Capability{delivery.CapabilityHumanApprove}
	for index := range transitions {
		transitions[index].RequiredCapabilities = delivery.KernelEffectCapabilities(transitions[index])
		capabilities = delivery.UnionCapabilities(capabilities, transitions[index].RequiredCapabilities)
	}
	return delivery.ProgramRuntimeManifest{
		ID: ID, Version: Version, ProtocolVersion: delivery.ProgramRuntimeProtocolVersion, RuntimeMode: delivery.ProgramRuntimeNative,
		SupportedObjectives: []delivery.ObjectiveKind{
			model.ObjectiveApprovedPlan, model.ObjectiveVerified, model.ObjectiveOpenPR,
			model.ObjectiveMerged, model.ObjectiveAbandoned,
		},
		ObjectiveContracts: []delivery.ObjectiveContract{
			contract(model.ObjectiveApprovedPlan,
				known(model.FacetPlan, string(model.PlanApproved))),
			contract(model.ObjectiveVerified,
				known(model.FacetVerification, string(model.VerificationCurrent)),
				known(model.FacetConfiguration, string(model.ConfigurationVerified)),
				known(model.FacetRuntime, string(model.RuntimeVerified)),
				known(model.FacetDelivery, string(model.DeliveryTerminal))),
			contract(model.ObjectiveOpenPR,
				known(model.FacetVerification, string(model.VerificationCurrent)),
				known(model.FacetConfiguration, string(model.ConfigurationVerified)),
				known(model.FacetRuntime, string(model.RuntimeVerified)),
				known(model.FacetPublication, string(model.PublicationOpen))),
			contract(model.ObjectiveMerged,
				known(model.FacetPublication, string(model.PublicationMerged)),
				known(model.FacetDelivery, string(model.DeliveryTerminal)),
				known(model.FacetWorkspace, string(model.WorkspaceLanded), string(model.WorkspaceAbsent))),
			contract(model.ObjectiveAbandoned,
				known(model.FacetDelivery, string(model.DeliveryDiscarded)),
				known(model.FacetWorkspace, string(model.WorkspaceAbandoned), string(model.WorkspaceAbsent))),
		},
		Facts:       []string{"plan", "workspace", "delivery", "verification", "publication"},
		Transitions: transitions, OwnedResources: resources, Effects: effects, Verifiers: verifiers, Capabilities: capabilities, RecoveryTransitions: recoveries,
		Settings: json.RawMessage(`{}`), ConfigurationSchema: json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

func decodeTransitions() ([]delivery.Transition, error) {
	decoder := json.NewDecoder(bytes.NewReader(transitionDeclarations))
	decoder.DisallowUnknownFields()
	var transitions []delivery.Transition
	if err := decoder.Decode(&transitions); err != nil {
		return nil, fmt.Errorf("decode StandardFlow transitions: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("StandardFlow transition declarations contain trailing JSON")
	}
	return transitions, nil
}

func declarations(transitions []delivery.Transition) ([]string, []string, []string, []delivery.TransitionID) {
	var resources, effects, verifiers []string
	var recoveries []delivery.TransitionID
	seenResources, seenEffects, seenVerifiers, seenRecoveries := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[delivery.TransitionID]bool{}
	for _, transition := range transitions {
		for _, resource := range transition.OwnedResources {
			if !seenResources[resource] {
				seenResources[resource], resources = true, append(resources, resource)
			}
		}
		if transition.Effect != "" && !seenEffects[string(transition.Effect)] {
			seenEffects[string(transition.Effect)], effects = true, append(effects, string(transition.Effect))
		}
		if transition.Verifier != "" && !seenVerifiers[transition.Verifier] {
			seenVerifiers[transition.Verifier], verifiers = true, append(verifiers, transition.Verifier)
		}
		if transition.Class == delivery.EventRecovery && !seenRecoveries[transition.ID] {
			seenRecoveries[transition.ID], recoveries = true, append(recoveries, transition.ID)
		}
	}
	return resources, effects, verifiers, recoveries
}

func contract(objective model.ObjectiveKind, conditions ...delivery.FacetCondition) delivery.ObjectiveContract {
	return delivery.ObjectiveContract{ObjectiveKind: objective, Conditions: conditions}
}

func known(facet model.FacetName, values ...string) delivery.FacetCondition {
	return delivery.FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: values}
}
