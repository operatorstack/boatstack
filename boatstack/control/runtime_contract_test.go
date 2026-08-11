package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgramRuntimeOperationResponsesAreAnExactTaggedUnion(t *testing.T) {
	verified := true
	valid := map[ProgramRuntimeOperation]ProgramRuntimeResponse{
		ProgramObserveOperation:         {Facts: []ExtensionFact{{ID: "flow.ready"}}},
		ProgramPlanLocalEffectOperation: {Writes: []ResourceWrite{{Resource: "flow.plan"}}},
		ProgramExecuteExternalOperation: {ExternalResult: json.RawMessage(`{"ok":true}`)},
		ProgramVerifyOperation:          {Verified: &verified},
		ProgramRecoverOperation:         {Writes: []ResourceWrite{{Resource: "flow.recovery"}}},
	}
	for operation, response := range valid {
		if err := ValidateProgramRuntimeOperationResponse(operation, response); err != nil {
			t.Fatalf("valid %q response: %v", operation, err)
		}
	}

	invalid := []struct {
		name      string
		operation ProgramRuntimeOperation
		response  ProgramRuntimeResponse
	}{
		{"observe-write", ProgramObserveOperation, ProgramRuntimeResponse{Writes: []ResourceWrite{{Resource: "wrong"}}}},
		{"local-fact", ProgramPlanLocalEffectOperation, ProgramRuntimeResponse{Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"external-empty", ProgramExecuteExternalOperation, ProgramRuntimeResponse{}},
		{"verify-missing", ProgramVerifyOperation, ProgramRuntimeResponse{}},
		{"recover-external", ProgramRecoverOperation, ProgramRuntimeResponse{ExternalResult: json.RawMessage(`{}`)}},
		{"partial-error", ProgramObserveOperation, ProgramRuntimeResponse{ErrorClass: "temporary"}},
		{"error-payload", ProgramObserveOperation, ProgramRuntimeResponse{ErrorClass: "temporary", Error: "failed", Facts: []ExtensionFact{{ID: "wrong"}}}},
		{"error-class-too-long", ProgramObserveOperation, ProgramRuntimeResponse{ErrorClass: strings.Repeat("x", 129), Error: "failed"}},
		{"error-message-too-long", ProgramObserveOperation, ProgramRuntimeResponse{ErrorClass: "temporary", Error: strings.Repeat("x", 4097)}},
		{"unknown-operation", ProgramRuntimeOperation("unknown"), ProgramRuntimeResponse{}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateProgramRuntimeOperationResponse(test.operation, test.response); err == nil {
				t.Fatalf("invalid %q response was accepted", test.operation)
			}
		})
	}
	if err := ValidateProgramRuntimeOperationResponse(ProgramObserveOperation, ProgramRuntimeResponse{ErrorClass: "temporary", Error: "failed"}); err != nil {
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
