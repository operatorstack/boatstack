package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlBundleCanonicalizesFilesAndBindsAbsence(t *testing.T) {
	left, err := NewControlBundleSnapshotWithAbsent(map[string][]byte{
		".boatstack/project.json":     []byte("project"),
		".agents/skills/run/SKILL.md": []byte("skill"),
	}, []string{".boatstack/runtime.json"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewControlBundleSnapshotWithAbsent(map[string][]byte{
		".agents/skills/run/SKILL.md": []byte("skill"),
		".boatstack/project.json":     []byte("project"),
	}, []string{".boatstack/runtime.json"})
	if err != nil {
		t.Fatal(err)
	}
	if left.Fingerprint != right.Fingerprint {
		t.Fatalf("ordering changed fingerprint: %s != %s", left.Fingerprint, right.Fingerprint)
	}
	replaced, err := ReplaceControlBundleFile(left, ".boatstack/runtime.json", []byte("pin"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Fingerprint == left.Fingerprint || replaced.Files[2].Absent {
		t.Fatalf("runtime pin replacement did not change the canonical bundle: %#v", replaced)
	}
}

func TestControlBundleVerifiesRootRevisionAndExactHead(t *testing.T) {
	repository := t.TempDir()
	runBundleGit(t, repository, "init", "-q")
	runBundleGit(t, repository, "config", "user.email", "bundle@example.invalid")
	runBundleGit(t, repository, "config", "user.name", "Bundle Test")
	if err := os.MkdirAll(filepath.Join(repository, ".boatstack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".boatstack", "project.json"), []byte("project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewControlBundleSnapshotWithAbsent(map[string][]byte{
		".boatstack/project.json": []byte("project\n"),
	}, []string{".boatstack/runtime.json"})
	if err != nil {
		t.Fatal(err)
	}
	runBundleGit(t, repository, "add", ".boatstack/project.json")
	runBundleGit(t, repository, "commit", "-q", "-m", "bundle")
	revision := strings.TrimSpace(runBundleGit(t, repository, "rev-parse", "HEAD"))
	if err := VerifyControlBundleRoot(repository, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControlBundleRevision(context.Background(), repository, revision, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControlBundleHead(context.Background(), repository, revision, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".boatstack", "runtime.json"), []byte("unexpected"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControlBundleRoot(repository, snapshot); err == nil || !strings.Contains(err.Error(), ".boatstack/runtime.json") {
		t.Fatalf("unexpected runtime pin was not rejected: %v", err)
	}
}

func runBundleGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return string(output)
}
