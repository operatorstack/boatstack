package boatstack

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (function updateRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func withUpdateGlobals(t *testing.T, version string, now time.Time, fetch func() (ReleaseInfo, error)) {
	t.Helper()
	oldVersion := Version
	oldCommit := SourceCommit
	oldChecksums := ChecksumsSHA256
	oldNow := updateNow
	oldFetch := fetchLatestRelease
	Version = version
	SourceCommit = "update-test-" + strings.TrimPrefix(version, "v")
	ChecksumsSHA256 = "update-test-checksums"
	updateNow = func() time.Time { return now }
	fetchLatestRelease = fetch
	t.Cleanup(func() {
		Version = oldVersion
		SourceCommit = oldCommit
		ChecksumsSHA256 = oldChecksums
		updateNow = oldNow
		fetchLatestRelease = oldFetch
	})
}

func updateCacheRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	return repo
}

func TestStableVersionComparison(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"v0.4.0", "v0.5.0", -1},
		{"0.5.0", "v0.5.0", 0},
		{"v1.0.0", "v0.9.9", 1},
	} {
		got, err := compareVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, %v; want %d", test.left, test.right, got, err, test.want)
		}
	}
	for _, invalid := range []string{"", "latest", "v0.5.0-rc.1", "v0.5", "v1.2.3.4"} {
		if _, err := parseStableVersion(invalid); err == nil {
			t.Fatalf("parseStableVersion accepted %q", invalid)
		}
	}
}

func TestLatestReleaseResponseValidation(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	for _, test := range []struct {
		name, body string
		status     int
		transport  error
		wantErr    bool
	}{
		{"stable", `{"tag_name":"v0.5.0","name":"v0.5.0","body":"Release notes","html_url":"https://example.invalid/v0.5.0"}`, 200, nil, false},
		{"prerelease", `{"tag_name":"v0.5.0-rc.1","prerelease":true,"html_url":"https://example.invalid/rc"}`, 200, nil, true},
		{"malformed", `{`, 200, nil, true},
		{"rate limit", `{}`, 429, nil, true},
		{"timeout", ``, 0, errors.New("request timed out"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			http.DefaultClient = &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
				if test.transport != nil {
					return nil, test.transport
				}
				return &http.Response{
					StatusCode: test.status,
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Header:     make(http.Header),
				}, nil
			})}
			release, err := defaultFetchLatestRelease()
			if test.wantErr && err == nil {
				t.Fatalf("defaultFetchLatestRelease accepted %s: %#v", test.name, release)
			}
			if !test.wantErr && (err != nil || release.Version != "v0.5.0" || release.Notes != "Release notes") {
				t.Fatalf("stable release = %#v, %v", release, err)
			}
		})
	}
}

func TestUpdateCheckCachesAndBoundsNotifications(t *testing.T) {
	repo := updateCacheRepo(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	latest := ReleaseInfo{Version: "v0.5.0", Name: "Boatstack v0.5.0", URL: "https://github.com/operatorstack/boatstack/releases/tag/v0.5.0"}
	fetches := 0
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) {
		fetches++
		return latest, nil
	})

	first, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Notify: true})
	if err != nil || first.Status != "available" || !first.ShouldNotify || first.FromCache {
		t.Fatalf("first check = %#v, %v", first, err)
	}
	if fetches != 1 {
		t.Fatalf("first check fetched %d times", fetches)
	}

	updateNow = func() time.Time { return now.Add(2 * time.Hour) }
	second, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Notify: true})
	if err != nil || second.ShouldNotify || !second.FromCache {
		t.Fatalf("cached check = %#v, %v", second, err)
	}
	if fetches != 1 {
		t.Fatalf("cached check fetched %d times", fetches)
	}

	updateNow = func() time.Time { return now.Add(8 * 24 * time.Hour) }
	reminder, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Notify: true})
	if err != nil || !reminder.ShouldNotify {
		t.Fatalf("weekly reminder = %#v, %v", reminder, err)
	}
	if fetches != 2 {
		t.Fatalf("expired cache fetched %d times", fetches)
	}

	latest.Version = "v0.6.0"
	latest.Name = "Boatstack v0.6.0"
	latest.URL = "https://github.com/operatorstack/boatstack/releases/tag/v0.6.0"
	updateNow = func() time.Time { return now.Add(8*24*time.Hour + time.Hour) }
	newRelease, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Force: true, Notify: true})
	if err != nil || newRelease.LatestVersion != "v0.6.0" || !newRelease.ShouldNotify {
		t.Fatalf("new release = %#v, %v", newRelease, err)
	}
}

func TestUpdateCheckCurrentAndFailures(t *testing.T) {
	repo := updateCacheRepo(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.5.0", now, func() (ReleaseInfo, error) {
		return ReleaseInfo{Version: "v0.5.0", URL: "https://example.invalid/v0.5.0"}, nil
	})
	result, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Force: true, Notify: true})
	if err != nil || result.Status != "current" || result.ShouldNotify {
		t.Fatalf("current check = %#v, %v", result, err)
	}
	if _, ok := CachedUpdate(repo); ok {
		t.Fatal("doctor cache exposed a current release as an update")
	}

	fetchLatestRelease = func() (ReleaseInfo, error) { return ReleaseInfo{}, errors.New("rate limited") }
	if _, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Force: true}); err == nil {
		t.Fatal("forced check hid its network failure")
	}
	if err := os.Remove(updateStatePath(repo)); err != nil {
		t.Fatal(err)
	}
	if notice, ok := PostShipUpdateNotice(repo); ok {
		t.Fatalf("release lookup failure changed post-ship behavior: %#v", notice)
	}
	if cached, ok := CachedUpdate(repo); ok {
		t.Fatalf("failed release discovery wrote a misleading cache: %#v", cached)
	}
}

