package deliverycontrol

import "sort"

// Edge is a directed, costed control between two delivery-flow states: a single
// move an agent can make, priced by its cost class.
type Edge struct {
	Transition TransitionID
	From       StateID
	To         StateID
	CostClass  TransitionCostClass
	Cost       int
}

// Graph is a weighted directed graph of delivery-flow controls. It is the
// optimization projection of the single registry declaration: the same
// transitions the conformance projection audits, arranged as a graph a
// deterministic oracle can score. Only state-changing transitions (To != "")
// become edges; read-only observes do not advance state and so are not moves on
// this graph.
type Graph struct {
	weights FlowCostWeights
	out     map[StateID][]Edge
	nodes   map[StateID]bool
}

// NewGraph returns an empty graph priced by the given weights.
func NewGraph(weights FlowCostWeights) *Graph {
	return &Graph{
		weights: weights,
		out:     map[StateID][]Edge{},
		nodes:   map[StateID]bool{},
	}
}

// AddEdge adds a directed edge priced by the transition's cost class. An edge
// whose cost class has no defined weight is skipped (the well-formedness
// conformance test forbids that in the registry, so this only guards ad-hoc
// graphs). Endpoints are registered as nodes even when the class is unknown, so
// a state that only appears on a skipped edge is still a known node.
func (g *Graph) AddEdge(from, to StateID, transition TransitionID, class TransitionCostClass) {
	g.nodes[from] = true
	g.nodes[to] = true
	cost, ok := g.weights.Cost(class)
	if !ok {
		return
	}
	g.out[from] = append(g.out[from], Edge{
		Transition: transition,
		From:       from,
		To:         to,
		CostClass:  class,
		Cost:       cost,
	})
}

// Out returns the out-edges of a state in insertion order (deterministic).
func (g *Graph) Out(state StateID) []Edge {
	return g.out[state]
}

// Has reports whether a state is a known node (appears as an edge endpoint).
// The oracle uses this to return Unresolved for a state it has never seen rather
// than fabricate a path from nowhere.
func (g *Graph) Has(state StateID) bool {
	return g.nodes[state]
}

// Nodes returns every known state, sorted for deterministic iteration.
func (g *Graph) Nodes() []StateID {
	out := make([]StateID, 0, len(g.nodes))
	for n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RegistryGraph projects the registry into a costed graph: one edge per
// (From, To) pair of every transition that changes delivery state. A transition
// with an empty To (a pure observation) advances nothing and contributes no
// edge; a transition with an empty From (delivery.next, resolved from any state)
// likewise contributes no edge because it changes no state. Iteration order
// follows the registry declaration, so the projection is deterministic.
func RegistryGraph(weights FlowCostWeights) *Graph {
	g := NewGraph(weights)
	for _, tr := range Transitions() {
		if tr.To == "" {
			continue
		}
		for _, from := range tr.From {
			g.AddEdge(from, tr.To, tr.ID, tr.CostClass)
		}
	}
	return g
}
