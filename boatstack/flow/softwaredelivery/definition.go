package softwaredelivery

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

// Definition is a trusted adapter. Repository IR selects bindings and adds
// conjunctive predicates; it never supplies native handlers or effects.
type Definition struct {
	compiled controlprogram.Compiled
	resolver Resolver
}

// EntryObjective binds a repository-owned target identity and terminal
// predicate to the trusted software-delivery objective class whose operators
// may make progress toward it.
type EntryObjective struct {
	TargetID     model.TargetID
	TrustedClass model.TargetID
	Contract     delivery.ObjectiveContract
}

func NewDefinition(compiled controlprogram.Compiled, resolver Resolver) (Definition, error) {
	for _, entry := range compiled.Document.Entries {
		for _, authority := range entry.Requires.Authorities {
			if authority != "human" {
				return Definition{}, fmt.Errorf("entry %q activation authority %q has no trusted software-delivery producer", entry.ID, authority)
			}
		}
	}
	if _, err := ObjectiveForEntry(context.Background(), compiled, resolver, compiled.Document.Entries[0].ID); err != nil {
		return Definition{}, err
	}
	return Definition{compiled: compiled, resolver: resolver}, nil
}

func (d Definition) Fingerprint() string { return d.compiled.Fingerprint }

func (d Definition) RuntimeManifest(ctx context.Context) (delivery.ProgramRuntimeManifest, error) {
	base, err := standard.Definition().RuntimeManifest(ctx)
	if err != nil {
		return delivery.ProgramRuntimeManifest{}, err
	}
	operatorByID := map[string]controlprogram.Operator{}
	for _, operator := range d.compiled.Document.Operators {
		operatorByID[operator.ID] = operator
	}
	workByID := map[string]controlprogram.WorkContract{}
	for _, work := range d.compiled.Document.Work {
		workByID[work.ID] = work
	}
	objectives := map[model.TargetID]EntryObjective{}
	contracts := map[model.TargetID]delivery.ObjectiveContract{}
	entriesByTarget := map[model.TargetID][]controlprogram.Entry{}
	addAcceptedWorkPackageObjective(&base)
	for _, entry := range d.compiled.Document.Entries {
		objective, objectiveErr := objectiveContractForEntry(d.compiled, base, entry.ID)
		if objectiveErr != nil {
			return delivery.ProgramRuntimeManifest{}, objectiveErr
		}
		objectives[objective.TargetID], contracts[objective.TargetID] = objective, objective.Contract
		entriesByTarget[objective.TargetID] = append(entriesByTarget[objective.TargetID], entry)
	}

	selected := make([]delivery.Transition, 0, len(d.compiled.Document.Transitions))
	seen := map[delivery.TransitionID]bool{}
	var admittedPackageWork *delivery.WorkContract
	var promotionPlanOutput string
	for _, declaration := range d.compiled.Document.Transitions {
		operator := operatorByID[declaration.Operator]
		if operator.Binding == nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("software-delivery transition %q requires a trusted binding", declaration.ID)
		}
		transition, ok := d.resolver.Transition(operator.Binding.Reference)
		if !ok || seen[transition.ID] {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("software-delivery binding %q is unknown or reused", operator.Binding.Reference)
		}
		if string(transition.ID) != declaration.ID {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q does not match trusted binding identity %q", declaration.ID, transition.ID)
		}
		seen[transition.ID] = true
		guard, predicateErr := conjunctiveConditions(declaration.Guard)
		if predicateErr != nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q guard does not strengthen the trusted binding: %w", declaration.ID, predicateErr)
		}
		target, predicateErr := conjunctiveConditions(declaration.Target)
		if predicateErr != nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q target does not strengthen the trusted binding: %w", declaration.ID, predicateErr)
		}
		if targetErr := requireImpliedTarget(transition.TargetConditions, target); targetErr != nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q target exceeds the trusted binding: %w", declaration.ID, targetErr)
		}
		transition.SourceConditions = append(transition.SourceConditions, guard...)
		transition.TargetConditions = append(transition.TargetConditions, target...)
		transition.Priority = declaration.Priority
		transition.ExecutionContext = operator.ExecutionContext
		if declaration.Work != "" {
			work, exists := workByID[declaration.Work]
			if !exists {
				return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q references unknown foreground work %q", declaration.ID, declaration.Work)
			}
			transition.Work, err = RuntimeWorkContract(work)
			if err != nil {
				return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q foreground work: %w", declaration.ID, err)
			}
			if transition.ID == WorkPackageAdmit {
				if err := validateWorkPackageContract(*transition.Work); err != nil {
					return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q foreground work: %w", declaration.ID, err)
				}
				copy := *transition.Work
				admittedPackageWork = &copy
			}
			transition.OwnedResources = append(transition.OwnedResources, "foreground-work-"+transition.Work.ID)
		}
		if transition.ID == WorkPackageAdmit && transition.Work == nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q requires foreground work", declaration.ID)
		}
		if transition.ID == PlanningPackagePromote {
			promotionPlanOutput, err = planningPackagePlanOutput(declaration.Parameters)
			if err != nil {
				return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q: %w", declaration.ID, err)
			}
		}
		for _, authority := range declaration.Requires.Authorities {
			transition.AuthorityAll = append(transition.AuthorityAll, delivery.AuthorityClass(authority))
		}
		transition.AuthorityAll = uniqueAuthorities(transition.AuthorityAll)
		trustedObjectives := append([]model.TargetID(nil), transition.TargetIDs...)
		transition.TargetIDs = transition.TargetIDs[:0]
		for targetID, objective := range objectives {
			if containsAll(trustedObjectives, []model.TargetID{objective.TrustedClass}) {
				transition.TargetIDs = append(transition.TargetIDs, targetID)
			}
		}
		if len(transition.TargetIDs) == 0 {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("transition %q supports none of the declared entry targets", declaration.ID)
		}
		if transition.ID == "plan.abandon" && hasTrustedClass(objectives, model.ObjectiveAbandoned) {
			// A repository Flow that explicitly exposes a safely-abandoned entry
			// makes abandonment progress for that objective only. Human authority
			// remains mandatory and other objectives cannot select this transition.
			transition.SelectionClass = delivery.SelectionProgramProgress
		}
		sort.Slice(transition.TargetIDs, func(i, j int) bool { return transition.TargetIDs[i] < transition.TargetIDs[j] })
		if err := requireReachableEntryInputs(transition, entriesByTarget); err != nil {
			return delivery.ProgramRuntimeManifest{}, err
		}
		selected = append(selected, transition)
	}
	if promotionPlanOutput != "" {
		if admittedPackageWork == nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("%s requires %s with foreground work", PlanningPackagePromote, WorkPackageAdmit)
		}
		if err := validatePlanningPackageWorkContract(*admittedPackageWork, promotionPlanOutput); err != nil {
			return delivery.ProgramRuntimeManifest{}, fmt.Errorf("%s: %w", PlanningPackagePromote, err)
		}
	}
	if len(selected) == 0 {
		return delivery.ProgramRuntimeManifest{}, fmt.Errorf("software-delivery Flow selects no transitions")
	}
	resources, effects, verifiers, recoveries := declarations(selected)
	capabilities := []delivery.Capability{delivery.CapabilityHumanApprove}
	for index := range selected {
		selected[index].RequiredCapabilities = delivery.KernelEffectCapabilities(selected[index])
		capabilities = delivery.UnionCapabilities(capabilities, selected[index].RequiredCapabilities)
	}
	supported := make([]model.TargetID, 0, len(objectives))
	objectiveContracts := make([]delivery.ObjectiveContract, 0, len(contracts))
	for objective := range objectives {
		supported = append(supported, objective)
		objectiveContracts = append(objectiveContracts, contracts[objective])
	}
	sort.Slice(supported, func(i, j int) bool { return supported[i] < supported[j] })
	sort.Slice(objectiveContracts, func(i, j int) bool { return objectiveContracts[i].TargetID < objectiveContracts[j].TargetID })
	settings, _ := json.Marshal(map[string]string{
		"flow_id": d.compiled.Document.Program.ID, "flow_fingerprint": d.compiled.Fingerprint,
		"human_identity_role": d.compiled.Document.Program.HumanIdentity,
	})
	base.Version = standard.Version + "+flow." + d.compiled.Fingerprint[:12]
	base.SupportedTargets = supported
	base.ObjectiveContracts = objectiveContracts
	base.Transitions, base.OwnedResources, base.Effects, base.Verifiers = selected, resources, effects, verifiers
	base.Capabilities, base.RecoveryTransitions, base.Settings = capabilities, recoveries, settings
	return base, nil
}

