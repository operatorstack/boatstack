package control

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const ExtensionProtocolVersion = 2

type ExtensionOperation string

const (
	ExtensionManifestOperation        ExtensionOperation = "manifest"
	ExtensionObserveOperation         ExtensionOperation = "observe"
	ExtensionPlanLocalEffectOperation ExtensionOperation = "plan-local-effect"
	ExtensionExecuteExternalOperation ExtensionOperation = "execute-external"
	ExtensionVerifyOperation          ExtensionOperation = "verify"
	ExtensionRecoverOperation         ExtensionOperation = "recover"
)

type ExtensionRequest struct {
	ProtocolVersion    int                `json:"protocol_version"`
	Operation          ExtensionOperation `json:"operation"`
	ExtensionID        string             `json:"extension_id"`
	ExtensionVersion   string             `json:"extension_version"`
	ProgramFingerprint string             `json:"program_fingerprint"`
	CorrelationID      string             `json:"correlation_id"`
	RepositoryRoot     string             `json:"repository_root,omitempty"`
	TransitionID       TransitionID       `json:"transition_id,omitempty"`
	Capabilities       []Capability       `json:"capabilities,omitempty"`
	Snapshot           json.RawMessage    `json:"snapshot,omitempty"`
	Parameters         json.RawMessage    `json:"parameters,omitempty"`
	Settings           json.RawMessage    `json:"settings,omitempty"`
}

type ExtensionFact struct {
	ID          string     `json:"id"`
	Status      FactStatus `json:"status"`
	Value       string     `json:"value,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Detail      string     `json:"detail,omitempty"`
}

type ResourceWrite struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	Content  []byte `json:"content,omitempty"`
	SHA256   string `json:"sha256"`
	Mode     uint32 `json:"mode,omitempty"`
	Delete   bool   `json:"delete,omitempty"`
}

type ExtensionResponse struct {
	ProtocolVersion  int                `json:"protocol_version"`
	Operation        ExtensionOperation `json:"operation"`
	ExtensionID      string             `json:"extension_id"`
	ExtensionVersion string             `json:"extension_version"`
	CorrelationID    string             `json:"correlation_id"`
	Manifest         *ExtensionManifest `json:"manifest,omitempty"`
	Facts            []ExtensionFact    `json:"facts,omitempty"`
	Writes           []ResourceWrite    `json:"writes,omitempty"`
	ExternalResult   json.RawMessage    `json:"external_result,omitempty"`
	Verified         *bool              `json:"verified,omitempty"`
	ErrorClass       string             `json:"error_class,omitempty"`
	Error            string             `json:"error,omitempty"`
}

// ValidateExtensionOperationResponse enforces the exact payload union for the
// language-neutral extension protocol. Identity and correlation are checked by
// the caller that owns the request boundary.
func ValidateExtensionOperationResponse(operation ExtensionOperation, response ExtensionResponse) error {
	if (response.Error == "") != (response.ErrorClass == "") {
		return fmt.Errorf("extension errors require an explicit classification and message")
	}
	hasPayload := response.Manifest != nil || len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	if response.Error != "" {
		if len(response.ErrorClass) > 128 || len(response.Error) > 4096 {
			return fmt.Errorf("extension error classification or message exceeds its bound")
		}
		if hasPayload {
			return fmt.Errorf("extension error response contains an operation payload")
		}
		return nil
	}
	invalid := false
	switch operation {
	case ExtensionManifestOperation:
		invalid = response.Manifest == nil || len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case ExtensionObserveOperation:
		invalid = response.Manifest != nil || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case ExtensionPlanLocalEffectOperation, ExtensionRecoverOperation:
		invalid = response.Manifest != nil || len(response.Facts) != 0 || len(response.ExternalResult) != 0 || response.Verified != nil
	case ExtensionExecuteExternalOperation:
		invalid = response.Manifest != nil || len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) == 0 || response.Verified != nil
	case ExtensionVerifyOperation:
		invalid = response.Manifest != nil || len(response.Facts) != 0 || len(response.Writes) != 0 || len(response.ExternalResult) != 0 || response.Verified == nil
	default:
		return fmt.Errorf("unsupported extension operation %q", operation)
	}
	if invalid {
		return fmt.Errorf("extension returned the wrong response type for %q", operation)
	}
	return nil
}

// ExtensionRuntime is the language-neutral execution contract shared by
// trusted in-process and subprocess extensions. Kernel admission and resource
// ownership remain outside the implementation.
type ExtensionRuntime interface {
	Invoke(context.Context, ExtensionRequest) (ExtensionResponse, error)
}

type RuntimeExtension interface {
	Extension
	Runtime() ExtensionRuntime
}

type SubprocessLimits struct {
	Deadline    time.Duration
	StdoutBytes int64
	StderrBytes int64
}
