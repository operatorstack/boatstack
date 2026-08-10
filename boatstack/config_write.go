package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// configMutationCheckpoint is a failure-injection seam for proving that every
// partial projection is restored before a configuration mutation returns.
var configMutationCheckpoint = func(string) error { return nil }

type configMutationResult struct {
	Changed bool
	Source  string // source-and-export | generated-only
}

type configMutationProjection struct {
	name  string
	root  string
	files map[string][]byte
}

type configSourceWrite struct {
	name  string
	path  string
	value []byte
}

// withConfigurationMutationLock serializes configuration changes across every
// worktree in one clone. Detached aliases share the same Git common directory,
// so two helpers cannot read the same base configuration and lose one update.
func withConfigurationMutationLock(repo string, apply func() error) error {
	common, err := gitCommonDir(repo)
	if err != nil {
		return err
	}
	lock := filepath.Join(common, "boatstack-configuration-mutation.lock")
	if err := rejectSymlinkComponents(common, lock); err != nil {
		return err
	}
	for attempt := 0; attempt < 100; attempt++ {
		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr == nil {
			_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), operationTimestamp())
			_ = file.Close()
			defer os.Remove(lock)
			return apply()
		}
		if !isLockContention(openErr, lock) {
			return openErr
		}
		if info, statErr := os.Stat(lock); statErr == nil && operationNow().Sub(info.ModTime()) > time.Minute {
			_ = os.Remove(lock)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("configuration mutation is busy")
}

func projectionTransactionPaths(projection configMutationProjection) []string {
	paths := make([]string, 0, len(projection.files))
	seen := map[string]bool{}
	for relative := range projection.files {
		path := filepath.Join(projection.root, filepath.FromSlash(relative))
		paths = append(paths, path)
		seen[relative] = true
	}
	// WriteExport may remove generated paths that disappeared from the new
	// bundle. Capture those pre-images too so rollback remains complete.
	for relative := range previousFiles(projection.root) {
		if seen[relative] {
			continue
		}
		paths = append(paths, filepath.Join(projection.root, filepath.FromSlash(relative)))
	}
	return paths
}

func verifyConfigurationSource(write configSourceWrite) error {
	current, err := os.ReadFile(write.path)
	if err != nil {
		return err
	}
	if string(current) != string(write.value) {
		return fmt.Errorf("%s configuration source did not match the accepted bytes", write.name)
	}
	return nil
}

