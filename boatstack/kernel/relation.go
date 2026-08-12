package kernel

import (
	"fmt"
	"sort"
)

// RelationCandidate is a domain-independent projection of one transition that
// has already satisfied its program and domain predicates. Rank and Priority
// are ordered ascending. Authority remains data: only Relate compares the
// required capabilities with the externally admitted capability set.
type RelationCandidate struct {
	ID          string
	Rank        int
	Priority    int
	Selectable  bool
	RequiredAll []Capability
	RequiredAny []Capability
}

type RelationInput struct {
	Requested   string
	Marked      bool
	NoCandidate DecisionKind
	Candidates  []RelationCandidate
	Available   []Capability
}

// Relate is the kernel's canonical transition-selection relation. Domains
// decide whether a transition satisfies domain predicates; the kernel alone
// applies target selection, ordering, ambiguity, and authority.
func Relate(input RelationInput) Decision {
	if input.Marked && input.Requested == "" {
		return Decision{Kind: Marked, Reason: "program-defined marked state is established"}
	}
	candidates := append([]RelationCandidate(nil), input.Candidates...)
	if input.Requested != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.ID == input.Requested {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		if input.Requested != "" {
			return Decision{Kind: Refused, Transition: input.Requested, Reason: "requested transition is not admissible under the canonical relation"}
		}
		kind := input.NoCandidate
		if kind == "" {
			kind = Unresolved
		}
		return Decision{Kind: kind, Reason: "no transition is admissible under the canonical relation"}
	}
	if input.Requested == "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.Selectable {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
		if len(candidates) == 0 {
			kind := input.NoCandidate
			if kind == "" {
				kind = Unresolved
			}
			return Decision{Kind: kind, Reason: "no transition is selectable under the canonical relation"}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return candidates[i].Rank < candidates[j].Rank
		}
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	top := candidates[0]
	var equal []string
	for _, candidate := range candidates {
		if candidate.Rank == top.Rank && candidate.Priority == top.Priority {
			equal = append(equal, candidate.ID)
		}
	}
	if len(equal) > 1 {
		return Decision{Kind: Frontier, Candidates: equal, Reason: "equally preferred admissible transitions require selection"}
	}
	if missing := missingAuthority(top, input.Available); len(missing) != 0 {
		return Decision{Kind: Frontier, Transition: top.ID, Candidates: []string{top.ID}, Reason: fmt.Sprintf("transition requires unavailable capabilities: %v", missing)}
	}
	return Decision{Kind: Prescribed, Transition: top.ID, Reason: "canonical relation admitted one highest-priority transition"}
}

func missingAuthority(candidate RelationCandidate, available []Capability) []Capability {
	set := make(map[Capability]bool, len(available))
	for _, value := range available {
		set[value] = true
	}
	missing := make([]Capability, 0, len(candidate.RequiredAll)+1)
	for _, value := range candidate.RequiredAll {
		if !set[value] {
			missing = append(missing, value)
		}
	}
	if len(candidate.RequiredAny) != 0 {
		satisfied := false
		for _, value := range candidate.RequiredAny {
			if set[value] {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missing = append(missing, candidate.RequiredAny...)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}
