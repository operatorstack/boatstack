package surfaces

import (
	"testing"
	"time"
)

func TestSurfaceSchemaIsFlagDayAndApplyRequiresPrescription(t *testing.T) {
	// control-law: every mutating surface reaches the exact prescription boundary
	base := Request{
		SchemaVersion: SchemaVersion,
		Operation:     OperationResolve,
		Repository:    "/repository",
		Host:          "cli",
		CorrelationID: "correlation",
	}
	old := base
	old.SchemaVersion--
	if err := old.Validate(time.Now()); err == nil {
		t.Fatal("older surface schema was accepted")
	}
	apply := base
	apply.Operation = OperationApply
	apply.FlowID = "flow"
	apply.TransitionID = "engagement.begin"
	if err := apply.Validate(time.Now()); err == nil {
		t.Fatal("apply without an exact prescription was accepted")
	}
}
