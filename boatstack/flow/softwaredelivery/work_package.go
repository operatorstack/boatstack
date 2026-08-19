package softwaredelivery

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	workpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/workpackage"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

var workPackageSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const planningPackagePlanOutputResolverPrefix = ParameterResolverPrefix + "planning-package-plan-output/"

const (
	WorkPackageAdmit       = "work.package.admit"
	WorkPackageApprove     = "work.package.approve"
	PlanningPackagePromote = "planning.package.promote"
)

func acceptedWorkTransitions(transitions map[string]delivery.Transition) ([]delivery.Transition, error) {
	clone := func(id string) (delivery.Transition, error) {
		transition, ok := transitions[id]
		if !ok {
			return delivery.Transition{}, fmt.Errorf("trusted accepted-work base %q is unavailable", id)
		}
		return transition, nil
	}

	admit, err := clone("plan.create")
	if err != nil {
		return nil, err
	}
	admit.ID, admit.Effect = WorkPackageAdmit, WorkPackageAdmit
	admit.Parameters = nil
	admit.LocalEffects = []delivery.EffectID{WorkPackageAdmit}
	admit.Prescription.Operation = WorkPackageAdmit
	admit.Prescription.ExpectedPostcondition = "a schema-valid work package is admitted"
	admit.SourcePredicate, admit.AdmissionPredicate, admit.TargetPredicate = "work-package-admit-source", "exact-work-and-transition-admission", "work-package-valid"
	admit.Verifier = "verifier:fresh-observation:" + WorkPackageAdmit
	admit.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "work-package-admit"}
	admit.SourceConditions = withFacetStatuses(
		withoutFacet(admit.SourceConditions, model.FacetPlan),
		model.FacetWorkPackage,
		[]model.FactStatus{model.FactKnown, model.FactStale},
		string(model.WorkPackageAbsent),
	)
	admit.SourceConditions = withFacetStatuses(
		withoutFacet(admit.SourceConditions, model.FacetTerminal),
		model.FacetTerminal,
		[]model.FactStatus{model.FactKnown},
		string(model.TerminalNonterminal), string(model.TerminalStale),
	)
	admit.TargetConditions = withFacet(withoutFacet(admit.TargetConditions, model.FacetPlan), model.FacetWorkPackage, string(model.WorkPackageValid))
	admit.TargetIDs = appendUniqueTarget(admit.TargetIDs, model.ObjectiveApprovedWorkPackage)
	admit.OwnedResources = []string{"work-package"}
	admit.Interruption.ResumptionPredicate = "recovery-contract-for:" + WorkPackageAdmit

	approve, err := clone("plan.approve")
	if err != nil {
		return nil, err
	}
	approve.ID, approve.Effect = WorkPackageApprove, WorkPackageApprove
	approve.LocalEffects = []delivery.EffectID{WorkPackageApprove}
	approve.Parameters = []delivery.ParameterSpec{{Name: "package_fingerprint", Required: true}}
	approve.Prescription.Operation = WorkPackageApprove
	approve.Prescription.ExpectedPostcondition = "the exact work package is approved"
	approve.SourcePredicate, approve.AdmissionPredicate, approve.TargetPredicate = "work-package-valid", "exact-package-approval", "work-package-approved"
	approve.Verifier = "verifier:fresh-observation:" + WorkPackageApprove
	approve.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "work-package-approve"}
	approve.SourceConditions = withFacet(withoutFacet(approve.SourceConditions, model.FacetPlan), model.FacetWorkPackage, string(model.WorkPackageValid))
	approve.TargetConditions = withFacet(withoutFacet(approve.TargetConditions, model.FacetPlan), model.FacetWorkPackage, string(model.WorkPackageApproved))
	approve.TargetIDs = appendUniqueTarget(approve.TargetIDs, model.ObjectiveApprovedWorkPackage)
	approve.OwnedResources = []string{"work-package-approval"}
	approve.Interruption.ResumptionPredicate = "recovery-contract-for:" + WorkPackageApprove

	promote, err := clone("plan.activate")
	if err != nil {
		return nil, err
	}
	promote.ID, promote.Effect = PlanningPackagePromote, PlanningPackagePromote
	promote.LocalEffects = []delivery.EffectID{PlanningPackagePromote}
	promote.Parameters = []delivery.ParameterSpec{{Name: "plan_output", Required: true}}
	promote.Prescription.Operation = PlanningPackagePromote
	promote.Prescription.ExpectedPostcondition = "one compiler-bound approved-package output is the canonical delivery plan"
	promote.SourcePredicate, promote.AdmissionPredicate, promote.TargetPredicate = "work-package-approved", "exact-package-output-promotion", "plan-approved"
	promote.Verifier = "verifier:fresh-observation:" + PlanningPackagePromote
	promote.StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "planning-package-promote"}
	promote.SourceConditions = withFacetStatuses(
		withFacet(withoutFacet(promote.SourceConditions, model.FacetPlan), model.FacetWorkPackage, string(model.WorkPackageApproved)),
		model.FacetPlan,
		[]model.FactStatus{model.FactKnown},
		string(model.PlanAbsent), string(model.PlanStale),
	)
	promote.SourceConditions = withFacetStatuses(
		withoutFacet(promote.SourceConditions, model.FacetTerminal),
		model.FacetTerminal,
		[]model.FactStatus{model.FactKnown},
		string(model.TerminalNonterminal), string(model.TerminalStale),
	)
	promote.TargetConditions = replaceFacet(promote.TargetConditions, model.FacetPlan, string(model.PlanApproved))
	promote.TargetPhases = appendUniquePhase(promote.TargetPhases, model.PhaseTerminal)
	promote.TargetIDs = appendUniqueTarget(promote.TargetIDs, model.ObjectiveApprovedPlan)
	promote.OwnedResources = []string{"plan", "planning-package-promotion"}
	promote.Interruption.ResumptionPredicate = "recovery-contract-for:" + PlanningPackagePromote

	return []delivery.Transition{admit, approve, promote}, nil
}

