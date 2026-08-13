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
	objectives := map[model.TargetID]EntryObjective{}
	contracts := map[model.TargetID]delivery.ObjectiveContract{}
	for _, entry := range d.compiled.Document.Entries {
		objective, objectiveErr := objectiveContractForEntry(d.compiled, base, entry.ID)
		if objectiveErr != nil {
			return delivery.ProgramRuntimeManifest{}, objectiveErr
		}
		objectives[objective.TargetID], contracts[objective.TargetID] = objective, objective.Contract
	}

	selected := make([]delivery.Transition, 0, len(d.compiled.Document.Transitions))
	seen := map[delivery.TransitionID]bool{}
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
		selected = append(selected, transition)
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
	settings, _ := json.Marshal(map[string]string{"flow_id": d.compiled.Document.Program.ID, "flow_fingerprint": d.compiled.Fingerprint})
	base.Version = standard.Version + "+flow." + d.compiled.Fingerprint[:12]
	base.SupportedTargets = supported
	base.ObjectiveContracts = objectiveContracts
	base.Transitions, base.OwnedResources, base.Effects, base.Verifiers = selected, resources, effects, verifiers
	base.Capabilities, base.RecoveryTransitions, base.Settings = capabilities, recoveries, settings
	return base, nil
}

func ObjectiveForEntry(ctx context.Context, compiled controlprogram.Compiled, resolver Resolver, entryID string) (EntryObjective, error) {
	base, err := standard.Definition().RuntimeManifest(ctx)
	if err != nil {
		return EntryObjective{}, err
	}
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
