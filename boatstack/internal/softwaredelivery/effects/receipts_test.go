package effects

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func committedWorkOutputFixture(t *testing.T, sequence, revision uint64) (journalRecord, CommittedWorkOutputSelector) {
	t.Helper()
	invocation := model.InvocationContext{
		RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/main", ControllerID: "controller",
		InvokingPath: filepath.Join(t.TempDir(), "repo"), RuntimeVersion: "test", RuntimePath: filepath.Join(t.TempDir(), "runtime"), RuntimeFingerprint: "runtime",
		Topology: model.TopologyEmbedded, Host: "cursor", Correlation: "proof",
	}
	objective := model.Objective{ID: "objective-proof", TargetID: "approved", TrustedClass: "approved", DeliveryID: "proof"}
	contract := catalog.WorkContract{
		ID: "work-a", Fingerprint: strings.Repeat("c", 64),
		Outputs: []catalog.WorkOutput{{ID: "architecture", Path: "architecture.md", MediaType: "text/markdown", Required: true, MaxBytes: 4096}},
	}
	work := &protocol.WorkEvidence{
		RunID: "run-proof", ProgramID: "proof", EntryID: "prove", TransitionID: "produce-a", ProgramFingerprint: strings.Repeat("3", 64),
		StateRevision: revision - 1, RepositoryID: "repo", WorktreeID: "worktree", ContractID: contract.ID, ContractFingerprint: contract.Fingerprint,
		ResultFingerprint: strings.Repeat("4", 64), Outputs: []protocol.WorkOutputEvidence{{ID: "architecture", Path: "architecture.md", MediaType: "text/markdown", SHA256: sha256Bytes([]byte("architecture")), Size: 12, Content: "architecture"}},
	}
	receipt := protocol.TransitionReceipt{
		ID: "trc-producer", FlowID: "run-proof", Sequence: sequence, TransitionID: "produce-a", PriorStateRevision: revision - 1, ResultingStateRevision: revision,
		Program: protocol.ProgramIdentity{ID: "proof", Fingerprint: strings.Repeat("3", 64)}, WorkResultFingerprint: work.ResultFingerprint,
	}
	record := journalRecord{Admission: protocol.Admission{
		Invocation: invocation, Objective: objective, ExpectedProgramFingerprint: strings.Repeat("3", 64), ExpectedStateRevision: revision - 1, Work: work,
	}, Receipt: &receipt}
	selector := CommittedWorkOutputSelector{
		FlowID: "run-proof", ProgramID: "proof", ProgramFingerprint: strings.Repeat("3", 64), EntryID: "prove", Objective: objective,
		Invocation: invocation, TransitionID: "produce-a", Work: contract, OutputID: "architecture", MaximumRevision: revision,
	}
	return record, selector
}

func TestCommittedWorkOutputSelectsApplicableOccurrenceWithoutFallback(t *testing.T) {
	older, selector := committedWorkOutputFixture(t, 4, 8)
	selected, found, err := findApplicableCommittedWorkOutput([]journalRecord{older}, selector)
	if err != nil || !found || selected.Receipt.ID != "trc-producer" || selected.Output.Content != "architecture" {
		t.Fatalf("selected=%#v found=%t err=%v", selected, found, err)
	}

	newer := older
	newerReceipt := *older.Receipt
	newerReceipt.ID, newerReceipt.Sequence, newerReceipt.ResultingStateRevision, newerReceipt.PriorStateRevision = "trc-stale", 5, 9, 8
	newer.Receipt = &newerReceipt
	newer.Admission.ExpectedStateRevision = 8
	newerWork := *older.Admission.Work
	newerWork.StateRevision = 8
	newerWork.ProgramFingerprint = strings.Repeat("5", 64)
	newer.Admission.Work = &newerWork
	selector.MaximumRevision = 9
	if _, found, err := findApplicableCommittedWorkOutput([]journalRecord{older, newer}, selector); err == nil || found || !strings.Contains(err.Error(), "stale or incompatible") {
		t.Fatalf("stale applicable occurrence fell back: found=%t err=%v", found, err)
	}

	duplicate := newer
	duplicateReceipt := *newer.Receipt
	duplicateReceipt.ID = "trc-ambiguous"
	duplicate.Receipt = &duplicateReceipt
	if _, found, err := findApplicableCommittedWorkOutput([]journalRecord{newer, duplicate}, selector); err == nil || found || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous maximal occurrence result: found=%t err=%v", found, err)
	}
}

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

func TestAcceptedConfigurationMutationAuthorizesFreshIdentityDelegationRequestOnly(t *testing.T) {
	invoking := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/main", ControllerID: "controller"}
	receipt := protocol.TransitionReceipt{
		ID: "configuration-one", FlowID: "run-one", Sequence: 9, TransitionID: "configuration.mutate", ResultingStateRevision: 12,
		ControlBundleTargetFingerprint: "bundle-b",
	}
	records := []journalRecord{{
		Admission: protocol.Admission{Invocation: invoking, Parameters: protocol.Parameters{{Name: "config_sha256", Value: "config-b"}}},
		Receipt:   &receipt,
	}}
	admitted, err := configurationReprojectionAdmits(records, "run-one", invoking, "config-b", "bundle-b", 12)
	if err != nil || !admitted {
		t.Fatalf("configuration reprojection admitted=%t err=%v", admitted, err)
	}
	if admitted, err := configurationReprojectionAdmits(records, "run-other", invoking, "config-b", "bundle-b", 12); err != nil || admitted {
		t.Fatalf("foreign Flow mutation admitted=%t err=%v", admitted, err)
	}
	if admitted, err := configurationReprojectionAdmits(records, "run-one", invoking, "config-other", "bundle-b", 12); err != nil || admitted {
		t.Fatalf("foreign configuration admitted=%t err=%v", admitted, err)
	}
	if admitted, err := configurationReprojectionAdmits(records, "run-one", invoking, "config-b", "bundle-b", 11); err != nil || admitted {
		t.Fatalf("future configuration receipt admitted=%t err=%v", admitted, err)
	}
	other := invoking
	other.WorktreeID = "other-worktree"
	if admitted, err := configurationReprojectionAdmits(records, "run-one", other, "config-b", "bundle-b", 12); err != nil || admitted {
		t.Fatalf("foreign worktree mutation admitted=%t err=%v", admitted, err)
	}
	if admitted, err := configurationReprojectionAdmits(records, "run-one", invoking, "config-b", "bundle-other", 12); err != nil || admitted {
		t.Fatalf("foreign control bundle admitted=%t err=%v", admitted, err)
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
