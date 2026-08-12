package effects

import (
	"fmt"
	"path/filepath"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

func validateTransitionStateFacets(transition catalog.Transition, changed []model.StateFacet) ([]model.StateFacet, error) {
	policy, err := catalog.DurableStateFacetPolicy(transition)
	if err != nil {
		return nil, err
	}
	return validateAllowedStateFacets(transition.ID, changed, policy.Writes)
}

func validateAllowedStateFacets(transition catalog.TransitionID, changed, allowed []model.StateFacet) ([]model.StateFacet, error) {
	canonical, err := model.NormalizeStateFacets("changed state facets", changed)
	if err != nil {
		return nil, err
	}
	allowedSet := map[model.StateFacet]bool{}
	for _, facet := range allowed {
		allowedSet[facet] = true
	}
	for _, facet := range canonical {
		if !allowedSet[facet] {
			return nil, fmt.Errorf("FACET_OWNERSHIP_VIOLATION: transition %q changed %q outside its kernel-approved durable state facets", transition, facet)
		}
	}
	return canonical, nil
}

func changedStateFacets(groups ...[2]durable.State) ([]model.StateFacet, error) {
	var result []model.StateFacet
	for _, group := range groups {
		changed, err := durable.ChangedFacets(group[0], group[1])
		if err != nil {
			return nil, err
		}
		result = model.UnionStateFacets(result, changed)
	}
	return result, nil
}

// journalStateFacets consumes the semantic state delta that was validated and
// staged with the interrupted transaction. Recovery cannot widen it.
func journalStateFacets(mutations []ports.ResourceMutation) ([]model.StateFacet, error) {
	var changed []model.StateFacet
	for _, mutation := range mutations {
		if filepath.Base(mutation.Path) != "state.json" {
			continue
		}
		stateMutation := false
		if mutation.PriorExists {
			_, err := durable.DecodeState(mutation.Prior)
			stateMutation = err == nil
		}
		if !stateMutation && !mutation.Delete && mutation.TargetLink == "" {
			_, err := durable.DecodeState(mutation.Target)
			stateMutation = err == nil
		}
		if !stateMutation {
			continue
		}
		facets, err := model.NormalizeStateFacets("journal state facets", mutation.StateFacets)
		if err != nil || len(facets) == 0 {
			return nil, fmt.Errorf("STATE_FACET_UNCLASSIFIED: interrupted durable state mutation %s has no valid staged facets: %v", mutation.Path, err)
		}
		changed = model.UnionStateFacets(changed, facets)
	}
	return changed, nil
}

// mutationStateFacets derives the semantic delta that the prepared mutation
// will actually commit. Recovery receipts must describe this delta, not the
// interrupted transition's wider staged envelope.
func mutationStateFacets(invocation model.InvocationContext, mutations []ports.ResourceMutation) ([]model.StateFacet, error) {
	var changed []model.StateFacet
	for _, mutation := range mutations {
		if filepath.Base(mutation.Path) != "state.json" || mutation.Delete || mutation.TargetLink != "" {
			continue
		}
		after, err := durable.DecodeState(mutation.Target)
		if err != nil {
			continue
		}
		before := durable.Default(invocation, after.UpdatedAt)
		if mutation.PriorExists {
			before, err = durable.DecodeState(mutation.Prior)
			if err != nil {
				return nil, fmt.Errorf("STATE_FACET_UNCLASSIFIED: prepared durable state mutation %s has invalid prior state: %w", mutation.Path, err)
			}
		}
		facets, err := durable.ChangedFacets(before, after)
		if err != nil {
			return nil, err
		}
		changed = model.UnionStateFacets(changed, facets)
	}
	if len(changed) == 0 {
		return nil, fmt.Errorf("STATE_FACET_UNCLASSIFIED: prepared effect has no durable state delta")
	}
	return changed, nil
}

func annotateStateFacetMutations(mutations []ports.ResourceMutation, facets []model.StateFacet) []ports.ResourceMutation {
	for index := range mutations {
		mutation := &mutations[index]
		stateMutation := false
		if mutation.PriorExists && filepath.Base(mutation.Path) == "state.json" {
			_, err := durable.DecodeState(mutation.Prior)
			stateMutation = err == nil
		}
		if !stateMutation && !mutation.Delete && mutation.TargetLink == "" && filepath.Base(mutation.Path) == "state.json" {
			_, err := durable.DecodeState(mutation.Target)
			stateMutation = err == nil
		}
		if stateMutation {
			mutation.StateFacets = append([]model.StateFacet(nil), facets...)
		}
	}
	return mutations
}
