package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlowOperationResponsesAreAnExactTaggedUnion(t *testing.T) {
	verified := true
	valid := map[FlowOperation]FlowResponse{
		FlowObserveOperation:         {Facts: []ExtensionFact{{ID: "flow.ready"}}},
		FlowPlanLocalEffectOperation: {Writes: []ResourceWrite{{Resource: "flow.plan"}}},
		FlowExecuteExternalOperation: {ExternalResult: json.RawMessage(`{"ok":true}`)},
		FlowVerifyOperation:          {Verified: &verified},
		FlowRecoverOperation:         {Writes: []ResourceWrite{{Resource: "flow.recovery"}}},
	}
	for operation, response := range valid {
		if err := ValidateFlowOperationResponse(operation, response); err != nil {
			t.Fatalf("valid %q response: %v", operation, err)
		}
	}

	invalid := []struct {
		name      string
		operation FlowOperation
		response  FlowResponse
	}{
		{"observe-write", FlowObserveOperation, FlowResponse{Writes: []ResourceWrite{{Resource: "wrong"}}}},
		{"local-fact", FlowPlanLocalEffectOperation, FlowResponse{Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"external-empty", FlowExecuteExternalOperation, FlowResponse{}},
		{"verify-missing", FlowVerifyOperation, FlowResponse{}},
		{"recover-external", FlowRecoverOperation, FlowResponse{ExternalResult: json.RawMessage(`{}`)}},
		{"partial-error", FlowObserveOperation, FlowResponse{ErrorClass: "temporary"}},
		{"error-payload", FlowObserveOperation, FlowResponse{ErrorClass: "temporary", Error: "failed", Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"error-class-too-long", FlowObserveOperation, FlowResponse{ErrorClass: strings.Repeat("x", 129), Error: "failed"}},
		{"error-message-too-long", FlowObserveOperation, FlowResponse{ErrorClass: "temporary", Error: strings.Repeat("x", 4097)}},
		{"unknown-operation", FlowOperation("unknown"), FlowResponse{}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateFlowOperationResponse(test.operation, test.response); err == nil {
				t.Fatalf("invalid %q response was accepted", test.operation)
			}
		})
	}
	if err := ValidateFlowOperationResponse(FlowObserveOperation, FlowResponse{ErrorClass: "temporary", Error: "failed"}); err != nil {
		t.Fatalf("classified error response: %v", err)
	}
}

func TestExtensionOperationResponsesAreAnExactTaggedUnion(t *testing.T) {
	verified := true
	valid := map[ExtensionOperation]ExtensionResponse{
		ExtensionManifestOperation:        {Manifest: &ExtensionManifest{ID: "example.extension"}},
		ExtensionObserveOperation:         {Facts: []ExtensionFact{{ID: "example.ready"}}},
		ExtensionPlanLocalEffectOperation: {Writes: []ResourceWrite{{Resource: "example.plan"}}},
		ExtensionExecuteExternalOperation: {ExternalResult: json.RawMessage(`{"ok":true}`)},
		ExtensionVerifyOperation:          {Verified: &verified},
		ExtensionRecoverOperation:         {Writes: []ResourceWrite{{Resource: "example.recovery"}}},
	}
	for operation, response := range valid {
		if err := ValidateExtensionOperationResponse(operation, response); err != nil {
			t.Fatalf("valid %q response: %v", operation, err)
		}
	}

	invalid := []struct {
		name      string
		operation ExtensionOperation
		response  ExtensionResponse
	}{
		{"manifest-missing", ExtensionManifestOperation, ExtensionResponse{}},
		{"observe-write", ExtensionObserveOperation, ExtensionResponse{Writes: []ResourceWrite{{Resource: "wrong"}}}},
		{"local-fact", ExtensionPlanLocalEffectOperation, ExtensionResponse{Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"external-empty", ExtensionExecuteExternalOperation, ExtensionResponse{}},
		{"verify-missing", ExtensionVerifyOperation, ExtensionResponse{}},
		{"recover-external", ExtensionRecoverOperation, ExtensionResponse{ExternalResult: json.RawMessage(`{}`)}},
		{"partial-error", ExtensionObserveOperation, ExtensionResponse{Error: "failed"}},
		{"error-payload", ExtensionObserveOperation, ExtensionResponse{ErrorClass: "temporary", Error: "failed", Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"error-class-too-long", ExtensionObserveOperation, ExtensionResponse{ErrorClass: strings.Repeat("x", 129), Error: "failed"}},
		{"error-message-too-long", ExtensionObserveOperation, ExtensionResponse{ErrorClass: "temporary", Error: strings.Repeat("x", 4097)}},
		{"unknown-operation", ExtensionOperation("unknown"), ExtensionResponse{}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateExtensionOperationResponse(test.operation, test.response); err == nil {
				t.Fatalf("invalid %q response was accepted", test.operation)
			}
		})
	}
	if err := ValidateExtensionOperationResponse(ExtensionObserveOperation, ExtensionResponse{ErrorClass: "temporary", Error: "failed"}); err != nil {
		t.Fatalf("classified error response: %v", err)
	}
}
