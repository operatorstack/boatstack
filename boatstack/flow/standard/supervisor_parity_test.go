package standard_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	. "github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func testObjectiveContracts() catalog.ObjectiveContracts {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		panic(err)
	}
	contracts, err := catalog.NewObjectiveContracts(manifest.ObjectiveContracts, nil)
	if err != nil {
		panic(err)
	}
	return contracts
}

func snapshotFor(t *testing.T, phase model.ProtocolPhase, terminal model.TerminalStatus) model.Snapshot {
	t.Helper()
	e := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(10, 0).UTC()}
	o := model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: 1,
		Invocation: model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "wt", Ref: "refs/heads/f", ControllerID: "ctl", InvokingPath: filepath.Join(t.TempDir(), "repo"), RuntimeVersion: "runtime-version", RuntimePath: filepath.Join(t.TempDir(), "runtime"), RuntimeFingerprint: "runtime", Topology: model.TopologyEmbedded, Host: "cli", Correlation: "c"},
		Phase:      model.Known(phase, e), Engagement: model.Known(model.EngagementActive, e), Delivery: model.Known(model.DeliveryActive, e),
		Workspace: model.Known(model.WorkspaceActive, e), Plan: model.Known(model.PlanValid, e),
		Configuration: model.Known(model.ConfigurationVerified, e), Runtime: model.Known(model.RuntimeVerified, e),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"}}, e),
		Publication:         model.Known(model.PublicationNone, e), Verification: model.Known(model.VerificationUnverified, e),
		Recovery: model.Known(model.RecoveryNone, e), Transaction: model.Known(model.TransactionNone, e),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", e), TransactionInfo: model.Absent[model.TransactionContext]("none", e),
		Terminal: model.Known(terminal, e), Objective: model.Known(objectiveFor(), e), ObservedAt: time.Unix(10, 0).UTC(),
	}
	if phase == model.PhaseRecovery {
		o.Recovery = model.Known(model.RecoveryReconcile, e)
		o.Transaction = model.Known(model.TransactionExternalUncertain, e)
		o.RecoveryInfo = model.Known(model.RecoveryContext{TransactionID: "adm-fixture", Cause: "interrupted", SourcePhase: model.PhaseActive, Permitted: []string{"recovery.resume"}, BudgetRemaining: 1, Resumption: model.PhaseActive}, e)
		o.TransactionInfo = model.Known(model.TransactionContext{ID: "adm-fixture", TransitionID: "plan.create", Status: "recovery-required"}, e)
	}
	if terminal == model.TerminalEstablished {
		o.Delivery = model.Known(model.DeliveryTerminal, e)
		o.Verification = model.Known(model.VerificationCurrent, e)
	}
	result, err := model.Canonicalize(o)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func objectiveFor() model.Objective {
	return model.Objective{ID: "objective", Kind: model.ObjectiveVerified, DeliveryID: "delivery"}
}

