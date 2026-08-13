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
