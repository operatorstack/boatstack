package softwaredelivery

import (
	"context"
	"fmt"
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
		{name: "missing plan", outputs: []delivery.WorkOutput{{ID: "questions", Path: "questions.md", Required: true, MaxBytes: 1}}, want: `designated output "plan"`},
		{name: "optional plan", outputs: []delivery.WorkOutput{{ID: "plan", Path: "plan.md", MaxBytes: 1}}, want: `designated output "plan"`},
		{name: "manifest collision", outputs: []delivery.WorkOutput{{ID: "plan", Path: "manifest.json", Required: true, MaxBytes: 1}}, want: "reserved"},
		{name: "approval descendant collision", outputs: []delivery.WorkOutput{{ID: "plan", Path: "plan.md", Required: true, MaxBytes: 1}, {ID: "evidence", Path: "approval.json/evidence", Required: true, MaxBytes: 1}}, want: "reserved"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePlanningPackageWorkContract(delivery.WorkContract{ID: "planning", Outputs: test.outputs}, "plan")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAcceptedWorkUsesDistinctFacetAndOnlyPromotionOwnsPlan(t *testing.T) {
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
	for _, transition := range transitions[:2] {
		if containsString(transition.OwnedResources, "plan") {
			t.Fatalf("%s unexpectedly owns the canonical plan resource: %v", transition.ID, transition.OwnedResources)
		}
		for _, condition := range append(append([]delivery.FacetCondition(nil), transition.SourceConditions...), transition.TargetConditions...) {
			if condition.Facet == model.FacetPlan {
				t.Fatalf("%s unexpectedly reaches plan state: %#v", transition.ID, condition)
			}
		}
	}
	if !containsString(transitions[2].OwnedResources, "plan") {
		t.Fatalf("%s does not own the canonical plan resource: %v", transitions[2].ID, transitions[2].OwnedResources)
	}
	var got []string
	for _, condition := range transitions[0].TargetConditions {
		if condition.Facet == model.FacetWorkPackage {
			got = condition.Values
		}
	}
	if !containsString(got, string(model.WorkPackageValid)) {
		t.Fatalf("work-package admission target = %v", got)
	}
	for _, condition := range transitions[0].SourceConditions {
		if condition.Facet != model.FacetWorkPackage {
			continue
		}
		if len(condition.Values) != 1 || condition.Values[0] != string(model.WorkPackageAbsent) ||
			!containsStatus(condition.Statuses, model.FactKnown) || !containsStatus(condition.Statuses, model.FactStale) {
			t.Fatalf("work-package admission source = statuses %v values %v", condition.Statuses, condition.Values)
		}
	}
	foundRecoverablePlan := false
	for _, condition := range transitions[2].SourceConditions {
		if condition.Facet == model.FacetPlan && containsString(condition.Values, string(model.PlanAbsent)) && containsString(condition.Values, string(model.PlanStale)) {
			foundRecoverablePlan = true
		}
	}
	if !foundRecoverablePlan {
		t.Fatal("planning promotion cannot recover a stale canonical plan")
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

func containsStatus(values []model.FactStatus, want model.FactStatus) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPlanningPackageWorkAcceptsRequiredPlanAndDomainOutputs(t *testing.T) {
	work := delivery.WorkContract{ID: "planning", Outputs: []delivery.WorkOutput{
		{ID: "plan", Path: "plan.md", Required: true, MaxBytes: 1},
		{ID: "questions", Path: "questions.md", Required: true, MaxBytes: 1},
	}}
	if err := validatePlanningPackageWorkContract(work, "plan"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkPackageTreatsDomainNamedOutputsUniformly(t *testing.T) {
	work := delivery.WorkContract{ID: "accepted-work", Outputs: []delivery.WorkOutput{
		{ID: "architecture-plan", Path: "architecture-plan.md", Required: true, MaxBytes: 1},
		{ID: "tasks", Path: "tasks.json", Required: true, MaxBytes: 1},
		{ID: "verification-contract", Path: "verification.json", Required: true, MaxBytes: 1},
		{ID: "journey", Path: "journey.md", Required: true, MaxBytes: 1},
	}}
	if err := validateWorkPackageContract(work); err != nil {
		t.Fatal(err)
	}
}

func TestPlanningPackageWorkRejectsContractAbovePortableMetadataBound(t *testing.T) {
	work := delivery.WorkContract{ID: "planning", InstructionPath: "instructions.md", InstructionSHA256: strings.Repeat("a", 64), InstructionContent: "Plan."}
	for index := 0; index < 17; index++ {
		id := fmt.Sprintf("output-%02d", index)
		work.Outputs = append(work.Outputs, delivery.WorkOutput{
			ID: id, Path: id + ".json", MediaType: "application/json", Required: true, MaxBytes: 1,
			SchemaPath: id + ".schema.json", SchemaSHA256: strings.Repeat("b", 64), SchemaContent: strings.Repeat("x", 1<<20),
		})
	}
	if err := validatePlanningPackageWorkContract(work, "output-00"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized portable contract = %v", err)
	}
}
