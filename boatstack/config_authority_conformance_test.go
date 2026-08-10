package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func writeRepositoryConfig(t *testing.T, repo, name string) []byte {
	t.Helper()
	raw := []byte(`{"schema_version":1,"project":{"name":"` + name + `","commands":{"test":"go test ./..."}}}` + "\n")
	if err := os.WriteFile(filepath.Join(repo, sourceConfigName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return raw
}

// Positive, relation, and failure-state conformance for:
// control-law: detached-config-divergence-never-controls-ordinary-tools.
func TestRepositoryDivergenceRequiresOnlyExplicitRebind(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/config-authority.git")
	writeRepositoryConfig(t, repo, "configuration-a")
	attached, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || attached.VerificationStatus != "VERIFIED" || attached.ConfigAuthority != ConfigAuthorityRepository {
		t.Fatalf("attach: %+v %v", attached, err)
	}
	rawB := writeRepositoryConfig(t, repo, "configuration-b")

	topology, err := ResolveConfigurationTopology(repo)
	if err != nil || topology.Relation != ConfigRelationDiverged || topology.Authority != ConfigAuthorityRepository {
		t.Fatalf("topology did not expose repository divergence: %+v %v", topology, err)
	}
	if output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: repo, Input: []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`)}); denied {
		t.Fatalf("ordinary inspection was blocked by configuration divergence: %s", output)
	}
	if _, err := RequireManagedConfiguration(repo); err == nil || !strings.Contains(err.Error(), "CONFIG_REBIND_REQUIRED") {
		t.Fatalf("explicit managed operation did not require rebind: %v", err)
	}

	preview, err := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository})
	if err != nil || preview.VerificationStatus != "VERIFIED" || preview.Applied || preview.Fingerprint == "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if !strings.Contains(preview.NextOperation, WorkspaceFor(repo).HelperPath()) {
		t.Fatalf("preview did not prescribe the workspace-bound helper: %s", preview.NextOperation)
	}
	if current, _ := os.ReadFile(WorkspaceFor(repo).SourceConfigPath()); string(current) == string(rawB) {
		t.Fatal("read-only preview changed controller configuration")
	}
	applied, err := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository, Apply: true, ExpectedFingerprint: preview.Fingerprint})
	if err != nil || applied.VerificationStatus != "VERIFIED" || !applied.Applied {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	status, err := DetachedStatus(repo)
	if err != nil || !status.Verified || status.ConfigRelation != ConfigRelationMatch || status.ConfigAuthority != ConfigAuthorityRepository || status.ControllerConfigSHA256 != SHA256Bytes(rawB) {
		t.Fatalf("rebound status: %+v %v", status, err)
	}
}

// Negative and bypass conformance for:
// control-law: config-rebind-apply-requires-current-preview.
func TestConfigRebindRejectsStaleFingerprintAndUnsafeSources(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/config-rebind-negative.git")
	writeRepositoryConfig(t, repo, "configuration-a")
	if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	writeRepositoryConfig(t, repo, "configuration-b")
	preview, _ := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository})
	writeRepositoryConfig(t, repo, "configuration-c")
	stale, err := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository, Apply: true, ExpectedFingerprint: preview.Fingerprint})
	if err != nil || stale.VerificationStatus != "BLOCKED" || stale.Applied {
		t.Fatalf("stale fingerprint accepted: %+v %v", stale, err)
	}
	inside := filepath.Join(repo, "inside.json")
	if err := os.WriteFile(inside, []byte(`{"schema_version":1,"project":{"name":"inside","commands":{"test":"go test ./..."}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	unsafe, err := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceFile, ConfigPath: inside})
	if err != nil || unsafe.VerificationStatus != "BLOCKED" || !strings.Contains(unsafe.Reason, "outside the repository") {
		t.Fatalf("unsafe file source accepted: %+v %v", unsafe, err)
	}
}

// Relation conformance for:
// control-law: external-snapshot-authority-is-independent-of-repository-source.
func TestExternalSnapshotIgnoresRepositoryConfigurationChanges(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/external-authority.git")
	external, _ := externalConfigFixture(t, "external", "go test ./...")
	result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: external})
	if err != nil || result.VerificationStatus != "VERIFIED" || result.ConfigAuthority != ConfigAuthorityExternalSnapshot {
		t.Fatalf("attach: %+v %v", result, err)
	}
	writeRepositoryConfig(t, repo, "repository-change")
	topology, err := RequireManagedConfiguration(repo)
	if err != nil || topology.Relation != ConfigRelationIndependent {
		t.Fatalf("repository change controlled external snapshot: %+v %v", topology, err)
	}
}

