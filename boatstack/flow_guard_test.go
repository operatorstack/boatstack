package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// Positive: a productive move is always allowed. Publishing from REVIEW_PASSED is
// the legal out-edge, so the controller must not interfere.
func TestGuardAllowsProductiveMove(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	gateSlice(t, repo, feature, "phase-one")

	guard := GuardFlowMove(repo, feature, PublishTransition)
	if !guard.Allow {
		t.Fatalf("publish from REVIEW_PASSED must be allowed; got deny %q", guard.Message)
	}
	if guard.From != deliverycontrol.StateReviewPassed {
		t.Errorf("guard.From = %s, want REVIEW_PASSED", guard.From)
	}
}

// Negative: publishing before review is proven friction — illegal from BUILD, so
// the real machine would reject it too. The controller pre-denies with guidance.
func TestGuardDeniesPublishBeforeReview(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)

	guard := GuardFlowMove(repo, feature, PublishTransition)
	if guard.Allow {
		t.Fatal("publish from BUILD must be pre-denied as friction")
	}
	if guard.From != deliverycontrol.StateBuild {
		t.Errorf("guard.From = %s, want BUILD", guard.From)
	}
	if guard.Message == "" {
		t.Error("a denied guard must carry guidance")
	}
}

// Negative: a review gate before the test gate is friction from BUILD.
func TestGuardDeniesReviewGateBeforeTest(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)

	guard := GuardFlowMove(repo, feature, GateTransition("review"))
	if guard.Allow {
		t.Fatal("review gate from BUILD must be pre-denied as friction")
	}
}

// Relation: the kill switch restores prior behavior exactly — the controller
// allows the move and defers entirely to the real handler.
func TestGuardKillSwitchAllows(t *testing.T) {
	t.Setenv(flowControlKillSwitch, "0")
	repo, feature := activateTwoSliceDelivery(t)

	if guard := GuardFlowMove(repo, feature, PublishTransition); !guard.Allow {
		t.Fatal("kill switch must restore prior (allow) behavior")
	}
}

// Bypass guard: verbs outside the enforceable-friction allowlist are never
// pre-denied, even when illegal in the graph — the real handler stays the sole
// authority for mode-sensitive/re-entrant moves.
func TestGuardNeverDeniesNonEnforceableVerb(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)

	// record_change is illegal from BUILD in the graph, but it is not enforceable.
	guard := GuardFlowMove(repo, feature, deliverycontrol.TransitionID("delivery.record_change"))
	if !guard.Allow {
		t.Fatal("non-enforceable verb must never be pre-denied")
	}
	// The test gate is legal from BUILD and also not enforced.
	if guard := GuardFlowMove(repo, feature, GateTransition("test")); !guard.Allow {
		t.Fatal("test gate from BUILD must be allowed")
	}
}

// Failure-state: an unresolved flow position falls back to allow — the controller
// never acts on a guessed state.
func TestGuardUnresolvedAllows(t *testing.T) {
	repo := prTestRepo(t)

	if guard := GuardFlowMove(repo, "", PublishTransition); !guard.Allow {
		t.Fatal("unresolved flow must fall back to allow")
	} else if guard.Resolved {
		t.Error("expected the guard to report the position unresolved")
	}
}