func withoutFacet(values []delivery.FacetCondition, facet model.FacetName) []delivery.FacetCondition {
	result := make([]delivery.FacetCondition, 0, len(values))
	for _, value := range values {
		if value.Facet != facet {
			result = append(result, value)
		}
	}
	return result
}

func withFacet(values []delivery.FacetCondition, facet model.FacetName, value string) []delivery.FacetCondition {
	return append(values, delivery.FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: []string{value}})
}

func withFacetStatuses(values []delivery.FacetCondition, facet model.FacetName, statuses []model.FactStatus, valuesAllowed ...string) []delivery.FacetCondition {
	return append(values, delivery.FacetCondition{
		Facet: facet, Statuses: append([]model.FactStatus(nil), statuses...), Values: append([]string(nil), valuesAllowed...),
	})
}

func replaceFacet(values []delivery.FacetCondition, facet model.FacetName, value string) []delivery.FacetCondition {
	result := append([]delivery.FacetCondition(nil), values...)
	for index := range result {
		if result[index].Facet == facet {
			result[index].Statuses = []model.FactStatus{model.FactKnown}
			result[index].Values = []string{value}
		}
	}
	return result
}

func appendUniquePhase(values []model.ProtocolPhase, phase model.ProtocolPhase) []model.ProtocolPhase {
	for _, value := range values {
		if value == phase {
			return values
		}
	}
	return append(values, phase)
}

func appendUniqueTarget(values []model.TargetID, target model.TargetID) []model.TargetID {
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}

func validateWorkPackageContract(work delivery.WorkContract) error {
	portable := workpackage.WorkContract{ID: work.ID, Fingerprint: work.Fingerprint, Instructions: workpackage.Asset{Path: work.InstructionPath, SHA256: work.InstructionSHA256, Content: work.InstructionContent}}
	for _, input := range work.Inputs {
		portable.Inputs = append(portable.Inputs, workpackage.WorkInput{ID: input.ID, Producer: input.Producer})
	}
	for _, output := range work.Outputs {
		item := workpackage.WorkOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: output.Required, MaxBytes: output.MaxBytes}
		if output.GuidancePath != "" {
			item.Guidance = &workpackage.Asset{Path: output.GuidancePath, SHA256: output.GuidanceSHA256, Content: output.GuidanceContent}
		}
		if output.SchemaPath != "" {
			item.Schema = &workpackage.Asset{Path: output.SchemaPath, SHA256: output.SchemaSHA256, Content: output.SchemaContent}
		}
		portable.Outputs = append(portable.Outputs, item)
	}
	if err := workpackage.ValidateOutputPaths(portable.Outputs); err != nil {
		return err
	}
	return workpackage.ValidateContractMetadata(portable)
}

func validatePlanningPackageWorkContract(work delivery.WorkContract, planOutputID string) error {
	if err := validateWorkPackageContract(work); err != nil {
		return err
	}
	for _, output := range work.Outputs {
		if output.ID == planOutputID && output.Required {
			return nil
		}
	}
	return fmt.Errorf("planning package requires designated output %q to exist exactly once and be required", planOutputID)
}

func planningPackagePlanOutput(bindings []controlprogram.TransitionParameterBinding) (string, error) {
	for _, binding := range bindings {
		if binding.Parameter != "plan_output" || binding.Producer.Kind != controlprogram.ParameterSourceTrustedResolver || binding.Producer.Binding == nil {
			continue
		}
		value, ok := strings.CutPrefix(binding.Producer.Binding.Reference, planningPackagePlanOutputResolverPrefix)
		if !ok || !workPackageSegment.MatchString(value) {
			return "", fmt.Errorf("planning-package plan output binding is invalid")
		}
		return value, nil
	}
	return "", fmt.Errorf("planning.package.promote requires an explicit plan_output binding")
}
