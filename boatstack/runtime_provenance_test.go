package boatstack

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests pin the two provenance boundaries introduced to close the
// "cross-origin identity/checksum split" failure mode: the update hand-off
// boundary (RunUpdate) and the install write boundary (RunInit). They override
// the stubbable package vars readBinaryIdentity/reexecProcess so no real
// multi-version helper binaries are required. Globals are mutated, so none of
// these run in parallel.

// stubIdentity replaces readBinaryIdentity for the duration of a test and
// restores it afterwards. The returned identity is independent of the path so a
// single stub covers both the update and install boundaries.
func stubIdentity(t *testing.T, version, sourceCommit string) {
	t.Helper()
	original := readBinaryIdentity
	readBinaryIdentity = func(string) (string, string, error) { return version, sourceCommit, nil }
	t.Cleanup(func() { readBinaryIdentity = original })
}

// captureReexec replaces reexecProcess with a recorder that never replaces the
// test process. It returns pointers the caller can inspect after the exercised
// call. The recorder returns nil so RunUpdate treats the hand-off as complete.
func captureReexec(t *testing.T, called *bool, gotPath *string, gotArgs *[]string) {
	t.Helper()
	original := reexecProcess
	reexecProcess = func(path string, args []string, _ []string) error {
		*called = true
		*gotPath = path
		*gotArgs = args
		return nil
	}
	t.Cleanup(func() { reexecProcess = original })
}

func candidateBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boatstack-helper-candidate")
	if err := os.WriteFile(path, []byte("candidate bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunUpdateHandsOffAcrossVersionInsteadOfInProcessInstall proves that a
// -binary self-reporting a different identity is re-executed rather than
// installed in-process by the running (old) helper. This is the exact hand-off
// that lets the candidate stamp its own version, closing the incident's root.
func TestRunUpdateHandsOffAcrossVersionInsteadOfInProcessInstall(t *testing.T) {
	repo := runtimeTestRepo(t)
	candidate := candidateBinary(t)
	stubIdentity(t, "v9.9.9-candidate", "candidate-commit")

	var called bool
	var gotPath string
	var gotArgs []string
	captureReexec(t, &called, &gotPath, &gotArgs)

	if err := RunUpdate(InitOptions{Repo: repo, BinaryPath: candidate, Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("cross-version update should defer to the candidate, got error: %v", err)
	}
	if !called {
		t.Fatal("cross-version update did not re-exec the candidate binary")
	}
	absoluteCandidate, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != absoluteCandidate {
		t.Fatalf("re-exec target = %q, want the candidate %q", gotPath, absoluteCandidate)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "update") || !strings.Contains(joined, "-binary "+absoluteCandidate) {
		t.Fatalf("re-exec args did not re-issue update against the candidate: %v", gotArgs)
	}
}

// TestRunUpdateSameVersionDoesNotHandOff proves the hand-off only fires on an
// identity mismatch: a -binary self-reporting the running identity proceeds
// in-process (no re-exec), so ordinary same-version updates are unchanged.
func TestRunUpdateSameVersionDoesNotHandOff(t *testing.T) {
	repo := runtimeTestRepo(t)
	stubIdentity(t, Version, SourceCommit)

	var called bool
	var gotPath string
	var gotArgs []string
	captureReexec(t, &called, &gotPath, &gotArgs)

	// os.Executable() is the running test binary; a matching identity must be
	// handled in-process. The provenance guard decides the hand-off before the
	// normal update path runs (which independently rejects the test binary's
	// non-semver "dev" version), so the property under test is precisely that no
	// re-exec occurs — the downstream update outcome is irrelevant here.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_ = RunUpdate(InitOptions{Repo: repo, BinaryPath: self, Yes: true, Output: &bytes.Buffer{}})
	if called {
		t.Fatal("same-version update should not re-exec")
	}
}

// TestRunInitRefusesVersionMismatchedBinary proves the write boundary refuses to
// stamp the running process's version onto a foreign binary and writes nothing.
func TestRunInitRefusesVersionMismatchedBinary(t *testing.T) {
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate := candidateBinary(t)
	stubIdentity(t, "v9.9.9-candidate", "candidate-commit")

	err := RunInit(InitOptions{Repo: repo, BinaryPath: candidate, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("install of a version-mismatched -binary should be refused")
	}
	if !strings.Contains(err.Error(), "version-mismatched") {
		t.Fatalf("refusal did not name the provenance mismatch: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".product-loop", "bin", "install.lock.json")); !os.IsNotExist(statErr) {
		t.Fatalf("refused install must leave no install lock behind, stat err = %v", statErr)
	}
}

// TestRunInitAdoptsMatchingBinaryIdentity proves an explicit -binary whose
// self-report matches the running identity installs and records that identity.
func TestRunInitAdoptsMatchingBinaryIdentity(t *testing.T) {
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stubIdentity(t, Version, SourceCommit)

	if err := RunInit(InitOptions{Repo: repo, BinaryPath: self, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("matching -binary install failed: %v", err)
	}
	lock := readInstallLock(t, repo)
	if lock.BoatstackVersion != Version || lock.SourceCommit != SourceCommit {
		t.Fatalf("install lock recorded %s (%s), want the verified identity %s (%s)", lock.BoatstackVersion, lock.SourceCommit, Version, SourceCommit)
	}
}

// TestSelfInstallBypassesProvenanceGuard proves a normal install (no -binary)
// is untouched by the guard: readBinaryIdentity is set to a poison stub that
// would fail any check, yet the self-install succeeds because the guard only
// runs for an explicit -binary.
func TestSelfInstallBypassesProvenanceGuard(t *testing.T) {
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := readBinaryIdentity
	readBinaryIdentity = func(string) (string, string, error) { return "poison", "poison", nil }
	t.Cleanup(func() { readBinaryIdentity = original })

	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("self-install should not consult the provenance guard: %v", err)
	}
	lock := readInstallLock(t, repo)
	if lock.BoatstackVersion != Version {
		t.Fatalf("self-install recorded %s, want the running version %s", lock.BoatstackVersion, Version)
	}
}

// TestReexecUpdatePreservesUpdateFlags proves the hand-off re-issues the update
// against the candidate itself and forwards the operator's flags verbatim, so a
// re-exec is a faithful continuation and terminates in a single hop.
func TestReexecUpdatePreservesUpdateFlags(t *testing.T) {
	candidate := candidateBinary(t)
	var called bool
	var gotPath string
	var gotArgs []string
	captureReexec(t, &called, &gotPath, &gotArgs)

	if err := reexecUpdate(candidate, InitOptions{Repo: "some/repo", Yes: true, Repair: true, AllowDowngrade: true}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("reexecUpdate did not invoke the process replacement")
	}
	absoluteCandidate, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"update", "-repo some/repo", "-binary " + absoluteCandidate, "-yes", "-repair", "-allow-downgrade"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("re-exec args missing %q: %v", want, gotArgs)
		}
	}
	if gotArgs[0] != absoluteCandidate {
		t.Fatalf("argv[0] = %q, want the candidate %q so it proceeds in-process", gotArgs[0], absoluteCandidate)
	}
}

func TestParseVersionOutput(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantVer    string
		wantCommit string
		wantErr    bool
	}{
		{name: "canonical", input: "Boatstack v0.7.57 (abc1234)\n", wantVer: "v0.7.57", wantCommit: "abc1234"},
		{name: "surrounding whitespace", input: "  Boatstack v1.2.3 (deadbeef)  ", wantVer: "v1.2.3", wantCommit: "deadbeef"},
		{name: "commit with spaces preserved by last-paren split", input: "Boatstack dev (unknown)", wantVer: "dev", wantCommit: "unknown"},
		{name: "missing prefix", input: "v0.7.57 (abc1234)", wantErr: true},
		{name: "missing parens", input: "Boatstack v0.7.57", wantErr: true},
		{name: "empty version", input: "Boatstack  (abc1234)", wantErr: true},
		{name: "empty commit", input: "Boatstack v0.7.57 ()", wantErr: true},
		{name: "garbage", input: "not a version line", wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			version, commit, err := parseVersionOutput(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got (%q, %q)", testCase.input, version, commit)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", testCase.input, err)
			}
			if version != testCase.wantVer || commit != testCase.wantCommit {
				t.Fatalf("parseVersionOutput(%q) = (%q, %q), want (%q, %q)", testCase.input, version, commit, testCase.wantVer, testCase.wantCommit)
			}
		})
	}
}

func readInstallLock(t *testing.T, repo string) installLock {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock installLock
	if err := json.Unmarshal(value, &lock); err != nil {
		t.Fatal(err)
	}
	return lock
}
