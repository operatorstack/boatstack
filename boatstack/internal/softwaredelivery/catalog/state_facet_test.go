package catalog

import (
	"slices"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestKernelOwnsDurableStateFacetPolicies(t *testing.T) {
	fixtures := []struct {
		id     TransitionID
		writes []model.StateFacet
	}{
		{"installation.update", []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}},
		{"installation.reconcile-update", []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation, model.StateFacetProgram}},
		{"catalog.reconcile", []model.StateFacet{model.StateFacetControl, model.StateFacetProgram}},
		{"objective.bind", []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}},
		{"recovery.escalate", []model.StateFacet{model.StateFacetControl}},
	}
	for _, fixture := range fixtures {
		policy, err := DurableStateFacetPolicy(Transition{ID: fixture.id, Class: EventOwnedLocal, Origin: TransitionOrigin{Kind: OriginCoreSystem}, OwnedFacets: fixture.writes})
		if err != nil {
			t.Fatalf("%s: %v", fixture.id, err)
		}
		if !slices.Equal(policy.Writes, fixture.writes) {
			t.Fatalf("%s writes=%v, want %v", fixture.id, policy.Writes, fixture.writes)
		}
		if !slices.Contains(policy.Reads, model.StateFacetProduct) {
			t.Fatalf("%s lost read/write separation evidence", fixture.id)
		}
	}
}

func TestRepositoryProgramCannotSelfGrantInstallationFacet(t *testing.T) {
	transition := Transition{
		ID: "repository-program/advance", Class: EventOwnedLocal, RuntimeExecution: true,
		Origin:      TransitionOrigin{Kind: OriginControlProgram, ID: "repository-program", Version: "1", ManifestFingerprint: "manifest"},
		OwnedFacets: []model.StateFacet{model.StateFacetInstallation},
	}
	if policy, err := DurableStateFacetPolicy(transition); err == nil || slices.Contains(policy.Writes, model.StateFacetInstallation) {
		t.Fatalf("repository program received installation ownership: %v / %v", policy.Writes, err)
	}
}

func TestControllableTransitionCannotOmitControlFacet(t *testing.T) {
	transition := Transition{
		ID: "repository-program/advance", Class: EventOwnedLocal,
		Origin:      TransitionOrigin{Kind: OriginControlProgram, ID: "repository-program", Version: "1", ManifestFingerprint: "manifest"},
		OwnedFacets: []model.StateFacet{model.StateFacetProduct},
	}
	if policy, err := DurableStateFacetPolicy(transition); err == nil || len(policy.Writes) != 0 {
		t.Fatalf("product-only transition received a writable state envelope: %v / %v", policy.Writes, err)
	}
}
