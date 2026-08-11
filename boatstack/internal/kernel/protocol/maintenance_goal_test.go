package protocol

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func TestMaintenanceGoalBindingUsesOnlyDurableProductState(t *testing.T) {
	// control-law: maintenance-admission-is-independent-from-command-product-intent
	transition := catalog.Transition{ID: "installation.update", Policy: catalog.PolicyContract{GoalScope: catalog.GoalScopeOptionalPreserve}}
	configured := model.Goal{ID: "configured", Kind: model.GoalOpenPR, DeliveryID: "delivery"}
	conflicting := model.Goal{ID: "command", Kind: model.GoalApprovedPlan, DeliveryID: "other"}

	tests := []struct {
		name     string
		fact     model.Fact[model.Goal]
		request  model.Goal
		want     model.Goal
		wantFail bool
	}{
		{name: "goal absent", fact: model.Fact[model.Goal]{Status: model.FactAbsent}, request: conflicting},
		{name: "goal known and preserved", fact: model.Fact[model.Goal]{Status: model.FactKnown, Value: configured}, want: configured},
		{name: "conflicting command-scoped goal ignored", fact: model.Fact[model.Goal]{Status: model.FactKnown, Value: configured}, request: conflicting, want: configured},
		{name: "unknown goal fails closed", fact: model.Fact[model.Goal]{Status: model.FactUnknown}, request: conflicting, wantFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := GoalForTransition(model.Snapshot{Observation: model.Observation{Goal: test.fact}}, test.request, transition)
			if test.wantFail {
				if err == nil {
					t.Fatalf("unknown product-goal evidence produced %#v", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("goal binding = %#v, %v; want %#v", got, err, test.want)
			}
		})
	}
}