// Positive conformance for:
// control-law: detached-only-update-performs-zero-plant-writes.
func TestDetachedOnlyUpdateLeavesRepositoryBytesUnchanged(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/detached-update.git")
	external, _ := externalConfigFixture(t, "detached-only", "go test ./...")
	if result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: external}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	before := filesystemSnapshot(t, repo)
	if err := RunUpdate(InitOptions{Repo: repo, Update: true, Yes: true}); err != nil {
		t.Fatalf("detached-only update: %v", err)
	}
	if after := filesystemSnapshot(t, repo); after != before {
		t.Fatal("detached-only update changed repository or Git bytes")
	}
}

// Relation conformance for:
// control-law: configuration-writers-preserve-the-declared-authority.
func TestCapabilityRegistrationCannotInvalidateItsRepositoryBinding(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/capability-authority.git")
	writeRepositoryConfig(t, repo, "capability-authority")
	if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	registered, err := RegisterCapabilityCommand(repo, "visual", "settings", "npm run capture:settings")
	if err != nil || registered.Alias != "visual:settings" {
		t.Fatalf("register: %+v %v", registered, err)
	}
	status, err := DetachedStatus(repo)
	if err != nil || !status.Verified || status.ConfigRelation != ConfigRelationMatch {
		t.Fatalf("capability registration invalidated binding: %+v %v", status, err)
	}
	repository, _, err := LoadConfig(filepath.Join(repo, sourceConfigName))
	if err != nil || repository.Project.Commands["visual:settings"] != "npm run capture:settings" {
		t.Fatalf("repository authority was not updated: %+v %v", repository, err)
	}
}

// Failure-state conformance for:
// control-law: successful-config-mutations-preserve-detached-verification.
func TestIgnoreDeliveryCannotInvalidateItsRepositoryBinding(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/ignore-authority.git")
	writeRepositoryConfig(t, repo, "ignore-authority")
	if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}

	for _, feature := range []string{"old-one", "old-two"} {
		added, err := IgnoreDelivery(repo, feature)
		if err != nil || !added {
			t.Fatalf("ignore %s: added=%v err=%v", feature, added, err)
		}
		status, statusErr := DetachedStatus(repo)
		if statusErr != nil || !status.Verified || status.ConfigRelation != ConfigRelationMatch {
			t.Fatalf("ignore %s invalidated detached binding: %+v %v", feature, status, statusErr)
		}
	}

	repository, _, err := LoadConfig(filepath.Join(repo, sourceConfigName))
	if err != nil || !reflect.DeepEqual(repository.Workflow.IgnoredDeliveries, []string{"old-one", "old-two"}) {
		t.Fatalf("repository authority was not updated: %+v %v", repository.Workflow.IgnoredDeliveries, err)
	}
}

