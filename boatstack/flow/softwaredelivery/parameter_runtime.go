package softwaredelivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/invocation"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

var managedBranchSegment = regexp.MustCompile(`[^a-z0-9._-]+`)

// RuntimeParameterResolver executes only trusted software-delivery resolver
// bindings copied into canonical IR. Repository Flow source cannot provide
// executable resolver code through this boundary.
type RuntimeParameterResolver struct {
	Context    context.Context
	Repository string
	DeliveryID string
	Binding    Resolver
}

// StateParameterValues projects software-delivery durable fields into the
// domain-neutral invocation value interface.
func StateParameterValues(state durable.State) map[string]invocation.Value {
	values := map[string]string{
		"workspace_branch":    state.WorkspaceBranch,
		"preview_fingerprint": state.PreviewFingerprint,
		"publication_id":      state.PublicationID,
		"transaction_id":      state.TransactionID,
	}
	result := map[string]invocation.Value{}
	for facet, value := range values {
		if value != "" {
			result[facet] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: value, Provenance: "durable-state:" + facet}
		}
	}
	return result
}

func (r RuntimeParameterResolver) ResolveParameter(binding controlprogram.ParameterResolverBinding, materialization invocation.Context) (invocation.Value, error) {
	resolved, err := r.Binding.ResolveParameterResolver(binding.Reference, binding.Version)
	if err != nil {
		return invocation.Value{}, err
	}
	if binding.Fingerprint == "" || binding.Fingerprint != resolved.Fingerprint {
		return invocation.Value{}, fmt.Errorf("trusted parameter resolver fingerprint drift")
	}
	value := invocation.Value{Type: resolved.OutputType, ProducerFingerprint: resolved.Fingerprint}
	switch binding.Reference {
	case ParameterResolverPrefix + "repository-default-branch":
		configPath := filepath.Join(r.Repository, ".boatstack", "project.json")
		raw, readErr := os.ReadFile(configPath)
		if readErr != nil {
			return invocation.Value{}, fmt.Errorf("read verified repository configuration: %w", readErr)
		}
		config, decodeErr := protocol.DecodeProjectConfig(raw)
		if decodeErr != nil {
			return invocation.Value{}, fmt.Errorf("decode verified repository configuration: %w", decodeErr)
		}
		_, configFingerprint, fingerprintErr := protocol.ProjectConfigFingerprint(raw)
		if fingerprintErr != nil || config.Project.DefaultBranch == "" {
			return invocation.Value{}, fmt.Errorf("repository default branch configuration is missing or unverified")
		}
		value.Canonical, value.Provenance = config.Project.DefaultBranch, "project-config:"+configFingerprint
	case ParameterResolverPrefix + "delivery-branch":
		segment := strings.Trim(managedBranchSegment.ReplaceAllString(strings.ToLower(r.DeliveryID), "-"), "-.")
		if segment == "" || r.DeliveryID == "" {
			return invocation.Value{}, fmt.Errorf("delivery identity cannot produce a managed branch")
		}
		branch := "feat/" + segment
		plantResolver, resolverErr := plant.NewResolver("")
		if resolverErr != nil {
			return invocation.Value{}, resolverErr
		}
		exists, inspectErr := plantResolver.BranchExists(r.Context, r.Repository, branch)
		if inspectErr != nil {
			return invocation.Value{}, fmt.Errorf("inspect managed branch: %w", inspectErr)
		}
		if exists {
			return invocation.Value{}, fmt.Errorf("managed branch %q already exists and cannot be silently reused", branch)
		}
		value.Canonical, value.Provenance = branch, "delivery:"+r.DeliveryID
	case ParameterResolverPrefix + "managed-worktree-destination":
		repository, canonicalErr := filepath.Abs(r.Repository)
		if canonicalErr != nil {
			return invocation.Value{}, canonicalErr
		}
		if resolvedRepository, resolveErr := filepath.EvalSymlinks(repository); resolveErr == nil {
			repository = resolvedRepository
		}
		root := filepath.Dir(repository)
		segment := strings.Trim(managedBranchSegment.ReplaceAllString(strings.ToLower(r.DeliveryID), "-"), "-.")
		if segment == "" {
			return invocation.Value{}, fmt.Errorf("delivery identity cannot produce a managed destination")
		}
		destination := filepath.Clean(filepath.Join(root, filepath.Base(repository)+"-"+segment))
		relative, relErr := filepath.Rel(root, destination)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return invocation.Value{}, fmt.Errorf("managed destination escapes trusted worktree root")
		}
		if _, statErr := os.Lstat(destination); statErr == nil {
			return invocation.Value{}, fmt.Errorf("managed destination already exists and conflicts with this run")
		} else if !os.IsNotExist(statErr) {
			return invocation.Value{}, statErr
		}
		plantResolver, resolverErr := plant.NewResolver("")
		if resolverErr != nil {
			return invocation.Value{}, resolverErr
		}
		invoking, resolveErr := plantResolver.ResolveInvocation(r.Context, r.Repository, "cli", "parameter-resolver")
		if resolveErr != nil {
			return invocation.Value{}, resolveErr
		}
		scopeFingerprint, fingerprintErr := general.Fingerprint(struct {
			RepositoryID string `json:"repository_id"`
			GitCommonID  string `json:"git_common_id"`
			WorktreeID   string `json:"worktree_id"`
			Ref          string `json:"ref"`
		}{invoking.RepositoryID, invoking.GitCommonID, invoking.WorktreeID, invoking.Ref})
		if fingerprintErr != nil || scopeFingerprint != materialization.ExecutionScopeFingerprint {
			return invocation.Value{}, fmt.Errorf("managed destination invocation scope drift")
		}
		identity := sha256.Sum256(bytes.Join([][]byte{[]byte(invoking.RepositoryID), []byte(invoking.GitCommonID), []byte(materialization.RunID), []byte(r.DeliveryID), []byte(invoking.WorktreeID), []byte(invoking.Ref)}, []byte{0}))
		value.Canonical, value.Provenance = destination, "workspace-layout:"+hex.EncodeToString(identity[:])
	default:
		return invocation.Value{}, fmt.Errorf("unknown runtime parameter resolver %q", binding.Reference)
	}
	return value, nil
}
