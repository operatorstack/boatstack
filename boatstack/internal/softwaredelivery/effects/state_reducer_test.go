package effects

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func TestRequiredVisualEvidenceParticipatesInVerifiedTerminal(t *testing.T) {
	// control-law: repository-visual-policy-is-terminal-authority-not-decoration
	objective := model.Objective{ID: "visual-objective", TargetID: model.ObjectiveVerified, DeliveryID: "visual-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryActive, Workspace: model.WorkspaceActive,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Objective: objective, ConfigFingerprint: "config", PlanApprovalPolicy: "human", VisualEvidencePolicy: "required",
		ExternalEffectPolicy: "human-or-autonomy-plus-provider", EnabledHosts: []string{"cli"},
	}
	apply := func(id catalog.TransitionID, parameters protocol.Parameters) {
		t.Helper()
		transition, ok := testprogram.StandardRegistry().Lookup(id)
		if !ok {
			t.Fatalf("missing transition %s", id)
		}
		if err := applyStateTransition(&state, protocol.Admission{Objective: objective, Parameters: parameters, SourceRevision: "revision", WorktreeFingerprint: "worktree"}, transition); err != nil {
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
	objective := model.Objective{ID: "publication-objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "publication-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseTerminal, Engagement: model.EngagementActive, Delivery: model.DeliveryTerminal, Workspace: model.WorkspacePublished,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationOpen,
		Verification: model.VerificationCurrent, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalEstablished,
		Objective: objective,
	}
	correct, _ := testprogram.StandardRegistry().Lookup("publication.correct")
	admission := protocol.Admission{Objective: objective, Parameters: protocol.Parameters{{Name: "publication_id", Value: "7"}, {Name: "body_path", Value: "/body"}}}
	if err := applyStateTransition(&state, admission, correct); err != nil {
		t.Fatal(err)
	}
	if state.Terminal != model.TerminalNonterminal || state.Publication != model.PublicationPublishedNotLanded || state.Phase != model.PhaseActive {
		t.Fatalf("external correction self-certified terminal: %#v", state)
	}
	state.Publication = model.PublicationOpen // supplied only by PrepareObservation through gh pr view
	observe, _ := testprogram.StandardRegistry().Lookup("publication.observe")
	if err := applyStateTransition(&state, admission, observe); err != nil {
		t.Fatal(err)
	}
	if state.Terminal != model.TerminalEstablished || state.Phase != model.PhaseTerminal {
		t.Fatalf("independent publication observation did not establish terminal: %#v", state)
	}
}

func TestWorkspaceReapPreservesEstablishedTerminalPhase(t *testing.T) {
	for _, fixture := range []struct {
		objectiveKind model.TargetID
		delivery      model.DeliveryState
		phase         model.ProtocolPhase
	}{
		{objectiveKind: model.ObjectiveMerged, delivery: model.DeliveryTerminal, phase: model.PhaseTerminal},
		{objectiveKind: model.ObjectiveAbandoned, delivery: model.DeliveryDiscarded, phase: model.PhaseAbandoned},
	} {
		objective := model.Objective{ID: "cleanup-objective", TargetID: fixture.objectiveKind, DeliveryID: "cleanup-delivery"}
		state := durable.State{
			SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
			Phase: fixture.phase, Engagement: model.EngagementActive, Delivery: fixture.delivery, Workspace: model.WorkspaceAbandoned,
			Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationMerged,
			Verification: model.VerificationCurrent, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalEstablished, Objective: objective,
		}
		transition, _ := testprogram.StandardRegistry().Lookup("workspace.reap")
		if err := applyStateTransition(&state, protocol.Admission{Objective: objective}, transition); err != nil {
			t.Fatal(err)
		}
		if state.Workspace != model.WorkspaceAbsent || state.Phase != fixture.phase || state.Terminal != model.TerminalEstablished {
			t.Fatalf("reap lost %s terminal: %#v", fixture.objectiveKind, state)
		}
	}
}

func TestEscalatedRecoveryCanOnlyBeReconfiguredTowardExplicitAbandonment(t *testing.T) {
	original := model.Objective{ID: "delivery-objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	abandoned := model.Objective{ID: "abandon-objective", TargetID: model.ObjectiveAbandoned, DeliveryID: "delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseFrontier, Engagement: model.EngagementActive, Delivery: model.DeliveryPublished, Workspace: model.WorkspacePublished,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationUnavailable,
		Verification: model.VerificationCurrent, Recovery: model.RecoveryEscalated, Transaction: model.TransactionNone, Terminal: model.TerminalStale,
		Objective: original, TransactionID: "adm-interrupted", RecoveryCause: "provider unknown", RecoverySourcePhase: model.PhaseActive,
		RecoveryResumption: model.PhaseFrontier, RecoveryBudget: 0,
	}
	configure, _ := testprogram.StandardRegistry().Lookup("objective.bind")
	configureAdmission := protocol.Admission{Objective: abandoned, Parameters: protocol.Parameters{
		{Name: "target_id", Value: string(abandoned.TargetID)}, {Name: "delivery_id", Value: abandoned.DeliveryID},
	}}
	if err := applyStateTransition(&state, configureAdmission, configure); err != nil {
		t.Fatal(err)
	}
	if state.Phase != model.PhaseFrontier || state.Recovery != model.RecoveryEscalated {
		t.Fatalf("objective reconfiguration bypassed escalated recovery: %#v", state)
	}
	abandon, _ := testprogram.StandardRegistry().Lookup("plan.abandon")
	if err := applyStateTransition(&state, protocol.Admission{Objective: abandoned}, abandon); err != nil {
		t.Fatal(err)
	}
	if state.Phase != model.PhaseAbandoned || state.Recovery != model.RecoveryNone || state.TransactionID != "" || state.Terminal != model.TerminalEstablished {
		t.Fatalf("explicit abandonment did not close recovery: %#v", state)
	}
}

func TestObjectiveBindStartsDifferentDeliveryFromCleanProductState(t *testing.T) {
	prior := model.Objective{ID: "prior", TargetID: model.ObjectiveAbandoned, DeliveryID: "prior-delivery"}
	next := model.Objective{ID: "next", TargetID: model.ObjectiveOpenPR, DeliveryID: "next-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseTerminal, Engagement: model.EngagementCommand, Delivery: model.DeliveryTerminal, Workspace: model.WorkspacePublished,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationOpen,
		Verification: model.VerificationCurrent, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalEstablished,
		Objective: prior, PlanFingerprint: "old-plan", PublicationID: "old-pr", PublicationURL: "https://example.invalid/pr/1",
		WorkspacePath: "/worktrees/prior", WorkspaceBranch: "feature/prior", WorkspaceSourcePath: "/source", WorkspaceSourceID: "source-id", WorkspaceSourceRef: "refs/heads/main",
		PreviewFingerprint: "old-preview", Gates: []durable.GateEvidence{{Gate: "test", Revision: "old", Fingerprint: "old-test"}},
	}
	transition, _ := testprogram.StandardRegistry().Lookup("objective.bind")
	admission := protocol.Admission{Objective: next, Parameters: protocol.Parameters{{Name: "target_id", Value: string(next.TargetID)}, {Name: "delivery_id", Value: next.DeliveryID}}}
	if err := applyStateTransition(&state, admission, transition); err != nil {
		t.Fatal(err)
	}
	if state.Objective != next || state.Delivery != model.DeliveryUninitialized || state.Workspace != model.WorkspaceAbsent || state.Plan != model.PlanAbsent || state.Publication != model.PublicationNone || state.Verification != model.VerificationUnverified || state.Terminal != model.TerminalNonterminal || len(state.Gates) != 0 || state.PlanFingerprint != "" || state.PublicationID != "" || state.WorkspacePath != "" {
		t.Fatalf("new delivery inherited prior product state: %#v", state)
	}
}

func TestObjectiveBindRejectsDifferentDeliveryBeforeSafeTerminal(t *testing.T) {
	prior := model.Objective{ID: "prior", TargetID: model.ObjectiveOpenPR, DeliveryID: "prior-delivery"}
	next := model.Objective{ID: "next", TargetID: model.ObjectiveOpenPR, DeliveryID: "next-delivery"}
	state := durable.State{
		SchemaVersion: durable.StateSchemaVersion, RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryActive, Workspace: model.WorkspaceActive,
		Plan: model.PlanLocked, Configuration: model.ConfigurationVerified, Runtime: model.RuntimeVerified, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Objective: prior,
	}
	transition, _ := testprogram.StandardRegistry().Lookup("objective.bind")
	admission := protocol.Admission{Objective: next, Parameters: protocol.Parameters{{Name: "target_id", Value: string(next.TargetID)}, {Name: "delivery_id", Value: next.DeliveryID}}}
	if err := applyStateTransition(&state, admission, transition); err == nil || !strings.Contains(err.Error(), "prior delivery") {
		t.Fatalf("different active delivery bind result = %v", err)
	}
}

func TestDeclaredAssignmentReducesUnknownTransitionWithoutGoDispatch(t *testing.T) {
	state := durable.Default(model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree"}, testTime())
	transition := catalog.Transition{
		ID: "fixture.declared-state", TargetPhases: []model.ProtocolPhase{model.PhaseObserved},
		Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeOptionalPreserve},
		StateEffect: catalog.StateEffect{Kind: catalog.StateEffectAssignments, Assignments: []catalog.StateAssignment{
			{Facet: "phase", Value: stringPointer(string(model.PhaseObserved))},
		}},
	}
	if err := applyStateTransition(&state, protocol.Admission{}, transition); err != nil {
		t.Fatal(err)
	}
	if state.Phase != model.PhaseObserved || state.LastTransition != transition.ID {
		t.Fatalf("declared state effect did not run: %#v", state)
	}
}

func TestDeclaredAssignmentRefusesMissingAdmittedParameter(t *testing.T) {
	// control-law: assignment-parameter-sources-are-total-before-effect-preparation
	state := ownershipState()
	transition := transitionFixture("fixture.parameter-assignment", catalog.OriginControlProgram, true)
	transition.TargetPhases = []model.ProtocolPhase{model.PhaseActive}
	transition.StateEffect = catalog.StateEffect{Kind: catalog.StateEffectAssignments, Assignments: []catalog.StateAssignment{
		{Facet: "phase", ValueFrom: catalog.StateValueReference{Parameter: "required_phase"}},
	}}
	if err := applyStateTransition(&state, protocol.Admission{Objective: state.Objective}, transition); err == nil || !strings.Contains(err.Error(), "parameter \"required_phase\" is absent") {
		t.Fatalf("missing assignment parameter was not refused: %v", err)
	}
}

func TestStandardNativeStateHandlersAreRegistered(t *testing.T) {
	for _, transition := range testprogram.StandardRegistry().All() {
		if transition.StateEffect.Kind != catalog.StateEffectNative {
			continue
		}
		if _, ok := nativeStateHandlers[transition.StateEffect.NativeHandler]; !ok {
			t.Errorf("transition %s names unregistered native state handler %q", transition.ID, transition.StateEffect.NativeHandler)
		}
	}
}

func stringPointer(value string) *string { return &value }

func testTime() time.Time { return time.Unix(100, 0).UTC() }
