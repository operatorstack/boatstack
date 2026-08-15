package durable

import (
	"bytes"
	"encoding/json"
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

func TestDecodeStatePromotesSchemaFiveWithoutChangingPriorBytes(t *testing.T) {
	// control-law: forward state migration is read-only until a transaction commits
	state := State{
		SchemaVersion: StateSchemaVersion, RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Revision: 7,
		Phase: model.PhaseActive, Engagement: model.EngagementActive, Delivery: model.DeliveryApproved, Workspace: model.WorkspaceAbsent,
		Plan: model.PlanApproved, Configuration: model.ConfigurationUnsupported, Runtime: model.RuntimeAbsent, Publication: model.PublicationNone,
		Verification: model.VerificationUnverified, Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal,
		Objective: model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}, PlanFingerprint: "legacy-plan", UpdatedAt: time.Unix(1, 0).UTC(),
	}
	current, err := EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(current, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["schema_version"] = float64(priorStateSchemaVersion)
	delete(legacy, "control_bundle_fingerprint")
	prior, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	prior = append(prior, '\n')
	rollback := append([]byte(nil), prior...)

	decoded, err := DecodeState(prior)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != StateSchemaVersion || decoded.Revision != state.Revision || decoded.Objective != state.Objective || decoded.PlanFingerprint != state.PlanFingerprint {
		t.Fatalf("promoted state = %#v", decoded)
	}
	if !bytes.Equal(prior, rollback) {
		t.Fatal("schema promotion changed the prior rollback bytes")
	}
}

func TestDecodeStateRejectsSchemaFiveWithSchemaSixControlBundle(t *testing.T) {
	raw := []byte(`{"schema_version":5,"repository_id":"repo","git_common_id":"common","worktree_id":"worktree","program_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","control_bundle_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","revision":1,"phase":"ACTIVE","engagement":"active","delivery":"approved","workspace":"absent","plan":"approved","configuration":"unsupported","runtime":"absent","publication":"none","verification":"unverified","recovery":"none","transaction":"none","terminal":"nonterminal","objective":{},"updated_at":"1970-01-01T00:00:01Z"}`)
	if _, err := DecodeState(raw); err == nil {
		t.Fatal("schema-5 state smuggled schema-6 control-bundle fields")
	}
}
