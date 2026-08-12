package boatstack

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

type fixedObservation struct{ value model.Observation }

func (o fixedObservation) Observe(context.Context, ports.ObservationRequest) (model.Observation, error) {
	return o.value, nil
}

func TestRuntimeErrorPreservesBoundedClassAndMessage(t *testing.T) {
	extension := control.CompiledExtension{
		Identity: control.ComponentIdentity{ID: "example.runtime", Version: "1.0.0"},
	}
	err := validateExtensionResponse(extension, control.ExtensionObserveOperation, "correlation", control.ExtensionResponse{
		ProtocolVersion: control.ExtensionProtocolVersion, Operation: control.ExtensionObserveOperation,
		ExtensionID: "example.runtime", ExtensionVersion: "1.0.0", CorrelationID: "correlation",
		ErrorClass: "temporary", Error: "provider response was incomplete",
	})
	var runtimeErr ComponentRuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Class != "temporary" || runtimeErr.Message != "provider response was incomplete" {
		t.Fatalf("runtime error detail was lost: %#v (%v)", runtimeErr, err)
	}
}

type isolatedObservationExtension struct {
	id         string
	forbid     string
	sawFact    *bool
	executable bool
	calls      *int
}

func (e isolatedObservationExtension) ExtensionManifest(context.Context) (control.ExtensionManifest, error) {
	manifest := control.ExtensionManifest{
		ID: e.id, Version: "1.0.0", ProtocolVersion: control.ExtensionProtocolVersion,
		SettingsSchema: json.RawMessage(`{"type":"object"}`), Facts: []string{e.id + ".fact"},
		Capabilities:          []control.Capability{control.CapabilityCommandExecute},
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}
	if e.executable {
		manifest.ExecutableSHA256 = strings.Repeat("a", 64)
	}
	return manifest, nil
}

func (e isolatedObservationExtension) Runtime() control.ExtensionRuntime { return e }

func (e isolatedObservationExtension) Invoke(_ context.Context, request control.ExtensionRequest) (control.ExtensionResponse, error) {
	if e.calls != nil {
		*e.calls++
	}
	var projection struct {
		ExtensionFacts map[string]json.RawMessage `json:"extension_facts"`
	}
	if err := json.Unmarshal(request.Snapshot, &projection); err != nil {
		return control.ExtensionResponse{}, err
	}
	_, *e.sawFact = projection.ExtensionFacts[e.forbid]
	return control.ExtensionResponse{
		ProtocolVersion: control.ExtensionProtocolVersion, Operation: request.Operation,
		ExtensionID: e.id, ExtensionVersion: "1.0.0", CorrelationID: request.CorrelationID,
		Facts: []control.ExtensionFact{{ID: e.id + ".fact", Status: control.FactKnown, Value: "observed", Fingerprint: e.id + "-fingerprint"}},
	}, nil
}

func TestExecutableExtensionObservationWaitsForVerifiedProgramBinding(t *testing.T) {
	// control-law: repository-selected-code-is-inert-before-exact-program-admission
	var calls int
	var sawFact bool
	extension := isolatedObservationExtension{id: "example.external", sawFact: &sawFact, executable: true, calls: &calls}
	program, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "test-kernel", Core: core.System(), Runtime: standard.Definition(), Extensions: []control.Extension{extension},
	})
	if err != nil {
		t.Fatal(err)
	}
	invocation := model.InvocationContext{Correlation: "pre-admission-gate"}
	base := model.Observation{
		Invocation: invocation, ObservedAt: time.Unix(100, 0).UTC(),
		Configuration: model.Known(model.ConfigurationVerified, model.Evidence{Source: "test", Fingerprint: "configuration", ObservedAt: time.Unix(100, 0).UTC()}),
	}
	observer := programObserver{base: fixedObservation{value: base}, program: program}
	observed, err := observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation, Capabilities: []control.Capability{control.CapabilityCommandExecute}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(observed.ExtensionFacts) != 0 {
		t.Fatalf("unbound program executed extension: calls=%d facts=%#v", calls, observed.ExtensionFacts)
	}
	base.RecordedProgramFingerprint = program.Fingerprint()
	observer.base = fixedObservation{value: base}
	observed, err = observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation, Capabilities: []control.Capability{control.CapabilityCommandExecute}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || observed.ExtensionFacts["example.external.fact"].Value != "observed" {
		t.Fatalf("verified binding did not execute extension once: calls=%d facts=%#v", calls, observed.ExtensionFacts)
	}
}

func TestExtensionObserversConsumeOneOrderIndependentProjection(t *testing.T) {
	// control-law: extension-observation-order-cannot-create-cross-extension-facts
	var alphaSawBeta, betaSawAlpha bool
	alpha := isolatedObservationExtension{id: "example.alpha", forbid: "example.beta.fact", sawFact: &alphaSawBeta}
	beta := isolatedObservationExtension{id: "example.beta", forbid: "example.alpha.fact", sawFact: &betaSawAlpha}
	program, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "test-kernel", Core: core.System(), Runtime: standard.Definition(),
		Extensions: []control.Extension{beta, alpha},
	})
	if err != nil {
		t.Fatal(err)
	}
	invocation := model.InvocationContext{Correlation: "observation-order"}
	observed, err := (programObserver{
		base:    fixedObservation{value: model.Observation{Invocation: invocation, ObservedAt: time.Unix(100, 0).UTC()}},
		program: program,
	}).Observe(context.Background(), ports.ObservationRequest{Invocation: invocation, Capabilities: []control.Capability{control.CapabilityCommandExecute}})
	if err != nil {
		t.Fatal(err)
	}
	if alphaSawBeta || betaSawAlpha {
		t.Fatalf("extension observer consumed another extension's invocation-order fact: alpha=%v beta=%v", alphaSawBeta, betaSawAlpha)
	}
	if len(observed.ExtensionFacts) != 2 || observed.ExtensionFacts["example.alpha.fact"].Value != "observed" || observed.ExtensionFacts["example.beta.fact"].Value != "observed" {
		t.Fatalf("final layered observation lost extension facts: %#v", observed.ExtensionFacts)
	}
}