func recanonicalize(t *testing.T, snapshot model.Snapshot) model.Snapshot {
	t.Helper()
	result, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func openPRSnapshot(t *testing.T, recordedGates ...string) (model.Snapshot, model.Objective) {
	t.Helper()
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	objective := model.Objective{ID: "objective", Kind: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	evidence := snapshot.Verification.Evidence[0]
	snapshot.Objective = model.Known(objective, evidence)
	snapshot.Plan = model.Known(model.PlanLocked, evidence)
	snapshot.Publication = model.Known(model.PublicationCandidate, evidence)
	snapshot.Verification = model.Known(model.VerificationCurrent, evidence)
	for _, gate := range recordedGates {
		gateEvidence := evidence
		gateEvidence.Source = "gate-evidence:" + gate + ":/fixture/" + gate + ".json"
		snapshot.Verification.Evidence = append(snapshot.Verification.Evidence, gateEvidence)
	}
	return recanonicalize(t, snapshot), objective
}

func TestTerminalObjectiveOutranksLocalTransitions(t *testing.T) {
	// control-law: configured-terminal-outranks-local-lifecycle
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	decision := s.Resolve(snapshotFor(t, model.PhaseTerminal, model.TerminalEstablished), objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	if decision.Kind != DecisionTerminal || decision.Transition != nil {
		t.Fatalf("decision = %#v, want terminal without transition", decision)
	}
}

func TestExplicitPostTerminalCleanupRemainsAdmissible(t *testing.T) {
	// control-law: terminal-proof-does-not-strand-proven-landed-resources
	snapshot := snapshotFor(t, model.PhaseTerminal, model.TerminalEstablished)
	snapshot.Workspace = model.Known(model.WorkspaceLanded, snapshot.Workspace.Evidence[0])
	snapshot.Publication = model.Known(model.PublicationMerged, snapshot.Publication.Evidence[0])
	canonical, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(canonical, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "workspace.cleanup")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "workspace.cleanup" {
		t.Fatalf("decision = %#v, want explicit post-terminal cleanup", decision)
	}
}

func TestTerminalEvidenceForOldObjectiveDoesNotTerminateNewObjective(t *testing.T) {
	// control-law: terminal-evidence-is-bound-to-exact-objective-not-local-phase
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	snapshot := snapshotFor(t, model.PhaseTerminal, model.TerminalEstablished)
	snapshot.Workspace = model.Known(model.WorkspacePublished, snapshot.Workspace.Evidence[0])
	snapshot.Publication = model.Known(model.PublicationOpen, snapshot.Publication.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	newObjective := model.Objective{ID: "next-objective", Kind: model.ObjectiveOpenPR, DeliveryID: "next-delivery"}
	decision := s.Resolve(snapshot, newObjective, catalog.AuthoritySet{catalog.AuthorityHuman: true}, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "objective.bind" {
		t.Fatalf("untargeted terminal replacement decision=%#v, want exact new-objective configuration", decision)
	}
}

func TestUntargetedResolutionReconfiguresDifferentObjectiveAndSkipsSatisfiedObjective(t *testing.T) {
	// control-law: untargeted-resolution-must-advance-the-exact-objective
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	authority := catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityRepository: true}

	newObjective := model.Objective{ID: "new-objective", Kind: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, newObjective, authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "objective.bind" {
		t.Fatalf("different-objective decision = %#v, want objective.bind", decision)
	}

	snapshot.Plan = model.Known(model.PlanValid, snapshot.Plan.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	decision = New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "plan.approve" {
		t.Fatalf("exact-objective decision = %#v, want plan.approve without objective.bind stutter", decision)
	}
}

func TestDormantBootstrapObjectiveReconfiguresBeforeEngagement(t *testing.T) {
	// control-law: a retained bootstrap objective cannot be bypassed by engagement
	snapshot := snapshotFor(t, model.PhaseDormant, model.TerminalNonterminal)
	requested := model.Objective{ID: "basic-project", Kind: model.ObjectiveApprovedPlan, DeliveryID: "basic-project"}
	authority := catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityRepository: true}

	untargeted := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, requested, authority, "")
	if untargeted.Kind != DecisionPrescribed || untargeted.Transition == nil || untargeted.Transition.ID != "objective.bind" {
		t.Fatalf("untargeted decision = %#v, want objective.bind", untargeted)
	}
	targeted := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, requested, authority, untargeted.Transition.ID)
	if targeted.Kind != DecisionPrescribed || targeted.Transition == nil || targeted.Transition.ID != untargeted.Transition.ID {
		t.Fatalf("targeted decision = %#v, want parity with %#v", targeted, untargeted)
	}
	engagement := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, requested, authority, "engagement.begin")
	if engagement.Kind != DecisionRefused {
		t.Fatalf("engagement decision = %#v, want refusal until objective.bind", engagement)
	}
}

func TestDisabledHostIsRefusedBeforeUntargetedSelection(t *testing.T) {
	// control-law: host policy applies before both targeted and untargeted selection
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.Invocation.Host = "codex"
	snapshot = recanonicalize(t, snapshot)
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	if decision.Kind != DecisionRefused || decision.Transition != nil {
		t.Fatalf("disabled-host decision = %#v, want REFUSED", decision)
	}
}

func TestPublicationObservationRemainsSelectableForVolatileExternalState(t *testing.T) {
	// control-law: a nonterminal provider observation is evidence, not permanent progress
	snapshot, objective := openPRSnapshot(t, "build", "test", "review", "change", "journey")
	objective.Kind = model.ObjectiveMerged
	snapshot.Objective = model.Known(objective, snapshot.Objective.Evidence[0])
	snapshot.Publication = model.Known(model.PublicationOpen, snapshot.Publication.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	var transitions []catalog.Transition
	for _, transition := range testprogram.StandardRegistry().All() {
		if transition.ID == "publication.observe" || transition.Class == catalog.EventRecovery {
			transitions = append(transitions, transition)
		}
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		t.Fatal(err)
	}
	decision := New(registry, testObjectiveContracts()).Resolve(snapshot, objective, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "publication.observe" {
		t.Fatalf("volatile publication decision = %#v, want publication.observe", decision)
	}
}

func TestUntargetedResolutionExcludesExplicitControlTransitions(t *testing.T) {
	// control-law: untargeted-resolution-cannot-invent-repair-or-slice-intent
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.Plan = model.Known(model.PlanLocked, snapshot.Plan.Evidence[0])
	snapshot.Verification = model.Known(model.VerificationUnverified, snapshot.Verification.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	authority := catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityRepository: true}

	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "gate.build.record" {
		t.Fatalf("untargeted decision = %#v, want gate.build.record", decision)
	}
	decision = New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), authority, "plan.invalidate")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "plan.invalidate" {
		t.Fatalf("explicit invalidation decision = %#v, want requested plan.invalidate", decision)
	}
	decision = New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), authority, "delivery.slice.advance")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "delivery.slice.advance" {
		t.Fatalf("explicit slice-marker decision = %#v, want requested delivery.slice.advance", decision)
	}
}

