// Package humanidentitybinding binds host-facing human identity presentations
// to the authoritative, verified project configuration. It never executes an
// identity descriptor command and grants no authority.
package humanidentitybinding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type ResponseHandler interface {
	Handle(context.Context, surfaces.Request) (surfaces.Response, error)
}

// Handle is the shared host response boundary. Human questions and program
// changes cannot leave it without the repository's verified identity
// presentation, regardless of whether the caller is the CLI or public SDK.
func Handle(ctx context.Context, externalStateRoot string, handler ResponseHandler, request surfaces.Request) (surfaces.Response, error) {
	response, err := handler.Handle(ctx, request)
	if identityErr := Attach(ctx, externalStateRoot, request, &response); identityErr != nil {
		return response, identityErr
	}
	return response, err
}

// Attach adds identity provenance only to authority surfaces that need it.
func Attach(ctx context.Context, externalStateRoot string, request surfaces.Request, response *surfaces.Response) error {
	if response == nil {
		return nil
	}
	programChangeRequiresHuman := response.ProgramChange != nil
	questionRequiresIdentity := response.Question != nil && questionRequiresHuman(*response.Question)
	if questionRequiresIdentity && questionUsesExplicitActor(response) {
		questionRequiresIdentity = false
	}
	if !programChangeRequiresHuman && !questionRequiresIdentity {
		return nil
	}
	presentation, err := PresentationForRequest(ctx, externalStateRoot, request, response.Snapshot)
	if err != nil {
		return err
	}
	if questionRequiresIdentity {
		response.Question.HumanIdentity = &presentation
	}
	if programChangeRequiresHuman {
		response.ProgramChange.HumanIdentity = &presentation
	}
	return nil
}

// PresentationForRequest resolves the descriptor selected by the exact
// configuration authority for this invocation.
func PresentationForRequest(ctx context.Context, externalStateRoot string, request surfaces.Request, observed *model.Snapshot) (humanidentity.Presentation, error) {
	if request.TransitionID == "installation.initialize" {
		if request.ControlBundle == nil || request.ControlBundle.Target == nil {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: initialization has no target control bundle")
		}
		configPath, ok := request.Parameters.Get("config_path")
		if !ok {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: initialization has no configuration path")
		}
		return PresentationFromBoundConfig(configPath, *request.ControlBundle.Target)
	}
	var bundle *boatstackruntime.ControlBundleSnapshot
	if request.ControlBundle != nil {
		bundle = &request.ControlBundle.Source
	}
	return PresentationForRepository(ctx, externalStateRoot, request.Repository, request.Host, request.CorrelationID, bundle, observed)
}

// PresentationForRepository resolves the controller layout before reading
// configuration. Repository and external configuration authority therefore
// select the same source used by observation and effects.
func PresentationForRepository(ctx context.Context, externalStateRoot, repository, host, correlation string, bundle *boatstackruntime.ControlBundleSnapshot, observed *model.Snapshot) (humanidentity.Presentation, error) {
	if repository == "" {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: repository is required")
	}
	if host == "" {
		host = "cli"
	}
	if correlation == "" {
		correlation = "human-identity"
	}
	resolver, err := plant.NewResolver(externalStateRoot)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, host, correlation)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	layout, current, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	config, raw, fingerprint, err := readConfig(layout.ConfigPath)
	if err != nil {
		return humanidentity.Presentation{}, err
	}

	trusted := false
	if layout.ConfigAuthority == "repository" && bundle != nil {
		if !bundleBindsRawConfig(*bundle, raw) {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: project configuration does not match the verified control bundle")
		}
	}
	if observed != nil {
		if observed.Invocation.RepositoryID != current.RepositoryID || observed.Invocation.GitCommonID != current.GitCommonID || observed.Invocation.WorktreeID != current.WorktreeID {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: observed configuration belongs to a different invocation")
		}
		if observed.Configuration.Status != model.FactKnown || observed.Configuration.Value != model.ConfigurationVerified {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: observed configuration is not verified")
		}
		if !snapshotBindsConfig(observed, fingerprint) {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: project configuration does not match the observed configuration")
		}
		trusted = true
	}
	if !trusted {
		stateRaw, readErr := os.ReadFile(layout.StatePath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: authoritative configuration has no verified state")
			}
			return humanidentity.Presentation{}, readErr
		}
		state, decodeErr := durable.DecodeState(stateRaw)
		if decodeErr != nil {
			return humanidentity.Presentation{}, decodeErr
		}
		if state.RepositoryID != current.RepositoryID || state.GitCommonID != current.GitCommonID || state.WorktreeID != current.WorktreeID {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: verified durable state belongs to a different invocation")
		}
		if state.Configuration != model.ConfigurationVerified || state.ConfigFingerprint != fingerprint {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: authoritative configuration does not match verified durable state")
		}
	}
	return humanidentity.NewPresentation(config.Identity.Human)
}

// PresentationFromBoundConfig verifies an exact candidate configuration
// against a control-bundle snapshot. This is used for initialization, before a
// controller layout can select an installed authority.
func PresentationFromBoundConfig(configPath string, snapshot boatstackruntime.ControlBundleSnapshot) (humanidentity.Presentation, error) {
	config, raw, _, err := readConfig(configPath)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	if !bundleBindsRawConfig(snapshot, raw) {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: project configuration does not match the verified control bundle")
	}
	return humanidentity.NewPresentation(config.Identity.Human)
}

func readConfig(configPath string) (protocol.ProjectConfig, []byte, string, error) {
	info, err := os.Lstat(configPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return protocol.ProjectConfig{}, nil, "", fmt.Errorf("HUMAN_IDENTITY_UNBOUND: project configuration is not a regular file")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return protocol.ProjectConfig{}, nil, "", err
	}
	config, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return protocol.ProjectConfig{}, nil, "", err
	}
	return config, raw, fingerprint, nil
}

func bundleBindsRawConfig(snapshot boatstackruntime.ControlBundleSnapshot, raw []byte) bool {
	digest := sha256.Sum256(raw)
	expected := hex.EncodeToString(digest[:])
	for _, binding := range snapshot.Files {
		if binding.Path == ".boatstack/project.json" {
			return !binding.Absent && binding.SHA256 == expected
		}
	}
	return false
}

func snapshotBindsConfig(snapshot *model.Snapshot, fingerprint string) bool {
	if snapshot == nil {
		return false
	}
	for _, evidence := range snapshot.Configuration.Evidence {
		if evidence.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

func questionRequiresHuman(question surfaces.Question) bool {
	authorities := append(append([]catalog.AuthorityClass(nil), question.Authority...), question.AuthorityAll...)
	for _, authority := range authorities {
		if authority == catalog.AuthorityHuman {
			return true
		}
	}
	return false
}

// questionUsesExplicitActor preserves the human authority question when no
// verified repository-selected identity exists. The host must collect an
// explicit actor; stale or candidate configuration never supplies one.
func questionUsesExplicitActor(response *surfaces.Response) bool {
	if response == nil || response.Question == nil {
		return false
	}
	switch response.Question.TransitionID {
	case "installation.initialize", "configuration.initialize", "configuration.mutate", "configuration.reconcile":
		return response.Snapshot == nil || response.Snapshot.Configuration.Status != model.FactKnown || response.Snapshot.Configuration.Value != model.ConfigurationVerified
	default:
		return false
	}
}