// Positive and relation conformance for:
// control-law: successful-config-mutations-preserve-declared-authority.
func TestConfigurationMutationPreservesEveryAuthorityMode(t *testing.T) {
	t.Run("embedded-source", func(t *testing.T) {
		repo := detachedTestRepo(t, "https://github.com/acme/embedded-source.git")
		raw := writeRepositoryConfig(t, repo, "embedded-source")
		config, _, err := LoadConfig(filepath.Join(repo, sourceConfigName))
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := BuildExportBundle(filepath.Join(repo, sourceConfigName), config, embeddedConfigBytes(raw), "boatstack")
		if err != nil || WriteExport(repo, bundle.Files) != nil {
			t.Fatalf("embedded fixture: %v", err)
		}
		if added, err := IgnoreDelivery(repo, "old-embedded"); err != nil || !added {
			t.Fatalf("ignore: added=%v err=%v", added, err)
		}
		for _, path := range []string{filepath.Join(repo, sourceConfigName), filepath.Join(repo, productLoopDirName, "project.json")} {
			loaded, _, err := LoadConfig(path)
			if err != nil || !reflect.DeepEqual(loaded.Workflow.IgnoredDeliveries, []string{"old-embedded"}) {
				t.Fatalf("%s did not receive mutation: %+v %v", path, loaded.Workflow.IgnoredDeliveries, err)
			}
		}
	})

	t.Run("detached-repository-hybrid", func(t *testing.T) {
		repo := detachedTestRepo(t, "https://github.com/acme/hybrid-authority.git")
		raw := writeRepositoryConfig(t, repo, "hybrid-authority")
		config, _, err := LoadConfig(filepath.Join(repo, sourceConfigName))
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := BuildExportBundle(filepath.Join(repo, sourceConfigName), config, embeddedConfigBytes(raw), "boatstack")
		if err != nil || WriteExport(repo, bundle.Files) != nil {
			t.Fatalf("hybrid fixture: %v", err)
		}
		if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
			t.Fatalf("attach: %+v %v", result, err)
		}
		if added, err := IgnoreDelivery(repo, "old-hybrid"); err != nil || !added {
			t.Fatalf("ignore: added=%v err=%v", added, err)
		}
		status, statusErr := DetachedStatus(repo)
		if statusErr != nil || !status.Verified || status.ConfigRelation != ConfigRelationMatch {
			t.Fatalf("hybrid mutation invalidated binding: %+v %v", status, statusErr)
		}
		repository, _, repositoryErr := LoadConfig(filepath.Join(repo, productLoopDirName, "project.json"))
		controller, _, controllerErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
		if repositoryErr != nil || controllerErr != nil {
			t.Fatalf("load hybrid projections: repository=%v controller=%v", repositoryErr, controllerErr)
		}
		if !reflect.DeepEqual(repository.Workflow.IgnoredDeliveries, []string{"old-hybrid"}) ||
			!reflect.DeepEqual(controller.Workflow.IgnoredDeliveries, []string{"old-hybrid"}) {
			t.Fatalf("hybrid projections diverged: repo=%v controller=%v", repository.Workflow.IgnoredDeliveries, controller.Workflow.IgnoredDeliveries)
		}
	})

	for _, test := range []struct {
		name       string
		authority  string
		configPath func(*testing.T) string
	}{
		{name: "detached-external", authority: ConfigAuthorityExternalSnapshot, configPath: func(t *testing.T) string {
			path, _ := externalConfigFixture(t, "external-ignore", "go test ./...")
			return path
		}},
		{name: "detached-synthesized", authority: ConfigAuthoritySynthesized},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := detachedTestRepo(t, "https://github.com/acme/"+test.name+".git")
			options := AttachOptions{Repo: repo}
			if test.configPath != nil {
				options.ConfigPath = test.configPath(t)
			}
			before := filesystemSnapshot(t, repo)
			attached, err := AttachDetached(options)
			if err != nil || attached.VerificationStatus != "VERIFIED" || attached.ConfigAuthority != test.authority {
				t.Fatalf("attach: %+v %v", attached, err)
			}
			if added, err := IgnoreDelivery(repo, "old-local"); err != nil || !added {
				t.Fatalf("ignore: added=%v err=%v", added, err)
			}
			if after := filesystemSnapshot(t, repo); after != before {
				t.Fatal("controller-authority mutation changed repository bytes")
			}
			status, statusErr := DetachedStatus(repo)
			if statusErr != nil || !status.Verified || status.ConfigAuthority != test.authority || status.ConfigRelation == ConfigRelationDiverged {
				t.Fatalf("controller-authority mutation invalidated binding: %+v %v", status, statusErr)
			}
			controller, _, err := LoadConfig(WorkspaceFor(repo).SourceConfigPath())
			if err != nil || !reflect.DeepEqual(controller.Workflow.IgnoredDeliveries, []string{"old-local"}) {
				t.Fatalf("controller source was not updated: %+v %v", controller.Workflow.IgnoredDeliveries, err)
			}
		})
	}
}

