// Package subprocess implements Boatstack's strict language-neutral extension
// protocol. A subprocess extension is a trusted executable boundary, not an OS
// sandbox.
package subprocess

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/operatorstack/boatstack/boatstack/control"
)

const maxRequestBytes = 1 << 20

type Config struct {
	ID         string
	Version    string
	Executable string
	SHA256     string
	Settings   json.RawMessage
	Limits     control.SubprocessLimits
}

type Extension struct {
	config Config
}

func New(config Config) (*Extension, error) {
	if config.ID == "" || config.Version == "" || !filepath.IsAbs(config.Executable) || len(config.SHA256) != 64 {
		return nil, fmt.Errorf("subprocess extension requires id, version, absolute executable path, and SHA-256")
	}
	clean := filepath.Clean(config.Executable)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, fmt.Errorf("resolve subprocess extension executable: %w", err)
	}
	if resolved != clean {
		return nil, fmt.Errorf("subprocess extension executable path must be exact and symlink-free")
	}
	config.Executable = clean
	if config.Limits.Deadline == 0 {
		config.Limits.Deadline = 5 * time.Second
	}
	if config.Limits.StdoutBytes == 0 {
		config.Limits.StdoutBytes = 1 << 20
	}
	if config.Limits.StderrBytes == 0 {
		config.Limits.StderrBytes = 64 << 10
	}
	if config.Limits.Deadline < time.Millisecond || config.Limits.Deadline > 30*time.Second ||
		config.Limits.StdoutBytes < 1 || config.Limits.StdoutBytes > 4<<20 ||
		config.Limits.StderrBytes < 1 || config.Limits.StderrBytes > 1<<20 {
		return nil, fmt.Errorf("subprocess extension limits are outside the supported bounds")
	}
	extension := &Extension{config: config}
	if err := extension.verifyExecutable(); err != nil {
		return nil, err
	}
	return extension, nil
}

func (e *Extension) Runtime() control.ExtensionRuntime { return e }

func (e *Extension) ExtensionManifest(ctx context.Context) (control.ExtensionManifest, error) {
	correlation := "manifest-" + e.config.SHA256[:16]
	response, err := e.Invoke(ctx, control.ExtensionRequest{
		ProtocolVersion: control.ExtensionProtocolVersion, Operation: control.ExtensionManifestOperation,
		ExtensionID: e.config.ID, ExtensionVersion: e.config.Version, CorrelationID: correlation,
	})
	if err != nil {
		return control.ExtensionManifest{}, err
	}
	if response.Manifest == nil {
		return control.ExtensionManifest{}, fmt.Errorf("subprocess extension returned no manifest")
	}
	manifest := *response.Manifest
	if manifest.ID != e.config.ID || manifest.Version != e.config.Version || manifest.ProtocolVersion != control.ExtensionProtocolVersion {
		return control.ExtensionManifest{}, fmt.Errorf("subprocess extension manifest identity mismatch")
	}
	if manifest.ExecutableSHA256 != "" && manifest.ExecutableSHA256 != e.config.SHA256 {
		return control.ExtensionManifest{}, fmt.Errorf("subprocess extension manifest executable fingerprint mismatch")
	}
	manifest.ExecutableSHA256 = e.config.SHA256
	manifest.Settings = append(json.RawMessage(nil), e.config.Settings...)
	return manifest, nil
}

func (e *Extension) Invoke(ctx context.Context, request control.ExtensionRequest) (control.ExtensionResponse, error) {
	if request.ProtocolVersion != control.ExtensionProtocolVersion || request.ExtensionID != e.config.ID || request.ExtensionVersion != e.config.Version || request.CorrelationID == "" {
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension request identity mismatch")
	}
	switch request.Operation {
	case control.ExtensionManifestOperation, control.ExtensionObserveOperation, control.ExtensionPlanLocalEffectOperation,
		control.ExtensionExecuteExternalOperation, control.ExtensionVerifyOperation, control.ExtensionRecoverOperation:
	default:
		return control.ExtensionResponse{}, fmt.Errorf("unsupported subprocess extension operation %q", request.Operation)
	}
	if err := e.verifyExecutable(); err != nil {
		return control.ExtensionResponse{}, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return control.ExtensionResponse{}, err
	}
	if len(raw) > maxRequestBytes {
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension request exceeds 1 MiB")
	}
	deadlineContext, cancel := context.WithTimeout(ctx, e.config.Limits.Deadline)
	defer cancel()
	command := exec.CommandContext(deadlineContext, e.config.Executable)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Stdin = bytes.NewReader(raw)
	stdout := &boundedBuffer{limit: e.config.Limits.StdoutBytes, cancel: cancel}
	stderr := &boundedBuffer{limit: e.config.Limits.StderrBytes, cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if stdout.exceeded || stderr.exceeded {
			return control.ExtensionResponse{}, fmt.Errorf("subprocess extension output exceeded its bound")
		}
		if errors.Is(deadlineContext.Err(), context.DeadlineExceeded) {
			return control.ExtensionResponse{}, fmt.Errorf("subprocess extension deadline exceeded")
		}
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension process failed")
	}
	if stdout.exceeded || stderr.exceeded {
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension output exceeded its bound")
	}
	var response control.ExtensionResponse
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return control.ExtensionResponse{}, fmt.Errorf("decode subprocess extension response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension response contains trailing JSON")
	}
	if response.ProtocolVersion != control.ExtensionProtocolVersion || response.Operation != request.Operation ||
		response.ExtensionID != e.config.ID || response.ExtensionVersion != e.config.Version || response.CorrelationID != request.CorrelationID {
		return control.ExtensionResponse{}, fmt.Errorf("subprocess extension response identity mismatch")
	}
	if err := control.ValidateExtensionOperationResponse(request.Operation, response); err != nil {
		return control.ExtensionResponse{}, err
	}
	return response, nil
}

func (e *Extension) verifyExecutable() error {
	info, err := os.Lstat(e.config.Executable)
	if err != nil {
		return fmt.Errorf("stat subprocess extension executable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("subprocess extension executable path drifted to a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("subprocess extension executable is not a regular file")
	}
	resolved, err := filepath.EvalSymlinks(e.config.Executable)
	if err != nil {
		return fmt.Errorf("resolve subprocess extension executable: %w", err)
	}
	if resolved != e.config.Executable {
		return fmt.Errorf("subprocess extension executable path must remain exact and symlink-free")
	}
	raw, err := os.ReadFile(e.config.Executable)
	if err != nil {
		return fmt.Errorf("read subprocess extension executable: %w", err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != e.config.SHA256 {
		return fmt.Errorf("subprocess extension executable fingerprint drifted")
	}
	return nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	if int64(b.buffer.Len()+len(value)) > b.limit {
		if !b.exceeded {
			b.exceeded = true
			if b.cancel != nil {
				b.cancel()
			}
		}
		remaining := int(b.limit) - b.buffer.Len()
		if remaining > 0 {
			_, _ = b.buffer.Write(value[:remaining])
		}
		// Report the write as consumed while cancellation terminates the child.
		// This keeps the os/exec copy goroutine deterministic and prevents an
		// output-flooding process from running until the independent deadline.
		return len(value), nil
	}
	return b.buffer.Write(value)
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }
