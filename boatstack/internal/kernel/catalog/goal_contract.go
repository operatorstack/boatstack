package catalog

import (
	"fmt"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

// GoalContract is the compiled terminal law supplied by the primary flow.
// Extension conditions are conjunctive and therefore can only narrow the
// terminal set.
type GoalContract struct {
	GoalKind   model.GoalKind   `json:"goal_kind"`
	Conditions []FacetCondition `json:"conditions"`
}

type GoalContracts map[model.GoalKind]GoalContract

func (c GoalContracts) Clone() GoalContracts {
	result := make(GoalContracts, len(c))
	for goal, contract := range c {
		contract.Conditions = cloneConditions(contract.Conditions)
		result[goal] = contract
	}
	return result
}

func NewGoalContracts(base []GoalContract, extension map[model.GoalKind][]FacetCondition) (GoalContracts, error) {
	contracts := make(GoalContracts, len(base))
	for _, contract := range base {
		if !contract.GoalKind.Valid() || len(contract.Conditions) == 0 {
			return nil, fmt.Errorf("goal contract requires a valid goal and conditions")
		}
		if _, exists := contracts[contract.GoalKind]; exists {
			return nil, fmt.Errorf("duplicate goal contract %q", contract.GoalKind)
		}
		conditions := append([]FacetCondition(nil), contract.Conditions...)
		conditions = append(conditions, extension[contract.GoalKind]...)
		for _, condition := range conditions {
			if !condition.Facet.Valid() || len(condition.Statuses) == 0 {
				return nil, fmt.Errorf("goal %q has invalid terminal condition", contract.GoalKind)
			}
			for _, status := range condition.Statuses {
				if !status.Valid() {
					return nil, fmt.Errorf("goal %q has invalid terminal status %q", contract.GoalKind, status)
				}
			}
		}
		contract.Conditions = conditions
		contracts[contract.GoalKind] = contract
	}
	for goal := range extension {
		if _, exists := contracts[goal]; !exists {
			return nil, fmt.Errorf("extension constrains unsupported goal %q", goal)
		}
	}
	return contracts, nil
}

func (c GoalContracts) Matches(snapshot model.Snapshot, goal model.Goal) bool {
	if snapshot.Goal.Status != model.FactKnown || snapshot.Goal.Value != goal {
		return false
	}
	contract, ok := c[goal.Kind]
	if !ok {
		return false
	}
	for _, condition := range contract.Conditions {
		if !condition.Matches(snapshot) {
			return false
		}
	}
	return true
}

func (c GoalContracts) All() []GoalContract {
	result := make([]GoalContract, 0, len(c))
	for _, contract := range c {
		contract.Conditions = append([]FacetCondition(nil), contract.Conditions...)
		result = append(result, contract)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GoalKind < result[j].GoalKind })
	return result
}