func TestSelectionClassOutranksComponentLocalPriority(t *testing.T) {
	// control-law: bounded-selection-class-prevents-lower-layer-priority-inversion
	transitions := testprogram.StandardRegistry().All()
	for index := range transitions {
		switch transitions[index].ID {
		case "gate.build.record":
			transitions[index].SelectionClass = catalog.SelectionObjectiveRequired
			transitions[index].Priority = 999
		case "gate.test.record":
			transitions[index].SelectionClass = catalog.SelectionProgramProgress
			transitions[index].Priority = 1
		}
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.Plan = model.Known(model.PlanLocked, snapshot.Plan.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	decision := New(registry, testObjectiveContracts()).Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "gate.build.record" {
		t.Fatalf("decision = %#v, want higher selection class despite lower-layer numeric priority", decision)
	}
}

func TestUntargetedResolutionUsesCurrentGateEvidenceForProgress(t *testing.T) {
	// control-law: verified-gate-progress-is-derived-from-canonical-evidence
	authority := catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityRepository: true}
	snapshot, objective := openPRSnapshot(t, "build")
	snapshot.Publication = model.Known(model.PublicationNone, snapshot.Publication.Evidence[0])
	snapshot = recanonicalize(t, snapshot)

	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objective, authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "gate.test.record" {
		t.Fatalf("one-gate decision = %#v, want gate.test.record", decision)
	}

	snapshot, objective = openPRSnapshot(t, "build", "test", "review")
	snapshot.Publication = model.Known(model.PublicationNone, snapshot.Publication.Evidence[0])
	snapshot = recanonicalize(t, snapshot)
	decision = New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objective, authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "publication.preview" {
		t.Fatalf("complete-gates decision = %#v, want publication.preview", decision)
	}
}

func TestUntargetedResolutionStopsAtSelectedProviderBoundary(t *testing.T) {
	// control-law: unavailable-authority-cannot-be-skipped-for-a-lower-priority-effect
	snapshot, objective := openPRSnapshot(t, "build", "test", "review")
	supervisor := New(testprogram.StandardRegistry(), testObjectiveContracts())
	authority := catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityRepository: true}

	decision := supervisor.Resolve(snapshot, objective, authority, "")
	if decision.Kind != DecisionFrontier || len(decision.Candidates) != 1 || decision.Candidates[0] != "publication.execute" {
		t.Fatalf("provider-free decision = %#v, want publication.execute FRONTIER", decision)
	}
	authority[catalog.AuthorityProvider] = true
	decision = supervisor.Resolve(snapshot, objective, authority, "")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "publication.execute" {
		t.Fatalf("provider-authorized decision = %#v, want publication.execute", decision)
	}
}

func TestRequestedTransitionRequiresExactAuthority(t *testing.T) {
	// control-law: useful-action-is-not-effect-authority
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	decision := s.Resolve(snapshotFor(t, model.PhaseActive, model.TerminalNonterminal), objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "plan.approve")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("decision = %s, want FRONTIER", decision.Kind)
	}
}

