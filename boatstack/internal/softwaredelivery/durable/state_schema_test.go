package durable

import "testing"

func TestStateRejectsPriorObjectiveSchema(t *testing.T) {
	state := State{SchemaVersion: StateSchemaVersion - 1}
	if err := state.Validate(); err == nil {
		t.Fatal("prior objective state schema was accepted")
	}
}
