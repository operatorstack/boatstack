package supervisor

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func snapshotFor(t *testing.T, phase model.ProtocolPhase, terminal model.TerminalStatus) model.Snapshot {
	t.Helper()
	e := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(10, 0).UTC()}
	o := model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion,
		Invocation:    model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "wt", Ref: "refs/heads/f", ControllerID: "ctl", InvokingPath: filepath.Join(t.TempDir(), "repo"), RuntimePath: filepath.Join(t.TempDir(), "runtime"), RuntimeFingerprint: "runtime", Topology: model.TopologyEmbedded, Host: "cli", Correlation: "c"},
		Phase:         model.Known(phase, e), Engagement: model.Known(model.EngagementActive, e), Delivery: model.Known(model.DeliveryActive, e),
		Workspace: model.Known(model.WorkspaceActive, e), Plan: model.Known(model.PlanValid, e),
		Configuration: model.Known(model.ConfigurationVerified, e), Runtime: model.Known(model.RuntimeVerified, e),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"}}, e),
		Publication:         model.Known(model.PublicationNone, e), Verification: model.Known(model.VerificationUnverified, e),
		Recovery: model.Known(model.RecoveryNone, e), Transaction: model.Known(model.TransactionNone, e),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", e), TransactionInfo: model.Absent[model.TransactionContext]("none", e),
		Terminal: model.Known(terminal, e), Goal: model.Known(goalFor(), e), ObservedAt: time.Unix(10, 0).UTC(),
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

func goalFor() model.Goal {
	return model.Goal{ID: "goal", Kind: model.GoalVerified, DeliveryID: "delivery"}
}

func TestTerminalGoalOutranksLocalTransitions(t *testing.T) {
	// control-law: configured-terminal-outranks-local-lifecycle
	s := New(catalog.Default())
	decision := s.Resolve(snapshotFor(t, model.PhaseTerminal, model.TerminalEstablished), goalFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
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
	decision := New(catalog.Default()).Resolve(canonical, goalFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "workspace.cleanup")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "workspace.cleanup" {
		t.Fatalf("decision = %#v, want explicit post-terminal cleanup", decision)
	}
}

func TestTerminalEvidenceForOldGoalDoesNotTerminateNewGoal(t *testing.T) {
	// control-law: terminal-evidence-is-bound-to-exact-goal-not-local-phase
	s := New(catalog.Default())
	newGoal := model.Goal{ID: "next-goal", Kind: model.GoalOpenPR, DeliveryID: "delivery"}
	decision := s.Resolve(snapshotFor(t, model.PhaseTerminal, model.TerminalEstablished), newGoal, catalog.AuthoritySet{catalog.AuthorityHuman: true}, "goal.configure")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "goal.configure" {
		t.Fatalf("decision=%#v, want exact new-goal configuration", decision)
	}
}

func TestRequestedTransitionRequiresExactAuthority(t *testing.T) {
	// control-law: useful-action-is-not-effect-authority
	s := New(catalog.Default())
	decision := s.Resolve(snapshotFor(t, model.PhaseActive, model.TerminalNonterminal), goalFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "plan.approve")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("decision = %s, want FRONTIER", decision.Kind)
	}
}

func TestPlanApprovalPolicyDistinguishesHumanFromAutonomyAuthority(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	autonomy := catalog.AuthoritySet{catalog.AuthorityAutonomy: true}
	decision := New(catalog.Default()).Resolve(snapshot, goalFor(), autonomy, "plan.approve")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("human-only policy decision = %#v, want FRONTIER", decision)
	}
	snapshot.ConfigurationPolicy.Value.PlanApproval = "human-or-autonomy"
	snapshot, err := model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision = New(catalog.Default()).Resolve(snapshot, goalFor(), autonomy, "plan.approve")
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
	decision := New(catalog.Default()).Resolve(snapshot, goalFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "plan.approve")
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
	supervisor := New(catalog.Default())
	decision := supervisor.Resolve(snapshot, goalFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "gate.review.record")
	if decision.Kind != DecisionFrontier {
		t.Fatalf("repository-only high-risk review = %#v, want FRONTIER", decision)
	}
	decision = supervisor.Resolve(snapshot, goalFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "gate.review.record")
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
	decision := New(catalog.Default()).Resolve(snapshot, goalFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "evidence.visual.attach")
	if decision.Kind != DecisionRefused {
		t.Fatalf("visual-off decision = %#v, want REFUSED", decision)
	}
}

func TestRecoveryModeOnlyPrescribesRecoveryTransition(t *testing.T) {
	// control-law: recovery-outranks-slice-position
	s := New(catalog.Default())
	decision := s.Resolve(snapshotFor(t, model.PhaseRecovery, model.TerminalNonterminal), goalFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "recovery.resume")
	if decision.Kind != DecisionPrescribed || decision.Transition == nil || decision.Transition.Class != catalog.EventRecovery {
		t.Fatalf("decision = %#v, want a recovery prescription", decision)
	}
}

func TestRecoveryModeRejectsARecoveryEventOutsideExactJournalContract(t *testing.T) {
	snapshot := snapshotFor(t, model.PhaseRecovery, model.TerminalNonterminal)
	decision := New(catalog.Default()).Resolve(snapshot, goalFor(), catalog.AuthoritySet{catalog.AuthorityHuman: true}, "recovery.rollback")
	if decision.Kind != DecisionRefused {
		t.Fatalf("unpermitted recovery decision = %#v, want REFUSED", decision)
	}
}

func TestUncontrollableEventCannotBeRequested(t *testing.T) {
	// control-law: surfaces-cannot-assert-external-facts
	s := New(catalog.Default())
	decision := s.Resolve(snapshotFor(t, model.PhaseActive, model.TerminalNonterminal), goalFor(), catalog.AuthoritySet{catalog.AuthorityRepository: true}, "external.pr-merged")
	if decision.Kind != DecisionRefused {
		t.Fatalf("decision = %s, want REFUSED", decision.Kind)
	}
}

func TestGuardDeniesDestructionAndRoutesManagedBypassThroughAdmission(t *testing.T) {
	s := New(catalog.Default())
	snapshot := snapshotFor(t, model.PhaseActive, model.TerminalNonterminal)
	destructive := s.Guard(snapshot, CommandIntent{Class: IntentDestructive, Operation: "git.reset-hard", Fingerprint: "fingerprint"})
	if destructive.Allowed {
		t.Fatal("destructive command was allowed")
	}
	managed := s.Guard(snapshot, CommandIntent{Class: IntentManagedBypass, Operation: "publication.push", Fingerprint: "fingerprint", Transition: "publication.execute"})
	if managed.Allowed || managed.RequiredTransition != "publication.execute" {
		t.Fatalf("managed bypass decision = %#v", managed)
	}
	ordinary := s.Guard(snapshot, CommandIntent{Class: IntentOrdinary, Operation: "repository.command", Fingerprint: "fingerprint"})
	if !ordinary.Allowed {
		t.Fatalf("ordinary operation was blocked: %#v", ordinary)
	}
}