// Failure-state conformance for:
// control-law: failed-config-mutations-restore-every-projection.
func TestConfigurationMutationRollsBackEveryDetachedCheckpoint(t *testing.T) {
	for _, checkpoint := range []string{
		"controller-projection-written",
		"repository-source-written",
		"controller-source-written",
		"binding-written",
	} {
		t.Run(checkpoint, func(t *testing.T) {
			repo := detachedTestRepo(t, "https://github.com/acme/rollback-"+checkpoint+".git")
			writeRepositoryConfig(t, repo, "rollback")
			attached, err := AttachDetached(AttachOptions{Repo: repo})
			if err != nil || attached.VerificationStatus != "VERIFIED" {
				t.Fatalf("attach: %+v %v", attached, err)
			}
			ctx := WorkspaceFor(repo)
			beforeRepo := filesystemSnapshot(t, repo)
			beforeController := filesystemSnapshot(t, ctx.controlRoot)
			previous := configMutationCheckpoint
			configMutationCheckpoint = func(stage string) error {
				if stage == checkpoint {
					return fmt.Errorf("simulated interruption at %s", stage)
				}
				return nil
			}
			t.Cleanup(func() { configMutationCheckpoint = previous })

			if added, err := IgnoreDelivery(repo, "must-rollback"); err == nil || added {
				t.Fatalf("checkpoint did not fail: added=%v err=%v", added, err)
			}
			if after := filesystemSnapshot(t, repo); after != beforeRepo {
				t.Fatalf("repository bytes changed after rollback at %s", checkpoint)
			}
			if after := filesystemSnapshot(t, ctx.controlRoot); after != beforeController {
				t.Fatalf("controller bytes changed after rollback at %s", checkpoint)
			}
			status, statusErr := DetachedStatus(repo)
			if statusErr != nil || !status.Verified || status.ConfigRelation != ConfigRelationMatch {
				t.Fatalf("rollback left controller invalid: %+v %v", status, statusErr)
			}
		})
	}
}

// Correlation and relation conformance for:
// control-law: concurrent-config-mutations-cannot-lose-accepted-updates.
func TestConfigurationMutationSerializesConcurrentWriters(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/concurrent-config.git")
	writeRepositoryConfig(t, repo, "concurrent-config")
	if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}

	features := []string{"old-alpha", "old-beta"}
	errors := make(chan error, len(features))
	var wait sync.WaitGroup
	for _, feature := range features {
		feature := feature
		wait.Add(1)
		go func() {
			defer wait.Done()
			added, err := IgnoreDelivery(repo, feature)
			if err != nil {
				errors <- err
				return
			}
			if !added {
				errors <- fmt.Errorf("%s was not added", feature)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	config, _, err := LoadConfig(filepath.Join(repo, sourceConfigName))
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), config.Workflow.IgnoredDeliveries...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, features) {
		t.Fatalf("concurrent update lost a slug: %v", got)
	}
	status, statusErr := DetachedStatus(repo)
	if statusErr != nil || !status.Verified {
		t.Fatalf("concurrent update invalidated binding: %+v %v", status, statusErr)
	}
}

