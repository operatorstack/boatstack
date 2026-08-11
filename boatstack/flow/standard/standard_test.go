package standard_test

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
)

func TestManifestOwnsOnlyStandardDeliverySemantics(t *testing.T) {
	// control-law: standard-flow-declarations-live-outside-kernel-mechanism
	manifest, err := standard.Definition().FlowManifest(context.Background())
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
	if len(manifest.GoalContracts) != 5 {
		t.Fatalf("goal contracts = %d, want 5", len(manifest.GoalContracts))
	}
	for _, goal := range []control.GoalKind{control.GoalApprovedPlan, control.GoalVerified, control.GoalOpenPR, control.GoalMerged, control.GoalAbandoned} {
		found := false
		for _, contract := range manifest.GoalContracts {
			found = found || contract.GoalKind == goal
		}
		if !found {
			t.Errorf("missing goal contract %s", goal)
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