// Boundary: ordinary Boatstack project-configuration mutation.
// Control law: a successful mutation preserves declared authority and leaves
// every bound source and generated projection verified.
// Authorized actor: command handlers admitted through this function.
// Required evidence: current topology, valid candidate bytes, collision-free
// projections, and successful post-write verification.
// Failure behavior: reject before writing or restore exact pre-images.
// Release condition: every selected projection verifies; detached acceptance is
// recorded by promoting the binding last.
func mutateManagedConfiguration(repoPath string, mutate func(*ProjectConfig) (bool, error)) (result configMutationResult, returnErr error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return configMutationResult{}, err
	}
	returnErr = withConfigurationMutationLock(repo, func() (transactionErr error) {
		topology, err := RequireManagedConfiguration(repo)
		if err != nil {
			return err
		}
		if topology.Mode == string(SupervisionDetached) && topology.Authority == ConfigAuthorityLegacyUnknown {
			return fmt.Errorf("CONFIG_REBIND_REQUIRED: configuration mutation needs an explicit detached configuration authority")
		}

		sourcePath := topology.RepositorySourcePath
		generatedOnly := false
		if topology.Mode == string(SupervisionDetached) &&
			(topology.Authority == ConfigAuthorityExternalSnapshot || topology.Authority == ConfigAuthoritySynthesized) {
			sourcePath = topology.ControllerSourcePath
		}
		if topology.Mode == string(SupervisionEmbedded) && !fileExists(sourcePath) {
			sourcePath = WorkspaceFor(repo).ProjectConfigPath()
			generatedOnly = true
		}
		if !fileExists(sourcePath) {
			return fmt.Errorf("CONFIG_REBIND_REQUIRED: declared configuration source is missing: %s", sourcePath)
		}

		config, _, err := LoadConfig(sourcePath)
		if err != nil {
			return err
		}
		changed, err := mutate(&config)
		if err != nil {
			return err
		}
		result.Source = "source-and-export"
		if generatedOnly {
			result.Source = "generated-only"
		}
		if !changed {
			return nil
		}
		if err := ValidateConfig(config); err != nil {
			return err
		}
		rawConfig, err := MarshalJSON(config)
		if err != nil {
			return err
		}

		if generatedOnly {
			project, err := GeneratedJSON(config)
			if err != nil {
				return err
			}
			saved, err := snapshotFiles([]string{sourcePath})
			if err != nil {
				return err
			}
			defer func() {
				if transactionErr != nil {
					if rollbackErr := restoreFiles(saved); rollbackErr != nil {
						transactionErr = fmt.Errorf("%v; configuration rollback failed: %w", transactionErr, rollbackErr)
					}
				}
			}()
			if err := atomicWriteMode(sourcePath, project, 0o644); err != nil {
				return err
			}
			if err := configMutationCheckpoint("generated-only-written"); err != nil {
				return err
			}
			loaded, _, err := LoadConfig(sourcePath)
			if err != nil || !equalProjectConfig(loaded, config) {
				return fmt.Errorf("generated-only configuration postcondition failed: %v", err)
			}
			result.Changed = true
			return nil
		}

		projections := []configMutationProjection{}
		sources := []configSourceWrite{}
		if topology.Mode == string(SupervisionEmbedded) || topology.Authority == ConfigAuthorityRepository {
			if topology.Mode == string(SupervisionEmbedded) || topology.RepositoryPackagePresent {
				bundle, buildErr := BuildExportBundle(topology.RepositorySourcePath, config, embeddedConfigBytes(rawConfig), "boatstack")
				if buildErr != nil {
					return buildErr
				}
				projections = append(projections, configMutationProjection{name: "repository", root: topology.RepositoryBundleRoot, files: bundle.Files})
			}
			sources = append(sources, configSourceWrite{name: "repository", path: topology.RepositorySourcePath, value: rawConfig})
		}
		if topology.Mode == string(SupervisionDetached) {
			controller, buildErr := BuildExportBundle(topology.ControllerSourcePath, config, rawConfig, "boatstack")
			if buildErr != nil {
				return buildErr
			}
			projections = append(projections, configMutationProjection{name: "controller", root: topology.ControllerBundleRoot, files: controller.Files})
			sources = append(sources, configSourceWrite{name: "controller", path: topology.ControllerSourcePath, value: rawConfig})
		}

		var binding DetachedBinding
		bindingTarget := ""
		if topology.Mode == string(SupervisionDetached) {
			stateRoot, stateErr := detachedStateRoot()
			if stateErr != nil {
				return stateErr
			}
			bindingTarget = bindingPath(stateRoot, topology.RepoID)
			binding, err = loadBinding(stateRoot, topology.RepoID)
			if err != nil {
				return err
			}
		}

		paths := []string{}
		for _, projection := range projections {
			if problems := ExportCollisions(projection.root, projection.files); len(problems) > 0 {
				return fmt.Errorf("refusing to overwrite user-owned files: %s", strings.Join(problems, ", "))
			}
			paths = append(paths, projectionTransactionPaths(projection)...)
		}
		for _, source := range sources {
			paths = append(paths, source.path)
		}
		if bindingTarget != "" {
			paths = append(paths, bindingTarget)
		}
		saved, err := snapshotFiles(paths)
		if err != nil {
			return err
		}
		defer func() {
			if transactionErr != nil {
				if rollbackErr := restoreFiles(saved); rollbackErr != nil {
					transactionErr = fmt.Errorf("%v; configuration rollback failed: %w", transactionErr, rollbackErr)
				}
				invalidateWorkspaceCache()
			}
		}()

		for _, projection := range projections {
			if err := WriteExport(projection.root, projection.files); err != nil {
				return err
			}
			if err := configMutationCheckpoint(projection.name + "-projection-written"); err != nil {
				return err
			}
		}
		for _, source := range sources {
			if err := atomicWriteMode(source.path, source.value, 0o644); err != nil {
				return err
			}
			if err := configMutationCheckpoint(source.name + "-source-written"); err != nil {
				return err
			}
		}

		if bindingTarget != "" {
			binding.SchemaVersion = detachedSchemaVersion
			binding.ConfigSHA256 = SHA256Bytes(rawConfig)
			binding.ConfigAuthority = topology.Authority
			binding.CreatedByVersion = Version
			bindingRaw, err := MarshalJSON(binding)
			if err != nil {
				return err
			}
			// The binding is the detached acceptance record and is always last.
			if err := atomicWrite(bindingTarget, bindingRaw); err != nil {
				return err
			}
			invalidateWorkspaceCache()
			if err := configMutationCheckpoint("binding-written"); err != nil {
				return err
			}
		}

		for _, projection := range projections {
			if err := CheckExport(projection.root, projection.files); err != nil {
				return err
			}
		}
		for _, source := range sources {
			if err := verifyConfigurationSource(source); err != nil {
				return err
			}
		}
		if topology.Mode == string(SupervisionDetached) {
			status, err := DetachedStatus(repo)
			if err != nil {
				return err
			}
			if !status.Verified || status.ConfigRelation == ConfigRelationDiverged {
				return fmt.Errorf("detached configuration postcondition failed: %s", status.Reason)
			}
		}
		result.Changed = true
		return nil
	})
	return result, returnErr
}

func equalProjectConfig(left, right ProjectConfig) bool {
	leftBytes, leftErr := MarshalJSON(left)
	rightBytes, rightErr := MarshalJSON(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}
