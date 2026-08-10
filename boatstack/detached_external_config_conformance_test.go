package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func externalConfigFixture(t *testing.T, name, command string) (string, []byte) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "project.json")
	raw := []byte(`{"schema_version":1,"project":{"name":"` + name + `","commands":{"test":"` + command + `"}}}` + "\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

func filesystemSnapshot(t *testing.T, root string) string {
	t.Helper()
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		line := filepath.ToSlash(relative) + " " + info.Mode().String()
		if info.Mode().IsRegular() {
			digest, err := SHA256File(path)
			if err != nil {
				return err
			}
			line += " " + digest
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			line += " " + target
		}
		entries = append(entries, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

// control-law: detached-config-digest-gates-resume
func TestDetachedAttachAcceptsExternalConfigWithoutPlantWrites(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/external-config.git")
	configPath, raw := externalConfigFixture(t, "works-yield", "pnpm --filter @works/yield-web test")
	before := filesystemSnapshot(t, repo)

	result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: configPath})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	if after := filesystemSnapshot(t, repo); after != before {
		t.Fatal("external configuration attachment changed repository or Git bytes")
	}
	wantSHA := SHA256Bytes(raw)
	if result.SchemaVersion != detachedSchemaVersion || result.ConfigSHA256 != wantSHA {
		t.Fatalf("attach digest = %q schema=%d, want %q schema=%d", result.ConfigSHA256, result.SchemaVersion, wantSHA, detachedSchemaVersion)
	}
	ctx := WorkspaceFor(repo)
	copied, err := os.ReadFile(ctx.SourceConfigPath())
	if err != nil || string(copied) != string(raw) {
		t.Fatalf("external source copy = %q, %v", copied, err)
	}
	generated, _, err := LoadConfig(ctx.ProjectConfigPath())
	if err != nil || generated.Project.Name != "works-yield" || generated.Project.Commands["test"] != "pnpm --filter @works/yield-web test" {
		t.Fatalf("generated config did not preserve supplied values: %+v %v", generated, err)
	}
	stateRoot, _ := detachedStateRoot()
	binding, err := loadBinding(stateRoot, result.RepoID)
	if err != nil || binding.ConfigSHA256 != wantSHA || binding.SchemaVersion != detachedSchemaVersion {
		t.Fatalf("binding did not capture config digest: %+v %v", binding, err)
	}
	lockRaw, err := os.ReadFile(filepath.Join(ctx.GeneratedRoot(), "generated.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock detachedGeneratedLock
	if err := json.Unmarshal(lockRaw, &lock); err != nil || lock.ConfigSHA256 != wantSHA {
		t.Fatalf("generated lock did not capture config digest: %+v %v", lock, err)
	}
	status, _ := DetachedStatus(repo)
	if !status.Verified || status.ConfigSHA256 != wantSHA || status.SchemaVersion != detachedSchemaVersion {
		t.Fatalf("detached status did not bind config digest: %+v", status)
	}

	// Attachment is copy-based. Later changes to the input path are not live
	// configuration changes and cannot alter the bound detached snapshot.
	if err := os.WriteFile(configPath, []byte(`{"schema_version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _ = DetachedStatus(repo)
	if !status.Verified || status.ConfigSHA256 != wantSHA {
		t.Fatalf("changing original input changed detached attachment: %+v", status)
	}
}

// control-law: detached-config-input-stays-outside-plant
func TestDetachedAttachRejectsInvalidOrNonExternalConfigBeforeWrites(t *testing.T) {
	tests := []struct {
		name  string
		build func(*testing.T, string) string
		want  string
	}{
		{name: "missing", build: func(t *testing.T, _ string) string { return filepath.Join(t.TempDir(), "missing.json") }, want: "missing or unreadable"},
		{name: "malformed", build: func(t *testing.T, _ string) string {
			p := filepath.Join(t.TempDir(), "project.json")
			_ = os.WriteFile(p, []byte("{\n"), 0o644)
			return p
		}, want: "parse JSON"},
		{name: "newer schema", build: func(t *testing.T, _ string) string {
			p := filepath.Join(t.TempDir(), "project.json")
			_ = os.WriteFile(p, []byte(`{"schema_version":2,"project":{"name":"x","commands":{"test":"true"}}}`), 0o644)
			return p
		}, want: "newer Boatstack"},
		{name: "repository local", build: func(t *testing.T, repo string) string {
			p := filepath.Join(repo, "project.json")
			_ = os.WriteFile(p, []byte(`{"schema_version":1,"project":{"name":"x","commands":{"test":"true"}}}`), 0o644)
			return p
		}, want: "outside the repository"},
		{name: "git local", build: func(t *testing.T, repo string) string {
			gitDir, _ := gitCommonDir(repo)
			p := filepath.Join(gitDir, "project.json")
			_ = os.WriteFile(p, []byte(`{"schema_version":1,"project":{"name":"x","commands":{"test":"true"}}}`), 0o644)
			return p
		}, want: "outside the repository"},
		{name: "symlink", build: func(t *testing.T, _ string) string {
			real, _ := externalConfigFixture(t, "x", "true")
			link := filepath.Join(t.TempDir(), "project.json")
			_ = os.Symlink(real, link)
			return link
		}, want: "non-symlink"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := detachedTestRepo(t, "https://github.com/acme/reject-"+strings.ReplaceAll(test.name, " ", "-")+".git")
			path := test.build(t, repo)
			result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: path})
			if err != nil || result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, test.want) {
				t.Fatalf("result = %+v, err=%v, want %q", result, err, test.want)
			}
			stateRoot, _ := detachedStateRoot()
			if _, statErr := os.Stat(stateRoot); !os.IsNotExist(statErr) {
				t.Fatalf("rejected config wrote detached state: %v", statErr)
			}
		})
	}
}

func assertDetachedDriftBlocked(t *testing.T, repo, want string) {
	t.Helper()
	status, err := DetachedStatus(repo)
	if err != nil || !status.Attached || status.Verified || status.ConfigSHA256 == "" || !strings.Contains(status.Reason, want) {
		t.Fatalf("status did not report %q drift: %+v %v", want, status, err)
	}
	next, err := ResolveNext(repo, "")
	if err != nil || next.VerificationStatus != "BLOCKED" || next.NextOperation != "attach" {
		t.Fatalf("next-status did not fail closed: %+v %v", next, err)
	}
	recovery, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "fix", SourceStage: "ci"})
	if err != nil || recovery.VerificationStatus != "BLOCKED" {
		t.Fatalf("recovery-status did not fail closed: %+v %v", recovery, err)
	}
	preflight := CheckRunPreflight(repo, "")
	if preflight.VerificationStatus != "BLOCKED" || preflight.Relation != "DETACHED_CONFIG_DRIFT" {
		t.Fatalf("run-preflight did not fail closed: %+v", preflight)
	}
	if _, err := LoadDeliveryState(repo, "missing-feature"); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("delivery state bypassed detached verification: %v", err)
	}
}

// control-law: detached-config-digest-gates-resume
func TestDetachedConfigDriftBlocksResumeAndRestoresExactly(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/drift.git")
	configPath, _ := externalConfigFixture(t, "drift", "true")
	result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: configPath})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	ctx := WorkspaceFor(repo)

	sourcePath := ctx.SourceConfigPath()
	source, _ := os.ReadFile(sourcePath)
	if err := os.WriteFile(sourcePath, append(source, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	assertDetachedDriftBlocked(t, repo, "drifted from bound SHA-256")
	if WorkspaceFor(repo).ProjectConfigPath() != ctx.ProjectConfigPath() {
		t.Fatal("unverified attachment redirected controller paths into repository")
	}
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	if status, _ := DetachedStatus(repo); !status.Verified {
		t.Fatalf("exact source restoration did not recover verification: %+v", status)
	}

	projectPath := ctx.ProjectConfigPath()
	project, _ := os.ReadFile(projectPath)
	if err := os.WriteFile(projectPath, append(project, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	assertDetachedDriftBlocked(t, repo, "generated project configuration drifted")
	if err := os.WriteFile(projectPath, project, 0o644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(ctx.GeneratedRoot(), "generated.lock.json")
	lock, _ := os.ReadFile(lockPath)
	var changed map[string]any
	if err := json.Unmarshal(lock, &changed); err != nil {
		t.Fatal(err)
	}
	changed["config_sha256"] = strings.Repeat("0", 64)
	changedRaw, _ := MarshalJSON(changed)
	if err := os.WriteFile(lockPath, changedRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	assertDetachedDriftBlocked(t, repo, "snapshot does not match")
	if err := os.WriteFile(lockPath, lock, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	if status, _ := DetachedStatus(repo); !status.Verified {
		t.Fatalf("exact generated-state restoration did not recover verification: %+v", status)
	}
}

// control-law: detached-config-digest-gates-resume
func TestDetachedConfigDriftBlocksMutationAndPublicationBypasses(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/drift-bypass.git")
	embeddedFeatureForDetach(t, repo, "feature-one", "")
	configPath, _ := externalConfigFixture(t, "drift-bypass", "true")
	result, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: configPath})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	ctx := WorkspaceFor(repo)
	source, _ := os.ReadFile(ctx.SourceConfigPath())
	if err := os.WriteFile(ctx.SourceConfigPath(), append(source, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	before := filesystemSnapshot(t, ctx.controlRoot)
	planPath := filepath.Join(ctx.FeatureDir("feature-one"), "plan.md")

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "activation", run: func() error {
			return ActivatePlan(ActivationOptions{
				Repo:     repo,
				PlanPath: planPath, OutDir: filepath.Join(ctx.FeatureDir("feature-one"), "compiled"),
				OutputPath: filepath.Join(ctx.FeatureDir("feature-one"), "plan.lock.json"), SourceCommit: "test",
			})
		}},
		{name: "repair", run: func() error {
			_, _, err := RecordChangeObservation(ChangeObservationOptions{Repo: repo, Feature: "feature-one", Classification: "implementation_repair", Message: "fix", SourceStage: "ci"})
			return err
		}},
		{name: "gate", run: func() error {
			_, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: "feature-one", SliceID: "delivery", Gate: "test", Status: "PASS"})
			return err
		}},
		{name: "pr-context", run: func() error {
			_, err := PreparePRContext(PRContextOptions{Repo: repo})
			return err
		}},
		{name: "publish", run: func() error {
			_, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: "missing.md", ExpectedFingerprint: "missing", Action: "open"})
			return err
		}},
		{name: "operation", run: func() error {
			_, err := PrepareOperation(OperationPrepareOptions{Repo: repo, Kind: "test", Target: "target", PackageFingerprint: "package", ExpectedPostcondition: "done", RetryClass: "ATOMIC_LOCAL"})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); err == nil || !strings.Contains(err.Error(), "drifted from bound SHA-256") {
				t.Fatalf("entry point did not reach detached config boundary: %v", err)
			}
		})
	}
	if after := filesystemSnapshot(t, ctx.controlRoot); after != before {
		t.Fatal("blocked resume path mutated detached controller state")
	}
}

// control-law: detached-config-rebinding-requires-explicit-force
func TestDetachedForceReattachRebindsExternalConfig(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/rebind.git")
	firstPath, firstRaw := externalConfigFixture(t, "first", "true")
	first, _ := AttachDetached(AttachOptions{Repo: repo, ConfigPath: firstPath})
	secondPath, secondRaw := externalConfigFixture(t, "second", "pnpm test")
	blocked, _ := AttachDetached(AttachOptions{Repo: repo, ConfigPath: secondPath})
	if blocked.VerificationStatus != "BLOCKED" {
		t.Fatalf("reattach without force succeeded: %+v", blocked)
	}
	second, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: secondPath, Force: true})
	if err != nil || second.VerificationStatus != "VERIFIED" || second.ConfigSHA256 != SHA256Bytes(secondRaw) || second.ConfigSHA256 == SHA256Bytes(firstRaw) || second.ConfigSHA256 == first.ConfigSHA256 {
		t.Fatalf("forced reattach did not rebind config: first=%+v second=%+v err=%v", first, second, err)
	}
}
