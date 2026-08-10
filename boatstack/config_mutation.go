package boatstack

import (
	"fmt"
	"os"
	"strings"
)

type ConfigMigrationResult struct {
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Target      string `json:"target,omitempty"`
	FromVersion int    `json:"from_version"`
	ToVersion   int    `json:"to_version"`
	Changed     bool   `json:"changed"`
}

func commitDetachedConfigBinding(topology ConfigurationTopology, raw []byte) error {
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return err
	}
	binding, err := loadBinding(stateRoot, topology.RepoID)
	if err != nil {
		return err
	}
	binding.SchemaVersion = detachedSchemaVersion
	binding.ConfigSHA256 = SHA256Bytes(raw)
	binding.ConfigAuthority = topology.Authority
	binding.CreatedByVersion = Version
	bindingRaw, err := MarshalJSON(binding)
	if err != nil {
		return err
	}
	if err := atomicWrite(bindingPath(stateRoot, topology.RepoID), bindingRaw); err != nil {
		return err
	}
	invalidateWorkspaceCache()
	return nil
}

func MigrateManagedConfiguration(repoPath, requestedTarget string, check bool) (ConfigMigrationResult, error) {
	topology, err := RequireManagedConfiguration(repoPath)
	if err != nil {
		return ConfigMigrationResult{Status: "FAIL", Message: err.Error()}, nil
	}
	target := strings.ToLower(strings.TrimSpace(requestedTarget))
	switch topology.Shape {
	case ConfigShapeEmbeddedOnly:
		if target == "" {
			target = "repository"
		}
	case ConfigShapeDetachedOnly:
		if target == "" {
			target = "controller"
			if topology.Authority == ConfigAuthorityRepository {
				target = "repository"
			}
		}
	case ConfigShapeHybrid:
		if target == "" {
			return ConfigMigrationResult{Status: "FAIL", Message: "migrate-config requires --target repository or --target controller for a hybrid installation"}, nil
		}
	}
	if target != "repository" && target != "controller" {
		return ConfigMigrationResult{Status: "FAIL", Message: "migrate-config --target must be repository or controller"}, nil
	}
	if topology.Authority == ConfigAuthorityLegacyUnknown && topology.Mode == string(SupervisionDetached) {
		return ConfigMigrationResult{Status: "FAIL", Message: "CONFIG_REBIND_REQUIRED: migration needs an explicit detached configuration authority"}, nil
	}
	if target == "repository" && topology.Mode == string(SupervisionDetached) && topology.Authority != ConfigAuthorityRepository {
		return ConfigMigrationResult{Status: "FAIL", Message: "CONFIG_REBIND_REQUIRED: repository migration would cross detached configuration authority"}, nil
	}
	if target == "controller" && topology.Shape == ConfigShapeHybrid && topology.Authority == ConfigAuthorityRepository {
		return ConfigMigrationResult{Status: "FAIL", Message: "CONFIG_REBIND_REQUIRED: controller-only migration would split repository authority"}, nil
	}

	sourcePath := topology.RepositorySourcePath
	if target == "controller" {
		sourcePath = topology.ControllerSourcePath
	}
	raw, err := osReadFile(sourcePath)
	if err != nil {
		return ConfigMigrationResult{Status: "FAIL", Message: fmt.Sprintf("failed to read config: %v", err), Target: target}, nil
	}
	upgraded, fromVersion, toVersion, changed, err := MigrateConfigBytes(raw)
	if err != nil {
		return ConfigMigrationResult{Status: "FAIL", Message: fmt.Sprintf("migration failed: %v", err), Target: target}, nil
	}
	result := ConfigMigrationResult{Status: "PASS", Target: target, FromVersion: fromVersion, ToVersion: toVersion, Changed: changed}
	if check || !changed {
		return result, nil
	}
	config, err := configFromBytes(sourcePath, upgraded)
	if err != nil {
		return ConfigMigrationResult{Status: "FAIL", Message: err.Error(), Target: target}, nil
	}
	if target == "repository" {
		repositoryBundle, buildErr := BuildExportBundle(topology.RepositorySourcePath, config, embeddedConfigBytes(upgraded), "boatstack")
		if buildErr != nil {
			return ConfigMigrationResult{}, buildErr
		}
		if topology.RepositoryPackagePresent {
			if err := WriteExport(topology.RepositoryBundleRoot, repositoryBundle.Files); err != nil {
				return ConfigMigrationResult{}, err
			}
		}
		if err := atomicWriteMode(topology.RepositorySourcePath, upgraded, 0o644); err != nil {
			return ConfigMigrationResult{}, err
		}
		if topology.Mode == string(SupervisionDetached) {
			controllerBundle, buildErr := BuildExportBundle(topology.ControllerSourcePath, config, upgraded, "boatstack")
			if buildErr != nil {
				return ConfigMigrationResult{}, buildErr
			}
			if err := WriteExport(topology.ControllerBundleRoot, controllerBundle.Files); err != nil {
				return ConfigMigrationResult{}, err
			}
			if err := atomicWriteMode(topology.ControllerSourcePath, upgraded, 0o644); err != nil {
				return ConfigMigrationResult{}, err
			}
		}
	} else {
		controllerBundle, buildErr := BuildExportBundle(topology.ControllerSourcePath, config, upgraded, "boatstack")
		if buildErr != nil {
			return ConfigMigrationResult{}, buildErr
		}
		if err := WriteExport(topology.ControllerBundleRoot, controllerBundle.Files); err != nil {
			return ConfigMigrationResult{}, err
		}
		if err := atomicWriteMode(topology.ControllerSourcePath, upgraded, 0o644); err != nil {
			return ConfigMigrationResult{}, err
		}
	}
	if topology.Mode == string(SupervisionDetached) {
		if err := commitDetachedConfigBinding(topology, upgraded); err != nil {
			return ConfigMigrationResult{}, err
		}
	}
	return result, nil
}

// Small indirections keep migration tests able to exercise read failures without
// making the public mutation API stateful.
var osReadFile = os.ReadFile

func configFromBytes(path string, raw []byte) (ProjectConfig, error) {
	var config ProjectConfig
	if err := DecodeJSON("load migrated project configuration", path, raw, &config); err != nil {
		return ProjectConfig{}, err
	}
	if err := ValidateConfig(config); err != nil {
		return ProjectConfig{}, err
	}
	return config, nil
}
