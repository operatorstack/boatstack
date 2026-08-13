package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestPlanEffectRejectsSourceChangedAfterEntryBinding(t *testing.T) {
	// control-law: plan bytes bound to a Flow run cannot change before effect preparation
	repository := t.TempDir()
	source := filepath.Join(t.TempDir(), "plan.md")
	bound := []byte("# Plan A\n")
	if err := os.WriteFile(source, bound, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := sha256Bytes(bound)
	if err := os.WriteFile(source, []byte("# Plan B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := durable.State{}
	mutations, err := prepareArtifacts(ports.ControllerLayout{RepositoryRoot: repository}, protocol.Admission{
		Objective: model.Objective{DeliveryID: "delivery-one"},
		Parameters: protocol.Parameters{
			{Name: "source_path", Value: source},
			{Name: "source_fingerprint", Value: expected},
		},
	}, catalog.Transition{ID: "plan.create"}, &state)
	if err == nil || !strings.Contains(err.Error(), "changed after entry binding") {
		t.Fatalf("replaced plan result = %v", err)
	}
	if len(mutations) != 0 || state.PlanFingerprint != "" {
		t.Fatalf("replaced plan prepared effects: mutations=%#v state=%#v", mutations, state)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "plans", "delivery-one.source")); !os.IsNotExist(statErr) {
		t.Fatalf("replaced plan created a managed effect: %v", statErr)
	}
}

func TestWorkspacePlanTransferCopiesOnlyRegularRuntimeOwnedArtifacts(t *testing.T) {
	repository := t.TempDir()
	workspace := t.TempDir()
	plan := "# Bound plan\n"
	for path, contents := range map[string]string{
		filepath.Join(repository, ".boatstack", "plans", "delivery-one.source"):   plan,
		filepath.Join(repository, ".boatstack", "approvals", "delivery-one.json"): `{"schema_version":1,"delivery_id":"delivery-one","plan_fingerprint":"` + sha256Bytes([]byte(plan)) + `","actor":"reviewer","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	approvalRaw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "approvals", "delivery-one.json"))
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := prepareWorkspacePlanTransfer(repository, workspace, "delivery-one", sha256Bytes([]byte(plan)), sha256Bytes(approvalRaw))
	if err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 2 {
		t.Fatalf("transfer mutations = %#v", mutations)
	}
	for _, mutation := range mutations {
		if !strings.HasPrefix(mutation.Path, filepath.Join(workspace, ".boatstack")+string(filepath.Separator)) || !mutation.PriorExists && len(mutation.Target) == 0 {
			t.Fatalf("invalid transfer mutation: %#v", mutation)
		}
	}
}

func TestWorkspacePlanTransferRejectsStaleOrMissingBoundArtifacts(t *testing.T) {
	repository := t.TempDir()
	workspace := t.TempDir()
	planPath := filepath.Join(repository, ".boatstack", "plans", "delivery-one.source")
	approvalPath := filepath.Join(repository, ".boatstack", "approvals", "delivery-one.json")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(approvalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	bound := []byte("# Bound plan\n")
	if err := os.WriteFile(planPath, bound, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, []byte(`{"schema_version":1,"delivery_id":"delivery-one","plan_fingerprint":"`+sha256Bytes(bound)+`","actor":"reviewer","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte("# Substituted plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	approvalRaw, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := prepareWorkspacePlanTransfer(repository, workspace, "delivery-one", sha256Bytes(bound), sha256Bytes(approvalRaw))
	if err == nil || len(mutations) != 0 {
		t.Fatalf("stale plan transfer = %#v, %v", mutations, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".boatstack", "plans", "delivery-one.source")); !os.IsNotExist(err) {
		t.Fatalf("stale plan created destination artifact: %v", err)
	}
	if err := os.WriteFile(planPath, bound, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, []byte(`{"schema_version":1,"delivery_id":"delivery-one","plan_fingerprint":"`+sha256Bytes(bound)+`","actor":"substitute","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareWorkspacePlanTransfer(repository, workspace, "delivery-one", sha256Bytes(bound), sha256Bytes(approvalRaw))
	if err == nil || len(mutations) != 0 {
		t.Fatalf("substituted approval transfer = %#v, %v", mutations, err)
	}
	if err := os.Remove(approvalPath); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareWorkspacePlanTransfer(repository, workspace, "delivery-one", sha256Bytes(bound), sha256Bytes(approvalRaw))
	if err == nil || len(mutations) != 0 {
		t.Fatalf("missing approval transfer = %#v, %v", mutations, err)
	}
}
