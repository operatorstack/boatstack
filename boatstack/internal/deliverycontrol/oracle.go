package deliverycontrol

// Resolution reports whether the oracle found a path. Unknown or unreachable
// states are Unresolved — the oracle never fabricates a route it cannot prove.
type Resolution string

const (
	Resolved   Resolution = "resolved"
	Unresolved Resolution = "unresolved"
)

// FlowPath is the oracle's answer: the lowest-cost sequence of controls from a
// start state to a goal state, or Unresolved when none exists.
type FlowPath struct {
	From       StateID
	Goal       StateID
	Edges      []Edge
	Cost       int
	Resolution Resolution
}

// ShortestFlow is the free oracle: Dijkstra over the owned graph for the
// lowest-cost walk from a state to a goal. It is deterministic — ties are broken
// by the frontier's insertion order, which follows the registry declaration — so
// the same graph and endpoints always yield the same path.
//
// It returns Unresolved, never a guess, when:
//   - the start state is not a known node (unknown/invalid state), or
//   - the goal is not a known node, or
//   - no path from start to goal exists.
//
// A start that already equals the goal resolves to a zero-cost, empty path.
func (g *Graph) ShortestFlow(from, goal StateID) FlowPath {
	result := FlowPath{From: from, Goal: goal, Resolution: Unresolved}

	if !g.Has(from) || !g.Has(goal) {
		return result
	}
	if from == goal {
		result.Cost = 0
		result.Resolution = Resolved
		return result
	}

	const unreached = -1
	dist := map[StateID]int{from: 0}
	prev := map[StateID]Edge{}
	visited := map[StateID]bool{}

	for {
		// Select the unvisited node with the smallest known distance. Nodes() is
		// sorted, so among equal distances the lexicographically-first node wins:
		// a stable, reproducible choice.
		current := StateID("")
		best := unreached
		for _, node := range g.Nodes() {
			if visited[node] {
				continue
			}
			d, seen := dist[node]
			if !seen {
				continue
			}
			if best == unreached || d < best {
				best = d
				current = node
			}
		}
		if best == unreached {
			// Frontier exhausted without reaching the goal.
			return result
		}
		if current == goal {
			break
		}
		visited[current] = true

		for _, edge := range g.Out(current) {
			candidate := dist[current] + edge.Cost
			if existing, seen := dist[edge.To]; !seen || candidate < existing {
				dist[edge.To] = candidate
				prev[edge.To] = edge
			}
		}
	}

	// Reconstruct the path from goal back to start.
	var reversed []Edge
	for at := goal; at != from; {
		edge, ok := prev[at]
		if !ok {
			return FlowPath{From: from, Goal: goal, Resolution: Unresolved}
		}
		reversed = append(reversed, edge)
		at = edge.From
	}
	edges := make([]Edge, len(reversed))
	for i, edge := range reversed {
		edges[len(reversed)-1-i] = edge
	}

	result.Edges = edges
	result.Cost = dist[goal]
	result.Resolution = Resolved
	return result
}
