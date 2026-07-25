package deliverycontrol

// FlowCostWeights maps a TransitionCostClass to its J_flow cost. Per the cmg
// model (../../notes/delivery-flow-navigation-model.md): a normal
// move/observe/inspect costs 1; a denied/blocked committed mutation is friction
// and costs 3 (it burns a turn and returns nothing).
type FlowCostWeights map[TransitionCostClass]int

// DefaultFlowCostWeights is the cost model the cmg prototype pins
// (composable-model-graph:python/examples/12-agent-trajectory/main.py).
func DefaultFlowCostWeights() FlowCostWeights {
	return FlowCostWeights{
		CostObserve:  1,
		CostInspect:  1,
		CostQuery:    1,
		CostMutation: 1,
		CostRecovery: 1,
		CostFriction: 3,
	}
}

// Cost returns the weight for a cost class and whether it is defined.
func (w FlowCostWeights) Cost(class TransitionCostClass) (int, bool) {
	v, ok := w[class]
	return v, ok
}
