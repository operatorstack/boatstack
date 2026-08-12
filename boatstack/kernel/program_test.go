package kernel

import (
	"strings"
	"testing"
)

func TestProgramFingerprintCanonicalizesSemanticSets(t *testing.T) {
	left, err := CompileProgram("canonical", "1", "kernel-v1", "idle", []string{"done", "closed"}, []Transition{
		{
			ID: "advance", SourceModes: []string{"ready", "idle"}, TargetMode: "done",
			ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective,
			RequiredCapabilities: []Capability{"state.write", "state.inspect"},
			OwnedFacets:          []string{"counter.value", "counter.audit"},
			Operation:            "counter.advance", Priority: 1,
		},
		{ID: "recover", SourceModes: []string{"idle", "ready"}, TargetMode: "idle", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"state.write"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.recover", Priority: 2, Recovers: []string{"advance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := CompileProgram("canonical", "1", "kernel-v1", "idle", []string{"closed", "done"}, []Transition{
		{
			ID: "advance", SourceModes: []string{"idle", "ready"}, TargetMode: "done",
			ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective,
			RequiredCapabilities: []Capability{"state.inspect", "state.write"},
			OwnedFacets:          []string{"counter.audit", "counter.value"},
			Operation:            "counter.advance", Priority: 1,
		},
		{ID: "recover", SourceModes: []string{"ready", "idle"}, TargetMode: "idle", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"state.write"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.recover", Priority: 2, Recovers: []string{"advance"}},
	})
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

func TestProgramRejectsObjectiveDependentRecovery(t *testing.T) {
	_, err := CompileProgram("blocked-objective-recovery", "1", "kernel-v1", "idle", []string{"done"}, []Transition{
		{ID: "advance", SourceModes: []string{"idle"}, TargetMode: "done", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 1},
		{ID: "recover", SourceModes: []string{"idle"}, TargetMode: "idle", ObjectiveScope: ObjectiveBoundExact, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"advance"}},
	})
	if err == nil || !strings.Contains(err.Error(), `recovery transition "recover" must preserve objective state without requiring an exact objective`) {
		t.Fatalf("compile error = %v", err)
	}
}

func TestProgramRejectsTransitionWithoutDeclaredRecovery(t *testing.T) {
	_, err := CompileProgram("unrecoverable", "1", "kernel-v1", "idle", []string{"done"}, []Transition{{
		ID: "advance", SourceModes: []string{"idle"}, TargetMode: "done", ObjectiveScope: ObjectiveNone, ObjectiveMutation: PreserveObjective,
		RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 1,
	}})
	if err == nil || !strings.Contains(err.Error(), `transition "advance" has no declared recovery`) {
		t.Fatalf("compile error = %v", err)
	}
}

func TestProgramAcceptsQualifiedTransitionIdentities(t *testing.T) {
	program, err := CompileProgram("qualified", "1", "kernel-v1", "idle", []string{"done"}, []Transition{
		{ID: "example-program/advance", SourceModes: []string{"idle"}, TargetMode: "done", ObjectiveScope: ObjectiveBoundExact, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "example-program/advance", Priority: 1},
		{ID: "example-program/recover", SourceModes: []string{"idle"}, TargetMode: "idle", ObjectiveScope: ObjectiveOptionalPreserve, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "example-program/recover", Priority: 2, Recovers: []string{"example-program/advance", "example-program/recover"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := program.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := program.Transition("example-program/advance"); !ok {
		t.Fatal("qualified transition identity was not retained")
	}
}
