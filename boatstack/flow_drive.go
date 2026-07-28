package boatstack

import (
	"os"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// flowDriveKillSwitch is the org-level master off switch for the opt-in execute
// driver. Execution is OFF by default — it happens only when the caller passes
// --execute per invocation — and setting this switch to "0" refuses execution
// even then, falling back to prescribe-and-stop. It never affects the read-only
// advisory (`flow next` without --execute).
const flowDriveKillSwitch = "BOATSTACK_FLOW_DRIVE"

// autoDrivableTransitions is the conservative allowlist of transitions the execute
// driver may run without any human input. A transition qualifies ONLY if its full
// argument set is state-derivable and it is read-only or reversible with nothing to
// fabricate (no evidence, gate status, preview fingerprint, or reviewer identity).
//
// Today every forward productive move owes human input — the test/review gate
// recordings need evidence and a status; publish needs a human-confirmed
// fingerprint — so none of them are auto-drivable and this allowlist is
// deliberately empty of forward moves. The driver therefore prescribes-and-stops
// at the first move, which is the correct, safe behavior. The allowlist is the
// mechanism: it grows only as specific verbs gain provably-derivable defaults, and
// execution is double-gated (a verb must be BOTH allowlisted here AND have an
// explicit executor handler), so nothing runs by accident.
var autoDrivableTransitions = map[deliverycontrol.TransitionID]bool{}

// DriveAction is the driver's decision for one step.
type DriveAction string

const (
	// DriveNone: there is no prescribed next move (unresolved or terminal flow).
	DriveNone DriveAction = "none"
	// DrivePrescribe: emit the exact command for a human/CI to run, and stop.
	DrivePrescribe DriveAction = "prescribe"
	// DriveExecute: the move is safe and fully derivable; the driver may run it.
	DriveExecute DriveAction = "execute"
)

// DriveDecision is what the execute driver should do for one flow step. Command
// is the prescribed command from the flow oracle (nil only when Action is
// DriveNone); Reason explains the decision for the operator and telemetry.
type DriveDecision struct {
	Action  DriveAction        `json:"action"`
	Command *PrescribedCommand `json:"command,omitempty"`
	Reason  string             `json:"reason"`
}

// canAutoDrive reports whether the driver may run a prescribed command with no
// human input. Both conditions must hold: the command owes no human input
// (AutoDerivable) AND its transition is on the allowlist. A derivable command off
// the allowlist is not driven, and an allowlisted transition that still owes input
// is not driven. A foreign-program command (Program != "", e.g. the prescribed
// `gh pr merge`) is refused CATEGORICALLY, before the allowlist is even
// consulted: the driver executes boatstack-helper verbs only, so no future
// allowlist entry can ever make Boatstack run someone else's program.
// control-law: merged-terminal-prescribes-merge-never-executes-it
func canAutoDrive(cmd *PrescribedCommand, allowlist map[deliverycontrol.TransitionID]bool) bool {
	if cmd == nil || !cmd.AutoDerivable {
		return false
	}
	if cmd.Program != "" {
		return false
	}
	return allowlist[cmd.Transition]
}

// decideDrive is the pure decision at the heart of the execute driver. It never
// executes an unresolved, human-gated, or off-allowlist move: such moves are
// prescribed-and-stopped so the operator supplies what only a human can. The
// allowlist is a parameter so the decision is testable independently of the
// production set.
func decideDrive(next FlowNext, executeOptIn, killed bool, allowlist map[deliverycontrol.TransitionID]bool) DriveDecision {
	if next.Prescribed == nil {
		return DriveDecision{Action: DriveNone, Reason: "no prescribed next move (flow position unresolved or already at goal)"}
	}
	if !executeOptIn {
		return DriveDecision{Action: DrivePrescribe, Command: next.Prescribed, Reason: "execute not requested; run the prescribed command by hand"}
	}
	if killed {
		return DriveDecision{Action: DrivePrescribe, Command: next.Prescribed, Reason: "execute disabled by " + flowDriveKillSwitch + "=0; prescribe-and-stop"}
	}
	if canAutoDrive(next.Prescribed, allowlist) {
		return DriveDecision{Action: DriveExecute, Command: next.Prescribed, Reason: "auto-derivable allowlisted move"}
	}
	return DriveDecision{Action: DrivePrescribe, Command: next.Prescribed, Reason: "move owes human input or is off the auto-drive allowlist; prescribe-and-stop"}
}

// DecideDrive is the production decision for one flow step, using the conservative
// package allowlist. executeOptIn is the per-invocation --execute flag; killed is
// the BOATSTACK_FLOW_DRIVE=0 kill switch.
func DecideDrive(next FlowNext, executeOptIn, killed bool) DriveDecision {
	return decideDrive(next, executeOptIn, killed, autoDrivableTransitions)
}

// FlowDriveKilled reports whether the execute kill switch is set to "0". Exposed so
// CLI wrappers in other packages can read the switch without importing the constant.
func FlowDriveKilled() bool {
	return os.Getenv(flowDriveKillSwitch) == "0"
}
