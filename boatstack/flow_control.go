package boatstack

import (
	"fmt"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// flowGoal is the accepted end state of a delivery flow — the sink the oracle
// scores paths toward.
const flowGoal = deliverycontrol.StatePublished

// FlowCheck runs the static conformance + liveness gate over the owned delivery
// model. It reads no repository state — it validates the single declaration and
// its graph — so the CLI gate is deterministic and side-effect free.
func FlowCheck() deliverycontrol.CheckResult {
	return deliverycontrol.Check()
}

// FormatFlowCheck renders a FlowCheck result as human-facing lines. A sound model
// reports PASS; drift lists the registry issues and any deadlocked or
// goal-unreachable states so the fault is actionable, not just a red exit code.
func FormatFlowCheck(result deliverycontrol.CheckResult) string {
	var b strings.Builder
	if result.OK {
		fmt.Fprintf(&b, "PASS: delivery flow model is conformant and live (goal %s)\n", result.Liveness.Goal)
	} else {
		fmt.Fprintf(&b, "BLOCKED: delivery flow model failed the static check (goal %s)\n", result.Liveness.Goal)
	}
	for _, issue := range result.RegistryIssues {
		fmt.Fprintf(&b, "REGISTRY_ISSUE=%s\n", issue)
	}
	for _, s := range result.Liveness.Deadlocks {
		fmt.Fprintf(&b, "DEADLOCK=%s\n", s)
	}
	for _, s := range result.Liveness.GoalUnreachable {
		fmt.Fprintf(&b, "GOAL_UNREACHABLE=%s\n", s)
	}
	fmt.Fprintf(&b, "REACHABLE=%d LIVE=%t\n", len(result.Liveness.Reachable), result.Liveness.Live)
	return b.String()
}

// flowStateFromStage maps a read-only NextStatus.ObservedStage to a
// delivery-flow StateID. It resolves ONLY the concrete slice-lifecycle stages,
// where the position is unambiguous; every planning, ambiguous, or invalid stage
// returns false so callers fall back to existing behavior rather than act on a
// guessed position. This conservative mapping is what keeps flow control from
// ever interfering with pre-activation or ambiguous flows.
func flowStateFromStage(stage string) (deliverycontrol.StateID, bool) {
	switch stage {
	case "BUILD":
		return deliverycontrol.StateBuild, true
	case "TEST_PASSED":
		return deliverycontrol.StateTestPassed, true
	case "REVIEW_PASSED", "PR_PREVIEW":
		return deliverycontrol.StateReviewPassed, true
	case "PUBLISHED", "FEATURE_COMPLETE":
		return deliverycontrol.StatePublished, true
	default:
		return "", false
	}
}

// CurrentFlowState resolves the delivery-flow state of the addressable slice via
// the read-only ResolveNext projection. The boolean is false whenever the
// position cannot be trusted — an error, a non-VERIFIED status (blocked,
// ambiguous, uninitialized), or a stage that is not a concrete slice-lifecycle
// state. Callers must treat false as "unknown" and never fabricate a position.
func CurrentFlowState(repo, feature string) (deliverycontrol.StateID, bool) {
	status, err := ResolveNext(repo, feature)
	if err != nil {
		return "", false
	}
	if status.VerificationStatus != "VERIFIED" {
		return "", false
	}
	return flowStateFromStage(status.ObservedStage)
}

// FlowNext is the advisory answer for `flow next`: the current delivery-flow
// state, the real recommended operation (from ResolveNext — the authoritative
// next-move table), and the oracle's lowest-cost next control plus the remaining
// cost to the goal. It is purely advisory; it changes no command, gate, or
// authority. Resolved is false when the oracle cannot place the flow, in which
// case only the real recommendation is meaningful.
type FlowNext struct {
	Resolved      bool                         `json:"resolved"`
	State         deliverycontrol.StateID      `json:"state,omitempty"`
	Goal          deliverycontrol.StateID      `json:"goal"`
	RecommendedOp string                       `json:"recommended_operation"`
	OracleNext    deliverycontrol.TransitionID `json:"oracle_next_transition,omitempty"`
	RemainingCost int                          `json:"remaining_flow_cost"`
	Reason        string                       `json:"reason"`
}

// NextControl composes the authoritative read-only recommendation (ResolveNext)
// with the deterministic oracle to advise the lowest-cost next move toward a
// published delivery. It performs no mutation and is safe to call at any time.
func NextControl(repo, feature string) (FlowNext, error) {
	status, err := ResolveNext(repo, feature)
	if err != nil {
		return FlowNext{}, err
	}
	out := FlowNext{
		Goal:          flowGoal,
		RecommendedOp: status.NextOperation,
		Reason:        status.Reason,
	}

	var state deliverycontrol.StateID
	resolved := false
	if status.VerificationStatus == "VERIFIED" {
		state, resolved = flowStateFromStage(status.ObservedStage)
	}
	if !resolved {
		return out, nil
	}
	out.State = state

	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	advice := graph.Advise(state, flowGoal)
	if advice.Resolution == deliverycontrol.Resolved {
		out.Resolved = true
		out.OracleNext = advice.NextTransition
		out.RemainingCost = advice.RemainingCost
	}
	return out, nil
}

// FormatFlowNext renders a FlowNext advisory as human-facing lines. The
// recommended operation always comes from the authoritative next-move table; the
// oracle line is shown only when the flow position resolves, and is explicitly
// labeled advisory so it is never mistaken for a gate.
func FormatFlowNext(next FlowNext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recommended: %s\n", next.RecommendedOp)
	if next.Reason != "" {
		fmt.Fprintf(&b, "Reason: %s\n", next.Reason)
	}
	if next.Resolved {
		fmt.Fprintf(&b, "Flow state: %s -> goal %s\n", next.State, next.Goal)
		fmt.Fprintf(&b, "Advisory (flow oracle): next %s, remaining cost %d\n", next.OracleNext, next.RemainingCost)
	} else {
		fmt.Fprintf(&b, "Flow state: unresolved (no oracle advisory)\n")
	}
	return b.String()
}
