package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestActiveFlowIdentityComesFromObjectiveBindingReceipt(t *testing.T) {
	objective := model.Objective{ID: "objective-product-delivery-run-one", TargetID: "published-pr", TrustedClass: model.ObjectiveOpenPR, DeliveryID: "one"}
	binding := protocol.TransitionReceipt{ID: "binding", FlowID: "run-original", TransitionID: "objective.bind", ObjectiveID: objective.ID, TargetID: objective.TargetID, TrustedClass: objective.TrustedClass, DeliveryID: objective.DeliveryID, ResultingStateRevision: 4}
	maintenance := protocol.TransitionReceipt{ID: "maintenance", FlowID: "run-maintenance", TransitionID: "installation.reconcile-update", ObjectiveID: objective.ID, TargetID: objective.TargetID, TrustedClass: objective.TrustedClass, DeliveryID: objective.DeliveryID, ResultingStateRevision: 5}
	if !matchesObjectiveBinding(binding, objective, 5) {
		t.Fatal("objective binding receipt was not recognized")
	}
	if matchesObjectiveBinding(maintenance, objective, 5) {
		t.Fatal("maintenance receipt replaced the active Flow identity")
	}
	if matchesObjectiveBinding(binding, objective, 3) {
		t.Fatal("future objective binding receipt was accepted")
	}
	wrongClass := binding
	wrongClass.TrustedClass = model.ObjectiveVerified
	if matchesObjectiveBinding(wrongClass, objective, 5) {
		t.Fatal("receipt from another trusted target class was accepted")
	}
}

func TestActiveFlowIdentityRequiresExactWorktreeLineage(t *testing.T) {
	current := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree-a", ControllerID: "controller"}
	otherWorktree := current
	otherWorktree.WorktreeID = "worktree-b"
	otherController := current
	otherController.ControllerID = "other-controller"
	if !sameStateLineage(current, current) {
		t.Fatal("exact worktree lineage was rejected")
	}
	if sameStateLineage(otherWorktree, current) {
		t.Fatal("linked worktree receipt was accepted for the current durable state")
	}
	if sameStateLineage(otherController, current) {
		t.Fatal("different controller lineage was accepted for the current durable state")
	}
}

func TestActiveFlowIdentityFollowsCommittedWorkspaceTransfer(t *testing.T) {
	objective := model.Objective{ID: "objective-product-delivery-run-one", TargetID: "published-pr", TrustedClass: model.ObjectiveOpenPR, DeliveryID: "one"}
	source := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "source", Ref: "detached:base", ControllerID: "controller"}
	destination := source
	destination.WorktreeID = "destination"
	destination.Ref = "refs/heads/feature"

	binding := protocol.TransitionReceipt{
		ID: "binding", FlowID: "run-original", Sequence: 1, TransitionID: "objective.bind",
		ObjectiveID: objective.ID, TargetID: objective.TargetID, TrustedClass: objective.TrustedClass, DeliveryID: objective.DeliveryID,
		ResultingStateRevision: 4,
	}
	cut := protocol.TransitionReceipt{
		ID: "cut", FlowID: "run-original", Sequence: 2, TransitionID: "workspace.cut",
		ExecutionContext: "advance", PriorInvocation: &source, ResultingInvocation: &destination,
	}
	records := []journalRecord{
		{Admission: protocol.Admission{Invocation: source}, Receipt: &binding},
		{Admission: protocol.Admission{Invocation: source}, Receipt: &cut},
	}

	found, ok, err := findLatestCommittedFlowForObjective(records, destination, objective, 16)
	if err != nil || !ok || found.FlowID != "run-original" {
		t.Fatalf("transferred active Flow identity = %#v, %t, %v", found, ok, err)
	}

	unchained := destination
	unchained.WorktreeID = "unchained"
	if found, ok, err := findLatestCommittedFlowForObjective(records, unchained, objective, 16); err != nil || ok {
		t.Fatalf("unchained worktree inherited Flow identity = %#v, %t, %v", found, ok, err)
	}

	otherController := destination
	otherController.ControllerID = "other-controller"
	if found, ok, err := findLatestCommittedFlowForObjective(records, otherController, objective, 16); err != nil || ok {
		t.Fatalf("other controller inherited Flow identity = %#v, %t, %v", found, ok, err)
	}
}
