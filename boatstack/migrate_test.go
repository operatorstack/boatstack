package boatstack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrateConfigBytes_SyntheticChain(t *testing.T) {
	// Backup and restore
	oldMigrations := configMigrations
	oldOverride := currentConfigSchemaVersionOverride
	defer func() {
		configMigrations = oldMigrations
		currentConfigSchemaVersionOverride = oldOverride
	}()

	// Simulate current schema version is 3
	currentConfigSchemaVersionOverride = 3

	// Register synthetic v1->v2 and v2->v3 migrations
	configMigrations = []configMigration{
		{
			from: 1,
			to:   2,
			apply: func(data map[string]any) (map[string]any, error) {
				data["v1_to_v2_applied"] = true
				return data, nil
			},
		},
		{
			from: 2,
			to:   3,
			apply: func(data map[string]any) (map[string]any, error) {
				data["v2_to_v3_applied"] = true
				return data, nil
			},
		},
	}

	raw := []byte(`{
		"schema_version": 1,
		"project": {
			"name": "test-project"
		}
	}`)

	upgraded, from, to, changed, err := MigrateConfigBytes(raw)
	if err != nil {
		t.Fatalf("MigrateConfigBytes failed: %v", err)
	}

	if !changed {
		t.Error("expected config to be changed")
	}
	if from != 1 {
		t.Errorf("expected from version 1, got %d", from)
	}
	if to != 3 {
		t.Errorf("expected to version 3, got %d", to)
	}

	var parsed map[string]any
	if err := json.Unmarshal(upgraded, &parsed); err != nil {
		t.Fatalf("failed to unmarshal upgraded config: %v", err)
	}

	if parsed["v1_to_v2_applied"] != true {
		t.Error("expected v1->v2 migration to be applied")
	}
	if parsed["v2_to_v3_applied"] != true {
		t.Error("expected v2->v3 migration to be applied")
	}
	if int(parsed["schema_version"].(float64)) != 3 {
		t.Errorf("expected upgraded schema_version to be 3, got %v", parsed["schema_version"])
	}
}

func TestMigrateConfigBytes_NoOp(t *testing.T) {
	oldOverride := currentConfigSchemaVersionOverride
	defer func() { currentConfigSchemaVersionOverride = oldOverride }()

	currentConfigSchemaVersionOverride = 1

	raw := []byte(`{
		"schema_version": 1,
		"project": {
			"name": "test-project"
		}
	}`)

	upgraded, from, to, changed, err := MigrateConfigBytes(raw)
	if err != nil {
		t.Fatalf("MigrateConfigBytes failed: %v", err)
	}

	if changed {
		t.Error("expected config to be unchanged (no-op)")
	}
	if from != 1 || to != 1 {
		t.Errorf("expected from=1 and to=1, got from=%d, to=%d", from, to)
	}
	if !bytes.Equal(raw, upgraded) {
		t.Error("expected upgraded bytes to match raw bytes exactly")
	}
}

func TestMigrateConfigBytes_GapDetection(t *testing.T) {
	oldMigrations := configMigrations
	oldOverride := currentConfigSchemaVersionOverride
	defer func() {
		configMigrations = oldMigrations
		currentConfigSchemaVersionOverride = oldOverride
	}()

	currentConfigSchemaVersionOverride = 3

	// Missing v2->v3 migration
	configMigrations = []configMigration{
		{
			from: 1,
			to:   2,
			apply: func(data map[string]any) (map[string]any, error) {
				return data, nil
			},
		},
	}

	raw := []byte(`{"schema_version": 1}`)
	_, _, _, _, err := MigrateConfigBytes(raw)
	if err == nil {
		t.Fatal("expected error due to missing migration (gap), got nil")
	}
	if !strings.Contains(err.Error(), "no migration found from version 2 to 3") {
		t.Errorf("expected gap error message, got: %v", err)
	}
}

func TestMigrateConfigBytes_RejectNewer(t *testing.T) {
	oldOverride := currentConfigSchemaVersionOverride
	defer func() { currentConfigSchemaVersionOverride = oldOverride }()

	currentConfigSchemaVersionOverride = 1

	raw := []byte(`{"schema_version": 2}`)
	_, _, _, _, err := MigrateConfigBytes(raw)
	if err == nil {
		t.Fatal("expected error rejecting newer version, got nil")
	}
	if !strings.Contains(err.Error(), "config was written by a newer Boatstack; update Boatstack") {
		t.Errorf("expected reject newer error message, got: %v", err)
	}
}

func TestValidateConfig_AcceptanceTable(t *testing.T) {
	oldOverride := currentConfigSchemaVersionOverride
	defer func() { currentConfigSchemaVersionOverride = oldOverride }()

	currentConfigSchemaVersionOverride = 2

	tests := []struct {
		name          string
		schemaVersion int
		wantErr       string
	}{
		{
			name:          "current version passes",
			schemaVersion: 2,
			wantErr:       "",
		},
		{
			name:          "older version is behind",
			schemaVersion: 1,
			wantErr:       "config schema is behind; run /boatstack-update",
		},
		{
			name:          "newer version is ahead",
			schemaVersion: 3,
			wantErr:       "config was written by a newer Boatstack; update Boatstack",
		},
		{
			name:          "invalid version < 1",
			schemaVersion: 0,
			wantErr:       "project config schema_version must be >= 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ProjectConfig{
				SchemaVersion: tt.schemaVersion,
				Project: Project{
					Name: "test-project",
					Commands: map[string]string{
						"test": "go test ./...",
					},
				},
			}
			err := ValidateConfig(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestDoctor_SchemaBehindAndAhead(t *testing.T) {
	oldOverride := currentConfigSchemaVersionOverride
	defer func() { currentConfigSchemaVersionOverride = oldOverride }()

	currentConfigSchemaVersionOverride = 2

	errBehind := fmt.Errorf("config schema is behind; run /boatstack-update")
	errAhead := fmt.Errorf("config was written by a newer Boatstack; update Boatstack")

	hintBehind := DoctorRepairHint(errBehind)
	hintAhead := DoctorRepairHint(errAhead)

	if !strings.Contains(hintBehind.Error(), "remediation: run /boatstack-update to migrate project configuration") {
		t.Errorf("expected behind hint, got: %v", hintBehind)
	}
	if !strings.Contains(hintAhead.Error(), "remediation: update your Boatstack installation to load this configuration") {
		t.Errorf("expected ahead hint, got: %v", hintAhead)
	}
}

func TestValidateUpdateWorkspace_ConformanceBlock(t *testing.T) {
	oldOverride := currentConfigSchemaVersionOverride
	oldVersion := Version
	oldSourceCommit := SourceCommit
	defer func() {
		currentConfigSchemaVersionOverride = oldOverride
		Version = oldVersion
		SourceCommit = oldSourceCommit
	}()

	currentConfigSchemaVersionOverride = 1
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.5.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)

	// Create a behind config (version 1)
	currentConfigSchemaVersionOverride = 1
	config, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	currentConfigSchemaVersionOverride = 2

	// Set the current checked-out branch to the update branch
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")

	// ValidateUpdateWorkspace with config.SchemaVersion = 1, while current is overridden to 2.
	err = ValidateUpdateWorkspace(repo, config)
	if err == nil {
		t.Fatal("expected ValidateUpdateWorkspace to fail for schema behind, got nil")
	}
	if !strings.Contains(err.Error(), "config schema is behind; run /boatstack-update") {
		t.Errorf("expected error to contain behind message, got: %v", err)
	}
}
