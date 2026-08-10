package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ConfigRebindSourceRepository = "repository"
	ConfigRebindSourceController = "controller"
	ConfigRebindSourceFile       = "file"
)

var configRebindCheckpoint = func(string) error { return nil }

type ConfigRebindOptions struct {
	Repo                string
	Source              string
	ConfigPath          string
	Apply               bool
	ExpectedFingerprint string
}

type ConfigRebindResult struct {
	SchemaVersion          int      `json:"schema_version"`
	VerificationStatus     string   `json:"verification_status"`
	Applied                bool     `json:"applied"`
	Source                 string   `json:"source"`
	OldAuthority           string   `json:"old_authority"`
	NewAuthority           string   `json:"new_authority"`
	OldConfigSHA256        string   `json:"old_config_sha256"`
	NewConfigSHA256        string   `json:"new_config_sha256"`
	RepositoryConfigSHA256 string   `json:"repository_config_sha256,omitempty"`
	ControllerConfigSHA256 string   `json:"controller_config_sha256"`
	AffectedWorktrees      []string `json:"affected_worktrees"`
	TouchedRoots           []string `json:"touched_roots"`
	Fingerprint            string   `json:"fingerprint"`
	NextOperation          string   `json:"next_operation,omitempty"`
	Reason                 string   `json:"reason"`
}

type configRebindPreview struct {
	result     ConfigRebindResult
	repo       string
	stateRoot  string
	ctx        WorkspaceContext
	binding    DetachedBinding
	rawConfig  []byte
	config     ProjectConfig
	repoBundle map[string][]byte
	ctrlBundle map[string][]byte
	authority  string
	sourcePath string
}

