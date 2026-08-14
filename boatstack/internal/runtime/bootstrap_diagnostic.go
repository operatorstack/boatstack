package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
)

const (
	BootstrapDiagnosticSchema         = "boatstack-bootstrap-diagnostic"
	BootstrapDiagnosticSchemaRevision = 1

	CodeRuntimePinMissing               = "BOATSTACK_RUNTIME_PIN_MISSING"
	CodeRuntimePinInvalid               = "BOATSTACK_RUNTIME_PIN_INVALID"
	CodeRuntimeNotInstalled             = "BOATSTACK_RUNTIME_NOT_INSTALLED"
	CodeRuntimeInvalid                  = "BOATSTACK_RUNTIME_INVALID"
	CodeRuntimeChecksumMismatch         = "BOATSTACK_RUNTIME_CHECKSUM_MISMATCH"
	CodeRuntimeArtifactUnavailable      = "BOATSTACK_RUNTIME_ARTIFACT_UNAVAILABLE"
	CodeRuntimeArtifactChecksumMismatch = "BOATSTACK_RUNTIME_ARTIFACT_CHECKSUM_MISMATCH"
)

var publicReleasePattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?$`)

type BootstrapRequiredRuntime struct {
	Version        string `json:"version"`
	SHA256         string `json:"sha256"`
	SourceRevision string `json:"source_revision"`
}

type BootstrapRecovery struct {
	Action               string `json:"action"`
	Command              string `json:"command,omitempty"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
}

type BootstrapDiagnostic struct {
	Schema              string                    `json:"schema"`
	SchemaRevision      int                       `json:"schema_revision"`
	Kind                string                    `json:"kind"`
	Code                string                    `json:"code"`
	Message             string                    `json:"message"`
	Repository          string                    `json:"repository"`
	RequiredRuntime     *BootstrapRequiredRuntime `json:"required_runtime,omitempty"`
	Recovery            *BootstrapRecovery        `json:"recovery,omitempty"`
	FlowRunCreated      bool                      `json:"flow_run_created"`
	ManagedStateChanged bool                      `json:"managed_state_changed"`
	Cause               error                     `json:"-"`
}

func (d *BootstrapDiagnostic) Error() string {
	if d == nil {
		return ""
	}
	return d.Message
}

func (d *BootstrapDiagnostic) Unwrap() error {
	if d == nil {
		return nil
	}
	return d.Cause
}

func newBootstrapDiagnostic(code, message, repository string, cause error) *BootstrapDiagnostic {
	return &BootstrapDiagnostic{
		Schema: BootstrapDiagnosticSchema, SchemaRevision: BootstrapDiagnosticSchemaRevision,
		Kind: "blocked", Code: code, Message: message, Repository: repository, Cause: cause,
	}
}

func runtimeBootstrapDiagnostic(code, message, repository string, identity Identity, cause error) *BootstrapDiagnostic {
	diagnostic := newBootstrapDiagnostic(code, message, repository, cause)
	diagnostic.RequiredRuntime = &BootstrapRequiredRuntime{
		Version: identity.Version, SHA256: identity.SHA256, SourceRevision: identity.SourceRevision,
	}
	if code == CodeRuntimeNotInstalled && publicReleasePattern.MatchString(identity.Version) && publicReleasePattern.MatchString(buildinfo.Version) {
		diagnostic.Recovery = &BootstrapRecovery{
			Action: "install-exact-runtime", Command: releaseInstallCommand(identity, buildinfo.Version), RequiresConfirmation: true,
		}
	}
	return diagnostic
}

func releaseInstallCommand(identity Identity, installerVersion string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("$env:BOATSTACK_MODE='hydrate'; $env:BOATSTACK_VERSION='%s'; $env:BOATSTACK_EXPECTED_RUNTIME_SHA256='%s'; Invoke-RestMethod https://raw.githubusercontent.com/operatorstack/boatstack/%s/install.ps1 | Invoke-Expression", identity.Version, identity.SHA256, installerVersion)
	}
	return fmt.Sprintf("BOATSTACK_MODE=hydrate BOATSTACK_VERSION=%s BOATSTACK_EXPECTED_RUNTIME_SHA256=%s /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/%s/install.sh)\"", identity.Version, identity.SHA256, installerVersion)
}

func requestedJSON(arguments []string) bool {
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "--format" && index+1 < len(arguments) {
			return arguments[index+1] == "json"
		}
		if strings.TrimPrefix(arguments[index], "--format=") != arguments[index] {
			return strings.TrimPrefix(arguments[index], "--format=") == "json"
		}
	}
	return false
}

func RenderBootstrapDiagnostic(writer io.Writer, err error, arguments []string) (bool, error) {
	var diagnostic *BootstrapDiagnostic
	if !errors.As(err, &diagnostic) {
		return false, nil
	}
	if requestedJSON(arguments) {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return true, encoder.Encode(diagnostic)
	}
	if _, writeErr := fmt.Fprintf(writer, "Blocked: %s\n\nCode: %s\n", diagnostic.Message, diagnostic.Code); writeErr != nil {
		return true, writeErr
	}
	if diagnostic.RequiredRuntime != nil {
		if _, writeErr := fmt.Fprintf(writer, "Required version: %s\nRequired SHA-256: %s\n", diagnostic.RequiredRuntime.Version, diagnostic.RequiredRuntime.SHA256); writeErr != nil {
			return true, writeErr
		}
	}
	if diagnostic.Recovery != nil && diagnostic.Recovery.Command != "" {
		if _, writeErr := fmt.Fprintf(writer, "\nInstall the exact runtime after explicit approval:\n\n%s\n", diagnostic.Recovery.Command); writeErr != nil {
			return true, writeErr
		}
	}
	_, writeErr := fmt.Fprintln(writer, "\nNo Flow run was created and no managed state was changed.")
	return true, writeErr
}

type runtimeVerificationError struct {
	code     string
	identity Identity
	actual   string
	cause    error
}

func (e *runtimeVerificationError) Error() string { return e.cause.Error() }
func (e *runtimeVerificationError) Unwrap() error { return e.cause }