func TestPlanApprovalPolicyDistinguishesHumanFromAutonomyAuthority(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	autonomy := catalog.AuthoritySet{catalog.AuthorityAutonomy: true}
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), autonomy, "plan.approve")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("human-only policy decision = %#v, want FRONTIER", decision)
	}
	snapshot.ConfigurationPolicy.Value.PlanApproval = "human-or-autonomy"
	snapshot, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision = New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), autonomy, "plan.approve")
	if decision.Kind != DecisionPrescribed {
		t.Fatalf("autonomy-enabled policy decision = %#v, want PRESCRIBED", decision)
	}
}

func TestDisabledHostCannotRequestManagedTransition(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.Invocation.Host = "codex"
	snapshot, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "plan.approve")
	if decision.Kind != DecisionRefused {
		t.Fatalf("disabled host decision = %#v, want REFUSED", decision)
	}
}

func TestHighRiskReviewPolicyRequiresHumanAuthority(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.Plan = model.Known(model.PlanLocked, snapshot.Plan.Evidence[0])
	snapshot.ConfigurationPolicy.Value.IndependentReviewForHighRisk = true
	snapshot.ConfigurationPolicy.Value.HighRiskChange = true
	snapshot, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := New(testprogram.StandardRegistry(), testObjectiveContracts())
	decision := supervisor.Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "gate.review.record")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("repository-only high-risk review = %#v, want FRONTIER", decision)
	}
	decision = supervisor.Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "gate.review.record")
	if decision.Kind != DecisionPrescribed {
		t.Fatalf("human high-risk review = %#v, want PRESCRIBED", decision)
	}
}

func TestVisualEvidenceOffRefusesAttachment(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	snapshot.ConfigurationPolicy.Value.VisualEvidence = "off"
	snapshot, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "evidence.visual.attach")
	if decision.Kind != DecisionRefused {
		t.Fatalf("visual-off decision = %#v, want REFUSED", decision)
	}
}

func TestRecoveryModeOnlyPrescribesRecoveryTransition(t *testing.T) {
	// control-law: recovery-outranks-slice-position
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	decision := s.Resolve(snapshotFor(t, model.PhaseRecovery, model.TerminalNonterminal), objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "recovery.resume")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.Class != catalog.EventRecovery {
		t.Fatalf("decision = %#v, want a recovery prescription", decision)
	}
}

func TestRecoveryModeRejectsARecoveryEventOutsideExactJournalContract(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseRecovery, model.TerminalNonterminal)
	decision := New(testprogram.StandardRegistry(), testObjectiveContracts()).Resolve(snapshot, objectiveFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "recovery.rollback")
	if decision.Kind != DecisionRefused {
		t.Fatalf("unpermitted recovery decision = %#v, want REFUSED", decision)
	}
}

func TestUncontrollableEventCannotBeRequested(t *testing.T) {
	// control-law: surfaces-cannot-assert-external-facts
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	decision := s.Resolve(snapshotFor(t, model.PhaseActive, model.TerminalNonterminal), objectiveFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "external.pr-merged")
	if decision.Kind != DecisionRefused {
		t.Fatalf("decision = %s, want REFUSED", decision.Kind)
	}
}

func TestGuardDeniesDestructionAndRoutesManagedBypassThroughAdmission(t *testing.T) {
	s := New(testprogram.StandardRegistry(), testObjectiveContracts())
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	destructive := s.Guard(snapshot, CommandIntent{Class: IntentDestructive, Operation: "git.reset-hard", Fingerprint: "fingerprint"})
	if destructive.Allowed {
		t.Fatal("destructive command was allowed")
	}
	managed := s.Guard(snapshot, CommandIntent{Class: IntentManagedBypass, Operation: "publication.push", Fingerprint: "fingerprint"})
	if managed.Allowed || managed.RequiredTransition != "publication.execute" {
		t.Fatalf("managed bypass decision = %#v", managed)
	}
	ordinary := s.Guard(snapshot, CommandIntent{Class: IntentOrdinary, Operation: "repository.command", Fingerprint: "fingerprint"})
	if !ordinary.Allowed {
		t.Fatalf("ordinary operation was blocked: %#v", ordinary)
	}
}
