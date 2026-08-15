package catalog

import (
	"fmt"
	"slices"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

type nativeStateHandlerContract struct {
	componentIDs            []string
	effects                 []EffectID
	ownedFacets             []model.StateFacet
	objectiveScopes         []ObjectiveScope
	bindsRequestedObjective bool
}

var nativeStateHandlerContracts = map[string]nativeStateHandlerContract{
	"runtime-verified-settled":       coreNative([]EffectID{"runtime.hydrate", "runtime.replace"}, []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}, ObjectiveScopeOptionalPreserve),
	"runtime-reconcile":              coreNative([]EffectID{"runtime.reconcile"}, []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}, ObjectiveScopeOptionalPreserve),
	"configuration-verified-settled": coreNative([]EffectID{"configuration.initialize", "configuration.mutate"}, []model.StateFacet{model.StateFacetControl}, ObjectiveScopeOptionalPreserve),
	"configuration-reconcile":        coreNative([]EffectID{"configuration.reconcile"}, []model.StateFacet{model.StateFacetControl}, ObjectiveScopeOptionalPreserve),
	"installation-initialize":        coreNative([]EffectID{"installation.initialize"}, []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation, model.StateFacetProgram}, ObjectiveScopeOptionalPreserve),
	"installation-reconcile-update":  coreNative([]EffectID{"installation.reconcile-update"}, []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation, model.StateFacetProgram}, ObjectiveScopeOptionalPreserve),
	"catalog-reconcile":              coreNative([]EffectID{"catalog.reconcile"}, []model.StateFacet{model.StateFacetControl, model.StateFacetProgram}, ObjectiveScopeOptionalPreserve),
	"objective-bind": {
		componentIDs: []string{"boatstack.core"}, effects: []EffectID{"objective.bind"},
		ownedFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}, objectiveScopes: []ObjectiveScope{ObjectiveScopeNone},
		bindsRequestedObjective: true,
	},
	"plan-approve":             standardNative([]EffectID{"plan.approve"}, ObjectiveScopeBoundExact),
	"planning-package-admit":   standardNative([]EffectID{"planning.package.admit"}, ObjectiveScopeBoundExact),
	"planning-package-approve": standardNative([]EffectID{"planning.package.approve"}, ObjectiveScopeBoundExact),
	"planning-package-promote": standardNative([]EffectID{"planning.package.promote"}, ObjectiveScopeBoundExact),
	"abandon-delivery":         standardNative([]EffectID{"plan.abandon", "publication.abandon"}, ObjectiveScopeBoundExact),
	"workspace-cleanup":        standardNative([]EffectID{"workspace.cleanup"}, ObjectiveScopeBoundExact),
	"workspace-reap":           standardNative([]EffectID{"workspace.reap"}, ObjectiveScopeBoundExact),
	"workspace-reconcile":      standardNative([]EffectID{"workspace.reconcile"}, ObjectiveScopeOptionalPreserve),
	"gate-build-record":        standardNative([]EffectID{"gate.build.record"}, ObjectiveScopeBoundExact),
	"gate-test-record":         standardNative([]EffectID{"gate.test.record"}, ObjectiveScopeBoundExact),
	"gate-review-record":       standardNative([]EffectID{"gate.review.record"}, ObjectiveScopeBoundExact),
	"gate-change-record":       standardNative([]EffectID{"gate.change.record"}, ObjectiveScopeBoundExact),
	"gate-journey-record":      standardNative([]EffectID{"gate.journey.record"}, ObjectiveScopeBoundExact),
	"visual-evidence-attach":   standardNative([]EffectID{"evidence.visual.attach"}, ObjectiveScopeBoundExact),
	"publication-observe": {
		componentIDs: []string{"boatstack.standard"}, effects: []EffectID{"publication.observe", "publication.reconcile"},
		ownedFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}, objectiveScopes: []ObjectiveScope{ObjectiveScopeBoundExact, ObjectiveScopeOptionalPreserve},
	},
}

func coreNative(effects []EffectID, facets []model.StateFacet, scope ObjectiveScope) nativeStateHandlerContract {
	return nativeStateHandlerContract{componentIDs: []string{"boatstack.core"}, effects: effects, ownedFacets: facets, objectiveScopes: []ObjectiveScope{scope}}
}

func standardNative(effects []EffectID, scope ObjectiveScope) nativeStateHandlerContract {
	return nativeStateHandlerContract{
		componentIDs: []string{"boatstack.standard"}, effects: effects,
		ownedFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}, objectiveScopes: []ObjectiveScope{scope},
	}
}

func validateNativeStateHandler(t Transition) error {
	contract, ok := nativeStateHandlerContracts[t.StateEffect.NativeHandler]
	if !ok {
		return fmt.Errorf("%s: native state handler %q is not registered", t.ID, t.StateEffect.NativeHandler)
	}
	if !slices.Contains(contract.componentIDs, t.Origin.ID) {
		return fmt.Errorf("%s: component %q cannot invoke native state handler %q", t.ID, t.Origin.ID, t.StateEffect.NativeHandler)
	}
	if !slices.Contains(contract.effects, t.Effect) {
		return fmt.Errorf("%s: native state handler %q is incompatible with effect %q", t.ID, t.StateEffect.NativeHandler, t.Effect)
	}
	writes, err := model.NormalizeStateFacets(string(t.ID)+".owned_facets", t.OwnedFacets)
	if err != nil {
		return err
	}
	expected, err := model.NormalizeStateFacets(t.StateEffect.NativeHandler+".owned_facets", contract.ownedFacets)
	if err != nil {
		return err
	}
	if !slices.Equal(writes, expected) {
		return fmt.Errorf("%s: native state handler %q requires owned facets %v", t.ID, t.StateEffect.NativeHandler, expected)
	}
	if !slices.Contains(contract.objectiveScopes, t.Policy.ObjectiveScope) || t.Policy.BindsRequestedObjective != contract.bindsRequestedObjective {
		return fmt.Errorf("%s: native state handler %q has incompatible objective policy", t.ID, t.StateEffect.NativeHandler)
	}
	return nil
}
