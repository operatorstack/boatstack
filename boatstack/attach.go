package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Attach, detach, and status operations for Detached Supervision. Attaching a
// repository writes Boatstack's controller state to an external control root and a
// binding that identifies the repository; it never writes into the target working
// tree or its Git directory. Detaching removes the attachment (and, unless asked
// to preserve it, the external state). Status reports the binding and verifies it.

// AttachOptions requests a detached attachment. StateRoot, when set, overrides the
// external control-state root for this process (the CLI wires --state-root to it).
type AttachOptions struct {
	Repo       string
	ConfigPath string
	// BinaryPath is the already verified helper to install into detached
	// controller state. The CLI leaves it empty and uses its running binary;
	// tests and embedders may bind an equivalent verified helper explicitly.
	BinaryPath string
	Force      bool
}

// AttachResult is the deterministic outcome of an attach request.
type AttachResult struct {
	SchemaVersion      int                        `json:"schema_version"`
	VerificationStatus string                     `json:"verification_status"` // VERIFIED | BLOCKED
	Mode               string                     `json:"mode,omitempty"`
	RepoID             string                     `json:"repo_id,omitempty"`
	RepoRoot           string                     `json:"repo_root,omitempty"`
	ControlRoot        string                     `json:"control_root,omitempty"`
	WorktreeID         string                     `json:"worktree_id,omitempty"`
	ConfigSHA256       string                     `json:"config_sha256,omitempty"`
	ConfigAuthority    string                     `json:"config_authority,omitempty"`
	Reason             string                     `json:"reason"`
	FeatureMigrations  []DetachedFeatureMigration `json:"feature_migrations,omitempty"`
}

func loadDetachedAttachConfig(root, explicitPath string) (ProjectConfig, []byte, error) {
	if strings.TrimSpace(explicitPath) == "" {
		configPath := filepath.Join(root, sourceConfigName)
		config, raw, err := LoadConfig(configPath)
		if os.IsNotExist(err) {
			config = defaultConfig(root, detectTestCommand(root))
			raw, err = MarshalJSON(config)
		}
		return config, raw, err
	}
	absolute, err := filepath.Abs(explicitPath)
	if err != nil {
		return ProjectConfig{}, nil, err
	}
	inputInfo, err := os.Lstat(absolute)
	if err != nil {
		return ProjectConfig{}, nil, fmt.Errorf("external project configuration is missing or unreadable: %w", err)
	}
	if inputInfo.Mode()&os.ModeSymlink != 0 {
		return ProjectConfig{}, nil, fmt.Errorf("external project configuration must be a regular non-symlink file")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ProjectConfig{}, nil, fmt.Errorf("external project configuration is missing or unreadable: %w", err)
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return ProjectConfig{}, nil, fmt.Errorf("external project configuration must be a readable regular file")
	}
	common, err := gitCommonDir(root)
	if err != nil {
		return ProjectConfig{}, nil, err
	}
	if pathWithin(root, resolved) || pathWithin(common, resolved) {
		return ProjectConfig{}, nil, fmt.Errorf("external project configuration must be outside the repository and its Git directory")
	}
	return LoadConfig(resolved)
}

