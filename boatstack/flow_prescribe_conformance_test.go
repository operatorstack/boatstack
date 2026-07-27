package boatstack

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: prescribed-command-is-legal-lowest-cost-or-none
//
// `flow next` now emits the exact runnable command for the oracle's lowest-cost
// next move. The contract these tests hold: the prescribed command is always the
// registry CLIVerb of the oracle's chosen (and therefore legal) transition; it
// never fabricates human-owed inputs (evidence, gate status, preview fingerprint,
// reviewer identity); and when the flow position cannot be assembled faithfully it
// prescribes NOTHING rather than a guess.

// forwardStates pairs each resolvable slice-lifecycle state with the transition
// the oracle takes toward the published goal from it.
var forwardStates = []struct {
	state      deliverycontrol.StateID
	transition deliverycontrol.TransitionID
	verb       string
}{
	{deliverycontrol.StateBuild, "delivery.record_gate_test", "record-delivery-gate"},
	{deliverycontrol.StateTestPassed, "delivery.record_gate_review", "record-delivery-gate"},
	{deliverycontrol.StateReviewPassed, "delivery.publish", "publish-pr"},
}

func syntheticStatus() NextStatus {
	return NextStatus{ActiveSlice: "slice-a", Feature: "demo"}
}

// Positive: from REVIEW_PASSED the prescribed command is publish-pr for the
// delivery.publish transition, carrying the derivable preview/action and owing
// only the human-confirmed fingerprint.
func TestPrescribePublishFromReviewPassed(t *testing.T) {
	cmd, ok := prescribeCommand("/repo", "demo", syntheticStatus(), PublishTransition)
	if !ok {
		t.Fatal("expected a prescribed command for delivery.publish")
	}
	if cmd.Verb != "publish-pr" || cmd.Transition != PublishTransition {
		t.Errorf("verb/transition = %s/%s, want publish-pr/delivery.publish", cmd.Verb, cmd.Transition)
	}
	if cmd.AutoDerivable {
		t.Error("publish must not be auto-derivable: it owes a human-confirmed fingerprint")
	}
	if !contains(cmd.RequiresHumanInput, "--preview-fingerprint") {
		t.Errorf("publish must require --preview-fingerprint; got %v", cmd.RequiresHumanInput)
	}
	if !contains(cmd.Args, "--preview") || !contains(cmd.Args, "--action") {
		t.Errorf("publish should derive --preview and --action; got %v", cmd.Args)
	}
}

// Relation: the emitted verb is exactly the registry CLIVerb of the transition,
// and the transition prescribed for each state is exactly the oracle's lowest-cost
// next edge from that state. Prescription can never diverge from the registry or
// from the oracle.
func TestPrescribedVerbMatchesRegistryAndOracle(t *testing.T) {
	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	for _, fs := range forwardStates {
		advice := graph.Advise(fs.state, flowGoal)
		if advice.Resolution != deliverycontrol.Resolved {
			t.Fatalf("oracle could not resolve from %s", fs.state)
		}
		if advice.NextTransition != fs.transition {
			t.Errorf("oracle next from %s = %s, want %s", fs.state, advice.NextTransition, fs.transition)
		}
		cmd, ok := prescribeCommand("/repo", "demo", syntheticStatus(), advice.NextTransition)
		if !ok {
			t.Fatalf("no prescription for oracle edge %s", advice.NextTransition)
		}
		desc, _ := deliverycontrol.Transition(advice.NextTransition)
		if cmd.Verb != desc.CLIVerb {
			t.Errorf("prescribed verb %q != registry CLIVerb %q for %s", cmd.Verb, desc.CLIVerb, advice.NextTransition)
		}
		if cmd.Verb != fs.verb {
			t.Errorf("prescribed verb %q != expected %q for %s", cmd.Verb, fs.verb, fs.state)
		}
	}
}

