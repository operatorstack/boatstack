package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: execute-drives-only-safe-derivable-moves
//
// The opt-in execute driver runs a move ONLY when three things hold at once: the
// caller asked to execute (--execute), the move owes no human input
// (AutoDerivable), and its transition is on the auto-drive allowlist. Any of those
// missing means prescribe-and-stop — the operator supplies what only a human can.
// The contract, by class: an allowlisted derivable move with execute on runs
// (Positive); an unresolved position never runs anything (Negative); a derivable
// move whose transition is off the allowlist does not run (Relation); an
// allowlisted move that still owes human input is never run — the driver cannot
// fabricate that input (Bypass); and the kill switch or an execute-off invocation
// forces prescribe even for an otherwise-runnable move (Failure-state).

// syntheticTransition is an allowlisted transition used only by these tests, so the
// decision logic can be exercised without depending on the production allowlist
// (which is intentionally empty of forward moves).
const syntheticTransition = deliverycontrol.TransitionID("test.synthetic_safe_move")

func syntheticAllowlist() map[deliverycontrol.TransitionID]bool {
	return map[deliverycontrol.TransitionID]bool{syntheticTransition: true}
}

// derivableMove is a resolved FlowNext whose prescribed command owes no human input
// and rides an allowlisted transition — the only shape the driver may execute.
func derivableMove() FlowNext {
	return FlowNext{
		Resolved: true,
		Prescribed: &PrescribedCommand{
			Verb:          "observe",
			AutoDerivable: true,
			Transition:    syntheticTransition,
		},
	}
}

// Positive: execute requested, kill switch off, move derivable and allowlisted ->
// the driver executes it.
func TestDecideDriveExecutesDerivableAllowlisted(t *testing.T) {
	d := decideDrive(derivableMove(), true, false, syntheticAllowlist())
	if d.Action != DriveExecute {
		t.Fatalf("expected DriveExecute for a derivable allowlisted move, got %s (%s)", d.Action, d.Reason)
	}
	if d.Command == nil {
		t.Error("an executable decision must carry the command it drives")
	}
}

// Negative: an unresolved position (no prescribed command) never drives anything,
// even with execute on — there is nothing legal to run.
func TestDecideDriveUnresolvedDrivesNothing(t *testing.T) {
	d := decideDrive(FlowNext{Resolved: false}, true, false, syntheticAllowlist())
	if d.Action != DriveNone {
		t.Fatalf("unresolved flow must decide DriveNone, got %s", d.Action)
	}
	if d.Command != nil {
		t.Error("DriveNone must carry no command")
	}
}

// Relation: only allowlisted transitions execute. A derivable move whose transition
// is NOT on the allowlist is prescribed, not run — allowlisting is necessary.
func TestDecideDriveOffAllowlistPrescribes(t *testing.T) {
	next := derivableMove()
	d := decideDrive(next, true, false, map[deliverycontrol.TransitionID]bool{})
	if d.Action != DrivePrescribe {
		t.Fatalf("a derivable move off the allowlist must prescribe, got %s", d.Action)
	}
	if !canAutoDrive(next.Prescribed, syntheticAllowlist()) {
		t.Error("sanity: the same move on the allowlist should be auto-drivable")
	}
	if canAutoDrive(next.Prescribed, map[deliverycontrol.TransitionID]bool{}) {
		t.Error("a move off the allowlist must never report auto-drivable")
	}
}

// Bypass: a move that still owes human input is never executed, even when its
// transition is allowlisted — the driver cannot fabricate the missing input, so it
// prescribes-and-stops. This is the core safety property.
func TestDecideDriveHumanGatedNeverExecutes(t *testing.T) {
	next := FlowNext{
		Resolved: true,
		Prescribed: &PrescribedCommand{
			Verb:               "record-gate",
			AutoDerivable:      false,
			RequiresHumanInput: []string{"--status", "--evidence"},
			Transition:         syntheticTransition, // allowlisted, yet still owes input
		},
	}
	d := decideDrive(next, true, false, syntheticAllowlist())
	if d.Action != DrivePrescribe {
		t.Fatalf("a human-gated move must prescribe even when allowlisted, got %s", d.Action)
	}
	if canAutoDrive(next.Prescribed, syntheticAllowlist()) {
		t.Error("a move owing human input must never be auto-drivable")
	}
}

// Bypass (production allowlist): gate moves remain excluded. Publish eligibility
// alone is insufficient because canAutoDrive also requires an exact fully-derived
// command (which only a current PR receipt can produce).
func TestProductionAllowlistContainsOnlyReceiptBoundPublish(t *testing.T) {
	gates := []deliverycontrol.TransitionID{
		deliverycontrol.TransitionID("delivery.record_gate_test"),
		deliverycontrol.TransitionID("delivery.record_gate_review"),
	}
	for _, tr := range gates {
		if autoDrivableTransitions[tr] {
			t.Errorf("human-gated transition %s must not be on the auto-drive allowlist", tr)
		}
	}
	if !autoDrivableTransitions[PublishTransition] {
		t.Fatal("receipt-bound publish transition must be allowlisted")
	}
	manual := &PrescribedCommand{Verb: "publish-pr", Transition: PublishTransition, RequiresHumanInput: []string{"--preview-fingerprint"}}
	if canAutoDrive(manual, autoDrivableTransitions) {
		t.Fatal("allowlisted publish with unresolved human input must not execute")
	}
}

// Failure-state: the kill switch (BOATSTACK_FLOW_DRIVE=0) and an execute-off
// invocation both force prescribe-and-stop, even for an otherwise-executable move.
func TestDecideDriveKillSwitchAndOptOutPrescribe(t *testing.T) {
	killed := decideDrive(derivableMove(), true, true, syntheticAllowlist())
	if killed.Action != DrivePrescribe {
		t.Errorf("kill switch must force prescribe, got %s", killed.Action)
	}
	optOut := decideDrive(derivableMove(), false, false, syntheticAllowlist())
	if optOut.Action != DrivePrescribe {
		t.Errorf("execute-off must prescribe, got %s", optOut.Action)
	}
}
