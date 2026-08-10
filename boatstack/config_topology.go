package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ConfigShapeEmbeddedOnly = "EMBEDDED_ONLY"
	ConfigShapeDetachedOnly = "DETACHED_ONLY"
	ConfigShapeHybrid       = "HYBRID"
)

// ConfigurationTopology is the read-only authority map for configuration.
// The repository package and detached controller are separate projections;
// neither source is selected merely because its path happened to be resolved
// first.
type ConfigurationTopology struct {
	SchemaVersion            int      `json:"schema_version"`
	Mode                     string   `json:"mode"`
	Shape                    string   `json:"shape"`
	Authority                string   `json:"authority"`
	Relation                 string   `json:"relation"`
	RepoRoot                 string   `json:"repo_root"`
	RepoID                   string   `json:"repo_id,omitempty"`
	WorktreeID               string   `json:"worktree_id,omitempty"`
	BindingPath              string   `json:"binding_path,omitempty"`
	RepositorySourcePath     string   `json:"repository_source_path,omitempty"`
	RepositoryBundleRoot     string   `json:"repository_bundle_root,omitempty"`
	ControllerSourcePath     string   `json:"controller_source_path,omitempty"`
	ControllerBundleRoot     string   `json:"controller_bundle_root,omitempty"`
	RepositoryConfigSHA256   string   `json:"repository_config_sha256,omitempty"`
	ControllerConfigSHA256   string   `json:"controller_config_sha256,omitempty"`
	BindingConfigSHA256      string   `json:"binding_config_sha256,omitempty"`
	RepositoryPackagePresent bool     `json:"repository_package_present"`
	AffectedWorktrees        []string `json:"affected_worktrees,omitempty"`
	NextOperation            string   `json:"next_operation,omitempty"`
}

func repositorySourceConfigPath(repo string) string {
	return filepath.Join(repo, sourceConfigName)
}

func repositoryPackagePresent(repo string) bool {
	return fileExists(filepath.Join(repo, productLoopDirName, "generated.lock.json"))
}

func fileSHAIfRegular(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("configuration source must be a regular non-symlink file: %s", path)
	}
	return SHA256File(path)
}

func detachedAliases(stateRoot, repoID string) ([]string, error) {
	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return nil, err
	}
	aliases := []string{}
	for path, registeredID := range registry.Repositories {
		if registeredID == repoID {
			aliases = append(aliases, path)
		}
	}
	sort.Strings(aliases)
	return aliases, nil
}

