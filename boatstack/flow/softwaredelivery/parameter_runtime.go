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
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/invocation"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

var managedBranchSegment = regexp.MustCompile(`[^a-z0-9._-]+`)

const RecoveryTransactionFacet = "recovery_transaction_id"

// RuntimeParameterResolver executes only trusted software-delivery resolver
// bindings copied into canonical IR. Repository Flow source cannot provide
// executable resolver code through this boundary.
type RuntimeParameterResolver struct {
	Context        context.Context
	Repository     string
	DeliveryID     string
	SourceRevision string
	Binding        Resolver
}

// StateParameterValues projects software-delivery durable fields into the
// domain-neutral invocation value interface.
func StateParameterValues(state durable.State) map[string]invocation.Value {
	values := map[string]string{
		"workspace_branch":             state.WorkspaceBranch,
		"preview_fingerprint":          state.PreviewFingerprint,
		"publication_id":               state.PublicationID,
		"transaction_id":               state.TransactionID,
		"planning_package_fingerprint": state.PlanningPackageFingerprint,
	}
	result := map[string]invocation.Value{}
	for facet, value := range values {
		if value != "" {
			result[facet] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: value, Provenance: "durable-state:" + facet}
		}
	}
	return result
}

// UsesObservationParameterValues reports whether a transition declares a
// trusted state producer whose value is owned by the current plant observation
// rather than durable state.
func UsesObservationParameterValues(bindings []controlprogram.TransitionParameterBinding) bool {
	for _, binding := range bindings {
		if binding.Producer.Kind == controlprogram.ParameterSourceState && binding.Producer.Facet == RecoveryTransactionFacet {
			return true
		}
	}
	return false
}

// ObservationParameterValues projects current recovery context into the
// domain-neutral invocation value interface. The pending-journal observation
// remains authoritative when an interrupted effect did not update durable
// transaction state.
func ObservationParameterValues(observation model.Observation) map[string]invocation.Value {
	result := map[string]invocation.Value{}
	if observation.RecoveryInfo.Status == model.FactKnown && observation.RecoveryInfo.Value.TransactionID != "" {
		result[RecoveryTransactionFacet] = invocation.Value{
			Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: observation.RecoveryInfo.Value.TransactionID,
			Provenance: "observation:recovery-info",
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
	switch {
	case binding.Reference == ParameterResolverPrefix+"admitted-planning-package-fingerprint":
		fingerprint, ok := materialization.State["planning_package_fingerprint"]
		if !ok || len(fingerprint.Canonical) != 64 {
			return invocation.Value{}, fmt.Errorf("admitted planning package fingerprint is unavailable from durable state")
		}
		value.Canonical, value.Provenance = fingerprint.Canonical, fingerprint.Provenance
	case strings.HasPrefix(binding.Reference, planningPackagePlanOutputResolverPrefix):
		planOutput := strings.TrimPrefix(binding.Reference, planningPackagePlanOutputResolverPrefix)
		if !planningPackageSegment.MatchString(planOutput) {
			return invocation.Value{}, fmt.Errorf("planning-package plan output binding is invalid")
		}
		value.Canonical, value.Provenance = planOutput, "compiled-planning-package-binding"
	case binding.Reference == ParameterResolverPrefix+"repository-default-branch":
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
	case binding.Reference == ParameterResolverPrefix+"delivery-branch":
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
	case binding.Reference == ParameterResolverPrefix+"managed-worktree-destination":
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
	case binding.Reference == ParameterResolverPrefix+"current-source-revision":
		if (len(r.SourceRevision) != 40 && len(r.SourceRevision) != 64) || strings.Trim(r.SourceRevision, "0123456789abcdef") != "" {
			return invocation.Value{}, fmt.Errorf("current committed source revision is unavailable")
		}
		value.Canonical, value.Provenance = r.SourceRevision, "repository-head"
	case strings.HasPrefix(binding.Reference, ParameterResolverPrefix+"gate-evidence-path/"):
		gate := strings.TrimPrefix(binding.Reference, ParameterResolverPrefix+"gate-evidence-path/")
		path, _, readErr := readCanonicalParameterArtifact(r.Repository, r.DeliveryID, gateEvidenceInputPath(gate))
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		value.Canonical, value.Provenance = path, "gate-evidence:"+gate
	case strings.HasPrefix(binding.Reference, ParameterResolverPrefix+"gate-evidence-fingerprint/"):
		gate := strings.TrimPrefix(binding.Reference, ParameterResolverPrefix+"gate-evidence-fingerprint/")
		_, raw, readErr := readCanonicalParameterArtifact(r.Repository, r.DeliveryID, gateEvidenceInputPath(gate))
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		digest := sha256.Sum256(raw)
		value.Canonical, value.Provenance = hex.EncodeToString(digest[:]), "gate-evidence:"+gate
	case binding.Reference == ParameterResolverPrefix+"visual-evidence-manifest-path":
		path, _, readErr := readCanonicalParameterArtifact(r.Repository, r.DeliveryID, "visual-manifest.input.json")
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		value.Canonical, value.Provenance = path, "visual-evidence-manifest"
	case binding.Reference == ParameterResolverPrefix+"visual-evidence-privacy-receipt":
		_, raw, readErr := readCanonicalParameterArtifact(r.Repository, r.DeliveryID, "visual-manifest.input.json")
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		digest := sha256.Sum256(raw)
		value.Canonical, value.Provenance = hex.EncodeToString(digest[:]), "visual-evidence-manifest"
	case binding.Reference == ParameterResolverPrefix+"publication-body-path":
		path, _, readErr := readCanonicalPublicationBody(r.Repository, r.DeliveryID)
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		value.Canonical, value.Provenance = path, "publication-body"
	case binding.Reference == ParameterResolverPrefix+"publication-body-sha256":
		_, raw, readErr := readCanonicalPublicationBody(r.Repository, r.DeliveryID)
		if readErr != nil {
			return invocation.Value{}, readErr
		}
		digest := sha256.Sum256(raw)
		value.Canonical, value.Provenance = hex.EncodeToString(digest[:]), "publication-body"
	default:
		return invocation.Value{}, fmt.Errorf("unknown runtime parameter resolver %q", binding.Reference)
	}
	return value, nil
}

func gateEvidenceInputPath(gate string) string {
	switch gate {
	case "build", "test", "review", "change", "journey":
		return gate + ".input.json"
	default:
		return ""
	}
}

func readCanonicalParameterArtifact(repository, deliveryID, name string) (string, []byte, error) {
	if !planningPackageSegment.MatchString(deliveryID) || name == "" || filepath.Base(name) != name {
		return "", nil, fmt.Errorf("canonical parameter artifact identity is invalid")
	}
	return readRegularParameterArtifact(repository, filepath.Join(".boatstack", "evidence", deliveryID, name))
}

func readCanonicalPublicationBody(repository, deliveryID string) (string, []byte, error) {
	if !planningPackageSegment.MatchString(deliveryID) {
		return "", nil, fmt.Errorf("publication body delivery identity is invalid")
	}
	return readRegularParameterArtifact(repository, filepath.Join(".boatstack", "publication", deliveryID+".body.md"))
}

func readRegularParameterArtifact(repository, relative string) (string, []byte, error) {
	root, err := filepath.Abs(repository)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(root, relative)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("canonical parameter artifact is unavailable: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("canonical parameter artifact escapes the repository")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil || len(raw) == 0 {
		return "", nil, fmt.Errorf("canonical parameter artifact is empty or unreadable: %s", resolved)
	}
	return resolved, raw, nil
}
