package softwaredelivery

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

var planningPackageSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	PlanningPackageAdmit   = "planning.package.admit"
	PlanningPackageApprove = "planning.package.approve"
	PlanningPackagePromote = "planning.package.promote"
)

// planningPackageTransitions derives optional trusted mechanisms from the
// standard delivery primitives. Repositories decide whether these transitions
// belong to their Flow and attach their own foreground-work contract to admit.
func planningPackageTransitions(transitions map[string]delivery.Transition) ([]delivery.Transition, error) {
	clone := func(id string) (delivery.Transition, error) {
		transition, ok := transitions[id]
		if !ok {
			return delivery.Transition{}, fmt.Errorf("trusted planning-package base %q is unavailable", id)
		}
		return transition, nil
	}
	admit, err := clone("plan.create")
	if err != nil {
		return nil, err
	}
	admit.ID, admit.Effect = PlanningPackageAdmit, PlanningPackageAdmit
	admit.LocalEffects = []delivery.EffectID{PlanningPackageAdmit}
	admit.Prescription.Operation = PlanningPackageAdmit
	admit.Prescription.ExpectedPostcondition = "a schema-valid planning package is admitted"
	admit.SourcePredicate, admit.AdmissionPredicate, admit.TargetPredicate = "planning-package-admit-source", "exact-work-and-transition-admission", "planning-package-valid"
	admit.Verifier = "verifier:fresh-observation:" + PlanningPackageAdmit
	admit.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "planning-package-admit"}
	admit.TargetConditions = replacePlanCondition(admit.TargetConditions, model.PlanPackageValid)
	admit.OwnedResources = []string{"plan", "planning-package"}
	admit.Interruption.ResumptionPredicate = "recovery-contract-for:" + PlanningPackageAdmit

	approve, err := clone("plan.approve")
	if err != nil {
		return nil, err
	}
	approve.ID, approve.Effect = PlanningPackageApprove, PlanningPackageApprove
	approve.LocalEffects = []delivery.EffectID{PlanningPackageApprove}
	approve.Parameters = []delivery.ParameterSpec{{Name: "package_fingerprint", Required: true}}
	approve.Prescription.Operation = PlanningPackageApprove
	approve.Prescription.ExpectedPostcondition = "the exact planning package is approved"
	approve.SourcePredicate, approve.AdmissionPredicate, approve.TargetPredicate = "planning-package-valid", "exact-package-approval", "planning-package-approved"
	approve.Verifier = "verifier:fresh-observation:" + PlanningPackageApprove
	approve.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "planning-package-approve"}
	approve.SourceConditions = replacePlanCondition(approve.SourceConditions, model.PlanPackageValid)
	approve.TargetConditions = replacePlanCondition(approve.TargetConditions, model.PlanPackageApproved)
	approve.OwnedResources = []string{"plan", "planning-package-approval"}
	approve.Interruption.ResumptionPredicate = "recovery-contract-for:" + PlanningPackageApprove

	promote, err := clone("plan.activate")
	if err != nil {
		return nil, err
	}
	promote.ID, promote.Effect = PlanningPackagePromote, PlanningPackagePromote
	promote.LocalEffects = []delivery.EffectID{PlanningPackagePromote}
	promote.Prescription.Operation = PlanningPackagePromote
	promote.Prescription.ExpectedPostcondition = "the approved package plan is the canonical delivery plan"
	promote.SourcePredicate, promote.AdmissionPredicate, promote.TargetPredicate = "planning-package-approved", "exact-package-promotion", "plan-approved"
	promote.Verifier = "verifier:fresh-observation:" + PlanningPackagePromote
	promote.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "planning-package-promote"}
	promote.SourceConditions = replacePlanCondition(promote.SourceConditions, model.PlanPackageApproved)
	promote.TargetConditions = replacePlanCondition(promote.TargetConditions, model.PlanApproved)
	promote.TargetIDs = append([]model.TargetID(nil), admit.TargetIDs...)
	promote.OwnedResources = []string{"plan", "planning-package-promotion"}
	promote.Interruption.ResumptionPredicate = "recovery-contract-for:" + PlanningPackagePromote

	return []delivery.Transition{admit, approve, promote}, nil
}

func replacePlanCondition(values []delivery.FacetCondition, state model.PlanState) []delivery.FacetCondition {
	result := append([]delivery.FacetCondition(nil), values...)
	for index := range result {
		if result[index].Facet == model.FacetPlan {
			result[index].Statuses = []model.FactStatus{model.FactKnown}
			result[index].Values = []string{string(state)}
		}
	}
	return result
}

func validatePlanningPackageWorkContract(work delivery.WorkContract) error {
	var planOutput *delivery.WorkOutput
	for index := range work.Outputs {
		output := &work.Outputs[index]
		for _, reserved := range []string{"manifest.json", "approval.json"} {
			if output.Path == reserved || strings.HasPrefix(output.Path, reserved+"/") || strings.HasPrefix(reserved, output.Path+"/") {
				return fmt.Errorf("output %q conflicts with runtime-owned planning-package metadata %q", output.ID, reserved)
			}
		}
		if output.ID == "plan" {
			planOutput = output
		}
	}
	if planOutput == nil || !planOutput.Required {
		return fmt.Errorf("planning-package admission requires a required output named %q", "plan")
	}
	if path.Clean(planOutput.Path) != planOutput.Path || planOutput.Path == "." {
		return fmt.Errorf("planning-package plan output path is not canonical")
	}
	return nil
}

// PlanningPackageFingerprint reads the current repository package projection.
// Effect preflight independently verifies the complete manifest before any
// mutation; this helper only binds the candidate parameter for continuation.
func PlanningPackageFingerprint(repository, deliveryID string) (string, error) {
	if !planningPackageSegment.MatchString(deliveryID) {
		return "", fmt.Errorf("invalid planning package delivery identity")
	}
	raw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "planning-packages", deliveryID, "manifest.json"))
	if err != nil {
		return "", err
	}
	var projection struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil || len(projection.Fingerprint) != 64 {
		return "", fmt.Errorf("planning package manifest has no valid fingerprint")
	}
	return projection.Fingerprint, nil
}
