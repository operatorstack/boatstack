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
	decision, _ := relate(input)
	return decision
}

// RelateWithTrace evaluates the same canonical relation as Relate and exposes
// its domain-neutral ordering and authority projection. The trace is an output
// of the selector and is never an input to it.
func RelateWithTrace(input RelationInput) (Decision, []CandidateTrace) {
	return relate(input)
}

func relate(input RelationInput) (Decision, []CandidateTrace) {
	traces := make([]CandidateTrace, len(input.Candidates))
	for index, candidate := range input.Candidates {
		traces[index] = CandidateTrace{
			TransitionID: candidate.ID, Selectable: candidate.Selectable,
			Rank: candidate.Rank, Priority: candidate.Priority, Survived: true,
			Disposition: DispositionShadowed,
			Authority:   authorityTrace(candidate, input.Available),
		}
	}
	finish := func(decision Decision) (Decision, []CandidateTrace) {
		for index := range traces {
			trace := &traces[index]
			if input.Requested == "" && !trace.Selectable {
				trace.Survived = false
			}
			switch {
			case input.Requested != "" && trace.TransitionID != input.Requested:
				trace.Disposition = DispositionIrrelevantToRequest
				trace.Survived = false
			case decision.Kind == Prescribed && trace.TransitionID == decision.Transition:
				trace.Disposition = DispositionSelected
			case decision.Kind == Frontier && decision.Transition == trace.TransitionID:
				trace.Disposition = DispositionAuthorityFrontier
			case decision.Kind == Frontier && contains(decision.Candidates, trace.TransitionID):
				trace.Disposition = DispositionAmbiguous
			case decision.Kind == Refused && input.Requested == trace.TransitionID:
				trace.Disposition = DispositionExplicitlyRefused
			}
		}
		return decision, traces
	}
	if input.Marked && input.Requested == "" {
		return finish(Decision{Kind: Marked, Reason: "program-defined marked state is established"})
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
			return finish(Decision{Kind: Refused, Transition: input.Requested, Reason: "requested transition is not admissible under the canonical relation"})
		}
		kind := input.NoCandidate
		if kind == "" {
			kind = Unresolved
		}
		return finish(Decision{Kind: kind, Reason: "no transition is admissible under the canonical relation"})
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
			return finish(Decision{Kind: kind, Reason: "no transition is selectable under the canonical relation"})
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
		return finish(Decision{Kind: Frontier, Candidates: equal, Reason: "equally preferred admissible transitions require selection"})
	}
	if missing := missingAuthority(top, input.Available); len(missing) != 0 {
		return finish(Decision{Kind: Frontier, Transition: top.ID, Candidates: []string{top.ID}, Reason: fmt.Sprintf("transition requires unavailable capabilities: %v", missing)})
	}
	return finish(Decision{Kind: Prescribed, Transition: top.ID, Reason: "canonical relation admitted one highest-priority transition"})
}

func authorityTrace(candidate RelationCandidate, available []Capability) AuthorityTrace {
	set := make(map[Capability]bool, len(available))
	for _, value := range available {
		set[value] = true
	}
	trace := AuthorityTrace{
		Available:    append([]Capability(nil), available...),
		RequiredAll:  append([]Capability(nil), candidate.RequiredAll...),
		RequiredAny:  append([]Capability(nil), candidate.RequiredAny...),
		AllSatisfied: true, AnySatisfied: len(candidate.RequiredAny) == 0,
	}
	for _, value := range candidate.RequiredAll {
		if !set[value] {
			trace.MissingAll = append(trace.MissingAll, value)
			trace.AllSatisfied = false
		}
	}
	for _, value := range candidate.RequiredAny {
		if set[value] {
			trace.AnySatisfied = true
		}
	}
	if !trace.AnySatisfied {
		trace.MissingAny = append(trace.MissingAny, candidate.RequiredAny...)
	}
	sort.Slice(trace.Available, func(i, j int) bool { return trace.Available[i] < trace.Available[j] })
	sort.Slice(trace.RequiredAll, func(i, j int) bool { return trace.RequiredAll[i] < trace.RequiredAll[j] })
	sort.Slice(trace.RequiredAny, func(i, j int) bool { return trace.RequiredAny[i] < trace.RequiredAny[j] })
	sort.Slice(trace.MissingAll, func(i, j int) bool { return trace.MissingAll[i] < trace.MissingAll[j] })
	sort.Slice(trace.MissingAny, func(i, j int) bool { return trace.MissingAny[i] < trace.MissingAny[j] })
	trace.Satisfied = trace.AllSatisfied && trace.AnySatisfied
	return trace
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
