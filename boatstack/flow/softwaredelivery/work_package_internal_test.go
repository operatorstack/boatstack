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

func TestWorkPackageAdmitRequiresEstablishedEngagement(t *testing.T) {
	// control-law: generic CoreSystem engagement precedes package-domain work
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
		if transition.ID != WorkPackageAdmit {
			continue
		}
		for _, condition := range transition.SourceConditions {
			if condition.Facet != model.FacetEngagement {
				continue
			}
			if containsValue(condition.Values, string(model.EngagementDormant)) ||
				!containsValue(condition.Values, string(model.EngagementCommand)) ||
				!containsValue(condition.Values, string(model.EngagementActive)) {
				t.Fatalf("work-package engagement source = %v", condition.Values)
			}
			return
		}
		t.Fatal("work-package admission has no engagement source condition")
	}
	t.Fatal("work-package admission transition is missing")
}

func containsValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
