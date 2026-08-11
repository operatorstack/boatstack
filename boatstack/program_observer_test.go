package boatstack

import (
	"context"
	"encoding/json"
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

type isolatedObservationExtension struct {
	id      string
	forbid  string
	sawFact *bool
}

func (e isolatedObservationExtension) ExtensionManifest(context.Context) (control.ExtensionManifest, error) {
	return control.ExtensionManifest{
		ID: e.id, Version: "1.0.0", ProtocolVersion: control.ExtensionProtocolVersion,
		SettingsSchema: json.RawMessage(`{"type":"object"}`), Facts: []string{e.id + ".fact"},
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

func (e isolatedObservationExtension) Runtime() control.ExtensionRuntime { return e }

func (e isolatedObservationExtension) Invoke(_ context.Context, request control.ExtensionRequest) (control.ExtensionResponse, error) {
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

func TestExtensionObserversConsumeOneOrderIndependentProjection(t *testing.T) {
	// control-law: extension-observation-order-cannot-create-cross-extension-facts
	var alphaSawBeta, betaSawAlpha bool
	alpha := isolatedObservationExtension{id: "example.alpha", forbid: "example.beta.fact", sawFact: &alphaSawBeta}
	beta := isolatedObservationExtension{id: "example.beta", forbid: "example.alpha.fact", sawFact: &betaSawAlpha}
	program, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "test-kernel", Core: core.System(), Flow: standard.Definition(),
		Extensions: []control.Extension{beta, alpha},
	})
	if err != nil {
		t.Fatal(err)
	}
	invocation := model.InvocationContext{Correlation: "observation-order"}
	observed, err := (programObserver{
		base:    fixedObservation{value: model.Observation{Invocation: invocation, ObservedAt: time.Unix(100, 0).UTC()}},
		program: program,
	}).Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
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
