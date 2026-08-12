package kernel

import (
	"strings"
	"testing"
)

func TestProgramFingerprintCanonicalizesSemanticSets(t *testing.T) {
	left, err := CompileProgram("canonical", "1", "kernel-v1", "idle", []string{"done", "closed"}, []Transition{{
		ID: "advance", SourceModes: []string{"ready", "idle"}, TargetMode: "done",
		ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective,
		RequiredCapabilities: []Capability{"state.write", "state.inspect"},
		OwnedFacets:          []string{"counter.value", "counter.audit"},
		Operation:            "counter.advance", Priority: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := CompileProgram("canonical", "1", "kernel-v1", "idle", []string{"closed", "done"}, []Transition{{
		ID: "advance", SourceModes: []string{"idle", "ready"}, TargetMode: "done",
		ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective,
		RequiredCapabilities: []Capability{"state.inspect", "state.write"},
		OwnedFacets:          []string{"counter.audit", "counter.value"},
		Operation:            "counter.advance", Priority: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if left.Fingerprint != right.Fingerprint {
		t.Fatalf("semantic reordering changed fingerprint: %s != %s", left.Fingerprint, right.Fingerprint)
	}
}

func TestProgramRejectsRecoveryThatCannotRunFromRecoveredSourceMode(t *testing.T) {
	_, err := CompileProgram("blocked-recovery", "1", "kernel-v1", "one", []string{"done"}, []Transition{
		{ID: "increment", SourceModes: []string{"one"}, TargetMode: "two", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 1},
		{ID: "recover", SourceModes: []string{"zero"}, TargetMode: "zero", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"increment"}},
	})
	if err == nil || !strings.Contains(err.Error(), `cannot recover "increment" from source mode "one"`) {
		t.Fatalf("compile error = %v", err)
	}
}
