package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestActiveFlowIdentityComesFromObjectiveBindingReceipt(t *testing.T) {
	objective := model.Objective{ID: "objective-product-delivery-run-one", Kind: model.ObjectiveOpenPR, DeliveryID: "one"}
	binding := protocol.TransitionReceipt{ID: "binding", FlowID: "run-original", TransitionID: "objective.bind", ObjectiveID: objective.ID, ObjectiveKind: objective.Kind, DeliveryID: objective.DeliveryID, ResultingStateRevision: 4}
	maintenance := protocol.TransitionReceipt{ID: "maintenance", FlowID: "run-maintenance", TransitionID: "installation.reconcile-update", ObjectiveID: objective.ID, ObjectiveKind: objective.Kind, DeliveryID: objective.DeliveryID, ResultingStateRevision: 5}
	if !matchesObjectiveBinding(binding, objective, 5) {
		t.Fatal("objective binding receipt was not recognized")
	}
	if matchesObjectiveBinding(maintenance, objective, 5) {
		t.Fatal("maintenance receipt replaced the active Flow identity")
	}
	if matchesObjectiveBinding(binding, objective, 3) {
		t.Fatal("future objective binding receipt was accepted")
	}
}
