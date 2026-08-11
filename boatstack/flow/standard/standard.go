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

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const (
	ID      = "boatstack.standard"
	Version = "1.0.0"
)

type definition struct{}

//go:embed transitions.json
var transitionDeclarations []byte

func Definition() control.FlowDefinition { return definition{} }

func (definition) FlowManifest(context.Context) (control.PrimaryFlowManifest, error) {
	transitions, err := decodeTransitions()
	if err != nil {
		return control.PrimaryFlowManifest{}, err
	}
	resources, effects, verifiers, recoveries := declarations(transitions)
	return control.PrimaryFlowManifest{
		ID: ID, Version: Version, ProtocolVersion: control.FlowProtocolVersion, RuntimeMode: control.FlowRuntimeNative,
		SupportedGoals: []control.GoalKind{
			model.GoalApprovedPlan, model.GoalVerified, model.GoalOpenPR,
			model.GoalMerged, model.GoalAbandoned,
		},
		GoalContracts: []control.GoalContract{
			contract(model.GoalApprovedPlan,
				known(model.FacetPlan, string(model.PlanApproved))),
			contract(model.GoalVerified,
				known(model.FacetVerification, string(model.VerificationCurrent)),
				known(model.FacetConfiguration, string(model.ConfigurationVerified)),
				known(model.FacetRuntime, string(model.RuntimeVerified)),
				known(model.FacetDelivery, string(model.DeliveryTerminal))),
			contract(model.GoalOpenPR,
				known(model.FacetVerification, string(model.VerificationCurrent)),
				known(model.FacetConfiguration, string(model.ConfigurationVerified)),
				known(model.FacetRuntime, string(model.RuntimeVerified)),
				known(model.FacetPublication, string(model.PublicationOpen))),
			contract(model.GoalMerged,
				known(model.FacetPublication, string(model.PublicationMerged)),
				known(model.FacetDelivery, string(model.DeliveryTerminal)),
				known(model.FacetWorkspace, string(model.WorkspaceLanded), string(model.WorkspaceAbsent))),
			contract(model.GoalAbandoned,
				known(model.FacetDelivery, string(model.DeliveryDiscarded)),
				known(model.FacetWorkspace, string(model.WorkspaceAbandoned), string(model.WorkspaceAbsent))),
		},
		Facts:       []string{"plan", "workspace", "delivery", "verification", "publication"},
		Transitions: transitions, OwnedResources: resources, Effects: effects, Verifiers: verifiers, RecoveryTransitions: recoveries,
		Settings: json.RawMessage(`{}`), ConfigurationSchema: json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

func decodeTransitions() ([]control.Transition, error) {
	decoder := json.NewDecoder(bytes.NewReader(transitionDeclarations))
	decoder.DisallowUnknownFields()
	var transitions []control.Transition
	if err := decoder.Decode(&transitions); err != nil {
		return nil, fmt.Errorf("decode StandardFlow transitions: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("StandardFlow transition declarations contain trailing JSON")
	}
	return transitions, nil
}

func declarations(transitions []control.Transition) ([]string, []string, []string, []control.TransitionID) {
	var resources, effects, verifiers []string
	var recoveries []control.TransitionID
	seenResources, seenEffects, seenVerifiers, seenRecoveries := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[control.TransitionID]bool{}
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
		if transition.Class == control.EventRecovery && !seenRecoveries[transition.ID] {
			seenRecoveries[transition.ID], recoveries = true, append(recoveries, transition.ID)
		}
	}
	return resources, effects, verifiers, recoveries
}

func contract(goal model.GoalKind, conditions ...control.FacetCondition) control.GoalContract {
	return control.GoalContract{GoalKind: goal, Conditions: conditions}
}

func known(facet model.FacetName, values ...string) control.FacetCondition {
	return control.FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: values}
}
