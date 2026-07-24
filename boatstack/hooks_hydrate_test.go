package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
}

// runGuard executes the installed bash guard for a host with a canonical,
// read-only event, returning its combined output and exit error.
func runGuard(t *testing.T, repo, host string, env ...string) (string, error) {
	t.Helper()
	guard := filepath.Join(repo, ".product-loop", "hooks", "guard.sh")
	cmd := exec.Command("bash", guard, host)
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git status --short"}}`)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// emptySharedSlot deletes the version-keyed shared runtime, modeling a teammate
// who pulled a version bump (or cloned fresh) and holds the committed pointers
// but no runtime bytes.
func emptySharedSlot(t *testing.T, repo string) (binaryPath, manifestPath string) {
	t.Helper()
	var err error
	binaryPath, manifestPath, err = sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(binaryPath)); err != nil {
		t.Fatal(err)
	}
	return binaryPath, manifestPath
}

// stageVerifiedHelper empties the slot and stages a tiny, checksum-consistent
// helper + manifest in a backup directory, returning a shell command that
// restores them into the slot — exactly what the real installer produces after
// download and checksum verification. The staged helper answers the guard's
// bootstrap-safety-hook exec by emitting a sentinel and allowing.
func stageVerifiedHelper(t *testing.T, repo string) (binaryPath, manifestPath, restoreCommand string) {
	t.Helper()
	binaryPath, manifestPath = emptySharedSlot(t, repo)
	backupDir := t.TempDir()
	fakeHelper := []byte("#!/usr/bin/env bash\necho boatstack-guard-hydration-sentinel >&2\nexit 0\n")
	backupHelper := filepath.Join(backupDir, filepath.Base(binaryPath))
	backupManifest := filepath.Join(backupDir, filepath.Base(manifestPath))
	if err := os.WriteFile(backupHelper, fakeHelper, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(fmt.Sprintf(`{"binary_sha256":"%s"}`, SHA256Bytes(fakeHelper)))
	if err := os.WriteFile(backupManifest, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	restoreCommand = fmt.Sprintf("mkdir -p %q && cp -p %q %q && cp -p %q %q",
		filepath.Dir(binaryPath),
		backupHelper, binaryPath,
		backupManifest, manifestPath)
	return binaryPath, manifestPath, restoreCommand
}

// TestGuardAutoHydratesMissingSharedRuntimeThenProceeds is the headline
// behavior: an absent slot self-heals through the verified hydrator and the
// guard clears every gate and reaches exec — a teammate never sees a deny.
func TestGuardAutoHydratesMissingSharedRuntimeThenProceeds(t *testing.T) {
	requireBash(t)
	repo := runtimeTestRepo(t)
	binaryPath, _, restore := stageVerifiedHelper(t, repo)

	output, err := runGuard(t, repo, "claude", "BOATSTACK_HYDRATE_COMMAND="+restore)
	if err != nil {
		t.Fatalf("guard did not proceed after auto-hydration: err=%v output=%s", err, output)
	}
	if strings.Contains(output, "shared runtime is missing") {
		t.Fatalf("guard reported a missing runtime despite successful hydration: %s", output)
	}
	if !strings.Contains(output, "boatstack-guard-hydration-sentinel") {
		t.Fatalf("guard did not exec the hydrated helper: %s", output)
	}
	if _, statErr := os.Stat(binaryPath); statErr != nil {
		t.Fatalf("shared slot was not populated: %v", statErr)
	}
}

// TestGuardFailsClosedWhenAutoHydrationFails proves hydration is purely
// additive: a failed installer never falls open; the existing missing-slot deny
// remains authoritative and now carries the exact self-heal command.
func TestGuardFailsClosedWhenAutoHydrationFails(t *testing.T) {
	requireBash(t)
	repo := runtimeTestRepo(t)
	emptySharedSlot(t, repo)

	output, err := runGuard(t, repo, "claude", "BOATSTACK_HYDRATE_COMMAND=exit 1")
	if err == nil {
		t.Fatalf("guard did not fail closed after a failed hydration: %s", output)
	}
	if !strings.Contains(output, "shared runtime is missing") {
		t.Fatalf("guard did not emit the fail-closed deny: %s", output)
	}
	if !strings.Contains(output, "BOATSTACK_MODE=hydrate BOATSTACK_VERSION="+Version) {
		t.Fatalf("deny did not embed the pinned installer command: %s", output)
	}
}

// TestGuardSkipsAutoHydrationWhenDisabled proves the kill switch: with
// BOATSTACK_AUTO_HYDRATE=0 the guard denies immediately and never runs the
// hydrator.
func TestGuardSkipsAutoHydrationWhenDisabled(t *testing.T) {
	requireBash(t)
	repo := runtimeTestRepo(t)
	emptySharedSlot(t, repo)
	sentinel := filepath.Join(t.TempDir(), "invoked")
	stub := fmt.Sprintf("touch %q", sentinel)

	output, err := runGuard(t, repo, "claude", "BOATSTACK_AUTO_HYDRATE=0", "BOATSTACK_HYDRATE_COMMAND="+stub)
	if err == nil {
		t.Fatalf("disabled guard did not deny: %s", output)
	}
	if !strings.Contains(output, "shared runtime is missing") {
		t.Fatalf("disabled guard did not emit the deny: %s", output)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatal("auto-hydration ran despite BOATSTACK_AUTO_HYDRATE=0")
	}
}

// TestGuardAutoHydrationInvokesPinnedHydrator proves the hydrator is always
// invoked with the worktree's pinned provenance — never a floating version.
func TestGuardAutoHydrationInvokesPinnedHydrator(t *testing.T) {
	requireBash(t)
	repo := runtimeTestRepo(t)
	emptySharedSlot(t, repo)
	record := filepath.Join(t.TempDir(), "env.txt")
	stub := fmt.Sprintf(`printf '%%s\n' "$BOATSTACK_MODE" "$BOATSTACK_VERSION" "$BOATSTACK_REPO" > %q`, record)

	// The stub records env then leaves the slot empty, so the guard denies after;
	// we only assert on how the hydrator was invoked.
	runGuard(t, repo, "claude", "BOATSTACK_HYDRATE_COMMAND="+stub)

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("hydrator was not invoked: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("hydrator recorded incomplete provenance: %q", data)
	}
	if lines[0] != "hydrate" || lines[1] != Version {
		t.Fatalf("hydrator invoked with wrong provenance: mode=%q version=%q (want hydrate/%s)", lines[0], lines[1], Version)
	}
	gotRepo, _ := filepath.EvalSymlinks(lines[2])
	wantRepo, _ := filepath.EvalSymlinks(repo)
	if gotRepo != wantRepo {
		t.Fatalf("hydrator repo = %q, want %q", lines[2], repo)
	}
}

// TestGuardAutoHydrationSerializesConcurrentFirstUse proves the clone-wide lock:
// two guards racing an absent slot invoke the hydrator at most once, and both
// still proceed.
func TestGuardAutoHydrationSerializesConcurrentFirstUse(t *testing.T) {
	requireBash(t)
	repo := runtimeTestRepo(t)
	binaryPath, manifestPath, restore := stageVerifiedHelper(t, repo)
	counter := filepath.Join(t.TempDir(), "count")
	stub := fmt.Sprintf("echo x >> %q && %s", counter, restore)
	_ = manifestPath

	var wg sync.WaitGroup
	outputs := make([]string, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			outputs[idx], errs[idx] = runGuard(t, repo, "claude", "BOATSTACK_HYDRATE_COMMAND="+stub)
		}(i)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("guard %d did not proceed under contention: err=%v output=%s", i, errs[i], outputs[i])
		}
	}
	if _, statErr := os.Stat(binaryPath); statErr != nil {
		t.Fatalf("shared slot was not populated under contention: %v", statErr)
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Fatalf("hydrator ran %d times under contention, want exactly 1", got)
	}
}
