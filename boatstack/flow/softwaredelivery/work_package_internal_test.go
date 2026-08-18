package softwaredelivery

import (
	"context"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestPlanningPackagePromoteSupportsApprovedPlanObjective(t *testing.T) {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	base := make(map[string]delivery.Transition, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		base[string(transition.ID)] = transition
	}
	transitions, err := acceptedWorkTransitions(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range transitions {
		if transition.ID != PlanningPackagePromote {
			continue
		}
		for _, target := range transition.TargetIDs {
			if target == model.ObjectiveApprovedPlan {
				return
			}
		}
		t.Fatalf("planning promotion targets = %#v", transition.TargetIDs)
	}
	t.Fatal("planning promotion transition is missing")
}