func blockedAttach(reason string) AttachResult {
	return AttachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

// AttachDetached attaches repo in detached mode. It leaves the repository working
// tree and Git directory byte-for-byte unchanged; all controller state is written
// under the external control root.
func AttachDetached(opts AttachOptions) (AttachResult, error) {
	root, err := ResolveRepository(opts.Repo)
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	identity, err := repoIdentity(root)
	if err != nil {
		return blockedAttach("Boatstack could not compute a repository identity: " + err.Error()), nil
	}

	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return blockedAttach("Boatstack could not read the attachment registry: " + err.Error()), nil
	}
	if existing, ok := registry.Repositories[root]; ok && !opts.Force {
		return blockedAttach(fmt.Sprintf("This repository is already attached (repo_id %s). Detach first, or re-run with --force.", existing)), nil
	}

	ctx := detachedContextFromIdentity(stateRoot, identity)

	config, rawConfig, err := loadDetachedAttachConfig(root, opts.ConfigPath)
	if err != nil {
		return blockedAttach("Boatstack could not load the detached project configuration: " + err.Error()), nil
	}
	configSHA256 := SHA256Bytes(rawConfig)
	configAuthority := ConfigAuthorityExternalSnapshot
	if strings.TrimSpace(opts.ConfigPath) == "" {
		configAuthority = ConfigAuthoritySynthesized
		if fileExists(filepath.Join(root, sourceConfigName)) {
			configAuthority = ConfigAuthorityRepository
		}
	}
	imports, migrationResults, migrationErr := planDetachedFeatureImports(root, ctx)
	if migrationErr != nil {
		result := blockedAttach("Boatstack refused detached feature migration: " + migrationErr.Error())
		result.FeatureMigrations = migrationResults
		return result, nil
	}

	// Generate the controller bundle and write it under the external control root.
	// The bundle layout mirrors embedded (.product-loop/** plus host adapter dirs),
	// only relocated outside the repository.
	bundle, err := BuildExportBundle(ctx.SourceConfigPath(), config, rawConfig, "boatstack")
	if err != nil {
		return blockedAttach("Boatstack could not build the controller bundle: " + err.Error()), nil
	}
	if err := os.MkdirAll(ctx.controlRoot, 0o755); err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := writeExport(ctx.controlRoot, bundle.Files, nil); err != nil {
		return blockedAttach("Boatstack could not write the controller bundle: " + err.Error()), nil
	}
	sourcePath, err := newControllerPath(ctx.controlRoot, ctx.SourceConfigPath())
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := atomicWrite(sourcePath.path, rawConfig); err != nil {
		return blockedAttach(err.Error()), nil
	}
	migrationResults, err = applyDetachedFeatureImports(imports, migrationResults)
	if err != nil {
		return blockedAttach("Boatstack could not import embedded feature state: " + err.Error()), nil
	}

	// Write the binding and index it in the registry.
	binding := DetachedBinding{
		SchemaVersion:     detachedSchemaVersion,
		Mode:              string(SupervisionDetached),
		RepoID:            identity.RepoID,
		CanonicalRepoPath: identity.CanonicalRepoPath,
		GitCommonIdentity: identity.GitCommonIdentity,
		InitialCommit:     identity.InitialCommit,
		NormalizedOrigin:  identity.NormalizedOrigin,
		ConfigSHA256:      configSHA256,
		ConfigAuthority:   configAuthority,
		CreatedByVersion:  Version,
		CreatedAt:         nowRFC3339(),
	}
	bindingRaw, err := MarshalJSON(binding)
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(bindingPath(stateRoot, identity.RepoID)), 0o755); err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := atomicWrite(bindingPath(stateRoot, identity.RepoID), bindingRaw); err != nil {
		return blockedAttach(err.Error()), nil
	}
	registry.Repositories[root] = identity.RepoID
	if err := saveRegistry(stateRoot, registry); err != nil {
		return blockedAttach(err.Error()), nil
	}
	invalidateWorkspaceCache()

	// Populate the external shared-runtime slot from the running helper so the
	// developer-level engagement probe has a stable helper to invoke. The binding is
	// written above, so WorkspaceFor now resolves detached and the slot is external.
	source := strings.TrimSpace(opts.BinaryPath)
	if source == "" {
		source, err = os.Executable()
		if err != nil {
			return blockedAttach("Boatstack could not locate its running helper: " + err.Error()), nil
		}
	}
	if _, runtimeErr := installDetachedRuntime(root, source); runtimeErr != nil {
		return blockedAttach("Boatstack could not install the external runtime: " + runtimeErr.Error()), nil
	}
	if _, _, runtimeErr := installControllerLocalRuntime(ctx.ExportRoot(), source, config.Integrations); runtimeErr != nil {
		return blockedAttach("Boatstack could not install the detached controller helper: " + runtimeErr.Error()), nil
	}

	return AttachResult{
		SchemaVersion:      detachedSchemaVersion,
		VerificationStatus: "VERIFIED",
		Mode:               string(SupervisionDetached),
		RepoID:             identity.RepoID,
		RepoRoot:           root,
		ControlRoot:        ctx.controlRoot,
		WorktreeID:         identity.WorktreeID,
		ConfigSHA256:       configSHA256,
		ConfigAuthority:    configAuthority,
		FeatureMigrations:  migrationResults,
		Reason:             "Attached Boatstack in detached mode. The repository was not modified; all controller state lives under the external control root.",
	}, nil
}

// DetachOptions requests removal of a detached attachment.
type DetachOptions struct {
	Repo          string
	PreserveState bool
}

// DetachResult is the deterministic outcome of a detach request.
type DetachResult struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"` // VERIFIED | BLOCKED
	RepoID             string `json:"repo_id,omitempty"`
	StateRemoved       bool   `json:"state_removed"`
	Reason             string `json:"reason"`
}