// Negative: the prescribed transition is always a LEGAL move from the state it is
// prescribed at — never a friction/illegal move the real machine would reject.
func TestPrescribedMoveIsLegalFromItsState(t *testing.T) {
	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	for _, fs := range forwardStates {
		if !graph.IsLegalMove(fs.state, fs.transition) {
			t.Errorf("prescribed transition %s is not legal from %s (would be friction)", fs.transition, fs.state)
		}
	}
}

// Bypass: no human-owed flag is ever placed in Args (never fabricated); the gate
// commands owe evidence + status, and the review gate additionally owes reviewer
// identity/method. The auto-derivable flag is true iff nothing is owed.
func TestPrescribeNeverFabricatesHumanInput(t *testing.T) {
	for _, fs := range forwardStates {
		cmd, ok := prescribeCommand("/repo", "demo", syntheticStatus(), fs.transition)
		if !ok {
			t.Fatalf("no prescription for %s", fs.transition)
		}
		for _, owed := range cmd.RequiresHumanInput {
			if contains(cmd.Args, owed) {
				t.Errorf("%s: human-owed flag %q leaked into auto-derived Args %v", fs.transition, owed, cmd.Args)
			}
		}
		if cmd.AutoDerivable != (len(cmd.RequiresHumanInput) == 0) {
			t.Errorf("%s: AutoDerivable=%t but RequiresHumanInput=%v", fs.transition, cmd.AutoDerivable, cmd.RequiresHumanInput)
		}
		// None of the forward gate/publish moves are auto-drivable today: each owes
		// evidence, status, or a fingerprint that must not be fabricated.
		if cmd.AutoDerivable {
			t.Errorf("%s: no forward gate/publish move should be auto-derivable (all owe human input)", fs.transition)
		}
	}
	// The CommandLine rendering surfaces owed inputs as explicit <REQUIRED>
	// placeholders, so it is never a fabricated, runnable-as-is command.
	cmd, _ := prescribeCommand(".", "demo", syntheticStatus(), PublishTransition)
	if !strings.Contains(cmd.CommandLine(), "--preview-fingerprint <REQUIRED>") {
		t.Errorf("CommandLine must mark owed input as <REQUIRED>; got %q", cmd.CommandLine())
	}
}

// Failure-state: a transition that is not a faithfully-assemblable move
// prescribes nothing — the caller emits no command rather than a guess.
// Observation rows are assembled by prescribeObserve, never here; unknown
// transitions are never assembled anywhere. (delivery.undo, record_change, and
// discard_delivery ARE assemblable since the solution set enumerates every
// legal edge; their fidelity is held by the closure conformance sweeps.)
func TestPrescribeEmitsNothingForUnassemblableTransition(t *testing.T) {
	for _, id := range []deliverycontrol.TransitionID{
		"delivery.status", "delivery.recovery_status", "not.a.transition",
	} {
		if cmd, ok := prescribeCommand("/repo", "demo", syntheticStatus(), id); ok {
			t.Errorf("transition %s should not be prescribed as a forward move; got %+v", id, cmd)
		}
	}
	// The rework/recovery edges owe exactly their human facts, never fabricated.
	for id, owed := range map[deliverycontrol.TransitionID][]string{
		"delivery.record_change":    {"--message", "--source-stage", "--classification"},
		"delivery.undo":             {"--mutation"},
		"delivery.discard_delivery": nil,
	} {
		cmd, ok := prescribeCommand("/repo", "demo", syntheticStatus(), id)
		if !ok {
			t.Fatalf("edge %s must be assemblable for the solution set", id)
		}
		if len(cmd.RequiresHumanInput) != len(owed) {
			t.Errorf("%s owes %v, got %v", id, owed, cmd.RequiresHumanInput)
			continue
		}
		for i, flag := range owed {
			if cmd.RequiresHumanInput[i] != flag {
				t.Errorf("%s owes %v, got %v", id, owed, cmd.RequiresHumanInput)
			}
		}
	}
}
