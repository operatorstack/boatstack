package deliverycontrol

import "testing"

// control-law: delivery-graph-is-live
// Deadlock-freedom: from every state reachable in the real registry graph, the
// flow can still move and still reach the goal (PUBLISHED). A regression that
// stranded a state — an out-edge removed, a goal made unreachable — fails here.
func TestRegistryGraphIsLive(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	result := CheckLiveness(g, EntryStates(), StatePublished, TerminalStates())

	if !result.Live {
		t.Fatalf("delivery graph is not live: deadlocks=%v goal-unreachable=%v", result.Deadlocks, result.GoalUnreachable)
	}
	// The core lifecycle states must all be reachable from the entries.
	want := []StateID{StateUninitialized, StateBuild, StateTestPassed, StateReviewPassed, StatePublished}
	reachable := map[StateID]bool{}
	for _, s := range result.Reachable {
		reachable[s] = true
	}
	for _, s := range want {
		if !reachable[s] {
			t.Errorf("expected %q reachable from entries", s)
		}
	}
}

// A hand-built graph with a genuine dead end is caught.
func TestCheckLivenessDetectsDeadlock(t *testing.T) {
	g := NewGraph(DefaultFlowCostWeights())
	g.AddEdge(StateUninitialized, StateBuild, "activate", CostMutation)
	// BUILD has no way forward and is not terminal → a deadlock.
	result := CheckLiveness(g, []StateID{StateUninitialized}, StatePublished, TerminalStates())
	if result.Live {
		t.Fatal("expected a deadlock to be reported")
	}
	found := false
	for _, s := range result.Deadlocks {
		if s == StateBuild {
			found = true
		}
	}
	if !found {
		t.Errorf("BUILD should be a deadlock; got %v", result.Deadlocks)
	}
}

// A reachable non-terminal state that cannot reach the goal is reported as
// goal-unreachable (not a deadlock — it can move, just not to the goal).
func TestCheckLivenessDetectsGoalUnreachable(t *testing.T) {
	g := NewGraph(DefaultFlowCostWeights())
	g.AddEdge(StateUninitialized, StateBuild, "activate", CostMutation)
	g.AddEdge(StateBuild, StateDiscarded, "discard", CostMutation) // moves, but only to a terminal
	result := CheckLiveness(g, []StateID{StateUninitialized}, StatePublished, TerminalStates())
	if result.Live {
		t.Fatal("expected goal-unreachable to make the graph non-live")
	}
	if len(result.GoalUnreachable) == 0 {
		t.Errorf("BUILD cannot reach PUBLISHED; expected it reported, got %v", result.GoalUnreachable)
	}
}

func TestCheckRegistryClean(t *testing.T) {
	if issues := CheckRegistry(); len(issues) != 0 {
		t.Errorf("registry should be well-formed; issues: %v", issues)
	}
}
