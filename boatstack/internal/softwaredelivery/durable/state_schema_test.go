package durable

import (
	"strings"
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
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryApproved, Workspace: model.WorkspaceAbsent, WorkPackage: model.WorkPackageAbsent,
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

func TestDecodeStateRejectsSchemaSevenWithoutMigration(t *testing.T) {
	state := State{
		SchemaVersion: StateSchemaVersion, RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Revision: 7,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryApproved, Workspace: model.WorkspaceAbsent, WorkPackage: model.WorkPackageAbsent,
		Plan: model.PlanApproved, Configuration: model.ConfigurationUnsupported, Runtime: model.RuntimeAbsent, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Objective: model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}, PlanFingerprint: "legacy-plan", UpdatedAt: time.Unix(1, 0).UTC(),
	}
	current, err := EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	prior := []byte(strings.Replace(string(current), `"schema_version": 8`, `"schema_version": 7`, 1))
	if _, err := DecodeState(prior); err == nil {
		t.Fatal("schema 7 was migrated instead of rejected")
	}
}

func TestDecodeStateRejectsBaseSchemaWithNewHumanIdentityRole(t *testing.T) {
	raw := []byte(`{"schema_version":6,"repository_id":"repo","git_common_id":"common","worktree_id":"worktree","program_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","program_human_identity_role":"developer","planning_package_fingerprint":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","control_bundle_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","revision":1,"phase":"ACTIVE","engagement":"active","delivery":"approved","workspace":"absent","plan":"approved","configuration":"unsupported","runtime":"absent","publication":"none","verification":"unverified","recovery":"none","transaction":"none","terminal":"nonterminal","objective":{},"updated_at":"1970-01-01T00:00:01Z"}`)
	if _, err := DecodeState(raw); err == nil {
		t.Fatal("base schema state smuggled the new human identity role")
	}
}

func TestDecodeStateRejectsUnreleasedSchemaFive(t *testing.T) {
	raw := []byte(`{"schema_version":5,"repository_id":"repo","git_common_id":"common","worktree_id":"worktree","revision":1,"phase":"ACTIVE","engagement":"active","delivery":"approved","workspace":"absent","plan":"approved","configuration":"unsupported","runtime":"absent","publication":"none","verification":"unverified","recovery":"none","transaction":"none","terminal":"nonterminal","objective":{},"updated_at":"1970-01-01T00:00:01Z"}`)
	if _, err := DecodeState(raw); err == nil {
		t.Fatal("unreleased schema 5 was accepted as a migration predecessor")
	}
}
