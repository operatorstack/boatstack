package boatstack

import (
	"fmt"
	"os"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// flowControlKillSwitch disables active flow control when set to "0". Control is
// ON by default: the controller pre-denies a committed mutation the delivery
// graph proves is friction — illegal from the current state, so the real state
// machine would reject it anyway — and points at the low-cost move instead.
// Setting the switch to "0" restores the pre-control behavior exactly: the real
// handler still rejects the move, just without the early guidance.
const flowControlKillSwitch = "BOATSTACK_FLOW_CONTROL"

// enforceableFrictions is the conservative allowlist of committed mutations whose
// graph legality provably matches the real state-machine guard, so pre-denying an
// illegal attempt changes no outcome (the handler would reject it too) and only
// improves the message and the trajectory record:
//
//   - delivery.publish requires REVIEW_PASSED (CheckDeliveryReadyForShip);
//     publishing earlier is the canonical friction the flow model targets.
//   - delivery.record_gate_review requires TEST_PASSED; a review gate before the
//     test gate is rejected by the same guard.
//
// Verbs whose real guards are mode-sensitive or re-entrant — record-change, undo,
// discard, ignore, and re-recording the test gate — are deliberately excluded.
// They are never pre-denied here; the real handler remains the sole authority.
var enforceableFrictions = map[deliverycontrol.TransitionID]bool{
	deliverycontrol.TransitionID("delivery.publish"):            true,
	deliverycontrol.TransitionID("delivery.record_gate_review"): true,
}

// PublishTransition is the registry transition for publishing a delivery PR,
// exported so CLI wrappers can guard it without importing the internal package.
var PublishTransition = deliverycontrol.TransitionID("delivery.publish")

// FlowGuard is the decision of the flow-control choke point for one committed
// delivery mutation. Allow=false means the move was pre-denied as proven
// friction; Message carries the guidance to surface. From/Transition/Resolved
// describe the resolved flow position for the caller's trajectory record.
type FlowGuard struct {
	Allow      bool
	Message    string
	From       deliverycontrol.StateID
	Transition deliverycontrol.TransitionID
	Resolved   bool
}

// GuardFlowMove is the pure decision the CLI choke point consults before running
// an enforced committed mutation. It is conservative by construction and has no
// side effects (recording is the caller's job, so the outcome reflects the real
// handler):
//
//   - unresolved flow position -> Allow (never act on a guessed state);
//   - transition outside the enforceable-friction allowlist -> Allow;
//   - kill switch BOATSTACK_FLOW_CONTROL=0 -> Allow (restores prior behavior);
//   - legal (productive) move from the current state -> Allow;
//   - otherwise -> pre-deny with guidance toward the low-cost move.
//
// A pre-denied move is one the real state machine would reject anyway, so
// enforcement changes guidance and telemetry, never the outcome of a move the
// machine would have allowed.
func GuardFlowMove(repo, feature string, transition deliverycontrol.TransitionID) FlowGuard {
	from, resolved := CurrentFlowState(repo, feature)
	guard := FlowGuard{Allow: true, From: from, Transition: transition, Resolved: resolved}

	if !resolved || !enforceableFrictions[transition] || os.Getenv(flowControlKillSwitch) == "0" {
		return guard
	}

	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	if graph.IsLegalMove(from, transition) {
		return guard
	}

	guard.Allow = false
	advice := graph.Advise(from, flowGoal)
	if advice.Resolution == deliverycontrol.Resolved && advice.NextTransition != "" {
		guard.Message = fmt.Sprintf("flow control: %s is not available from %s; the low-cost next move is %s (set %s=0 to disable)",
			transition, from, advice.NextTransition, flowControlKillSwitch)
	} else {
		guard.Message = fmt.Sprintf("flow control: %s is not available from %s (set %s=0 to disable)",
			transition, from, flowControlKillSwitch)
	}
	return guard
}

// GateTransition maps a record-delivery-gate --gate value to its registry
// transition, or "" when the gate is not one the controller reasons about.
func GateTransition(gate string) deliverycontrol.TransitionID {
	switch gate {
	case "test":
		return deliverycontrol.TransitionID("delivery.record_gate_test")
	case "review":
		return deliverycontrol.TransitionID("delivery.record_gate_review")
	default:
		return ""
	}
}
