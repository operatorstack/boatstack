package deliverycontrol

import "testing"

// Incident-graph node names, from the cmg design note
// (../../notes/delivery-flow-navigation-model.md). The published-slice incident
// is modeled there as a small costed graph; this test rebuilds that graph and
// walk and runs the real Go oracle over it, pinning the note's SELF-CHECK
// numbers (J_flow=15, J_flow*=5, regret=10) to this implementation.
const (
	sNeedsFix    StateID = "NEEDS_FIX"
	sObserved    StateID = "OBSERVED"
	sPushDenied  StateID = "PUSH_DENIED"
	sUndoBlocked StateID = "UNDO_BLOCKED"
	sRepair      StateID = "REPAIR"
	sAsk         StateID = "ASK"
	sDiagnosed   StateID = "DIAGNOSED"
	sUpgraded    StateID = "UPGRADED"
	sRegated     StateID = "REGATED"
	sLanded      StateID = "LANDED"
)

func incidentGraph() *Graph {
	g := NewGraph(DefaultFlowCostWeights())
	// The unique low-cost exit of the start state: observe before acting.
	g.AddEdge(sNeedsFix, sObserved, "observe-state", CostObserve)
	// The friction region — reachable only by acting before observing.
	g.AddEdge(sNeedsFix, sPushDenied, "git-push", CostFriction)
	g.AddEdge(sPushDenied, sUndoBlocked, "undo", CostFriction)
	g.AddEdge(sUndoBlocked, sRepair, "undo-mutation", CostFriction)
	g.AddEdge(sRepair, sAsk, "repair-state", CostRecovery)
	g.AddEdge(sAsk, sObserved, "ask-then-observe", CostObserve)
	// The shared tail every path takes once oriented.
	g.AddEdge(sObserved, sDiagnosed, "diagnose", CostInspect)
	g.AddEdge(sDiagnosed, sUpgraded, "upgrade-helper", CostInspect)
	g.AddEdge(sUpgraded, sRegated, "re-gate", CostMutation)
	g.AddEdge(sRegated, sLanded, "publish-update", CostMutation)
	return g
}

// observedIncidentWalk is the real session from the note: the agent acts before
// it observes, hits friction on three committed/reversible mutations, then
// orients. Friction is derived by ChargedCostClass from the denied outcome — not
// hardcoded — so the model's "denied mutation = friction" rule is what produces
// the cost.
func observedIncidentWalk() Trajectory {
	deny := func(from StateID, id TransitionID, kind TransitionKind) TransitionAttempt {
		return TransitionAttempt{
			From: from, Transition: id, Goal: sLanded, Outcome: OutcomeDenied,
			CostClass: ChargedCostClass(kind, CostMutation, OutcomeDenied),
		}
	}
	allow := func(from StateID, id TransitionID, class TransitionCostClass) TransitionAttempt {
		return TransitionAttempt{
			From: from, Transition: id, Goal: sLanded, Outcome: OutcomeAllowed,
			CostClass: ChargedCostClass(KindObserve, class, OutcomeAllowed),
		}
	}
	return Trajectory{
		deny(sNeedsFix, "git-push", KindCommittedMutation),
		deny(sPushDenied, "undo", KindReversibleMutation),
		deny(sUndoBlocked, "undo-mutation", KindCommittedMutation),
		allow(sRepair, "repair-state", CostRecovery),
		allow(sAsk, "ask-then-observe", CostObserve),
		allow(sObserved, "diagnose", CostInspect),
		allow(sDiagnosed, "upgrade-helper", CostInspect),
		allow(sUpgraded, "re-gate", CostMutation),
		allow(sRegated, "publish-update", CostMutation),
	}
}

// control-law: flow-regret-matches-cmg-note
func TestPublishedSliceIncidentRegret(t *testing.T) {
	g := incidentGraph()
	weights := DefaultFlowCostWeights()

	// The oracle path is Dijkstra over the owned graph, not learned: observe first.
	oracle := g.ShortestFlow(sNeedsFix, sLanded)
	if oracle.Resolution != Resolved {
		t.Fatalf("oracle could not resolve the incident: %s", oracle.Resolution)
	}
	if oracle.Cost != 5 {
		t.Errorf("J_flow* = %d, want 5 (oracle path: %v)", oracle.Cost, edgeIDs(oracle.Edges))
	}
	if got := edgeIDs(oracle.Edges); len(got) != 5 || got[0] != "observe-state" {
		t.Errorf("oracle should exit via observe-state; got %v", got)
	}

	report := ComputeReport(observedIncidentWalk(), g, weights, sLanded)
	if report.Resolution != Resolved {
		t.Fatalf("report unresolved")
	}
	if report.JFlow != 15 {
		t.Errorf("J_flow = %d, want 15", report.JFlow)
	}
	if report.JFlowStar != 5 {
		t.Errorf("J_flow* = %d, want 5", report.JFlowStar)
	}
	if report.Regret != 10 {
		t.Errorf("regret = %d, want 10 (all of it in J_flow)", report.Regret)
	}
}

func TestChargedCostClass(t *testing.T) {
	cases := []struct {
		kind     TransitionKind
		declared TransitionCostClass
		outcome  Outcome
		want     TransitionCostClass
	}{
		{KindCommittedMutation, CostMutation, OutcomeDenied, CostFriction},
		{KindReversibleMutation, CostMutation, OutcomeDenied, CostFriction},
		{KindCommittedMutation, CostMutation, OutcomeAllowed, CostMutation},
		{KindObserve, CostObserve, OutcomeDenied, CostObserve},    // reads are never friction
		{KindRecovery, CostRecovery, OutcomeDenied, CostRecovery}, // recovery denial is not modeled as friction
	}
	for _, tc := range cases {
		if got := ChargedCostClass(tc.kind, tc.declared, tc.outcome); got != tc.want {
			t.Errorf("ChargedCostClass(%s,%s,%s) = %s, want %s", tc.kind, tc.declared, tc.outcome, got, tc.want)
		}
	}
}

// A trajectory whose goal the oracle cannot resolve reports Unresolved and no
// regret — never a fabricated baseline.
func TestComputeReportUnresolvedGoal(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	traj := Trajectory{{From: StatePending, Transition: "delivery.discard_delivery", Outcome: OutcomeAllowed, CostClass: CostMutation}}
	report := ComputeReport(traj, g, DefaultFlowCostWeights(), StatePublished)
	if report.Resolution != Unresolved {
		t.Errorf("resolution = %s, want Unresolved", report.Resolution)
	}
	if report.Regret != 0 || report.JFlowStar != 0 {
		t.Errorf("unresolved report must not fabricate a baseline: %+v", report)
	}
	if report.JFlow != 1 {
		t.Errorf("observed J_flow should still be measured: got %d, want 1", report.JFlow)
	}
}
