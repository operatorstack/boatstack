package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
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

func TestTransitionOutputComesFromLatestCommittedReceiptInFlowLineage(t *testing.T) {
	source := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "source", Ref: "refs/heads/feature", ControllerID: "controller"}
	destination := source
	destination.WorktreeID, destination.Ref = "destination", "refs/heads/feature-two"
	execute := protocol.TransitionReceipt{
		ID: "execute", FlowID: "run-one", Sequence: 7, TransitionID: "publication.execute", ResultingStateRevision: 9,
		EffectOutputs: protocol.Parameters{{Name: "publication_id", Value: "222"}},
	}
	cut := protocol.TransitionReceipt{
		ID: "cut", FlowID: "run-one", Sequence: 8, TransitionID: "workspace.cut", ResultingStateRevision: 10,
		ExecutionContext: "advance", PriorInvocation: &source, ResultingInvocation: &destination,
	}
	foreign := execute
	foreign.ID, foreign.FlowID, foreign.Sequence, foreign.EffectOutputs = "foreign", "run-two", 9, protocol.Parameters{{Name: "publication_id", Value: "999"}}
	records := []journalRecord{
		{Admission: protocol.Admission{Invocation: source}, Receipt: &execute},
		{Admission: protocol.Admission{Invocation: source}, Receipt: &cut},
		{Admission: protocol.Admission{Invocation: source}, Receipt: &foreign},
	}
	value, receiptID, found, err := findLatestCommittedTransitionOutput(records, "run-one", destination, catalog.TransitionID("publication.execute"), "publication_id", 10)
	if err != nil || !found || value != "222" || receiptID != "execute" {
		t.Fatalf("transition output = %q receipt=%q found=%t err=%v", value, receiptID, found, err)
	}
	if _, _, found, err := findLatestCommittedTransitionOutput(records, "run-one", destination, catalog.TransitionID("publication.execute"), "publication_id", 8); err != nil || found {
		t.Fatalf("future output crossed revision boundary: found=%t err=%v", found, err)
	}
}

func TestAcceptedProgramReconciliationAuthorizesFreshDelegationRequestOnly(t *testing.T) {
	invoking := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/main", ControllerID: "controller"}
	first := protocol.TransitionReceipt{
		ID: "reconcile-one", FlowID: "run-one", Sequence: 7, TransitionID: "installation.reconcile-update", ProgramChangeAccepted: true,
		PriorProgramFingerprint: "program-a", Program: protocol.ProgramIdentity{Fingerprint: "program-b"},
		ControlBundleTargetFingerprint: "bundle-b",
		ObjectiveID:                    "objective-prior-abandoned", TargetID: "safely-abandoned", TrustedClass: model.ObjectiveAbandoned, DeliveryID: "prior-delivery",
	}
	second := first
	second.ID, second.Sequence, second.PriorProgramFingerprint, second.Program.Fingerprint, second.ControlBundleTargetFingerprint = "reconcile-two", 8, "program-b", "program-c", "bundle-c"
	records := []journalRecord{
		{Admission: protocol.Admission{Invocation: invoking}, Receipt: &first},
		{Admission: protocol.Admission{Invocation: invoking}, Receipt: &second},
	}
	admitted, err := installationReprojectionAdmits(records, "run-one", invoking, "bundle-c")
	if err != nil || !admitted {
		t.Fatalf("installation reprojection admitted=%t err=%v", admitted, err)
	}
	if admitted, err := installationReprojectionAdmits(records, "run-other", invoking, "bundle-c"); err != nil || admitted {
		t.Fatalf("foreign Flow installation admitted=%t err=%v", admitted, err)
	}
	if admitted, err := installationReprojectionAdmits(records, "run-one", invoking, "bundle-other"); err != nil || admitted {
		t.Fatalf("foreign bundle installation admitted=%t err=%v", admitted, err)
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

func TestReprojectedDelegationAnchorsAtCurrentCommittedWorktree(t *testing.T) {
	source := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "source", Ref: "refs/heads/main", ControllerID: "controller"}
	managed := source
	managed.WorktreeID, managed.Ref = "managed", "refs/heads/feature"
	next := managed
	next.WorktreeID, next.Ref = "next", "refs/heads/next"

	cut := protocol.TransitionReceipt{
		ID: "cut", FlowID: "run-one", Sequence: 8, TransitionID: "workspace.cut", ExecutionContext: "advance",
		PriorInvocation: &source, ResultingInvocation: &managed,
	}
	records := []journalRecord{{Admission: protocol.Admission{Invocation: source}, Receipt: &cut}}

	admitted, err := invocationAuthorizedByRecords(records, "run-one", managed, managed)
	if err != nil || !admitted {
		t.Fatalf("fresh managed-worktree delegation admitted=%t err=%v", admitted, err)
	}

	advance := protocol.TransitionReceipt{
		ID: "advance", FlowID: "run-one", Sequence: 9, TransitionID: "workspace.advance", ExecutionContext: "advance",
		PriorInvocation: &managed, ResultingInvocation: &next,
	}
	records = append(records, journalRecord{Admission: protocol.Admission{Invocation: managed}, Receipt: &advance})
	admitted, err = invocationAuthorizedByRecords(records, "run-one", managed, next)
	if err != nil || !admitted {
		t.Fatalf("post-delegation advance admitted=%t err=%v", admitted, err)
	}
}

func TestReprojectedDelegationRejectsDisconnectedAdvanceAfterAnchor(t *testing.T) {
	source := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "source", Ref: "refs/heads/main", ControllerID: "controller"}
	managed := source
	managed.WorktreeID, managed.Ref = "managed", "refs/heads/feature"
	foreign := source
	foreign.WorktreeID, foreign.Ref = "foreign", "refs/heads/foreign"
	next := foreign
	next.WorktreeID, next.Ref = "next", "refs/heads/next"

	cut := protocol.TransitionReceipt{
		ID: "cut", FlowID: "run-one", Sequence: 8, TransitionID: "workspace.cut", ExecutionContext: "advance",
		PriorInvocation: &source, ResultingInvocation: &managed,
	}
	disconnected := protocol.TransitionReceipt{
		ID: "disconnected", FlowID: "run-one", Sequence: 9, TransitionID: "workspace.advance", ExecutionContext: "advance",
		PriorInvocation: &foreign, ResultingInvocation: &next,
	}
	records := []journalRecord{
		{Admission: protocol.Admission{Invocation: source}, Receipt: &cut},
		{Admission: protocol.Admission{Invocation: foreign}, Receipt: &disconnected},
	}

	if admitted, err := invocationAuthorizedByRecords(records, "run-one", managed, next); err == nil || admitted {
		t.Fatalf("disconnected post-anchor advance admitted=%t err=%v", admitted, err)
	}
}
