package deliverycontrol

import (
	"fmt"
	"sort"
)

// TerminalStates are delivery-flow states from which no further move is expected:
// PUBLISHED is the accepted goal, DISCARDED is the archived end. A reachable
// state that is neither terminal nor able to move is a deadlock.
func TerminalStates() []StateID {
	return []StateID{StatePublished, StateDiscarded}
}

// EntryStates are where a delivery flow can begin: a fresh delivery
// (UNINITIALIZED) and the recovery entry (INVALID, re-entered via repair). The
// liveness check reaches the rest of the graph from these.
func EntryStates() []StateID {
	return []StateID{StateUninitialized, StateInvalid}
}

// LivenessResult reports deadlock-freedom over the delivery graph: every state
// reachable from the entries either is terminal, is the goal, or can still move
// and still reach the goal.
type LivenessResult struct {
	Goal            StateID   `json:"goal"`
	Reachable       []StateID `json:"reachable"`
	Deadlocks       []StateID `json:"deadlocks"`        // reachable, non-terminal, non-goal states with no out-edge
	GoalUnreachable []StateID `json:"goal_unreachable"` // reachable, non-terminal, non-goal states from which the goal is Unresolved
	Live            bool      `json:"live"`
}

// CheckLiveness verifies the delivery graph is free of deadlocks and that the
// goal stays reachable. From every state reachable from the entries, a
// non-terminal non-goal state must have at least one out-edge (it can move) and
// the oracle must resolve a path from it to the goal (it is not stranded). The
// result is deterministic: all reported slices are sorted.
func CheckLiveness(g *Graph, entries []StateID, goal StateID, terminals []StateID) LivenessResult {
	terminal := map[StateID]bool{}
	for _, s := range terminals {
		terminal[s] = true
	}

	// Breadth-first reachability from the entries, following out-edges.
	seen := map[StateID]bool{}
	queue := append([]StateID{}, entries...)
	for _, e := range entries {
		seen[e] = true
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range g.Out(current) {
			if !seen[edge.To] {
				seen[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
	}

	result := LivenessResult{Goal: goal, Live: true}
	for s := range seen {
		result.Reachable = append(result.Reachable, s)
		if s == goal || terminal[s] {
			continue
		}
		if len(g.Out(s)) == 0 {
			result.Deadlocks = append(result.Deadlocks, s)
			result.Live = false
			continue
		}
		if g.ShortestFlow(s, goal).Resolution != Resolved {
			result.GoalUnreachable = append(result.GoalUnreachable, s)
			result.Live = false
		}
	}
	sort.Slice(result.Reachable, func(i, j int) bool { return result.Reachable[i] < result.Reachable[j] })
	sort.Slice(result.Deadlocks, func(i, j int) bool { return result.Deadlocks[i] < result.Deadlocks[j] })
	sort.Slice(result.GoalUnreachable, func(i, j int) bool { return result.GoalUnreachable[i] < result.GoalUnreachable[j] })
	return result
}

// CheckResult is the outcome of the runtime flow-check gate: registry
// well-formedness plus deadlock-freedom over the delivery graph.
type CheckResult struct {
	RegistryIssues []string       `json:"registry_issues"`
	Liveness       LivenessResult `json:"liveness"`
	OK             bool           `json:"ok"`
}

// Check runs the full static gate over the single declaration and its graph. It
// takes no repository state — it validates the owned model itself, so the CLI
// gate is deterministic and side-effect free.
func Check() CheckResult {
	result := CheckResult{RegistryIssues: CheckRegistry()}
	graph := RegistryGraph(DefaultFlowCostWeights())
	result.Liveness = CheckLiveness(graph, EntryStates(), StatePublished, TerminalStates())
	result.OK = len(result.RegistryIssues) == 0 && result.Liveness.Live
	return result
}

// CheckRegistry validates the single declaration at runtime: unique ids, valid
// kinds and cost classes with defined weights, declared endpoint states, and a
// non-empty registry. It returns a sorted list of human-readable issues, empty
// when the registry is well-formed. This is the runtime half of the conformance
// gate; the compile-time parity test in package boatstack guarantees the handler
// references name real functions.
func CheckRegistry() []string {
	weights := DefaultFlowCostWeights()
	states := map[StateID]bool{}
	for _, s := range States() {
		states[s] = true
	}
	kinds := map[TransitionKind]bool{}
	for _, k := range AllKinds() {
		kinds[k] = true
	}
	classes := map[TransitionCostClass]bool{}
	for _, c := range AllCostClasses() {
		classes[c] = true
	}

	var issues []string
	seen := map[TransitionID]bool{}
	for _, tr := range Transitions() {
		if tr.ID == "" {
			issues = append(issues, "transition with empty ID")
			continue
		}
		if seen[tr.ID] {
			issues = append(issues, fmt.Sprintf("%s: duplicate transition ID", tr.ID))
		}
		seen[tr.ID] = true
		if !kinds[tr.Kind] {
			issues = append(issues, fmt.Sprintf("%s: undeclared kind %q", tr.ID, tr.Kind))
		}
		if !classes[tr.CostClass] {
			issues = append(issues, fmt.Sprintf("%s: undeclared cost class %q", tr.ID, tr.CostClass))
		} else if _, ok := weights.Cost(tr.CostClass); !ok {
			issues = append(issues, fmt.Sprintf("%s: cost class %q has no weight", tr.ID, tr.CostClass))
		}
		for _, from := range tr.From {
			if !states[from] {
				issues = append(issues, fmt.Sprintf("%s: undeclared From state %q", tr.ID, from))
			}
		}
		if tr.To != "" && !states[tr.To] {
			issues = append(issues, fmt.Sprintf("%s: undeclared To state %q", tr.ID, tr.To))
		}
		if tr.HandlerRef == "" {
			issues = append(issues, fmt.Sprintf("%s: empty HandlerRef", tr.ID))
		}
	}
	if len(seen) == 0 {
		issues = append(issues, "registry is empty")
	}
	sort.Strings(issues)
	return issues
}
