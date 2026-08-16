package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedHistoryAcceptsOnlyExactEarlierSnapshotFingerprint(t *testing.T) {
	files := []ControlBundleFile{{Path: ".boatstack/project.json", SHA256: strings.Repeat("a", 64)}}
	historicalFingerprint, err := controlBundleDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ControlBundleSnapshot{Fingerprint: historicalFingerprint, Files: files}
	contract := ControlBundleContract{SchemaVersion: ControlBundleSchemaVersion, Source: snapshot}
	identity := contract
	identity.Fingerprint = ""
	contract.Fingerprint, err = controlBundleDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(); err == nil || !strings.Contains(err.Error(), "snapshot fingerprint mismatch") {
		t.Fatalf("current validation accepted historical snapshot: %v", err)
	}
	if err := contract.ValidateCommittedHistory(); err != nil {
		t.Fatalf("committed-history validation rejected exact historical snapshot: %v", err)
	}

	tampered := contract
	tampered.Source.Files = append([]ControlBundleFile(nil), contract.Source.Files...)
	tampered.Source.Files[0].SHA256 = strings.Repeat("b", 64)
	if err := tampered.ValidateCommittedHistory(); err == nil {
		t.Fatal("committed-history validation accepted tampered file identity")
	}
	withMembers := contract
	withMembers.Source.MemberSets = []ControlBundleMemberSet{{Root: ".boatstack", Suffix: ".json", Paths: []string{".boatstack/project.json"}}}
	if err := withMembers.ValidateCommittedHistory(); err == nil {
		t.Fatal("historical fingerprint encoding accepted a member-set snapshot")
	}
}

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

func TestResolveWorkspaceBaseRevisionFallsBackToOriginTrackingBranch(t *testing.T) {
	repository := t.TempDir()
	runBundleGit(t, repository, "init", "-q")
	runBundleGit(t, repository, "config", "user.email", "bundle@example.invalid")
	runBundleGit(t, repository, "config", "user.name", "Bundle Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runBundleGit(t, repository, "add", "README.md")
	runBundleGit(t, repository, "commit", "-q", "-m", "base")
	runBundleGit(t, repository, "branch", "-M", "main")
	want := strings.TrimSpace(runBundleGit(t, repository, "rev-parse", "HEAD"))
	runBundleGit(t, repository, "update-ref", "refs/remotes/origin/main", want)
	runBundleGit(t, repository, "switch", "-q", "-c", "feature")
	runBundleGit(t, repository, "branch", "-D", "main")

	got, err := ResolveWorkspaceBaseRevision(context.Background(), repository, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolved revision = %s, want %s", got, want)
	}
}

func TestResolveWorkspaceBaseRevisionPrefersLocalBranch(t *testing.T) {
	repository := t.TempDir()
	runBundleGit(t, repository, "init", "-q")
	runBundleGit(t, repository, "config", "user.email", "bundle@example.invalid")
	runBundleGit(t, repository, "config", "user.name", "Bundle Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runBundleGit(t, repository, "add", "README.md")
	runBundleGit(t, repository, "commit", "-q", "-m", "local")
	runBundleGit(t, repository, "branch", "-M", "main")
	localRevision := strings.TrimSpace(runBundleGit(t, repository, "rev-parse", "HEAD"))
	runBundleGit(t, repository, "commit", "--allow-empty", "-q", "-m", "remote")
	remoteRevision := strings.TrimSpace(runBundleGit(t, repository, "rev-parse", "HEAD"))
	runBundleGit(t, repository, "update-ref", "refs/remotes/origin/main", remoteRevision)
	runBundleGit(t, repository, "reset", "--hard", "-q", localRevision)

	got, err := ResolveWorkspaceBaseRevision(context.Background(), repository, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != localRevision {
		t.Fatalf("resolved revision = %s, want local %s", got, localRevision)
	}
}

func TestControlBundleBindsCompleteExecutableDirectoryMembership(t *testing.T) {
	repository := t.TempDir()
	runBundleGit(t, repository, "init", "-q")
	runBundleGit(t, repository, "config", "user.email", "bundle@example.invalid")
	runBundleGit(t, repository, "config", "user.name", "Bundle Test")
	flows := filepath.Join(repository, ".boatstack", "flows")
	if err := os.MkdirAll(flows, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flows, "primary.flow.ir.json"), []byte("primary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewControlBundleSnapshotWithMemberSets(map[string][]byte{
		".boatstack/flows/primary.flow.ir.json": []byte("primary\n"),
	}, nil, []ControlBundleMemberSet{{Root: ".boatstack/flows", Suffix: ".flow.ir.json", Paths: []string{".boatstack/flows/primary.flow.ir.json"}}})
	if err != nil {
		t.Fatal(err)
	}
	runBundleGit(t, repository, "add", ".boatstack/flows")
	runBundleGit(t, repository, "commit", "-q", "-m", "primary")
	if err := os.WriteFile(filepath.Join(flows, "secondary.flow.ir.json"), []byte("secondary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControlBundleRoot(repository, snapshot); err == nil || !strings.Contains(err.Error(), "member set") {
		t.Fatalf("extra executable member was accepted at root: %v", err)
	}
	runBundleGit(t, repository, "add", ".boatstack/flows/secondary.flow.ir.json")
	runBundleGit(t, repository, "commit", "-q", "-m", "secondary")
	revision := strings.TrimSpace(runBundleGit(t, repository, "rev-parse", "HEAD"))
	if err := VerifyControlBundleRevision(context.Background(), repository, revision, snapshot); err == nil || !strings.Contains(err.Error(), "member set") {
		t.Fatalf("extra executable member was accepted at revision: %v", err)
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