func previewConfigRebind(opts ConfigRebindOptions) (configRebindPreview, error) {
	repo, err := ResolveRepository(opts.Repo)
	if err != nil {
		return configRebindPreview{}, err
	}
	topology, err := ResolveConfigurationTopology(repo)
	if err != nil {
		return configRebindPreview{}, err
	}
	if topology.Mode != string(SupervisionDetached) {
		return configRebindPreview{}, fmt.Errorf("config-rebind requires a detached attachment")
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return configRebindPreview{}, err
	}
	ctx, attached, err := detachedContextFor(repo)
	if err != nil || !attached {
		if err == nil {
			err = fmt.Errorf("config-rebind requires a verified detached attachment")
		}
		return configRebindPreview{}, err
	}
	binding, err := loadBinding(stateRoot, ctx.RepoID)
	if err != nil {
		return configRebindPreview{}, err
	}

	source := strings.ToLower(strings.TrimSpace(opts.Source))
	var config ProjectConfig
	var raw []byte
	var sourcePath, authority string
	switch source {
	case ConfigRebindSourceRepository:
		sourcePath = repositorySourceConfigPath(repo)
		config, raw, err = LoadConfig(sourcePath)
		authority = ConfigAuthorityRepository
	case ConfigRebindSourceController:
		sourcePath = ctx.SourceConfigPath()
		config, raw, err = LoadConfig(sourcePath)
		authority = ConfigAuthorityExternalSnapshot
	case ConfigRebindSourceFile:
		if strings.TrimSpace(opts.ConfigPath) == "" {
			return configRebindPreview{}, fmt.Errorf("config-rebind --source file requires --config")
		}
		config, raw, err = loadDetachedAttachConfig(repo, opts.ConfigPath)
		sourcePath, _ = filepath.Abs(opts.ConfigPath)
		authority = ConfigAuthorityExternalSnapshot
	default:
		return configRebindPreview{}, fmt.Errorf("config-rebind --source must be repository, controller, or file")
	}
	if err != nil {
		return configRebindPreview{}, err
	}
	if err := ValidateConfig(config); err != nil {
		return configRebindPreview{}, err
	}
	controller, err := BuildExportBundle(ctx.SourceConfigPath(), config, raw, "boatstack")
	if err != nil {
		return configRebindPreview{}, err
	}
	var repositoryFiles map[string][]byte
	if source == ConfigRebindSourceRepository && topology.RepositoryPackagePresent {
		repository, buildErr := BuildExportBundle(repositorySourceConfigPath(repo), config, embeddedConfigBytes(raw), "boatstack")
		if buildErr != nil {
			return configRebindPreview{}, buildErr
		}
		repositoryFiles = repository.Files
	}

	newSHA := SHA256Bytes(raw)
	fingerprintInput := struct {
		SchemaVersion    int      `json:"schema_version"`
		BoatstackVersion string   `json:"boatstack_version"`
		SourceCommit     string   `json:"source_commit"`
		RepoID           string   `json:"repo_id"`
		RepoRoot         string   `json:"repo_root"`
		WorktreeID       string   `json:"worktree_id"`
		Branch           string   `json:"branch"`
		Head             string   `json:"head"`
		Aliases          []string `json:"aliases"`
		Shape            string   `json:"shape"`
		Source           string   `json:"source"`
		SourcePath       string   `json:"source_path"`
		SourceSHA256     string   `json:"source_sha256"`
		BindingSHA256    string   `json:"binding_sha256"`
		RepositorySHA256 string   `json:"repository_sha256"`
		ControllerSHA256 string   `json:"controller_sha256"`
	}{
		SchemaVersion: detachedSchemaVersion, BoatstackVersion: Version, SourceCommit: SourceCommit,
		RepoID: ctx.RepoID, RepoRoot: repo, WorktreeID: ctx.WorktreeID,
		Branch: gitOutput(repo, "branch", "--show-current"), Head: gitOutput(repo, "rev-parse", "HEAD"),
		Aliases: topology.AffectedWorktrees, Shape: topology.Shape, Source: source, SourcePath: sourcePath,
		SourceSHA256: newSHA, BindingSHA256: binding.ConfigSHA256,
		RepositorySHA256: topology.RepositoryConfigSHA256, ControllerSHA256: topology.ControllerConfigSHA256,
	}
	fingerprintBytes, err := MarshalJSON(fingerprintInput)
	if err != nil {
		return configRebindPreview{}, err
	}
	fingerprint := SHA256Bytes(fingerprintBytes)
	touched := []string{ctx.ExportRoot(), bindingPath(stateRoot, ctx.RepoID)}
	if len(repositoryFiles) > 0 {
		touched = append(touched, repo)
	}
	nextArgs := []string{"--repo", repo, "--source", source}
	if source == ConfigRebindSourceFile {
		nextArgs = append(nextArgs, "--config", sourcePath)
	}
	nextArgs = append(nextArgs, "--apply", "--expected-fingerprint", fingerprint, "--json")
	next := PrescribedCommand{Program: ctx.HelperPath(), Verb: "config-rebind", Args: nextArgs}.CommandLine()
	result := ConfigRebindResult{
		SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED", Source: source,
		OldAuthority: normalizedConfigAuthority(binding), NewAuthority: authority,
		OldConfigSHA256: binding.ConfigSHA256, NewConfigSHA256: newSHA,
		RepositoryConfigSHA256: topology.RepositoryConfigSHA256, ControllerConfigSHA256: topology.ControllerConfigSHA256,
		AffectedWorktrees: topology.AffectedWorktrees, TouchedRoots: touched, Fingerprint: fingerprint,
		NextOperation: next, Reason: "Preview verified. No files changed.",
	}
	return configRebindPreview{result: result, repo: repo, stateRoot: stateRoot, ctx: ctx, binding: binding, rawConfig: raw, config: config, repoBundle: repositoryFiles, ctrlBundle: controller.Files, authority: authority, sourcePath: sourcePath}, nil
}

type savedFile struct {
	path    string
	value   []byte
	mode    os.FileMode
	existed bool
}

func snapshotFiles(paths []string) ([]savedFile, error) {
	seen := map[string]bool{}
	out := []savedFile{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			out = append(out, savedFile{path: path})
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			if err == nil {
				err = fmt.Errorf("refusing non-regular transaction path: %s", path)
			}
			return nil, err
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, savedFile{path: path, value: value, mode: info.Mode().Perm(), existed: true})
	}
	return out, nil
}

