package softwaredelivery

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	planningpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/planningpackage"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

var planningPackageSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const planningPackagePlanOutputResolverPrefix = ParameterResolverPrefix + "planning-package-plan-output/"

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
	// The planning-package effect consumes exact foreground-work evidence. The
	// plan.create parameter contract is not part of this derived operation.
	admit.Parameters = nil
	admit.Parameters = []delivery.ParameterSpec{{Name: "plan_output", Required: true}}
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

func validatePlanningPackageWorkContract(work delivery.WorkContract, planOutputID string) error {
	outputs := make([]planningpackage.WorkOutput, 0, len(work.Outputs))
	var planOutput *delivery.WorkOutput
	for index := range work.Outputs {
		output := &work.Outputs[index]
		outputs = append(outputs, planningpackage.WorkOutput{ID: output.ID, Path: output.Path, MaxBytes: output.MaxBytes})
		if output.ID == planOutputID {
			planOutput = output
		}
	}
	if err := planningpackage.ValidateOutputPaths(outputs); err != nil {
		return err
	}
	if planOutput == nil || !planOutput.Required {
		return fmt.Errorf("planning-package admission requires designated output %q to exist exactly once and be required", planOutputID)
	}
	return nil
}

func planningPackagePlanOutput(bindings []controlprogram.TransitionParameterBinding) (string, error) {
	for _, binding := range bindings {
		if binding.Parameter != "plan_output" || binding.Producer.Kind != controlprogram.ParameterSourceTrustedResolver || binding.Producer.Binding == nil {
			continue
		}
		value, ok := strings.CutPrefix(binding.Producer.Binding.Reference, planningPackagePlanOutputResolverPrefix)
		if !ok || !planningPackageSegment.MatchString(value) {
			return "", fmt.Errorf("planning-package plan output binding is invalid")
		}
		return value, nil
	}
	return "", fmt.Errorf("planning-package admit requires an explicit plan_output binding")
}