func updateInstalledRepo(t *testing.T) (string, map[string]IntegrationState) {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"scripts":{"test":"node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".boatstack-project.json")
	config, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gstack := config.Integrations["gstack"]
	gstack.Requested = true
	config.Integrations["gstack"] = gstack
	rawConfig, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, rawConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(configPath, config, rawConfig, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	states := map[string]IntegrationState{
		"gstack":   {Requested: true, Status: "installed", Version: GStackRef, Detail: "fixture installation"},
		"spec-kit": {Requested: false, Status: "not_selected", Version: SpecKitVersion},
	}
	var prior installLock
	lockValue, err := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if err != nil || json.Unmarshal(lockValue, &prior) != nil {
		t.Fatalf("read install lock: %v", err)
	}
	binaryPath, err := resolveRepositoryRelativePath(repo, prior.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := SHA256File(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeInstallLock(repo, binaryPath, binaryHash, states); err != nil {
		t.Fatal(err)
	}
	cursorHooks := filepath.Join(repo, ".cursor", "hooks.json")
	hooks := map[string]any{}
	hooksValue, err := os.ReadFile(cursorHooks)
	if err != nil || json.Unmarshal(hooksValue, &hooks) != nil {
		t.Fatalf("read Cursor hooks: %v", err)
	}
	hooks["user_setting"] = "preserve-me"
	updatedHooks, err := MarshalJSON(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorHooks, updatedHooks, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "install Boatstack")
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "--set-upstream", "origin", "main")
	return repo, states
}

func TestUpdateRequiresCleanCurrentDedicatedBranch(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	config, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"

	if err := ValidateUpdateWorkspace(repo, config); err == nil || !strings.Contains(err.Error(), "chore/update-boatstack-v0.5.0") {
		t.Fatalf("default branch was not blocked: %v", err)
	}
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	if err := ValidateUpdateWorkspace(repo, config); err != nil {
		t.Fatalf("healthy update workspace was rejected: %v", err)
	}
	head := runGit(t, repo, "rev-parse", "HEAD")
	tree := runGit(t, repo, "write-tree")
	remoteAdvance := runGit(t, repo, "commit-tree", tree, "-p", head, "-m", "remote advance")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", remoteAdvance)
	if err := ValidateUpdateWorkspace(repo, config); err == nil || !strings.Contains(err.Error(), "current origin/main") {
		t.Fatalf("stale default branch was not blocked: %v", err)
	}
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", head)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUpdateWorkspace(repo, config); err == nil || !strings.Contains(err.Error(), "clean worktree") {
		t.Fatalf("dirty update was not blocked: %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(repo, ".product-loop", "workflow.md")
	value, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, append(value, []byte("\ndrift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckPreviousGeneratedState(repo); err == nil || !strings.Contains(err.Error(), "workflow.md") {
		t.Fatalf("generated drift was not detected: %v", err)
	}
	if err := ValidateUpdateWorkspace(repo, config); err == nil || !strings.Contains(err.Error(), "clean worktree") {
		t.Fatalf("tracked generated drift was not blocked before mutation: %v", err)
	}
}

func TestDoctorReadsCachedUpdateWithoutNetwork(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) {
		panic("doctor initiated release traffic")
	})
	repo, _ := updateInstalledRepo(t)
	if err := writeUpdateState(repo, UpdateState{
		SchemaVersion:  1,
		CurrentVersion: "v0.4.0",
		LatestVersion:  "v0.5.0",
		ReleaseURL:     "https://example.invalid/v0.5.0",
		CheckedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err != nil {
		t.Fatal(err)
	}
	if cached, ok := CachedUpdate(repo); !ok || cached.LatestVersion != "v0.5.0" {
		t.Fatalf("cached update missing after offline doctor: %#v, %t", cached, ok)
	}
}

func TestRunUpdatePreservesConfigurationAndIntegrations(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, beforeStates := updateInstalledRepo(t)
	configPath := filepath.Join(repo, ".boatstack-project.json")
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	var output bytes.Buffer
	if err := RunUpdate(InitOptions{Repo: repo, Yes: true, Output: &output}); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != runGit(t, repo, "rev-parse", "origin/main") {
		t.Fatal("update preparation committed before open update PR")
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeConfig, afterConfig) {
		t.Fatal("update rewrote project configuration")
	}
	config, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	afterStates, err := readInstalledIntegrations(repo, config)
	if err != nil {
		t.Fatal(err)
	}
	for name, before := range beforeStates {
		if afterStates[name] != before {
			t.Fatalf("integration %s changed: %#v -> %#v", name, before, afterStates[name])
		}
	}
	hooksValue, err := os.ReadFile(filepath.Join(repo, ".cursor", "hooks.json"))
	if err != nil || !strings.Contains(string(hooksValue), "preserve-me") {
		t.Fatalf("update removed user-owned host settings: %v", err)
	}
	for _, expected := range []string{"updated to v0.5.0", "no product files changed", "open update PR", "never merge automatically"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("update output is missing %q: %s", expected, output.String())
		}
	}
	for _, changed := range updateChangedPaths(repo) {
		if changed == "package.json" || strings.HasSuffix(changed, ".go") {
			t.Fatalf("update touched product path %s", changed)
		}
	}
}
