package catalog

import (
	"fmt"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

// ObjectiveContract is the compiled terminal law supplied by the program runtime.
// Extension conditions are conjunctive and therefore can only narrow the
// terminal set.
type ObjectiveContract struct {
	TargetID     model.TargetID   `json:"target_id"`
	TrustedClass model.TargetID   `json:"trusted_class"`
	Conditions   []FacetCondition `json:"conditions"`
}

type ObjectiveContracts map[model.TargetID]ObjectiveContract

func (c ObjectiveContracts) Clone() ObjectiveContracts {
	result := make(ObjectiveContracts, len(c))
	for objective, contract := range c {
		contract.Conditions = cloneConditions(contract.Conditions)
		result[objective] = contract
	}
	return result
}

func NewObjectiveContracts(base []ObjectiveContract, extension map[model.TargetID][]FacetCondition) (ObjectiveContracts, error) {
	contracts := make(ObjectiveContracts, len(base))
	for _, contract := range base {
		if !contract.TargetID.Valid() || len(contract.Conditions) == 0 {
			return nil, fmt.Errorf("objective contract requires a valid objective and conditions")
		}
		if contract.TrustedClass == "" {
			contract.TrustedClass = contract.TargetID
		}
		if !contract.TrustedClass.Valid() {
			return nil, fmt.Errorf("objective contract %q requires a valid trusted class", contract.TargetID)
		}
		if _, exists := contracts[contract.TargetID]; exists {
			return nil, fmt.Errorf("duplicate objective contract %q", contract.TargetID)
		}
		conditions := append([]FacetCondition(nil), contract.Conditions...)
		conditions = append(conditions, extension[contract.TargetID]...)
		for _, condition := range conditions {
			if !condition.Facet.Valid() || len(condition.Statuses) == 0 {
				return nil, fmt.Errorf("objective %q has invalid terminal condition", contract.TargetID)
			}
			for _, status := range condition.Statuses {
				if !status.Valid() {
					return nil, fmt.Errorf("objective %q has invalid terminal status %q", contract.TargetID, status)
				}
			}
		}
		contract.Conditions = conditions
		contracts[contract.TargetID] = contract
	}
	for objective := range extension {
		if _, exists := contracts[objective]; !exists {
			return nil, fmt.Errorf("extension constrains unsupported objective %q", objective)
		}
	}
	return contracts, nil
}

func (c ObjectiveContracts) Accepts(objective model.Objective) bool {
	contract, ok := c[objective.TargetID]
	return ok && contract.TrustedClass == objective.TrustedObjectiveClass()
}

func (c ObjectiveContracts) Matches(snapshot model.Snapshot, objective model.Objective) bool {
	if snapshot.Objective.Status != model.FactKnown || snapshot.Objective.Value != objective {
		return false
	}
	contract, ok := c[objective.TargetID]
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

func (c ObjectiveContracts) All() []ObjectiveContract {
	result := make([]ObjectiveContract, 0, len(c))
	for _, contract := range c {
		contract.Conditions = append([]FacetCondition(nil), contract.Conditions...)
		result = append(result, contract)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TargetID < result[j].TargetID })
	return result
}
