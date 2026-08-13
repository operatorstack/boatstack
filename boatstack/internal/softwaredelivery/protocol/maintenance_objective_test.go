package protocol

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestMaintenanceObjectiveBindingUsesOnlyDurableProductState(t *testing.T) {
	// control-law: maintenance-admission-is-independent-from-command-product-intent
	transition := catalog.Transition{ID: "installation.update", Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve}}
	configured := model.Objective{ID: "configured", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	conflicting := model.Objective{ID: "command", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "other"}

	tests := []struct {
		name     string
		fact     model.Fact[model.Objective]
		request  model.Objective
		want     model.Objective
		wantFail bool
	}{
		{name: "objective absent", fact: model.Fact[model.Objective]{Status: model.FactAbsent}, request: conflicting},
		{name: "objective known and preserved", fact: model.Fact[model.Objective]{Status: model.FactKnown, Value: configured}, want: configured},
		{name: "conflicting command-scoped objective ignored", fact: model.Fact[model.Objective]{Status: model.FactKnown, Value: configured}, request: conflicting, want: configured},
		{name: "unknown objective fails closed", fact: model.Fact[model.Objective]{Status: model.FactUnknown}, request: conflicting, wantFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ObjectiveForTransition(model.Snapshot{Observation: model.Observation{Objective: test.fact}}, test.request, transition)
			if test.wantFail {
				if err == nil {
					t.Fatalf("unknown objective evidence produced %#v", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("objective binding = %#v, %v; want %#v", got, err, test.want)
			}
		})
	}
}
