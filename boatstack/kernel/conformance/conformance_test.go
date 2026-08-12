package conformance

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

func TestMarkedStateCheckRejectsUntargetedPriorityCycle(t *testing.T) {
	targeted := integerPriorityCycleFixture()
	targetedRuntime := mustRuntime(t, targeted)
	transitions := append([]string{targeted.Scenario.BindTransition}, targeted.Scenario.AdvanceTransitions...)
	for _, transition := range transitions {
		resolveAndApply(t, targetedRuntime, targeted.Scenario, transition, &targeted.Scenario.Objective)
	}
	if !targeted.Program.Marked(targeted.Scenario.Snapshot().State.Mode) {
		t.Fatal("counterexample fixture must retain a targeted path to marked")
	}

	untargeted := integerPriorityCycleFixture()
	untargetedRuntime := mustRuntime(t, untargeted)
	_, err := runUntargetedToMarked(context.Background(), untargetedRuntime, untargeted.Scenario, len(untargeted.Scenario.AdvanceTransitions)+1)
	if err == nil || !strings.Contains(err.Error(), "repeated control state") {
		t.Fatalf("expected untargeted priority cycle rejection, got %v", err)
	}
}

func integerPriorityCycleFixture() KernelConformance {
	fixture := newIntegerFixture(SetupUnbound)
	base, err := IntegerProgram()
	if err != nil {
		panic(err)
	}
	transitions := append([]kernel.Transition(nil), base.Transitions...)
	for index := range transitions {
		if transitions[index].ID == fixture.Scenario.MaintenanceTransition {
			transitions[index].Priority = 5
		}
	}
	fixture.Program, err = kernel.CompileProgram(base.ID, base.Version, base.RuntimeCompatibility, base.InitialMode, base.MarkedModes, transitions)
	if err != nil {
		panic(err)
	}
	state, _ := fixture.Store.(*MemoryStateStore).snapshot()
	state.Program = fixture.Program.Identity()
	fixture.Store.(*MemoryStateStore).state = state
	return fixture
}

func mustRuntime(t *testing.T, fixture KernelConformance) kernel.Runtime {
	t.Helper()
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