func restoreFiles(saved []savedFile) error {
	for i := len(saved) - 1; i >= 0; i-- {
		item := saved[i]
		if !item.existed {
			if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := atomicWriteMode(item.path, item.value, item.mode); err != nil {
			return err
		}
	}
	return nil
}

func ConfigRebind(opts ConfigRebindOptions) (result ConfigRebindResult, returnErr error) {
	preview, err := previewConfigRebind(opts)
	if err != nil {
		return ConfigRebindResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	if !opts.Apply {
		return preview.result, nil
	}
	if strings.TrimSpace(opts.ExpectedFingerprint) == "" || opts.ExpectedFingerprint != preview.result.Fingerprint {
		preview.result.VerificationStatus = "BLOCKED"
		preview.result.Reason = "The configuration topology changed or the expected fingerprint is missing. Preview again."
		return preview.result, nil
	}

	paths := []string{preview.ctx.SourceConfigPath(), bindingPath(preview.stateRoot, preview.ctx.RepoID)}
	for relative := range preview.ctrlBundle {
		paths = append(paths, filepath.Join(preview.ctx.ExportRoot(), filepath.FromSlash(relative)))
	}
	for relative := range preview.repoBundle {
		paths = append(paths, filepath.Join(preview.repo, filepath.FromSlash(relative)))
	}
	receiptPath := filepath.Join(preview.ctx.controlRoot, "operations", "config-rebind", preview.result.Fingerprint+".json")
	paths = append(paths, receiptPath)
	saved, err := snapshotFiles(paths)
	if err != nil {
		return ConfigRebindResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	defer func() {
		if returnErr != nil {
			if rollbackErr := restoreFiles(saved); rollbackErr != nil {
				returnErr = fmt.Errorf("%v; config-rebind rollback failed: %w", returnErr, rollbackErr)
			}
		}
	}()

	receipt, err := MarshalJSON(map[string]any{
		"schema_version": 1, "operation": "config-rebind", "state": "PREPARED",
		"fingerprint": preview.result.Fingerprint, "repo_id": preview.ctx.RepoID,
		"old_config_sha256": preview.binding.ConfigSHA256, "new_config_sha256": preview.result.NewConfigSHA256,
	})
	if err != nil {
		return ConfigRebindResult{}, err
	}
	if err := atomicWrite(receiptPath, receipt); err != nil {
		return ConfigRebindResult{}, err
	}
	if len(preview.repoBundle) > 0 {
		for _, relative := range sortedKeys(preview.repoBundle) {
			if err := atomicWriteMode(filepath.Join(preview.repo, filepath.FromSlash(relative)), preview.repoBundle[relative], generatedFileMode(relative)); err != nil {
				return ConfigRebindResult{}, err
			}
		}
		if err := CheckExport(preview.repo, preview.repoBundle); err != nil {
			return ConfigRebindResult{}, err
		}
	}
	for _, relative := range sortedKeys(preview.ctrlBundle) {
		if err := atomicWriteMode(filepath.Join(preview.ctx.ExportRoot(), filepath.FromSlash(relative)), preview.ctrlBundle[relative], generatedFileMode(relative)); err != nil {
			return ConfigRebindResult{}, err
		}
	}
	if err := atomicWriteMode(preview.ctx.SourceConfigPath(), preview.rawConfig, 0o644); err != nil {
		return ConfigRebindResult{}, err
	}
	if err := CheckExport(preview.ctx.ExportRoot(), preview.ctrlBundle); err != nil {
		return ConfigRebindResult{}, err
	}
	if err := configRebindCheckpoint("projections-written"); err != nil {
		return ConfigRebindResult{}, fmt.Errorf("config-rebind checkpoint projections-written: %w", err)
	}

	preview.binding.SchemaVersion = detachedSchemaVersion
	preview.binding.ConfigSHA256 = preview.result.NewConfigSHA256
	preview.binding.ConfigAuthority = preview.authority
	preview.binding.CreatedByVersion = Version
	bindingRaw, err := MarshalJSON(preview.binding)
	if err != nil {
		return ConfigRebindResult{}, err
	}
	// The binding is the acceptance record and is promoted last. Any interruption
	// before this write leaves the mixed projection unaccepted and fail-closed.
	if err := atomicWrite(bindingPath(preview.stateRoot, preview.ctx.RepoID), bindingRaw); err != nil {
		return ConfigRebindResult{}, err
	}
	invalidateWorkspaceCache()
	preview.result.Applied = true
	preview.result.ControllerConfigSHA256 = preview.result.NewConfigSHA256
	preview.result.NextOperation = ""
	preview.result.Reason = "Configuration authority rebound and both selected projections verify."
	return preview.result, nil
}
