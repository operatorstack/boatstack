package control

import (
	"context"
	"encoding/json"
	"fmt"
)

const FlowProtocolVersion = 1

type FlowRuntimeMode string

const (
	// FlowRuntimeNative selects a trusted in-process first-party adapter.
	FlowRuntimeNative FlowRuntimeMode = "native"
	// FlowRuntimeProtocol selects the bounded public FlowRuntime contract.
	FlowRuntimeProtocol FlowRuntimeMode = "protocol"
)

type FlowOperation string

const (
	FlowObserveOperation         FlowOperation = "observe"
	FlowPlanLocalEffectOperation FlowOperation = "plan-local-effect"
	FlowExecuteExternalOperation FlowOperation = "execute-external"
	FlowVerifyOperation          FlowOperation = "verify"
	FlowRecoverOperation         FlowOperation = "recover"
)

type FlowRequest struct {
	ProtocolVersion    int             `json:"protocol_version"`
	Operation          FlowOperation   `json:"operation"`
	FlowID             string          `json:"flow_id"`
	FlowVersion        string          `json:"flow_version"`
	ProgramFingerprint string          `json:"program_fingerprint"`
	CorrelationID      string          `json:"correlation_id"`
	RepositoryRoot     string          `json:"repository_root,omitempty"`
	TransitionID       TransitionID    `json:"transition_id,omitempty"`
	Snapshot           json.RawMessage `json:"snapshot,omitempty"`
	Parameters         json.RawMessage `json:"parameters,omitempty"`
	Settings           json.RawMessage `json:"settings,omitempty"`
}

type FlowResponse struct {
	ProtocolVersion int             `json:"protocol_version"`
	Operation       FlowOperation   `json:"operation"`
	FlowID          string          `json:"flow_id"`
	FlowVersion     string          `json:"flow_version"`
	CorrelationID   string          `json:"correlation_id"`
	Facts           []ExtensionFact `json:"facts,omitempty"`
	Writes          []ResourceWrite `json:"writes,omitempty"`
	ExternalResult  json.RawMessage `json:"external_result,omitempty"`
	Verified        *bool           `json:"verified,omitempty"`
	ErrorClass      string          `json:"error_class,omitempty"`
	Error           string          `json:"error,omitempty"`
}

// ValidateFlowOperationResponse enforces the exact payload union for a custom
// in-process PrimaryFlow runtime. Identity and correlation are checked by the
// Kernel boundary that owns the request.
func ValidateFlowOperationResponse(operation FlowOperation, response FlowResponse) error {
	if (response.Error == "") != (response.ErrorClass == "") {
		return fmt.Errorf("primary-flow errors require an explicit classification and message")
	}
	hasPayload := len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	if response.Error != "" {
		if len(response.ErrorClass) > 128 || len(response.Error) > 4096 {
			return fmt.Errorf("primary-flow error classification or message exceeds its bound")
		}
		if hasPayload {
			return fmt.Errorf("primary-flow error response contains an operation payload")
		}
		return nil
	}
	invalid := false
	switch operation {
	case FlowObserveOperation:
		invalid = len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case FlowPlanLocalEffectOperation, FlowRecoverOperation:
		invalid = len(response.Facts) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case FlowExecuteExternalOperation:
		invalid = len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) == 0 || response.Verified != nil
	case FlowVerifyOperation:
		invalid = len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified == nil
	default:
		return fmt.Errorf("unsupported primary-flow operation %q", operation)
	}
	if invalid {
		return fmt.Errorf("primary flow returned the wrong response type for %q", operation)
	}
	return nil
}

// FlowRuntime is the bounded in-process runtime contract for a custom primary
// flow. It receives projections and returns declarations; it never receives a
// mutable Kernel object.
type FlowRuntime interface {
	InvokeFlow(context.Context, FlowRequest) (FlowResponse, error)
}

// RuntimeFlowDefinition supplies a public runtime together with its trusted
// primary-flow manifest.
type RuntimeFlowDefinition interface {
	FlowDefinition
	FlowRuntime() FlowRuntime
}
