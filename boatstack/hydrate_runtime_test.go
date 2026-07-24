package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunHydrateRuntimePopulatesSlotIdempotentlyWithoutTouchingCommittedState
// models a teammate's clone after a version bump: the committed pointers exist
// but both the shared slot and the worktree bin are empty. Hydration must
// repopulate both from the running binary, be safe to repeat, and never rewrite
// any committed generated file (the property that separates it from `update`).
func TestRunHydrateRuntimePopulatesSlotIdempotentlyWithoutTouchingCommittedState(t *testing.T) {
	repo := runtimeTestRepo(t)
	binaryPath, _, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(binaryPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".product-loop", "bin")); err != nil {
		t.Fatal(err)
	}

	before := readGeneratedLockBytes(t, repo)

	if err := RunHydrateRuntime(repo); err != nil {
		t.Fatalf("hydration failed: %v", err)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("hydration did not populate the shared slot: %v", err)
	}
	if err := verifyLocalRuntime(repo); err != nil {
		t.Fatalf("hydration did not populate a verified worktree runtime: %v", err)
	}
	if err := Doctor(repo); err != nil {
		t.Fatalf("hydrated repository is unhealthy: %v", err)
	}

	// Idempotent: a second cold hydration from a fully populated state is a no-op.
	if err := RunHydrateRuntime(repo); err != nil {
		t.Fatalf("second hydration was not idempotent: %v", err)
	}

	if after := readGeneratedLockBytes(t, repo); string(before) != string(after) {
		t.Fatalf("hydration mutated committed generated.lock.json:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestRunHydrateRuntimeRefusesRunningVersusPinMismatch pins the incident-
// prevention invariant: hydration must never populate a version-keyed slot with
// a binary whose identity disagrees with the worktree's committed pin.
func TestRunHydrateRuntimeRefusesRunningVersusPinMismatch(t *testing.T) {
	repo := runtimeTestRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, ".product-loop", "bin")); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".product-loop", "generated.lock.json")
	value, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(value, &lock); err != nil {
		t.Fatal(err)
	}
	lock["boatstack_version"] = "v99.0.0"
	value, err = MarshalJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunHydrateRuntime(repo); err == nil || !strings.Contains(err.Error(), "does not match this worktree's pin") {
		t.Fatalf("expected a provenance refusal, got %v", err)
	}
}

func readGeneratedLockBytes(t *testing.T, repo string) []byte {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "generated.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
