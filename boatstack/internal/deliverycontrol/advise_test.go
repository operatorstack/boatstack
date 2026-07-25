package deliverycontrol

import "testing"

func TestCheckPasses(t *testing.T) {
	result := Check()
	if !result.OK {
		t.Fatalf("static flow check failed: registry=%v live=%v", result.RegistryIssues, result.Liveness.Live)
	}
}

func TestAdviseRecommendsFirstOracleStep(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())

	advice := g.Advise(StateBuild, StatePublished)
	if advice.Resolution != Resolved {
		t.Fatalf("expected resolved advice, got %s", advice.Resolution)
	}
	if advice.NextTransition != "delivery.record_gate_test" {
		t.Errorf("next move from BUILD = %s, want delivery.record_gate_test", advice.NextTransition)
	}
	if advice.NextTo != StateTestPassed {
		t.Errorf("next state = %s, want TEST_PASSED", advice.NextTo)
	}
	if advice.RemainingCost != 3 {
		t.Errorf("remaining cost = %d, want 3", advice.RemainingCost)
	}
}

func TestAdviseSameStateHasNoNextMove(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	advice := g.Advise(StatePublished, StatePublished)
	if advice.Resolution != Resolved || advice.NextTransition != "" || advice.RemainingCost != 0 {
		t.Errorf("goal-reached advice should be resolved with no move: %+v", advice)
	}
}

func TestAdviseUnresolved(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	if advice := g.Advise(StateID("BOGUS"), StatePublished); advice.Resolution != Unresolved || advice.NextTransition != "" {
		t.Errorf("unknown state must not recommend a move: %+v", advice)
	}
}

func TestIsLegalMove(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	if !g.IsLegalMove(StateReviewPassed, "delivery.publish") {
		t.Error("publish should be legal from REVIEW_PASSED")
	}
	if g.IsLegalMove(StateBuild, "delivery.publish") {
		t.Error("publish must not be legal from BUILD (that is friction)")
	}
}