func ResolveConfigurationTopology(repoPath string) (ConfigurationTopology, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return ConfigurationTopology{}, err
	}
	repositorySource := repositorySourceConfigPath(repo)
	repositorySHA, err := fileSHAIfRegular(repositorySource)
	if err != nil {
		return ConfigurationTopology{}, err
	}
	packagePresent := repositoryPackagePresent(repo)
	ctx, attached, detachedErr := detachedContextFor(repo)
	if detachedErr != nil {
		return ConfigurationTopology{}, detachedErr
	}
	if !attached {
		relation := ConfigRelationRepositoryAbsent
		if repositorySHA != "" {
			relation = ConfigRelationMatch
		}
		return ConfigurationTopology{
			SchemaVersion: detachedSchemaVersion, Mode: string(SupervisionEmbedded), Shape: ConfigShapeEmbeddedOnly,
			Authority: ConfigAuthorityRepository, Relation: relation, RepoRoot: repo,
			RepositorySourcePath: repositorySource, RepositoryBundleRoot: repo,
			RepositoryConfigSHA256: repositorySHA, RepositoryPackagePresent: packagePresent,
		}, nil
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return ConfigurationTopology{}, err
	}
	binding, err := loadBinding(stateRoot, ctx.RepoID)
	if err != nil {
		return ConfigurationTopology{}, err
	}
	authority := normalizedConfigAuthority(binding)
	relation := ConfigRelationMatch
	switch {
	case repositorySHA == "":
		relation = ConfigRelationRepositoryAbsent
	case authority == ConfigAuthorityExternalSnapshot || authority == ConfigAuthoritySynthesized:
		relation = ConfigRelationIndependent
	case repositorySHA != binding.ConfigSHA256:
		relation = ConfigRelationDiverged
	}
	aliases, err := detachedAliases(stateRoot, ctx.RepoID)
	if err != nil {
		return ConfigurationTopology{}, err
	}
	shape := ConfigShapeDetachedOnly
	if packagePresent {
		shape = ConfigShapeHybrid
	}
	next := ""
	if relation == ConfigRelationDiverged && (authority == ConfigAuthorityRepository || authority == ConfigAuthorityLegacyUnknown) {
		next = PrescribedCommand{Program: ctx.HelperPath(), Verb: "config-rebind", Args: []string{"--repo", repo, "--source", "repository", "--json"}}.CommandLine()
	}
	return ConfigurationTopology{
		SchemaVersion: detachedSchemaVersion, Mode: string(SupervisionDetached), Shape: shape,
		Authority: authority, Relation: relation, RepoRoot: repo, RepoID: ctx.RepoID,
		WorktreeID: ctx.WorktreeID, BindingPath: bindingPath(stateRoot, ctx.RepoID),
		RepositorySourcePath: repositorySource, RepositoryBundleRoot: repo,
		ControllerSourcePath: ctx.SourceConfigPath(), ControllerBundleRoot: ctx.ExportRoot(),
		RepositoryConfigSHA256: repositorySHA, ControllerConfigSHA256: binding.ConfigSHA256,
		BindingConfigSHA256: binding.ConfigSHA256, RepositoryPackagePresent: packagePresent,
		AffectedWorktrees: aliases, NextOperation: next,
	}, nil
}

// RequireManagedConfiguration is called only after explicit Boatstack
// invocation. Ambient safety uses the verified controller directly and never
// turns repository/source divergence into ordinary-work interference.
func RequireManagedConfiguration(repo string) (ConfigurationTopology, error) {
	topology, err := ResolveConfigurationTopology(repo)
	if err != nil {
		return ConfigurationTopology{}, err
	}
	if topology.Relation == ConfigRelationDiverged &&
		(topology.Authority == ConfigAuthorityRepository || topology.Authority == ConfigAuthorityLegacyUnknown) {
		return topology, fmt.Errorf("CONFIG_REBIND_REQUIRED: repository configuration %s differs from detached controller %s; preview the explicit repair with %s", topology.RepositoryConfigSHA256, topology.ControllerConfigSHA256, strings.TrimSpace(topology.NextOperation))
	}
	return topology, nil
}

// ValidateConfigurationExport prevents the generic exporter from becoming an
// untracked cross-authority writer. Read-only dry runs remain available.
func ValidateConfigurationExport(repoPath, configPath string, write bool) error {
	if !write {
		return nil
	}
	// Distribution export into a fresh staging directory has no repository
	// authority to cross. The guard applies only once the destination resolves to
	// a live Git repository.
	if _, err := ResolveRepository(repoPath); err != nil {
		return nil
	}
	topology, err := RequireManagedConfiguration(repoPath)
	if err != nil {
		return err
	}
	absoluteConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	absoluteConfig = filepath.Clean(absoluteConfig)
	if topology.Mode == string(SupervisionEmbedded) {
		if absoluteConfig != filepath.Clean(topology.RepositorySourcePath) {
			return fmt.Errorf("export --write requires the repository configuration source %s", topology.RepositorySourcePath)
		}
		return nil
	}
	if topology.Authority != ConfigAuthorityRepository || absoluteConfig != filepath.Clean(topology.RepositorySourcePath) {
		return fmt.Errorf("CONFIG_REBIND_REQUIRED: export --write cannot cross detached configuration authority; use config-rebind or write the declared controller projection")
	}
	return nil
}
