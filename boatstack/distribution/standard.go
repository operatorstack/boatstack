// Package distribution is the composition root for shipped Boatstack
// distributions. Kernel mechanism packages do not import it.
package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/extension/subprocess"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
)

func StandardProgram(ctx context.Context, extensions ...control.Extension) (control.ControlProgram, error) {
	return control.Compile(ctx, control.CompileRequest{
		KernelVersion: boatstack.Version,
		Core:          core.System(), Runtime: standard.Definition(), Extensions: extensions,
		Settings: programSettings{},
	})
}

// programSettings contains only repository policy that changes the compiled
// graph. Approval, host, visual, and risk policy remain controlling snapshot
// facts; the configured extension composition changes the ControlProgram.
type programSettings struct {
	Extensions []protocol.SubprocessExtensionSettings `json:"extensions"`
}

// RepositoryProgramRequest identifies one immutable repository-scoped
// composition. It contains no authority receipt and cannot replace
// StandardFlow.
type RepositoryProgramRequest struct {
	Repository               string
	ExternalStateRoot        string
	Host                     string
	CorrelationID            string
	Extensions               []control.Extension
	ConfigurationPath        string
	ConfigurationFingerprint string
}

// StandardProgramForRepository compiles StandardFlow plus the exact
// checksum-verified subprocess extensions selected by the repository's strict
// project configuration. A new value is returned per call, so concurrent
// repositories never share mutable program state.
func StandardProgramForRepository(ctx context.Context, request RepositoryProgramRequest) (control.ControlProgram, error) {
	configured, settings, err := ConfiguredExtensions(ctx, request)
	if err != nil {
		return control.ControlProgram{}, err
	}
	extensions := append([]control.Extension(nil), request.Extensions...)
	extensions = append(extensions, configured...)
	return control.Compile(ctx, control.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: standard.Definition(),
		Extensions: extensions, Settings: settings,
	})
}

// ConfiguredExtensions resolves only additive subprocess extensions. The
// returned settings identity binds every repository policy byte that can
// affect the compiled program.
func ConfiguredExtensions(ctx context.Context, request RepositoryProgramRequest) ([]control.Extension, any, error) {
	if request.Repository == "" {
		return nil, programSettings{}, nil
	}
	host := request.Host
	if host == "" {
		host = "cli"
	}
	correlation := request.CorrelationID
	if correlation == "" {
		correlation = "program-assembly"
	}
	resolver, err := plant.NewResolver(request.ExternalStateRoot)
	if err != nil {
		return nil, nil, err
	}
	invocation, err := resolver.ResolveInvocation(ctx, request.Repository, host, correlation)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve repository control program: %w", err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve repository program layout: %w", err)
	}
	configurationPath := layout.ConfigPath
	if request.ConfigurationPath != "" {
		if !filepath.IsAbs(request.ConfigurationPath) || filepath.Clean(request.ConfigurationPath) != request.ConfigurationPath {
			return nil, nil, fmt.Errorf("candidate repository program configuration path must be exact and absolute")
		}
		configurationPath = request.ConfigurationPath
	}
	raw, err := os.ReadFile(configurationPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, programSettings{}, nil
		}
		return nil, nil, fmt.Errorf("read repository program configuration: %w", err)
	}
	configuration, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("verify repository program configuration: %w", err)
	}
	if request.ConfigurationFingerprint != "" && request.ConfigurationFingerprint != fingerprint {
		return nil, nil, fmt.Errorf("candidate repository program configuration fingerprint mismatch")
	}
	configured := make([]control.Extension, 0, len(configuration.Extensions))
	for _, declaration := range configuration.Extensions {
		extension, extensionErr := subprocess.New(subprocess.Config{
			ID: declaration.ID, Version: declaration.Version, Executable: declaration.Executable, SHA256: declaration.SHA256,
			Manifest: declaration.Manifest, Settings: declaration.Settings,
			Limits: control.SubprocessLimits{Deadline: time.Duration(declaration.DeadlineMillis) * time.Millisecond, StdoutBytes: declaration.StdoutBytes, StderrBytes: declaration.StderrBytes},
		})
		if extensionErr != nil {
			return nil, nil, fmt.Errorf("verify configured subprocess extension %q: %w", declaration.ID, extensionErr)
		}
		configured = append(configured, extension)
	}
	settings, err := canonicalProgramSettings(configuration.Extensions)
	if err != nil {
		return nil, nil, err
	}
	return configured, settings, nil
}

func canonicalProgramSettings(values []protocol.SubprocessExtensionSettings) (programSettings, error) {
	extensions := append([]protocol.SubprocessExtensionSettings(nil), values...)
	for index := range extensions {
		items := []struct {
			name  string
			value *json.RawMessage
		}{{"manifest", &extensions[index].Manifest}, {"settings", &extensions[index].Settings}}
		for _, item := range items {
			name, value := item.name, item.value
			if len(*value) == 0 {
				continue
			}
			var decoded any
			if err := json.Unmarshal(*value, &decoded); err != nil {
				return programSettings{}, fmt.Errorf("canonicalize extension %q %s: %w", extensions[index].ID, name, err)
			}
			canonical, err := json.Marshal(decoded)
			if err != nil {
				return programSettings{}, fmt.Errorf("canonicalize extension %q %s: %w", extensions[index].ID, name, err)
			}
			*value = canonical
		}
	}
	sort.Slice(extensions, func(i, j int) bool { return extensions[i].ID < extensions[j].ID })
	return programSettings{Extensions: extensions}, nil
}
