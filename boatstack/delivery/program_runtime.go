package delivery

import (
	"context"
	"encoding/json"
	"fmt"
)

const ProgramRuntimeProtocolVersion = 2

type ProgramRuntimeMode string

const (
	// ProgramRuntimeNative selects a trusted in-process first-party adapter.
	ProgramRuntimeNative ProgramRuntimeMode = "native"
	// ProgramRuntimeProtocol selects the bounded public ProgramRuntime contract.
	ProgramRuntimeProtocol ProgramRuntimeMode = "protocol"
)

type ProgramRuntimeOperation string

const (
	ProgramObserveOperation         ProgramRuntimeOperation = "observe"
	ProgramPlanLocalEffectOperation ProgramRuntimeOperation = "plan-local-effect"
	ProgramExecuteExternalOperation ProgramRuntimeOperation = "execute-external"
	ProgramVerifyOperation          ProgramRuntimeOperation = "verify"
	ProgramRecoverOperation         ProgramRuntimeOperation = "recover"
)

type ProgramRuntimeRequest struct {
	ProtocolVersion    int                     `json:"protocol_version"`
	Operation          ProgramRuntimeOperation `json:"operation"`
	ProgramID          string                  `json:"program_id"`
	ProgramVersion     string                  `json:"program_version"`
	ProgramFingerprint string                  `json:"program_fingerprint"`
	CorrelationID      string                  `json:"correlation_id"`
	RepositoryRoot     string                  `json:"repository_root,omitempty"`
	TransitionID       TransitionID            `json:"transition_id,omitempty"`
	Capabilities       []Capability            `json:"capabilities,omitempty"`
	Snapshot           json.RawMessage         `json:"snapshot,omitempty"`
	Parameters         json.RawMessage         `json:"parameters,omitempty"`
	Settings           json.RawMessage         `json:"settings,omitempty"`
}

type ProgramRuntimeResponse struct {
	ProtocolVersion int                     `json:"protocol_version"`
	Operation       ProgramRuntimeOperation `json:"operation"`
	ProgramID       string                  `json:"program_id"`
	ProgramVersion  string                  `json:"program_version"`
	CorrelationID   string                  `json:"correlation_id"`
	Facts           []ExtensionFact         `json:"facts,omitempty"`
	Writes          []ResourceWrite         `json:"writes,omitempty"`
	ExternalResult  json.RawMessage         `json:"external_result,omitempty"`
	Verified        *bool                   `json:"verified,omitempty"`
	ErrorClass      string                  `json:"error_class,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

// ValidateProgramRuntimeOperationResponse enforces the exact payload union for a custom
// in-process ProgramRuntime runtime. Identity and correlation are checked by the
// Kernel boundary that owns the request.
func ValidateProgramRuntimeOperationResponse(operation ProgramRuntimeOperation, response ProgramRuntimeResponse) error {
	if (response.Error == "") != (response.ErrorClass == "") {
		return fmt.Errorf("control-program errors require an explicit classification and message")
	}
	hasPayload := len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	if response.Error != "" {
		if len(response.ErrorClass) > 128 || len(response.Error) > 4096 {
			return fmt.Errorf("control-program error classification or message exceeds its bound")
		}
		if hasPayload {
			return fmt.Errorf("control-program error response contains an operation payload")
		}
		return nil
	}
	invalid := false
	switch operation {
	case ProgramObserveOperation:
		invalid = len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case ProgramPlanLocalEffectOperation, ProgramRecoverOperation:
		invalid = len(response.Facts) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case ProgramExecuteExternalOperation:
		invalid = len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) == 0 || response.Verified != nil
	case ProgramVerifyOperation:
		invalid = len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified == nil
	default:
		return fmt.Errorf("unsupported control-program operation %q", operation)
	}
	if invalid {
		return fmt.Errorf("program runtime returned the wrong response type for %q", operation)
	}
	return nil
}

// ProgramRuntime is the bounded in-process execution contract for a Control
// Program. It receives projections and returns declarations; it never receives a
// mutable Kernel object. Capabilities are the exact admitted upper bound; the
// runtime cannot widen them.
type ProgramRuntime interface {
	InvokeProgram(context.Context, ProgramRuntimeRequest) (ProgramRuntimeResponse, error)
}

// RuntimeProgramDefinition supplies a public runtime together with its trusted
// control-program manifest.
type RuntimeProgramDefinition interface {
	ProgramRuntimeDefinition
	ProgramRuntime() ProgramRuntime
}
