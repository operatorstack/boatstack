package engine

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

func TestMaintenanceReplayBindsDurableGoalState(t *testing.T) {
	// control-law: maintenance-replay-preserves-verified-product-goal-state
	configured := model.Goal{ID: "configured", Kind: model.GoalOpenPR, DeliveryID: "delivery"}
	commandGoal := model.Goal{ID: "command", Kind: model.GoalApprovedPlan, DeliveryID: "other"}
	request := ApplyRequest{ResolveRequest: ResolveRequest{Goal: commandGoal}, FlowID: "flow"}

	tests := []struct {
		name    string
		receipt protocol.TransitionReceipt
		fact    model.Fact[model.Goal]
		wantErr bool
	}{
		{name: "absent survives command goal and retry", receipt: protocol.TransitionReceipt{GoalScope: catalog.GoalScopeOptionalPreserve, GoalStatus: model.FactAbsent}, fact: model.Fact[model.Goal]{Status: model.FactAbsent}},
		{name: "known survives conflicting command goal and retry", receipt: protocol.TransitionReceipt{GoalScope: catalog.GoalScopeOptionalPreserve, GoalStatus: model.FactKnown, GoalID: configured.ID, GoalKind: configured.Kind, DeliveryID: configured.DeliveryID}, fact: model.Fact[model.Goal]{Status: model.FactKnown, Value: configured}},
		{name: "absent cannot replay after product goal appears", receipt: protocol.TransitionReceipt{GoalScope: catalog.GoalScopeOptionalPreserve, GoalStatus: model.FactAbsent}, fact: model.Fact[model.Goal]{Status: model.FactKnown, Value: configured}, wantErr: true},
		{name: "known cannot replay after product goal changes", receipt: protocol.TransitionReceipt{GoalScope: catalog.GoalScopeOptionalPreserve, GoalStatus: model.FactKnown, GoalID: configured.ID, GoalKind: configured.Kind, DeliveryID: configured.DeliveryID}, fact: model.Fact[model.Goal]{Status: model.FactKnown, Value: commandGoal}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.receipt.FlowID = request.FlowID
			test.receipt.ProgramFingerprint = syntheticProgramFingerprint
			if err := validateReplayRequest(test.receipt, request, syntheticProgramFingerprint); err != nil {
				t.Fatalf("command goal affected maintenance replay identity: %v", err)
			}
			err := validateReplayGoalState(test.receipt, model.Snapshot{Observation: model.Observation{Goal: test.fact}})
			if test.wantErr && err == nil {
				t.Fatal("changed durable product-goal state was accepted for replay")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unchanged durable product-goal state rejected: %v", err)
			}
		})
	}
}
