package boatstack

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOwnedRetiredHookEventMigratesWithoutRepair(t *testing.T) {
	retired := desiredHostHookForEvent("cursor", "beforeShellExecution")
	installed := map[string]any{"retiredCursorEvent": retired}
	config := map[string]any{
		"version": float64(1),
		"hooks": map[string]any{
			"retiredCursorEvent": []any{retired, map[string]any{"command": "./keep-user-hook.sh"}},
		},
	}
	if err := mergeHostHookWithOwnership(config, "cursor", installed, false); err != nil {
		t.Fatal(err)
	}
	hooks := config["hooks"].(map[string]any)
	retiredEntries := hooks["retiredCursorEvent"].([]any)
	if len(retiredEntries) != 1 || containsBoatstackHook(retiredEntries) {
		t.Fatalf("retired owned hook was not removed without disturbing user hook: %#v", retiredEntries)
	}
	for _, event := range hookEvents("cursor") {
		if !containsBoatstackHook(hooks[event]) {
			t.Fatalf("target event %s was not installed", event)
		}
	}
}

func TestExactInstalledHookDuplicatesAreDeduplicated(t *testing.T) {
	entry := desiredHostHookForEvent("cursor", "beforeShellExecution")
	config := map[string]any{"version": float64(1), "hooks": map[string]any{"beforeShellExecution": []any{entry, entry}}}
	if err := mergeHostHookWithOwnership(config, "cursor", map[string]any{"beforeShellExecution": entry}, false); err != nil {
		t.Fatal(err)
	}
	entries := config["hooks"].(map[string]any)["beforeShellExecution"].([]any)
	if len(entries) != 1 {
		t.Fatalf("verified duplicate hooks were not deduplicated: %#v", entries)
	}
}

func TestDriftedOwnedHookRequiresRepairAndFingerprintIsStable(t *testing.T) {
	installedEntry := desiredHostHookForEvent("cursor", "beforeShellExecution")
	drifted := desiredHostHookForEvent("cursor", "beforeShellExecution")
	drifted["timeout"] = float64(99)
	config := map[string]any{"version": float64(1), "hooks": map[string]any{"beforeShellExecution": []any{drifted}}}
	if err := mergeHostHookWithOwnership(config, "cursor", map[string]any{"beforeShellExecution": installedEntry}, false); err == nil || !strings.Contains(err.Error(), "--repair") {
		t.Fatalf("drifted hook did not require repair: %v", err)
	}
	if err := mergeHostHookWithOwnership(config, "cursor", map[string]any{"beforeShellExecution": installedEntry}, true); err != nil {
		t.Fatal(err)
	}
	entries := config["hooks"].(map[string]any)["beforeShellExecution"].([]any)
	if len(entries) != 1 || !sameJSON(entries[0], desiredHostHookForEvent("cursor", "beforeShellExecution")) {
		t.Fatalf("repair did not replace the exact owned entry: %#v", entries)
	}
}

