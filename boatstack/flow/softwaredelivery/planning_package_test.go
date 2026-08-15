package softwaredelivery

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestPlanningPackageWorkRequiresOwnedPlanOutput(t *testing.T) {
	tests := []struct {
		name    string
		outputs []delivery.WorkOutput
		want    string
	}{
		{name: "missing plan", outputs: []delivery.WorkOutput{{ID: "questions", Path: "questions.md", Required: true}}, want: `required output named "plan"`},
		{name: "optional plan", outputs: []delivery.WorkOutput{{ID: "plan", Path: "plan.md"}}, want: `required output named "plan"`},
		{name: "manifest collision", outputs: []delivery.WorkOutput{{ID: "plan", Path: "manifest.json", Required: true}}, want: "runtime-owned"},
		{name: "approval descendant collision", outputs: []delivery.WorkOutput{{ID: "plan", Path: "plan.md", Required: true}, {ID: "evidence", Path: "approval.json/evidence", Required: true}}, want: "runtime-owned"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePlanningPackageWorkContract(delivery.WorkContract{ID: "planning", Outputs: test.outputs})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPlanningPackageUsesDistinctPlanStateAndSharedPlanLease(t *testing.T) {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	base := make(map[string]delivery.Transition, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		base[string(transition.ID)] = transition
	}
	transitions, err := planningPackageTransitions(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range transitions {
		if !containsString(transition.OwnedResources, "plan") {
			t.Fatalf("%s does not serialize on the canonical plan resource: %v", transition.ID, transition.OwnedResources)
		}
	}
	var got []string
	for _, condition := range transitions[0].TargetConditions {
		if condition.Facet == model.FacetPlan {
			got = condition.Values
		}
	}
	if !containsString(got, string(model.PlanPackageValid)) || containsString(got, string(model.PlanValid)) {
		t.Fatalf("planning-package admission target = %v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPlanningPackageWorkAcceptsRequiredPlanAndDomainOutputs(t *testing.T) {
	work := delivery.WorkContract{ID: "planning", Outputs: []delivery.WorkOutput{
		{ID: "plan", Path: "plan.md", Required: true},
		{ID: "questions", Path: "questions.md", Required: true},
	}}
	if err := validatePlanningPackageWorkContract(work); err != nil {
		t.Fatal(err)
	}
}