// DetachDetached removes a repository's detached attachment. It always removes the
// registry entry; it removes the external controller state only when PreserveState
// is false. It never touches the repository itself.
func DetachDetached(opts DetachOptions) (DetachResult, error) {
	root, err := ResolveRepository(opts.Repo)
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	repoID, ok := registry.Repositories[root]
	if !ok {
		return DetachResult{
			SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED",
			Reason: "This repository is not attached in detached mode; nothing to detach.",
		}, nil
	}
	delete(registry.Repositories, root)
	if err := saveRegistry(stateRoot, registry); err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", RepoID: repoID, Reason: err.Error()}, nil
	}
	stateRemoved := false
	if !opts.PreserveState {
		if err := os.RemoveAll(repositoryControlRoot(stateRoot, repoID)); err != nil {
			return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", RepoID: repoID, Reason: err.Error()}, nil
		}
		stateRemoved = true
	}
	invalidateWorkspaceCache()
	reason := "Detached Boatstack. The external controller state was removed."
	if opts.PreserveState {
		reason = "Detached Boatstack. The external controller state was preserved."
	}
	return DetachResult{
		SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED",
		RepoID: repoID, StateRemoved: stateRemoved, Reason: reason,
	}, nil
}

// DetachedStatusResult reports whether a repository is attached in detached mode
// and whether its binding verifies.
type DetachedStatusResult struct {
	SchemaVersion          int      `json:"schema_version"`
	Attached               bool     `json:"attached"`
	Verified               bool     `json:"verified"`
	Mode                   string   `json:"mode"`
	RepoID                 string   `json:"repo_id,omitempty"`
	RepoRoot               string   `json:"repo_root,omitempty"`
	ControlRoot            string   `json:"control_root,omitempty"`
	WorktreeID             string   `json:"worktree_id,omitempty"`
	ConfigSHA256           string   `json:"config_sha256,omitempty"`
	ConfigAuthority        string   `json:"config_authority,omitempty"`
	ConfigRelation         string   `json:"config_relation,omitempty"`
	RepositoryConfigSHA256 string   `json:"repository_config_sha256,omitempty"`
	ControllerConfigSHA256 string   `json:"controller_config_sha256,omitempty"`
	AffectedWorktrees      []string `json:"affected_worktrees,omitempty"`
	NextOperation          string   `json:"next_operation,omitempty"`
	Reason                 string   `json:"reason"`
}

// DetachedStatus reports the detached attachment state for a repository. It is
// read-only.
func DetachedStatus(repoPath string) (DetachedStatusResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return DetachedStatusResult{SchemaVersion: detachedSchemaVersion, Reason: err.Error()}, nil
	}
	ctx, ok, verifyErr := detachedContextFor(root)
	if !ok {
		return DetachedStatusResult{
			SchemaVersion: detachedSchemaVersion, Attached: false, Mode: string(SupervisionEmbedded),
			RepoRoot: root, Reason: "This repository is not attached in detached mode.",
		}, nil
	}
	if verifyErr != nil {
		configSHA256 := ""
		stateRoot, rootErr := detachedStateRoot()
		if rootErr == nil {
			registry, registryErr := loadRegistry(stateRoot)
			if registryErr == nil {
				if binding, bindingErr := loadBinding(stateRoot, registry.Repositories[root]); bindingErr == nil {
					configSHA256 = binding.ConfigSHA256
				}
			}
		}
		return DetachedStatusResult{
			SchemaVersion: detachedSchemaVersion, Attached: true, Verified: false, Mode: string(SupervisionDetached),
			RepoID: ctx.RepoID, RepoRoot: root, ControlRoot: ctx.controlRoot, WorktreeID: ctx.WorktreeID,
			ConfigSHA256: configSHA256, ControllerConfigSHA256: configSHA256, Reason: verifyErr.Error(),
		}, nil
	}
	topology, topologyErr := ResolveConfigurationTopology(root)
	if topologyErr != nil {
		return DetachedStatusResult{SchemaVersion: detachedSchemaVersion, Attached: true, Verified: false, Mode: string(SupervisionDetached), RepoID: ctx.RepoID, RepoRoot: root, ControlRoot: ctx.controlRoot, WorktreeID: ctx.WorktreeID, Reason: topologyErr.Error()}, nil
	}
	return DetachedStatusResult{
		SchemaVersion: detachedSchemaVersion, Attached: true, Verified: true, Mode: string(SupervisionDetached),
		RepoID: ctx.RepoID, RepoRoot: ctx.RepoRoot, ControlRoot: ctx.controlRoot, WorktreeID: ctx.WorktreeID,
		ConfigSHA256: topology.ControllerConfigSHA256, ConfigAuthority: topology.Authority,
		ConfigRelation: topology.Relation, RepositoryConfigSHA256: topology.RepositoryConfigSHA256,
		ControllerConfigSHA256: topology.ControllerConfigSHA256, AffectedWorktrees: topology.AffectedWorktrees,
		NextOperation: topology.NextOperation,
		Reason:        "This repository is attached in detached mode and its binding verifies.",
	}, nil
}

func bindingConfigSHA256(ctx WorkspaceContext) string {
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return ""
	}
	binding, err := loadBinding(stateRoot, ctx.RepoID)
	if err != nil {
		return ""
	}
	return binding.ConfigSHA256
}
