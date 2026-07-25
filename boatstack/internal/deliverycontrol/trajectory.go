package deliverycontrol

// Outcome is what happened when an agent attempted a control.
type Outcome string

const (
	// OutcomeAllowed: the control was accepted and advanced (or observed) state.
	OutcomeAllowed Outcome = "allowed"
	// OutcomeDenied: the control was refused — a blocked committed mutation is the
	// friction the model prices at 3 (a burned turn that returns nothing).
	OutcomeDenied Outcome = "denied"
)

// TransitionAttempt is one recorded move on the delivery-flow graph: the control
// an agent tried, from which state, and how it landed. Trajectories are built
// from these, append-only.
type TransitionAttempt struct {
	Sequence   int                 `json:"sequence"`
	From       StateID             `json:"from"`
	Transition TransitionID        `json:"transition"`
	Goal       StateID             `json:"goal,omitempty"`
	Outcome    Outcome             `json:"outcome"`
	CostClass  TransitionCostClass `json:"cost_class"`
	Note       string              `json:"note,omitempty"`
}

// Trajectory is an ordered walk of attempts — one real (or replayed) session.
type Trajectory []TransitionAttempt

// ChargedCostClass returns the cost class an attempt is billed at. A denied
// mutation (committed or reversible) is friction; every other outcome keeps the
// transition's declared class. Read-only controls are never denied in the model,
// so they always bill their own class. Centralizing the rule here keeps the
// recorder and the report in agreement.
func ChargedCostClass(kind TransitionKind, declared TransitionCostClass, outcome Outcome) TransitionCostClass {
	if outcome == OutcomeDenied {
		switch kind {
		case KindCommittedMutation, KindReversibleMutation:
			return CostFriction
		}
	}
	return declared
}

// FlowTrajectoryReport is the meter: the observed navigation cost of a walk,
// the oracle's cost for the same start→goal, and the regret between them. When
// the oracle cannot resolve the endpoints, Resolution is Unresolved and Regret
// is left at zero (there is no baseline to regret against — never a fabricated
// one).
type FlowTrajectoryReport struct {
	Start      StateID
	Goal       StateID
	JFlow      int
	JFlowStar  int
	Regret     int
	Steps      int
	Resolution Resolution
}

// WalkCost sums a trajectory's observed J_flow: each attempt billed at its
// charged cost class. Attempts whose class has no weight are skipped rather than
// silently counted as zero-defined.
func (t Trajectory) WalkCost(weights FlowCostWeights) int {
	total := 0
	for _, attempt := range t {
		if cost, ok := weights.Cost(attempt.CostClass); ok {
			total += cost
		}
	}
	return total
}

// ComputeReport measures a trajectory against the oracle. Start is the walk's
// first From; the caller supplies the goal (the accepted end state B). J_flow is
// the observed walk cost; J_flow* is the oracle's shortest cost start→goal over
// the graph; Regret = J_flow − J_flow* when the oracle resolves.
func ComputeReport(t Trajectory, g *Graph, weights FlowCostWeights, goal StateID) FlowTrajectoryReport {
	report := FlowTrajectoryReport{
		Goal:       goal,
		JFlow:      t.WalkCost(weights),
		Steps:      len(t),
		Resolution: Unresolved,
	}
	if len(t) > 0 {
		report.Start = t[0].From
	}
	oracle := g.ShortestFlow(report.Start, goal)
	report.Resolution = oracle.Resolution
	if oracle.Resolution == Resolved {
		report.JFlowStar = oracle.Cost
		report.Regret = report.JFlow - oracle.Cost
	}
	return report
}
