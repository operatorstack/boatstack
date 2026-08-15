package engine

import (
	"testing"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestMaintenanceReplayAcceptsCurrentCommittedControlBundleTarget(t *testing.T) {
	before, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": []byte("before")})
	if err != nil {
		t.Fatal(err)
	}
	after, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": []byte("after")})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := boatstackruntime.NewControlBundleContract(before, &after, "")
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := boatstackruntime.NewControlBundleContract(after, &after, "")
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{FlowID: "flow"}
	receipt := protocol.TransitionReceipt{
		FlowID: "flow", Program: syntheticProgram,
		ControlBundleSourceFingerprint: committed.Source.Fingerprint,
		ControlBundleTargetFingerprint: committed.Target.Fingerprint,
	}
	if err := validateReplayRequest(receipt, request, syntheticProgramFingerprint, &rebuilt); err != nil {
		t.Fatalf("current committed bundle could not replay prior source-to-target receipt: %v", err)
	}
	other, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": []byte("other")})
	if err != nil {
		t.Fatal(err)
	}
	drifted, err := boatstackruntime.NewControlBundleContract(other, &other, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReplayRequest(receipt, request, syntheticProgramFingerprint, &drifted); err == nil {
		t.Fatal("unrelated bundle replay was accepted")
	}
}

func TestMaintenanceReplayBindsDurableObjectiveState(t *testing.T) {
	// control-law: maintenance-replay-preserves-verified-objective-state
	configured := model.Objective{ID: "configured", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	commandObjective := model.Objective{ID: "command", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "other"}
	request := ApplyRequest{ResolveRequest: ResolveRequest{Objective: commandObjective}, FlowID: "flow"}

	tests := []struct {
		name    string
		receipt protocol.TransitionReceipt
		fact    model.Fact[model.Objective]
		wantErr bool
	}{
		{name: "absent survives command objective and retry", receipt: protocol.TransitionReceipt{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve, ObjectiveStatus: model.FactAbsent}, fact: model.Fact[model.Objective]{Status: model.FactAbsent}},
		{name: "known survives conflicting command objective and retry", receipt: protocol.TransitionReceipt{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve, ObjectiveStatus: model.FactKnown, ObjectiveID: configured.ID, TargetID: configured.TargetID, DeliveryID: configured.DeliveryID}, fact: model.Fact[model.Objective]{Status: model.FactKnown, Value: configured}},
		{name: "absent cannot replay after product objective appears", receipt: protocol.TransitionReceipt{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve, ObjectiveStatus: model.FactAbsent}, fact: model.Fact[model.Objective]{Status: model.FactKnown, Value: configured}, wantErr: true},
		{name: "known cannot replay after product objective changes", receipt: protocol.TransitionReceipt{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve, ObjectiveStatus: model.FactKnown, ObjectiveID: configured.ID, TargetID: configured.TargetID, DeliveryID: configured.DeliveryID}, fact: model.Fact[model.Objective]{Status: model.FactKnown, Value: commandObjective}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.receipt.FlowID = request.FlowID
			test.receipt.Program = syntheticProgram
			if err := validateReplayRequest(test.receipt, request, syntheticProgramFingerprint, nil); err != nil {
				t.Fatalf("command objective affected maintenance replay identity: %v", err)
			}
			err := validateReplayObjectiveState(test.receipt, model.Snapshot{Observation: model.Observation{Objective: test.fact}})
			if test.wantErr && err == nil {
				t.Fatal("changed durable objective state was accepted for replay")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unchanged durable objective state rejected: %v", err)
			}
		})
	}
}
