package deliverycontrol

import "testing"

// control-law: oracle-resolves-real-paths-or-unresolved
// The oracle must return the true shortest cost over the registry graph for
// reachable goals and Unresolved — never a fabricated path — for unknown or
// unreachable states.
func TestShortestFlowOverRegistry(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())

	cases := []struct {
		name string
		from StateID
		goal StateID
		cost int
	}{
		{"activate through publish", StateUninitialized, StatePublished, 4},
		{"build to published", StateBuild, StatePublished, 3},
		{"review to published", StateReviewPassed, StatePublished, 1},
		{"repair out of invalid", StateInvalid, StateUninitialized, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := g.ShortestFlow(tc.from, tc.goal)
			if path.Resolution != Resolved {
				t.Fatalf("%s→%s: expected Resolved, got %s", tc.from, tc.goal, path.Resolution)
			}
			if path.Cost != tc.cost {
				t.Errorf("%s→%s: cost = %d, want %d (edges: %v)", tc.from, tc.goal, path.Cost, tc.cost, edgeIDs(path.Edges))
			}
			// The reconstructed path's costs must sum to the reported cost, and
			// each edge must chain from the previous To.
			sum, at := 0, tc.from
			for _, e := range path.Edges {
				if e.From != at {
					t.Errorf("%s→%s: broken chain at edge %s (from %s, expected %s)", tc.from, tc.goal, e.Transition, e.From, at)
				}
				sum += e.Cost
				at = e.To
			}
			if sum != path.Cost {
				t.Errorf("%s→%s: edge cost sum %d != reported %d", tc.from, tc.goal, sum, path.Cost)
			}
			if at != tc.goal {
				t.Errorf("%s→%s: path ends at %s", tc.from, tc.goal, at)
			}
		})
	}
}

func TestShortestFlowSameStateIsFree(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	path := g.ShortestFlow(StateBuild, StateBuild)
	if path.Resolution != Resolved || path.Cost != 0 || len(path.Edges) != 0 {
		t.Fatalf("same-state path: got %+v, want resolved zero-cost empty", path)
	}
}

func TestShortestFlowUnresolved(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())

	// Unknown start state — the oracle has never seen it, so it must not guess.
	if path := g.ShortestFlow(StateID("MADE_UP"), StatePublished); path.Resolution != Unresolved {
		t.Errorf("unknown start: got %s, want Unresolved", path.Resolution)
	}
	// Unknown goal state.
	if path := g.ShortestFlow(StateBuild, StateID("NOWHERE")); path.Resolution != Unresolved {
		t.Errorf("unknown goal: got %s, want Unresolved", path.Resolution)
	}
	// Known states with no connecting path: PENDING can only be discarded, so it
	// cannot reach PUBLISHED.
	if path := g.ShortestFlow(StatePending, StatePublished); path.Resolution != Unresolved {
		t.Errorf("unreachable goal: got %s, want Unresolved", path.Resolution)
	}
}

func edgeIDs(edges []Edge) []TransitionID {
	ids := make([]TransitionID, len(edges))
	for i, e := range edges {
		ids[i] = e.Transition
	}
	return ids
}