func requireReachableEntryInputs(transition delivery.Transition, entriesByTarget map[model.TargetID][]controlprogram.Entry) error {
	if transition.Work == nil || len(transition.Work.Inputs) == 0 {
		return nil
	}
	for _, targetID := range transition.TargetIDs {
		for _, entry := range entriesByTarget[targetID] {
			declared := map[string]bool{}
			for _, input := range entry.Inputs {
				declared[input.ID] = true
			}
			for _, input := range transition.Work.Inputs {
				if !declared[input.EntryInput] {
					return fmt.Errorf("transition %q foreground work %q requires entry input %q, but reachable entry %q does not declare it", transition.ID, transition.Work.ID, input.EntryInput, entry.ID)
				}
			}
		}
	}
	return nil
}

// RuntimeWorkContract projects one exact canonical foreground-work contract
// for both runtime admission and invocation producer validation.
func RuntimeWorkContract(declaration controlprogram.WorkContract) (*delivery.WorkContract, error) {
	work := &delivery.WorkContract{
		ID: declaration.ID, InstructionPath: declaration.Instructions.Path,
		InstructionSHA256: declaration.Instructions.SHA256, InstructionContent: declaration.Instructions.Content,
	}
	for _, input := range declaration.Inputs {
		work.Inputs = append(work.Inputs, delivery.WorkInput{ID: input.ID, EntryInput: input.EntryInput})
	}
	for _, output := range declaration.Outputs {
		runtimeOutput := delivery.WorkOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: output.Required, MaxBytes: output.MaxBytes}
		if output.Guidance != nil {
			runtimeOutput.GuidancePath, runtimeOutput.GuidanceSHA256, runtimeOutput.GuidanceContent = output.Guidance.Path, output.Guidance.SHA256, output.Guidance.Content
		}
		if output.Schema != nil {
			runtimeOutput.SchemaPath, runtimeOutput.SchemaSHA256, runtimeOutput.SchemaContent = output.Schema.Path, output.Schema.SHA256, output.Schema.Content
		}
		work.Outputs = append(work.Outputs, runtimeOutput)
	}
	fingerprint, err := general.Fingerprint(struct {
		ID                 string                `json:"id"`
		InstructionPath    string                `json:"instruction_path"`
		InstructionSHA256  string                `json:"instruction_sha256"`
		InstructionContent string                `json:"instruction_content"`
		Inputs             []delivery.WorkInput  `json:"inputs,omitempty"`
		Outputs            []delivery.WorkOutput `json:"outputs"`
	}{work.ID, work.InstructionPath, work.InstructionSHA256, work.InstructionContent, work.Inputs, work.Outputs})
	if err != nil {
		return nil, err
	}
	work.Fingerprint = fingerprint
	return work, nil
}

