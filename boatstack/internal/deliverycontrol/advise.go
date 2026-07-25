package deliverycontrol

// Advice is the oracle's recommendation from a state: the lowest-cost next
// control to take toward the goal, and the remaining cost from here. It is
// purely advisory — a projection of the shortest path's first step — and is
// Unresolved (with no recommendation) whenever the oracle cannot resolve the
// endpoints, so a caller never acts on a fabricated route.
type Advice struct {
	From           StateID
	Goal           StateID
	NextTransition TransitionID
	NextTo         StateID
	NextCostClass  TransitionCostClass
	RemainingCost  int
	Resolution     Resolution
}

// Advise returns the recommended next move from a state toward a goal. When the
// start already equals the goal, the advice is Resolved with no next move (the
// walk is complete).
func (g *Graph) Advise(from, goal StateID) Advice {
	advice := Advice{From: from, Goal: goal, Resolution: Unresolved}
	path := g.ShortestFlow(from, goal)
	if path.Resolution != Resolved {
		return advice
	}
	advice.Resolution = Resolved
	advice.RemainingCost = path.Cost
	if len(path.Edges) > 0 {
		first := path.Edges[0]
		advice.NextTransition = first.Transition
		advice.NextTo = first.To
		advice.NextCostClass = first.CostClass
	}
	return advice
}

// IsLegalMove reports whether a transition is a legal out-edge from a state in
// this graph — i.e. taking it now would advance rather than hit friction. The
// controller uses this to distinguish a productive move from a friction move
// without re-deriving the state machine's guards.
func (g *Graph) IsLegalMove(from StateID, transition TransitionID) bool {
	for _, edge := range g.Out(from) {
		if edge.Transition == transition {
			return true
		}
	}
	return false
}
