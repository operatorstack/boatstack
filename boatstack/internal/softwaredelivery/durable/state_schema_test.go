package durable

import (
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestStateRejectsPriorObjectiveSchema(t *testing.T) {
	state := State{SchemaVersion: StateSchemaVersion - 1}
	if err := state.Validate(); err == nil {
		t.Fatal("prior objective state schema was accepted")
	}
}

func TestStateSchemaPermitsLegacyApprovedStateWithoutApprovalFingerprint(t *testing.T) {
	state := State{
		SchemaVersion: StateSchemaVersion, RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Revision: 1,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryApproved, Workspace: model.WorkspaceAbsent,
		Plan: model.PlanApproved, Configuration: model.ConfigurationUnsupported, Runtime: model.RuntimeAbsent, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Objective: model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}, PlanFingerprint: "legacy-plan", UpdatedAt: time.Unix(1, 0).UTC(),
	}
	raw, err := EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ApprovalFingerprint != "" {
		t.Fatalf("legacy approval fingerprint = %q", decoded.ApprovalFingerprint)
	}
}