func uniqueAuthorities(values []delivery.AuthorityClass) []delivery.AuthorityClass {
	seen := map[delivery.AuthorityClass]bool{}
	result := make([]delivery.AuthorityClass, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func addAcceptedWorkPackageObjective(base *delivery.ProgramRuntimeManifest) {
	base.ObjectiveContracts = append(base.ObjectiveContracts, delivery.ObjectiveContract{
		TargetID: model.ObjectiveApprovedWorkPackage,
		Conditions: []delivery.FacetCondition{{
			Facet:    model.FacetWorkPackage,
			Statuses: []model.FactStatus{model.FactKnown},
			Values:   []string{string(model.WorkPackageApproved)},
		}},
	})
}

func ObjectiveForEntry(ctx context.Context, compiled controlprogram.Compiled, resolver Resolver, entryID string) (EntryObjective, error) {
	base, err := standard.Definition().RuntimeManifest(ctx)
	if err != nil {
		return EntryObjective{}, err
	}
	addAcceptedWorkPackageObjective(&base)
	return objectiveContractForEntry(compiled, base, entryID)
}

func objectiveContractForEntry(compiled controlprogram.Compiled, base delivery.ProgramRuntimeManifest, entryID string) (EntryObjective, error) {
	var targetID string
	for _, entry := range compiled.Document.Entries {
		if entry.ID == entryID {
			targetID = entry.Target
			break
		}
	}
	if targetID == "" {
		return EntryObjective{}, fmt.Errorf("unknown Flow entry %q", entryID)
	}
	if !model.TargetID(targetID).Valid() {
		return EntryObjective{}, fmt.Errorf("entry %q target identity is invalid", entryID)
	}
	var predicate controlprogram.Predicate
	for _, target := range compiled.Document.Targets {
		if target.ID == targetID {
			predicate = target.Predicate
			break
		}
	}
	conditions, err := conjunctiveConditions(predicate)
	if err != nil {
		return EntryObjective{}, fmt.Errorf("entry %q target is not a software-delivery marked state: %w", entryID, err)
	}
	var matches []delivery.ObjectiveContract
	for _, contract := range base.ObjectiveContracts {
		if conditionsStrengthen(conditions, contract.Conditions) {
			matches = append(matches, contract)
		}
	}
	if len(matches) != 1 {
		return EntryObjective{}, fmt.Errorf("entry %q target must strengthen exactly one trusted software-delivery marked state", entryID)
	}
	target := model.TargetID(targetID)
	return EntryObjective{
		TargetID:     target,
		TrustedClass: matches[0].TargetID,
		Contract:     delivery.ObjectiveContract{TargetID: target, TrustedClass: matches[0].TargetID, Conditions: conditions},
	}, nil
}

func conditionsStrengthen(repository, trusted []delivery.FacetCondition) bool {
	for _, required := range trusted {
		found := false
		for _, declared := range repository {
			if declared.Facet == required.Facet && conditionImplies(declared, required) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasTrustedClass(objectives map[model.TargetID]EntryObjective, class model.TargetID) bool {
	for _, objective := range objectives {
		if objective.TrustedClass == class {
			return true
		}
	}
	return false
}

func conjunctiveConditions(predicate controlprogram.Predicate) ([]delivery.FacetCondition, error) {
	if predicate.True != nil {
		if !*predicate.True {
			return nil, fmt.Errorf("false predicates are not admissible")
		}
		return nil, nil
	}
	if predicate.Fact != nil {
		statuses := make([]model.FactStatus, len(predicate.Fact.Statuses))
		for index, status := range predicate.Fact.Statuses {
			statuses[index] = model.FactStatus(status)
		}
		return []delivery.FacetCondition{{Facet: model.FacetName(predicate.Fact.Facet), Statuses: statuses, Values: append([]string(nil), predicate.Fact.Values...)}}, nil
	}
	if len(predicate.All) != 0 {
		var result []delivery.FacetCondition
		for _, child := range predicate.All {
			conditions, err := conjunctiveConditions(child)
			if err != nil {
				return nil, err
			}
			result = append(result, conditions...)
		}
		return result, nil
	}
	return nil, fmt.Errorf("only true, fact, and all predicates are supported")
}

func requireImpliedTarget(trusted, repository []delivery.FacetCondition) error {
	for _, candidate := range repository {
		implied := false
		for _, established := range trusted {
			if established.Facet == candidate.Facet && conditionImplies(established, candidate) {
				implied = true
				break
			}
		}
		if !implied {
			return fmt.Errorf("facet %q is not established by the trusted operator", candidate.Facet)
		}
	}
	return nil
}

func conditionImplies(established, candidate delivery.FacetCondition) bool {
	if !containsAll(candidate.Statuses, established.Statuses) {
		return false
	}
	if len(candidate.Values) == 0 {
		return true
	}
	return len(established.Values) != 0 && containsAll(candidate.Values, established.Values)
}

func containsAll[T comparable](superset, subset []T) bool {
	for _, value := range subset {
		found := false
		for _, candidate := range superset {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func declarations(transitions []delivery.Transition) ([]string, []string, []string, []delivery.TransitionID) {
	resources, effects, verifiers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	recoveries := map[delivery.TransitionID]bool{}
	for _, transition := range transitions {
		for _, resource := range transition.OwnedResources {
			resources[resource] = true
		}
		if transition.Effect != "" {
			effects[string(transition.Effect)] = true
		}
		if transition.Verifier != "" {
			verifiers[transition.Verifier] = true
		}
		if transition.Class == delivery.EventRecovery {
			recoveries[transition.ID] = true
		}
	}
	resourceList, effectList, verifierList := mapKeys(resources), mapKeys(effects), mapKeys(verifiers)
	recoveryList := make([]delivery.TransitionID, 0, len(recoveries))
	for value := range recoveries {
		recoveryList = append(recoveryList, value)
	}
	sort.Slice(recoveryList, func(i, j int) bool { return recoveryList[i] < recoveryList[j] })
	return resourceList, effectList, verifierList, recoveryList
}

func mapKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