func driftCursorHook(t *testing.T, repo string) []byte {
	t.Helper()
	path := filepath.Join(repo, ".cursor", "hooks.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(before, &config); err != nil {
		t.Fatal(err)
	}
	entries := config["hooks"].(map[string]any)["beforeShellExecution"].([]any)
	entries[0].(map[string]any)["timeout"] = float64(99)
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestUpdateRepairPromptAndNonInteractiveRecovery(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	drifted := driftCursorHook(t, repo)

	var noninteractive bytes.Buffer
	err := RunInit(InitOptions{Repo: repo, Update: true, Yes: true, Input: strings.NewReader(""), Output: &noninteractive})
	retryMarker := "BOATSTACK_REPAIR=1"
	installerMarker := "v0.5.0/install.sh"
	if runtime.GOOS == "windows" {
		retryMarker = "BOATSTACK_REPAIR"
		installerMarker = "v0.5.0/install.ps1"
	}
	if err == nil || !strings.Contains(err.Error(), retryMarker) || !strings.Contains(err.Error(), installerMarker) {
		t.Fatalf("noninteractive update did not return one repair action: %v\n%s", err, noninteractive.String())
	}
	if !strings.Contains(noninteractive.String(), "Repair package:") {
		t.Fatalf("repair fingerprint was not displayed: %s", noninteractive.String())
	}
	current, _ := os.ReadFile(filepath.Join(repo, ".cursor", "hooks.json"))
	if !bytes.Equal(current, drifted) {
		t.Fatal("failed update changed the drifted file")
	}

	var declined bytes.Buffer
	err = RunInit(InitOptions{Repo: repo, Update: true, Input: strings.NewReader("\n"), Output: &declined})
	if err == nil || !strings.Contains(declined.String(), "Repair Boatstack-owned state and continue the update? [y/N]") {
		t.Fatalf("interactive update did not default to a visible repair refusal: %v\n%s", err, declined.String())
	}
}

func TestRepairCompletesAndWritesGitCommonBackup(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	driftCursorHook(t, repo)
	var output bytes.Buffer
	if err := RunInit(InitOptions{Repo: repo, Update: true, Repair: true, Yes: true, Input: strings.NewReader(""), Output: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Repair package") || !strings.Contains(output.String(), "repair-backups") {
		t.Fatalf("repair backup was not reported: %s", output.String())
	}
	if err := CheckHostHooks(repo, []string{"cursor"}); err != nil {
		t.Fatalf("repaired hooks do not match target: %v", err)
	}
}

func TestInteractiveRepairAuthorityIsBoundBeforeOperationLease(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	driftCursorHook(t, repo)
	if err := RunUpdate(InitOptions{Repo: repo, Input: strings.NewReader("y\ny\n"), Output: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	receipts, err := operationReceipts(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := SHA256Bytes([]byte(Version + "\x00" + SourceCommit + "\x00" + ChecksumsSHA256 + "\x00repair=true\x00allow-downgrade=false"))
	found := false
	for _, receipt := range receipts {
		if receipt.Kind == "install-update" && receipt.PackageFingerprint == want && receipt.State == OperationSucceeded {
			found = true
		}
	}
	if !found {
		t.Fatalf("interactive repair did not create a repair-bound terminal operation: %#v", receipts)
	}
}

func TestRepairRejectsMixedUserAndOwnedHookEdits(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	driftCursorHook(t, repo)
	path := filepath.Join(repo, ".cursor", "hooks.json")
	value, _ := os.ReadFile(path)
	var config map[string]any
	if err := json.Unmarshal(value, &config); err != nil {
		t.Fatal(err)
	}
	config["new_user_setting"] = "do-not-package"
	value, _ = MarshalJSON(config)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	project, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ClassifyInstallationRepair(repo, project.Adapters, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateUpdateWorkspaceForRepair(repo, project, result, true); err == nil || !strings.Contains(err.Error(), "non-repairable changes") {
		t.Fatalf("mixed user and owned edits entered repair: %v", err)
	}
}

func TestRepairNeverOverwritesPartialInterceptorBoundary(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	path := filepath.Join(repo, "CLAUDE.md")
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(value), interceptorFooter, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	project, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ClassifyInstallationRepair(repo, project.Adapters, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(strings.Join(result.Blockers, " "), "markers") {
		t.Fatalf("partial interceptor was not blocked: %#v", result)
	}
}

func TestRepairRejectsSymlinkedOwnedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix permissions")
	}
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	path := filepath.Join(repo, ".cursor", "hooks.json")
	target := filepath.Join(t.TempDir(), "outside.json")
	value, _ := os.ReadFile(path)
	if err := os.WriteFile(target, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	project, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ClassifyInstallationRepair(repo, project.Adapters, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(strings.Join(result.Blockers, " "), "symlink") {
		t.Fatalf("symlinked owned path was not blocked: %#v", result)
	}
}

func TestDowngradeRequiresRepairAndSeparateAuthority(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.6.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	result, err := ClassifyInstallationRepair(repo, []string{"cursor"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Direction != "DOWNGRADE" || result.VerificationStatus != "BLOCKED" || !strings.Contains(strings.Join(result.Blockers, " "), "--allow-downgrade") {
		t.Fatalf("downgrade was not independently blocked: %#v", result)
	}
	result, err = ClassifyInstallationRepair(repo, []string{"cursor"}, true)
	if err != nil || result.Direction != "DOWNGRADE" || result.VerificationStatus == "BLOCKED" {
		t.Fatalf("explicit downgrade projection was not available: %#v %v", result, err)
	}
}

func TestRepairPreservesIntegrationFallbackWhenInstallLockIsMissing(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	if err := os.Remove(filepath.Join(repo, ".product-loop", "bin", "install.lock.json")); err != nil {
		t.Fatal(err)
	}
	config, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ClassifyInstallationRepair(repo, config.Adapters, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "REPAIR_AVAILABLE" || len(result.PreservedIntegrations) == 0 {
		t.Fatalf("missing install lock was not recoverable from config: %#v", result)
	}
}

func TestRepairReconstructsCorruptGeneratedProvenance(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	lockPath := filepath.Join(repo, ".product-loop", "generated.lock.json")
	if err := os.WriteFile(lockPath, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, Update: true, Repair: true, Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if len(previousFiles(repo)) == 0 {
		t.Fatal("repair did not reconstruct generated provenance")
	}
}

func TestRepairReconstructsCorruptHookFragmentFromCommittedProvenance(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	withUpdateGlobals(t, "v0.4.0", now, func() (ReleaseInfo, error) { return ReleaseInfo{}, nil })
	repo, _ := updateInstalledRepo(t)
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-v0.5.0")
	Version = "v0.5.0"
	SourceCommit = "update-test-0.5.0"
	fragmentPath := filepath.Join(repo, ".product-loop", "hooks", "cursor.fragment.json")
	if err := os.WriteFile(fragmentPath, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, Update: true, Repair: true, Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadInstalledHookEvents(repo, "cursor"); err != nil {
		t.Fatalf("repair did not reconstruct hook fragment: %v", err)
	}
}
