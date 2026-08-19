package effects

import (
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestParkedSourceStateClearsWorkPackageLineageBeforeNewObjective(t *testing.T) {
	// control-law: workspace-cut-parks-source-without-transferred-delivery-lineage
	now := time.Unix(3_000, 0).UTC()
	priorObjective := model.Objective{ID: "objective-a", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "delivery-a"}
	state := durable.State{
		Revision:                       8,
		Phase:                          model.PhaseActive,
		Engagement:                     model.EngagementActive,
		Delivery:                       model.DeliveryApproved,
		Workspace:                      model.WorkspaceCut,
		WorkPackage:                    model.WorkPackageApproved,
		Plan:                           model.PlanApproved,
		Objective:                      priorObjective,
		PlanFingerprint:                "plan-a",
		WorkPackageFingerprint:         "package-a",
		WorkPackageApprovalFingerprint: "approval-a",
	}

	parked := parkedSourceState(state, 9, "workspace.cut", now)
	if parked.WorkPackage != model.WorkPackageAbsent || parked.WorkPackageFingerprint != "" || parked.WorkPackageApprovalFingerprint != "" {
		t.Fatalf("parked source retained work-package lineage: %#v", parked)
	}
	if state.WorkPackage != model.WorkPackageApproved || state.WorkPackageFingerprint != "package-a" || state.WorkPackageApprovalFingerprint != "approval-a" {
		t.Fatalf("source parking mutated the transferred destination candidate: %#v", state)
	}

	nextObjective := model.Objective{ID: "objective-b", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "delivery-b"}
	admission := protocol.Admission{
		Objective: nextObjective,
		Parameters: protocol.Parameters{
			{Name: "target_id", Value: string(nextObjective.TargetID)},
			{Name: "delivery_id", Value: nextObjective.DeliveryID},
		},
	}
	if err := applyObjectiveBind(&parked, admission, catalog.Transition{}); err != nil {
		t.Fatalf("bind a different objective after source parking: %v", err)
	}
	if parked.Objective != nextObjective || parked.Phase != model.PhaseObserved || parked.Engagement != model.EngagementDormant || parked.WorkPackage != model.WorkPackageAbsent {
		t.Fatalf("parked source did not return to a clean engagement-begin state: %#v", parked)
	}
}
