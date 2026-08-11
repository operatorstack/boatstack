package reducer

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

func TestRequiredVisualEvidenceParticipatesInVerifiedTerminal(t *testing.T) {
	// control-law: repository-visual-policy-is-terminal-authority-not-decoration
	goal := model.Goal{ID: "visual-goal", Kind: model.GoalVerified, DeliveryID: "visual-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryActive, Workspace: model.WorkspaceActive,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Goal: goal, ConfigFingerprint: "config", PlanApprovalPolicy: "human", VisualEvidencePolicy: "required",
		ExternalEffectPolicy: "human-or-autonomy-plus-provider", EnabledHosts: []string{"cli"},
	}
	apply := func(id catalog.TransitionID, parameters protocol.Parameters) {
		t.Helper()
		transition, ok := catalog.Default().Lookup(id)
		if !ok {
			t.Fatalf("missing transition %s", id)
		}
		if err := Apply(&state, protocol.Admission{Goal: goal, Parameters: parameters, SourceRevision: "revision", WorktreeFingerprint: "worktree"}, transition); err != nil {
			t.Fatalf("apply %s: %v", id, err)
		}
	}
	for _, gate := range []string{"build", "test", "review"} {
		apply(catalog.TransitionID("gate."+gate+".record"), protocol.Parameters{
			{Name: "source_revision", Value: "revision"}, {Name: "evidence_fingerprint", Value: "evidence-" + gate},
		})
	}
	if state.Terminal == model.TerminalEstablished {
		t.Fatal("required visual evidence was bypassed by build/test/review gates")
	}
	apply("evidence.visual.attach", protocol.Parameters{
		{Name: "manifest_path", Value: "/manifest"}, {Name: "privacy_receipt", Value: "visual-proof"}, {Name: "source_revision", Value: "revision"},
	})
	if state.Terminal != model.TerminalEstablished || state.Phase != model.PhaseTerminal || !hasGates(state, "visual") {
		t.Fatalf("visual evidence did not establish verified terminal: %#v", state)
	}
}

func TestPublicationCorrectionRequiresIndependentObservationForTerminal(t *testing.T) {
	// control-law: external-writer-cannot-self-certify-provider-state
	goal := model.Goal{ID: "publication-goal", Kind: model.GoalOpenPR, DeliveryID: "publication-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseTerminal, Engagement: model.EngagementActive, Delivery: model.DeliveryTerminal, Workspace: model.WorkspacePublished,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationOpen,
		Verification: model.VerificationCurrent, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalEstablished,
		Goal: goal,
	}
	correct, _ := catalog.Default().Lookup("publication.correct")
	admission := protocol.Admission{Goal: goal, Parameters: protocol.Parameters{{Name: "publication_id", Value: "7"}, {Name: "body_path", Value: "/body"}}}
	if err := Apply(&state, admission, correct); err != nil {
		t.Fatal(err)
	}
	if state.Terminal != model.TerminalNonterminal || state.Publication != model.PublicationPublishedNotLanded || state.Phase != model.PhaseActive {
		t.Fatalf("external correction self-certified terminal: %#v", state)
	}
	state.Publication = model.PublicationOpen // supplied only by PrepareObservation through gh pr view
	observe, _ := catalog.Default().Lookup("publication.observe")
	if err := Apply(&state, admission, observe); err != nil {
		t.Fatal(err)
	}
	if state.Terminal != model.TerminalEstablished || state.Phase != model.PhaseTerminal {
		t.Fatalf("independent publication observation did not establish terminal: %#v", state)
	}
}

func TestWorkspaceReapPreservesEstablishedTerminalPhase(t *testing.T) {
	for _, fixture := range []struct {
		goalKind model.GoalKind
		delivery model.DeliveryState
		phase    model.ProtocolPhase
	}{
		{goalKind: model.GoalMerged, delivery: model.DeliveryTerminal, phase: model.PhaseTerminal},
		{goalKind: model.GoalAbandoned, delivery: model.DeliveryDiscarded, phase: model.PhaseAbandoned},
	} {
		goal := model.Goal{ID: "cleanup-goal", Kind: fixture.goalKind, DeliveryID: "cleanup-delivery"}
		state := durable.State{
			SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
			Phase: fixture.phase, Engagement: model.EngagementActive, Delivery: fixture.delivery, Workspace: model.WorkspaceAbandoned,
			Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationMerged,
			Verification: model.VerificationCurrent, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalEstablished, Goal: goal,
		}
		transition, _ := catalog.Default().Lookup("workspace.reap")
		if err := Apply(&state, protocol.Admission{Goal: goal}, transition); err != nil {
			t.Fatal(err)
		}
		if state.Workspace != model.WorkspaceAbsent || state.Phase != fixture.phase || state.Terminal != model.TerminalEstablished {
			t.Fatalf("reap lost %s terminal: %#v", fixture.goalKind, state)
		}
	}
}

func TestEscalatedRecoveryCanOnlyBeReconfiguredTowardExplicitAbandonment(t *testing.T) {
	original := model.Goal{ID: "delivery-goal", Kind: model.GoalOpenPR, DeliveryID: "delivery"}
	abandoned := model.Goal{ID: "abandon-goal", Kind: model.GoalAbandoned, DeliveryID: "delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseFrontier, Engagement: model.EngagementActive, Delivery: model.DeliveryPublished, Workspace: model.WorkspacePublished,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationUnavailable,
		Verification: model.VerificationCurrent, Recovery: model.RecoveryEscalated, Transaction: model.TransactionNone, Terminal: model.TerminalStale,
		Goal: original, TransactionID: "adm-interrupted", RecoveryCause: "provider unknown", RecoverySourcePhase: model.PhaseActive,
		RecoveryResumption: model.PhaseFrontier, RecoveryBudget: 0,
	}
	configure, _ := catalog.Default().Lookup("goal.configure")
	configureAdmission := protocol.Admission{Goal: abandoned, Parameters: protocol.Parameters{
		{Name: "goal_kind", Value: string(abandoned.Kind)}, {Name: "delivery_id", Value: abandoned.DeliveryID},
	}}
	if err := Apply(&state, configureAdmission, configure); err != nil {
		t.Fatal(err)
	}
	if state.Phase != model.PhaseFrontier || state.Recovery != model.RecoveryEscalated {
		t.Fatalf("goal reconfiguration bypassed escalated recovery: %#v", state)
	}
	abandon, _ := catalog.Default().Lookup("plan.abandon")
	if err := Apply(&state, protocol.Admission{Goal: abandoned}, abandon); err != nil {
		t.Fatal(err)
	}
	if state.Phase != model.PhaseAbandoned || state.Recovery != model.RecoveryNone || state.TransactionID != "" || state.Terminal != model.TerminalEstablished {
		t.Fatalf("explicit abandonment did not close recovery: %#v", state)
	}
}
