package boatstack

import (
	"os"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// flowTraceKillSwitch disables shadow trajectory recording when set to "0".
// Recording is best-effort and off the critical path either way; the switch
// exists so an operator can silence it entirely without a rebuild.
const flowTraceKillSwitch = "BOATSTACK_FLOW_TRACE"

// flowLogDirectory is the append-only trajectory log location,
// <gitdir>/boatstack/flow, a sibling of the delivery-state directory. It reuses
// deliveryStateDirectory's Git-dir resolution so the two stay in lockstep.
func flowLogDirectory(repo string) (string, error) {
	return WorkspaceFor(repo).FlowDir()
}

// RecordFlowTransition appends a best-effort shadow record of one delivery-flow
// control attempt. It is deliberately inert with respect to command behavior:
// it never returns an error, never panics into a caller, and writes nothing when
// disabled or when anything goes wrong. Nothing consumes the log at runtime yet
// — this is the measurement substrate for the flow-navigation meter, not a
// control point.
//
// transition is the registry TransitionID being attempted; from is the
// delivery-flow state the attempt started in ("" if unknown); ok reports whether
// the underlying handler succeeded (a false ok on a mutation is billed as
// friction, per the cmg model).
func RecordFlowTransition(repo string, transition deliverycontrol.TransitionID, from deliverycontrol.StateID, ok bool) {
	// A trace must never take down a command. Swallow any panic from the
	// best-effort path.
	defer func() { _ = recover() }()

	if os.Getenv(flowTraceKillSwitch) == "0" {
		return
	}
	descriptor, found := deliverycontrol.Transition(transition)
	if !found {
		return
	}

	outcome := deliverycontrol.OutcomeAllowed
	if !ok {
		outcome = deliverycontrol.OutcomeDenied
	}
	directory, err := flowLogDirectory(repo)
	if err != nil {
		return
	}
	_ = deliverycontrol.AppendAttempt(directory, deliverycontrol.TransitionAttempt{
		From:       from,
		Transition: transition,
		Outcome:    outcome,
		CostClass:  deliverycontrol.ChargedCostClass(descriptor.Kind, descriptor.CostClass, outcome),
	})
}

func RecordFlowAttribution(repo, category string, cost deliverycontrol.TransitionCostClass, denied bool, note string) {
	defer func() { _ = recover() }()
	if os.Getenv(flowTraceKillSwitch) == "0" {
		return
	}
	directory, err := flowLogDirectory(repo)
	if err != nil {
		return
	}
	outcome := deliverycontrol.OutcomeAllowed
	if denied {
		outcome = deliverycontrol.OutcomeDenied
	}
	_ = deliverycontrol.AppendAttempt(directory, deliverycontrol.TransitionAttempt{
		From:       deliverycontrol.StateUninitialized,
		Transition: deliverycontrol.TransitionID("attribution." + category),
		Outcome:    outcome, CostClass: cost, Category: category, Note: note,
	})
}
