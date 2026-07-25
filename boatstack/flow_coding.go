package boatstack

import (
	"os"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// RecordCodingEffort appends a best-effort coding-effort signal to the shadow
// log. Like the flow-transition recorder it never returns an error, never panics
// into a caller, and writes nothing when disabled — it honors the same
// BOATSTACK_FLOW_TRACE kill switch. Coding effort is telemetry only: it is never
// gated, never optimized, and never added to J_flow. It is stored in its own log
// so J_coding cannot be conflated with flow navigation cost.
//
// units is the coding effort to record (non-positive counts as one unit); note is
// an optional free-text marker for what the effort was.
func RecordCodingEffort(repo string, units int, note string) {
	// Telemetry must never take down a command.
	defer func() { _ = recover() }()

	if os.Getenv(flowTraceKillSwitch) == "0" {
		return
	}
	directory, err := flowLogDirectory(repo)
	if err != nil {
		return
	}
	_ = deliverycontrol.AppendCodingSignal(directory, deliverycontrol.CodingSignal{Units: units, Note: note})
}
