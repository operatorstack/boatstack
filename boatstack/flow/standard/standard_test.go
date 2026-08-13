package standard_test

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
)

func TestManifestOwnsOnlyStandardDeliverySemantics(t *testing.T) {
	// control-law: standard-flow-declarations-live-outside-kernel-mechanism
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != standard.ID || manifest.Version != standard.Version || len(manifest.Transitions) != 30 {
		t.Fatalf("StandardFlow identity/count = %s@%s/%d", manifest.ID, manifest.Version, len(manifest.Transitions))
	}
	for _, transition := range manifest.Transitions {
		id := string(transition.ID)
		if !hasPrefix(id, "plan.", "workspace.", "gate.", "evidence.", "delivery.", "publication.") {
			t.Errorf("StandardFlow owns non-delivery transition %s", id)
		}
	}
	if len(manifest.ObjectiveContracts) != 5 {
		t.Fatalf("objective contracts = %d, want 5", len(manifest.ObjectiveContracts))
	}
	for _, objective := range []delivery.TargetID{delivery.ObjectiveApprovedPlan, delivery.ObjectiveVerified, delivery.ObjectiveOpenPR, delivery.ObjectiveMerged, delivery.ObjectiveAbandoned} {
		found := false
		for _, contract := range manifest.ObjectiveContracts {
			found = found || contract.TargetID == objective
		}
		if !found {
			t.Errorf("missing objective contract %s", objective)
		}
	}
}

func hasPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
