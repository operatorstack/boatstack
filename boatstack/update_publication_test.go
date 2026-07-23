package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func updatePublicationTestRepo(t *testing.T, version string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Adapters = []string{"cursor"}
	configValue, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := ".cursor/commands/boatstack-update.md"
	generatedValue := []byte("<!-- " + Marker + " -->\nold\n")
	for path, value := range map[string][]byte{
		".product-loop/project.json": configValue,
		generatedPath:                generatedValue,
		"README.md":                  []byte("fixture\n"),
	} {
		absolute := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, value, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := MarshalJSON(map[string]any{"files": map[string]string{generatedPath: SHA256Bytes(generatedValue)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "generated.lock.json"), lock, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := execCommand("git", "init", "--bare", remote); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, repo, "switch", "-c", "chore/update-boatstack-"+version)
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(generatedPath)), []byte("<!-- "+Marker+" -->\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func execCommand(name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	value, err := command.CombinedOutput()
	return string(value), err
}

func TestPrepareUpdatePublicationIsAtomicAndRejectsProductPaths(t *testing.T) {
	repo := updatePublicationTestRepo(t, "v9.8.7")
	statusBefore := gitOutput(repo, "status", "--porcelain=v1", "--untracked-files=all")
	preview, err := PrepareUpdatePublication(repo, "v9.8.7")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Fingerprint == "" || preview.PackageFingerprint == "" || len(preview.ChangedPaths) != 1 || preview.ChangedPaths[0] != ".cursor/commands/boatstack-update.md" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	value, err := os.ReadFile(preview.PreviewPath)
	common, commonErr := gitCommonDir(repo)
	relativeToCommon, relativeErr := filepath.Rel(common, preview.PreviewPath)
	outsideCommon := relativeErr != nil || relativeToCommon == ".." || strings.HasPrefix(relativeToCommon, ".."+string(filepath.Separator))
	statusAfter := gitOutput(repo, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || commonErr != nil || len(value) == 0 || outsideCommon || statusAfter != statusBefore {
		t.Fatalf("preview was not a complete Git-common artifact: path=%s common=%s relative=%s size=%d read_err=%v common_err=%v rel_err=%v worktree_changed=%t", preview.PreviewPath, common, relativeToCommon, len(value), err, commonErr, relativeErr, statusAfter != statusBefore)
	}
	if err := os.WriteFile(filepath.Join(repo, "product.go"), []byte("package product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareUpdatePublication(repo, "v9.8.7"); err == nil || !strings.Contains(err.Error(), "non-Boatstack paths") {
		t.Fatalf("product path entered update package: %v", err)
	}
}

func TestUpdatePreviewCarriesFingerprintRepairProvenance(t *testing.T) {
	repo := updatePublicationTestRepo(t, "v9.8.7")
	result := InstallationRepairResult{
		SchemaVersion: 1, VerificationStatus: "REPAIR_AVAILABLE", InstalledVersion: "v9.8.6",
		TargetVersion: "v9.8.7", Direction: "UPGRADE", PackageFingerprint: strings.Repeat("a", 64),
		HeadBranch: "chore/update-boatstack-v9.8.7", StartingHeadCommit: gitOutput(repo, "rev-parse", "HEAD"),
		Items: []InstallationRepairItem{{Path: ".cursor/commands/boatstack-update.md", Classification: RepairOwnedDrifted, Reason: "fixture drift"}},
	}
	backup, err := writeInstallationRepairBackup(repo, result)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareUpdatePublication(repo, "v9.8.7")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Repair == nil || preview.Repair.PackageFingerprint != result.PackageFingerprint || preview.Repair.BackupPath != "boatstack/repair-backups/"+result.PackageFingerprint || !strings.Contains(preview.Body, result.PackageFingerprint) {
		t.Fatalf("repair provenance missing from update preview: %#v", preview)
	}
	if backup == "" {
		t.Fatal("repair backup path was not returned to the local operator")
	}
}

func TestPublishUpdatePublicationOwnsCommitPushAndSinglePR(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh fixture uses a POSIX shell")
	}
	repo := updatePublicationTestRepo(t, "v9.8.7")
	preview, err := PrepareUpdatePublication(repo, "v9.8.7")
	if err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	marker := filepath.Join(fakeDir, "created")
	script := filepath.Join(fakeDir, "gh")
	scriptBody := `#!/bin/sh
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  if [ -f "$BOATSTACK_UPDATE_CREATED" ]; then echo "https://github.com/example/repo/pull/42"; exit 0; fi
  echo "no pull requests found for branch" >&2; exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  touch "$BOATSTACK_UPDATE_CREATED"
  echo "https://github.com/example/repo/pull/42"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BOATSTACK_UPDATE_CREATED", marker)
	url, err := PublishUpdatePublication(UpdatePublishOptions{Repo: repo, PreviewPath: preview.PreviewPath, ExpectedFingerprint: preview.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/example/repo/pull/42" || !fileExists(marker) {
		t.Fatalf("unexpected publication: %s", url)
	}
	if status := strings.TrimSpace(gitOutput(repo, "status", "--porcelain")); status != "" {
		t.Fatalf("publisher left dirty state: %s", status)
	}
	if subject := gitOutput(repo, "log", "-1", "--pretty=%s"); subject != "chore: update Boatstack to v9.8.7" {
		t.Fatalf("publisher did not own the exact commit: %s", subject)
	}
	secondURL, err := PublishUpdatePublication(UpdatePublishOptions{Repo: repo, PreviewPath: preview.PreviewPath, ExpectedFingerprint: preview.Fingerprint})
	if err != nil || secondURL != url {
		t.Fatalf("terminal receipt did not suppress duplicate publication: %s %v", secondURL, err)
	}
}

func TestInterruptedUpdatePublicationReconcilesExistingPRWithoutDuplicate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh fixture uses a POSIX shell")
	}
	repo := updatePublicationTestRepo(t, "v9.8.6")
	preview, err := PrepareUpdatePublication(repo, "v9.8.6")
	if err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	marker := filepath.Join(fakeDir, "created")
	count := filepath.Join(fakeDir, "count")
	script := filepath.Join(fakeDir, "gh")
	scriptBody := `#!/bin/sh
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  if [ -f "$BOATSTACK_UPDATE_CREATED" ]; then echo "https://github.com/example/repo/pull/43"; exit 0; fi
  echo "no pull requests found for branch" >&2; exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  touch "$BOATSTACK_UPDATE_CREATED"
  echo x >> "$BOATSTACK_UPDATE_COUNT"
  echo "connection closed after request" >&2
  exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "edit" ]; then exit 0; fi
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BOATSTACK_UPDATE_CREATED", marker)
	t.Setenv("BOATSTACK_UPDATE_COUNT", count)
	if _, err := PublishUpdatePublication(UpdatePublishOptions{Repo: repo, PreviewPath: preview.PreviewPath, ExpectedFingerprint: preview.Fingerprint}); err == nil {
		t.Fatal("interrupted GitHub response unexpectedly reported success")
	}
	url, err := PublishUpdatePublication(UpdatePublishOptions{Repo: repo, PreviewPath: preview.PreviewPath, ExpectedFingerprint: preview.Fingerprint})
	if err != nil || url != "https://github.com/example/repo/pull/43" {
		t.Fatalf("reconciliation did not recover observed PR: %s %v", url, err)
	}
	value, err := os.ReadFile(count)
	if err != nil || strings.Count(string(value), "x") != 1 {
		t.Fatalf("publication was duplicated: %q %v", value, err)
	}
}
