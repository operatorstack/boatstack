package kernel_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/kernel"
	"github.com/operatorstack/boatstack/boatstack/kernel/conformance"
)

func TestRuntimeConformance(t *testing.T) {
	conformance.IntegerFixture().Run(t)
}

func TestResolveTraceIsNonInterferingForNonSoftwareDomain(t *testing.T) {
	// control-law: controller-observation-cannot-change-controller
	fixture := conformance.IntegerFixture()
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	request := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority}
	before := fixture.Scenario.Snapshot()
	plain, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Trace = true
	traced, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := runtime.Resolve(context.Background(), request)
	if err != nil || repeated.Trace == nil || !reflect.DeepEqual(traced.Trace.Candidates, repeated.Trace.Candidates) {
		t.Fatalf("trace order is not deterministic: first=%#v repeated=%#v err=%v", traced.Trace, repeated.Trace, err)
	}
	after := fixture.Scenario.Snapshot()
	if !reflect.DeepEqual(plain.Decision, traced.Decision) {
		t.Fatalf("trace changed decision: %#v != %#v", plain.Decision, traced.Decision)
	}
	plainPrescription, _ := json.Marshal(plain.Prescription)
	tracedPrescription, _ := json.Marshal(traced.Prescription)
	if string(plainPrescription) != string(tracedPrescription) {
		t.Fatalf("trace changed prescription bytes: %s != %s", plainPrescription, tracedPrescription)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("explain mutated state, effects, or receipts: before=%#v after=%#v", before, after)
	}
	if traced.Trace == nil || traced.Trace.Decision.Kind != string(kernel.Prescribed) || traced.Trace.Decision.Transition != fixture.Scenario.BindTransition {
		t.Fatalf("trace does not identify prescribed transition: %#v", traced.Trace)
	}
	var selected *kernel.CandidateTrace
	for index := range traced.Trace.Candidates {
		candidate := &traced.Trace.Candidates[index]
		if candidate.TransitionID == fixture.Scenario.BindTransition {
			selected = candidate
		}
	}
	if selected == nil || selected.Disposition != kernel.DispositionSelected || !selected.DomainAdmissible.Satisfied || !strings.Contains(selected.DomainAdmissible.Reason, "not yet bound") {
		t.Fatalf("domain reason was not preserved: %#v", selected)
	}
}

func TestRequestedInadmissibleTransitionTraceNamesSourceRejection(t *testing.T) {
	fixture := conformance.IntegerFixture()
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective,
		Authority: fixture.Scenario.Authority, Requested: fixture.Scenario.AdvanceTransitions[0], Trace: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != kernel.Refused || resolution.Trace == nil {
		t.Fatalf("requested rejection = %#v", resolution)
	}
	for _, candidate := range resolution.Trace.Candidates {
		if candidate.TransitionID == fixture.Scenario.AdvanceTransitions[0] {
			if candidate.Disposition != kernel.DispositionSourceModeRejected || candidate.SourceMode.Satisfied {
				t.Fatalf("source rejection trace = %#v", candidate)
			}
			return
		}
	}
	t.Fatal("requested candidate is absent from trace")
}

func TestTraceExplainsObjectiveMismatchRecoveryAndMarkedState(t *testing.T) {
	fixture := conformance.IntegerFixture().New(t, conformance.SetupMaintenanceBound)
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	advance := fixture.Scenario.AdvanceTransitions[1]
	mismatch, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.ConflictingObjective,
		Authority: fixture.Scenario.Authority, Requested: advance, Trace: true,
	})
	if err != nil || mismatch.Trace == nil || mismatch.Decision.Kind != kernel.Refused {
		t.Fatalf("objective mismatch = %#v, %v", mismatch, err)
	}
	assertDisposition(t, mismatch.Trace, advance, kernel.DispositionObjectiveRejected)

	request := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority, Requested: advance}
	prescribed, err := runtime.Resolve(context.Background(), request)
	if err != nil || prescribed.Prescription == nil {
		t.Fatalf("prescribe interrupted transition: %#v, %v", prescribed, err)
	}
	fixture.Scenario.InterruptNextOperator()
	if _, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: *prescribed.Prescription}); !kernel.IsRecoveryRequired(err) {
		t.Fatalf("interrupted apply = %v", err)
	}
	recovery, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority, Trace: true})
	if err != nil || recovery.Trace == nil || recovery.Trace.Recovery == nil || !recovery.Trace.Recovery.Active || recovery.Decision.Transition != fixture.Scenario.RecoveryTransition {
		t.Fatalf("recovery trace = %#v, %v", recovery, err)
	}
	assertDisposition(t, recovery.Trace, advance, kernel.DispositionRecoveryRejected)

	markedFixture := conformance.IntegerFixture().New(t, conformance.SetupMaintenanceBound)
	markedRuntime, _ := kernel.NewRuntime(markedFixture.Program, markedFixture.Domain, markedFixture.Operator, markedFixture.CapabilityClassifier, markedFixture.Store, markedFixture.Locker, markedFixture.Clock)
	markedRequest := kernel.ResolveRequest{InstanceID: markedFixture.Scenario.InstanceID, Objective: &markedFixture.Scenario.Objective, Authority: markedFixture.Scenario.Authority, Requested: markedFixture.Scenario.AdvanceTransitions[1]}
	markedPrescription, err := markedRuntime.Resolve(context.Background(), markedRequest)
	if err != nil || markedPrescription.Prescription == nil {
		t.Fatalf("marked setup resolve = %#v, %v", markedPrescription, err)
	}
	if _, err := markedRuntime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: markedRequest, Prescription: *markedPrescription.Prescription}); err != nil {
		t.Fatal(err)
	}
	marked, err := markedRuntime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: markedFixture.Scenario.InstanceID, Objective: &markedFixture.Scenario.Objective, Authority: markedFixture.Scenario.Authority, Trace: true})
	if err != nil || marked.Decision.Kind != kernel.Marked || marked.Trace == nil || !marked.Trace.Marked {
		t.Fatalf("marked trace = %#v, %v", marked, err)
	}
}

func TestTraceExplainsStaleControlProgramIdentity(t *testing.T) {
	fixture := conformance.IntegerFixture()
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Scenario.RetargetProgram(fixture.Scenario.AlternateProgram.Identity())
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective,
		Authority: fixture.Scenario.Authority, Trace: true,
	})
	if err != nil || resolution.Trace == nil || resolution.Decision.Kind != kernel.Unresolved || !strings.Contains(resolution.Decision.Reason, "program identity is stale") {
		t.Fatalf("stale program trace = %#v, %v", resolution, err)
	}
	if resolution.Trace.Program != fixture.Program.Identity() || resolution.Trace.StateProgram == nil || *resolution.Trace.StateProgram != fixture.Scenario.AlternateProgram.Identity() {
		t.Fatalf("stale program identities = %#v", resolution.Trace)
	}
}

func assertDisposition(t *testing.T, trace *kernel.DecisionTrace, transition string, disposition kernel.CandidateDisposition) {
	t.Helper()
	for _, candidate := range trace.Candidates {
		if candidate.TransitionID == transition {
			if candidate.Disposition != disposition {
				t.Fatalf("%s disposition = %s, want %s", transition, candidate.Disposition, disposition)
			}
			return
		}
	}
	t.Fatalf("trace does not contain %s", transition)
}
