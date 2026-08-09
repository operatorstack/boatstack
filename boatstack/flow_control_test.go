package boatstack

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// FlowCheck must pass on the shipped model: the CLI gate would otherwise block
// every invocation.
func TestFlowCheckPassesOnShippedModel(t *testing.T) {
	result := FlowCheck()
	if !result.OK {
		t.Fatalf("shipped flow model failed static check: registry=%v deadlocks=%v goal-unreachable=%v",
			result.RegistryIssues, result.Liveness.Deadlocks, result.Liveness.GoalUnreachable)
	}
	if !result.Liveness.Live {
		t.Error("shipped model must be live")
	}
}

// CurrentFlowState resolves the concrete slice-lifecycle position from the
// read-only projection; NextControl advises the lowest-cost next move that
// matches the oracle.
func TestNextControlAdvisesFromBuild(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)

	state, ok := CurrentFlowState(repo, feature)
	if !ok || state != deliverycontrol.StateBuild {
		t.Fatalf("current flow state = %s (ok=%t), want BUILD", state, ok)
	}

	next, err := NextControl(repo, feature)
	if err != nil {
		t.Fatalf("NextControl: %v", err)
	}
	if next.RecommendedOp == "" {
		t.Error("expected an authoritative recommended operation")
	}
	if !next.Resolved {
		t.Fatal("expected the oracle to resolve a BUILD flow")
	}
	if next.OracleNext != "delivery.record_gate_test" {
		t.Errorf("oracle next = %s, want delivery.record_gate_test", next.OracleNext)
	}
	if next.RemainingCost != 3 {
		t.Errorf("remaining flow cost = %d, want 3", next.RemainingCost)
	}
}

// After the review gate the slice is REVIEW_PASSED, one low-cost publish from the
// goal.
func TestNextControlAdvisesFromReviewPassed(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	gateSlice(t, repo, feature, "phase-one")

	state, ok := CurrentFlowState(repo, feature)
	if !ok || state != deliverycontrol.StateReviewPassed {
		t.Fatalf("current flow state = %s (ok=%t), want REVIEW_PASSED", state, ok)
	}

	next, err := NextControl(repo, feature)
	if err != nil {
		t.Fatalf("NextControl: %v", err)
	}
	if !next.Resolved || next.OracleNext != "delivery.publish" || next.RemainingCost != 1 {
		t.Errorf("advisory from REVIEW_PASSED = %+v, want publish at cost 1", next)
	}
}

// A stage that is not a concrete slice-lifecycle position must resolve to unknown
// so callers fall back rather than act on a guess. Pre-activation stages are
// covered by prescribePlanning instead — a prescription, never a flow state.
func TestFlowStateFromStageIsConservative(t *testing.T) {
	for _, stage := range []string{"NOT_STARTED", "AMBIGUOUS", "INVALID_STATE", "POLICY_READY", "DRAFT_PLAN", ""} {
		if _, ok := flowStateFromStage(stage); ok {
			t.Errorf("stage %q must not resolve to a flow state", stage)
		}
	}
}

// A pre-activation stage keeps Resolved=false (flow-state conservativeness) yet
// still names its exact runnable command, and the rendering shows the honest
// pre-activation label rather than the bare unresolved line.
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
func TestNextControlPrescribesWithoutResolvingBeforeActivation(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "demo")

	next, err := NextControl(repo, "")
	if err != nil {
		t.Fatalf("NextControl: %v", err)
	}
	if next.Resolved {
		t.Fatal("pre-activation must never resolve a flow state")
	}
	if next.Prescribed == nil || next.Prescribed.Verb != "check-plan" {
		t.Fatalf("DRAFT_PLAN must prescribe check-plan: %+v", next.Prescribed)
	}
	out := FormatFlowNext(next)
	if !strings.Contains(out, "pre-activation (delivery oracle not engaged)") || !strings.Contains(out, "Run: .product-loop/boatstack check-plan") {
		t.Fatalf("pre-activation rendering must label the state and carry the Run line: %q", out)
	}
}
