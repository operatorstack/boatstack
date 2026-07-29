package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// A recorded session with one friction attempt followed by the productive path
// reports the observed cost, the oracle baseline, the flow regret between them,
// and coding effort as a separate figure.
func TestFlowReportMeasuresSession(t *testing.T) {
	repo := prTestRepo(t)

	// Friction: publish attempted (and denied) from BUILD -> billed at 3.
	RecordFlowTransition(repo, PublishTransition, deliverycontrol.StateBuild, false)
	// Then the productive path, each move billed at 1.
	RecordFlowTransition(repo, GateTransition("test"), deliverycontrol.StateBuild, true)
	RecordFlowTransition(repo, GateTransition("review"), deliverycontrol.StateTestPassed, true)
	RecordFlowTransition(repo, PublishTransition, deliverycontrol.StateReviewPassed, true)

	RecordCodingEffort(repo, 2, "repair")
	RecordCodingEffort(repo, 0, "amend") // bare marker -> one unit

	report, err := FlowReport(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Steps != 4 {
		t.Errorf("steps = %d, want 4", report.Steps)
	}
	if report.JFlow != 6 { // 3 + 1 + 1 + 1
		t.Errorf("J_flow = %d, want 6", report.JFlow)
	}
	if report.Resolution != deliverycontrol.Resolved || report.JFlowStar != 3 || report.Regret != 3 {
		t.Errorf("oracle baseline wrong: %+v (want J_flow*=3, regret=3)", report)
	}
	if report.JCoding != 3 { // 2 + 1, never folded into regret
		t.Errorf("J_coding = %d, want 3", report.JCoding)
	}
}

// An empty session is well-defined: zero steps, no fabricated oracle baseline.
func TestFlowReportEmptySession(t *testing.T) {
	repo := prTestRepo(t)

	report, err := FlowReport(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Steps != 0 || report.JFlow != 0 || report.JCoding != 0 {
		t.Errorf("empty session should be all zero: %+v", report)
	}
	if report.Resolution != deliverycontrol.Unresolved {
		t.Errorf("empty session has no start, so the oracle must be unresolved; got %s", report.Resolution)
	}
}

// control-law: positive-flow-gaps-are-attributable-by-control-class
func TestFlowReportAttributesPositiveGapCategories(t *testing.T) {
	repo := prTestRepo(t)
	RecordFlowTransition(repo, PublishTransition, deliverycontrol.StateBuild, false)
	RecordFlowAttribution(repo, "readiness", deliverycontrol.CostQuery, false, "blocked")
	RecordFlowAttribution(repo, "repair.review_repair", deliverycontrol.CostFriction, true, "duplicate")
	report, err := FlowReport(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Regret <= 0 {
		t.Fatalf("fixture must produce positive regret: %+v", report)
	}
	if report.PositiveGapByCategory["readiness"] != 1 || report.PositiveGapByCategory["repair.review_repair"] != 3 {
		t.Fatalf("unexpected positive-gap attribution: %+v", report.PositiveGapByCategory)
	}
}