// Negative and failure-state conformance for:
// control-law: ambiguous-config-authority-is-refused-before-writes.
func TestConfigurationMutationRefusesLegacyAuthorityBeforeWrites(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/legacy-config-writer.git")
	writeRepositoryConfig(t, repo, "legacy-config-writer")
	attached, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || attached.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", attached, err)
	}
	stateRoot, _ := detachedStateRoot()
	binding, err := loadBinding(stateRoot, attached.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	binding.SchemaVersion = detachedSchemaVersionWithConfigDigest
	binding.ConfigAuthority = ""
	raw, _ := MarshalJSON(binding)
	if err := atomicWrite(bindingPath(stateRoot, attached.RepoID), raw); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	beforeRepo := filesystemSnapshot(t, repo)
	beforeController := filesystemSnapshot(t, WorkspaceFor(repo).controlRoot)
	if added, err := IgnoreDelivery(repo, "old-legacy"); err == nil || added || !strings.Contains(err.Error(), "CONFIG_REBIND_REQUIRED") {
		t.Fatalf("legacy authority was not refused: added=%v err=%v", added, err)
	}
	if filesystemSnapshot(t, repo) != beforeRepo || filesystemSnapshot(t, WorkspaceFor(repo).controlRoot) != beforeController {
		t.Fatal("legacy refusal changed configuration bytes")
	}
}

// Negative and bypass conformance for:
// control-law: generic-writers-never-cross-configuration-authority.
func TestMigrationAndExportCannotCrossOrGuessAuthority(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/config-writer-boundary.git")
	external, _ := externalConfigFixture(t, "external", "go test ./...")
	if result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: external}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	writeRepositoryConfig(t, repo, "repository")
	if err := os.MkdirAll(filepath.Join(repo, productLoopDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, productLoopDirName, "generated.lock.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := MigrateManagedConfiguration(repo, "", true)
	if err != nil || report.Status != "FAIL" || !strings.Contains(report.Message, "requires --target") {
		t.Fatalf("hybrid migration guessed a target: %+v %v", report, err)
	}
	if err := ValidateConfigurationExport(repo, filepath.Join(repo, sourceConfigName), true); err == nil || !strings.Contains(err.Error(), "cannot cross") {
		t.Fatalf("export crossed external authority: %v", err)
	}
}

// Failure-state conformance for:
// control-law: interrupted-rebind-never-accepts-a-mixed-projection.
func TestInterruptedConfigRebindRestoresAcceptedState(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/config-rebind-interrupted.git")
	rawA := writeRepositoryConfig(t, repo, "configuration-a")
	if result, err := AttachDetached(AttachOptions{Repo: repo}); err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	writeRepositoryConfig(t, repo, "configuration-b")
	preview, _ := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository})
	previousCheckpoint := configRebindCheckpoint
	configRebindCheckpoint = func(stage string) error { return fmt.Errorf("simulated interruption at %s", stage) }
	defer func() { configRebindCheckpoint = previousCheckpoint }()
	if _, err := ConfigRebind(ConfigRebindOptions{Repo: repo, Source: ConfigRebindSourceRepository, Apply: true, ExpectedFingerprint: preview.Fingerprint}); err == nil {
		t.Fatal("simulated interruption unexpectedly succeeded")
	}
	controller, err := os.ReadFile(WorkspaceFor(repo).SourceConfigPath())
	if err != nil || string(controller) != string(rawA) {
		t.Fatalf("interrupted rebind left mixed controller source: %q %v", controller, err)
	}
	status, err := DetachedStatus(repo)
	if err != nil || !status.Verified || status.ControllerConfigSHA256 != SHA256Bytes(rawA) {
		t.Fatalf("interrupted rebind changed accepted binding: %+v %v", status, err)
	}
}

// Compatibility conformance for schema-v2 attachments.
func TestMatchingSchemaV2AttachmentContinuesWithoutMigration(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/config-schema-v2.git")
	writeRepositoryConfig(t, repo, "legacy-matching")
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	stateRoot, _ := detachedStateRoot()
	binding, err := loadBinding(stateRoot, result.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	binding.SchemaVersion = detachedSchemaVersionWithConfigDigest
	binding.ConfigAuthority = ""
	raw, _ := MarshalJSON(binding)
	if err := atomicWrite(bindingPath(stateRoot, result.RepoID), raw); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	status, err := DetachedStatus(repo)
	if err != nil || !status.Verified || status.ConfigAuthority != ConfigAuthorityLegacyUnknown || status.ConfigRelation != ConfigRelationMatch {
		t.Fatalf("matching schema-v2 attachment did not continue: %+v %v", status, err)
	}
}
