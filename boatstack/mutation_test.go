package boatstack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeRepoFile(t *testing.T, repo, rel, content string) string {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return SHA256Bytes([]byte(content))
}

func readRepoFile(t *testing.T, repo, rel string) (string, bool) {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(value), true
}

func singleFileMutation(rel string, candidate string, base map[string]string) MutationSet {
	return MutationSet{
		Protocol:   MutationProtocol,
		Kind:       "test-artifact",
		Scope:      []string{rel},
		Base:       base,
		Authority:  MutationAuthority{Expected: "authority-1", Observed: "authority-1"},
		Operations: []MutationOperation{{Path: rel, Candidate: []byte(candidate)}},
	}
}

// TestMutationBoundaryConformance is the acceptance suite for the transactional
// mutation boundary. Each subtest is one of the six required properties stated
// as a black-box behavior of the generic primitive, independent of any artifact.
func TestMutationBoundaryConformance(t *testing.T) {
	t.Run("invalid candidate leaves accepted state unchanged", func(t *testing.T) {
		repo := nextTestRepo(t)
		base := writeRepoFile(t, repo, "src/auth.go", "original")
		m := singleFileMutation("src/auth.go", "rewritten", map[string]string{"src/auth.go": base})
		m.PreCheck = func(candidate map[string][]byte) error { return fmt.Errorf("candidate is not well-formed") }

		receipt, err := ApplyMutation(repo, m)
		if !errors.Is(err, ErrMutationInvalidCandidate) {
			t.Fatalf("expected invalid-candidate refusal, got %v", err)
		}
		if receipt.Status != "REJECTED" {
			t.Fatalf("expected REJECTED receipt, got %q", receipt.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "original" {
			t.Fatalf("accepted state changed after invalid candidate: %q", got)
		}
	})

	t.Run("valid candidate promotes exact bytes", func(t *testing.T) {
		repo := nextTestRepo(t)
		m := singleFileMutation("src/auth.go", "exact-new-bytes", nil)

		receipt, err := ApplyMutation(repo, m)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Status != "APPLIED" {
			t.Fatalf("expected APPLIED, got %q", receipt.Status)
		}
		got, ok := readRepoFile(t, repo, "src/auth.go")
		if !ok || got != "exact-new-bytes" {
			t.Fatalf("promoted bytes are not exact: %q (present=%v)", got, ok)
		}
		if receipt.Changes[0].AfterSHA256 != SHA256Bytes([]byte("exact-new-bytes")) {
			t.Fatal("receipt after-hash does not match the promoted bytes")
		}
	})

	t.Run("post-write validation failure rolls back", func(t *testing.T) {
		repo := nextTestRepo(t)
		base := writeRepoFile(t, repo, "src/auth.go", "original")
		m := singleFileMutation("src/auth.go", "rewritten", map[string]string{"src/auth.go": base})
		m.PostCheck = func() error {
			// The candidate is on disk at this point; prove rollback undoes it.
			if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "rewritten" {
				t.Fatalf("post-check ran before promotion: %q", got)
			}
			return fmt.Errorf("verification failed")
		}

		receipt, err := ApplyMutation(repo, m)
		if !errors.Is(err, ErrMutationVerificationFailed) {
			t.Fatalf("expected verification-failed rollback, got %v", err)
		}
		if receipt.Status != "ROLLED_BACK" {
			t.Fatalf("expected ROLLED_BACK, got %q", receipt.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "original" {
			t.Fatalf("rollback did not restore exact bytes: %q", got)
		}
	})

	t.Run("duplicate application is idempotent", func(t *testing.T) {
		repo := nextTestRepo(t)
		m := singleFileMutation("src/auth.go", "once", nil)

		first, err := ApplyMutation(repo, m)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ApplyMutation(repo, m)
		if err != nil {
			t.Fatal(err)
		}
		if first.MutationID != second.MutationID {
			t.Fatalf("idempotent replay changed identity: %s vs %s", first.MutationID, second.MutationID)
		}
		if second.Status != "APPLIED" {
			t.Fatalf("replay was not reported APPLIED: %q", second.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "once" {
			t.Fatalf("idempotent replay corrupted the artifact: %q", got)
		}
	})

	t.Run("changed base rejects the stale mutation", func(t *testing.T) {
		repo := nextTestRepo(t)
		base := writeRepoFile(t, repo, "src/auth.go", "original")
		// The base drifts after it was read into the proposal.
		writeRepoFile(t, repo, "src/auth.go", "changed-underneath")
		m := singleFileMutation("src/auth.go", "rewritten", map[string]string{"src/auth.go": base})

		receipt, err := ApplyMutation(repo, m)
		if !errors.Is(err, ErrMutationStaleBase) {
			t.Fatalf("expected stale-base refusal, got %v", err)
		}
		if receipt.Status != "REJECTED" {
			t.Fatalf("expected REJECTED, got %q", receipt.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "changed-underneath" {
			t.Fatalf("stale mutation mutated state: %q", got)
		}
	})

	t.Run("changed supervisor authority rejects the outdated mutation", func(t *testing.T) {
		repo := nextTestRepo(t)
		base := writeRepoFile(t, repo, "src/auth.go", "original")
		m := singleFileMutation("src/auth.go", "rewritten", map[string]string{"src/auth.go": base})
		m.Authority = MutationAuthority{Expected: "authorized-under-S1", Observed: "supervisor-now-S2"}

		receipt, err := ApplyMutation(repo, m)
		if !errors.Is(err, ErrMutationOutdatedAuthority) {
			t.Fatalf("expected outdated-authority refusal, got %v", err)
		}
		if receipt.Status != "REJECTED" {
			t.Fatalf("expected REJECTED, got %q", receipt.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "original" {
			t.Fatalf("outdated-authority mutation mutated state: %q", got)
		}
	})
}

// TestMutationBoundaryPreservesALegalTrajectory encodes the nonblocking
// supervisory-control invariant: a deterministic refusal never deadlocks. A
// mutation recomputed against the current base and authority still reaches the
// next valid state.
func TestMutationBoundaryPreservesALegalTrajectory(t *testing.T) {
	t.Run("after stale-base rejection", func(t *testing.T) {
		repo := nextTestRepo(t)
		stale := writeRepoFile(t, repo, "src/auth.go", "v1")
		current := writeRepoFile(t, repo, "src/auth.go", "v2")

		if _, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "v3", map[string]string{"src/auth.go": stale})); !errors.Is(err, ErrMutationStaleBase) {
			t.Fatalf("expected stale-base refusal, got %v", err)
		}
		// Recompute the proposal against current state — a legal trajectory remains.
		receipt, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "v3", map[string]string{"src/auth.go": current}))
		if err != nil {
			t.Fatalf("recomputed mutation was blocked: %v", err)
		}
		if receipt.Status != "APPLIED" {
			t.Fatalf("recomputed mutation did not apply: %q", receipt.Status)
		}
		if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "v3" {
			t.Fatalf("recomputed mutation did not reach the next state: %q", got)
		}
	})

	t.Run("after outdated-authority rejection", func(t *testing.T) {
		repo := nextTestRepo(t)
		base := writeRepoFile(t, repo, "src/auth.go", "v1")
		outdated := singleFileMutation("src/auth.go", "v2", map[string]string{"src/auth.go": base})
		outdated.Authority = MutationAuthority{Expected: "S1", Observed: "S2"}
		if _, err := ApplyMutation(repo, outdated); !errors.Is(err, ErrMutationOutdatedAuthority) {
			t.Fatalf("expected outdated-authority refusal, got %v", err)
		}
		// Re-authorize against the current supervisor token — same identity, now legal.
		current := singleFileMutation("src/auth.go", "v2", map[string]string{"src/auth.go": base})
		current.Authority = MutationAuthority{Expected: "S2", Observed: "S2"}
		receipt, err := ApplyMutation(repo, current)
		if err != nil {
			t.Fatalf("re-authorized mutation was blocked: %v", err)
		}
		if receipt.Status != "APPLIED" {
			t.Fatalf("re-authorized mutation did not apply: %q", receipt.Status)
		}
	})
}

func TestApplyMutationRejectsOutOfScopePath(t *testing.T) {
	repo := nextTestRepo(t)
	m := MutationSet{
		Protocol: MutationProtocol, Kind: "test-artifact",
		Scope:      []string{"src/allowed.go"},
		Authority:  MutationAuthority{Expected: "a", Observed: "a"},
		Operations: []MutationOperation{{Path: "src/secret.go", Candidate: []byte("x")}},
	}
	if _, err := ApplyMutation(repo, m); !errors.Is(err, ErrMutationScope) {
		t.Fatalf("expected scope refusal, got %v", err)
	}
	if _, ok := readRepoFile(t, repo, "src/secret.go"); ok {
		t.Fatal("out-of-scope path was written")
	}
}

func TestApplyMutationRejectsPathEscape(t *testing.T) {
	repo := nextTestRepo(t)
	m := singleFileMutation("../escape.go", "x", nil)
	if _, err := ApplyMutation(repo, m); err == nil {
		t.Fatal("path escaping the repository was accepted")
	}
}

func TestApplyMutationRejectsSymlinkComponent(t *testing.T) {
	repo := nextTestRepo(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := singleFileMutation("linked/auth.go", "x", nil)
	if _, err := ApplyMutation(repo, m); err == nil {
		t.Fatal("mutation through a symlinked component was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "auth.go")); err == nil {
		t.Fatal("mutation escaped through a symlink")
	}
}

func TestApplyMutationMultiFileRollbackIsAllOrNothing(t *testing.T) {
	repo := nextTestRepo(t)
	baseA := writeRepoFile(t, repo, "a.txt", "A0")
	baseB := writeRepoFile(t, repo, "b.txt", "B0")
	// c.txt is absent before the mutation.
	m := MutationSet{
		Protocol: MutationProtocol, Kind: "test-artifact",
		Scope:     []string{"a.txt", "b.txt", "c.txt"},
		Base:      map[string]string{"a.txt": baseA, "b.txt": baseB},
		Authority: MutationAuthority{Expected: "a", Observed: "a"},
		Operations: []MutationOperation{
			{Path: "a.txt", Candidate: []byte("A1")},
			{Path: "b.txt", Candidate: []byte("B1")},
			{Path: "c.txt", Candidate: []byte("C1")},
		},
		PostCheck: func() error { return fmt.Errorf("reject the whole batch") },
	}
	if _, err := ApplyMutation(repo, m); !errors.Is(err, ErrMutationVerificationFailed) {
		t.Fatalf("expected batch rollback, got %v", err)
	}
	if got, _ := readRepoFile(t, repo, "a.txt"); got != "A0" {
		t.Fatalf("a.txt not restored: %q", got)
	}
	if got, _ := readRepoFile(t, repo, "b.txt"); got != "B0" {
		t.Fatalf("b.txt not restored: %q", got)
	}
	if _, ok := readRepoFile(t, repo, "c.txt"); ok {
		t.Fatal("c.txt was created but existed nowhere before the rolled-back batch")
	}
}

func TestApplyMutationConcurrentDuplicatesApplyOnce(t *testing.T) {
	repo := nextTestRepo(t)
	m := singleFileMutation("src/auth.go", "converged", nil)

	const workers = 8
	var wg sync.WaitGroup
	results := make([]MutationReceipt, workers)
	errs := make([]error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = ApplyMutation(repo, m)
		}(i)
	}
	wg.Wait()

	id := ""
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d failed: %v", i, errs[i])
		}
		if results[i].Status != "APPLIED" {
			t.Fatalf("worker %d saw %q", i, results[i].Status)
		}
		if id == "" {
			id = results[i].MutationID
		} else if results[i].MutationID != id {
			t.Fatalf("concurrent applies diverged in identity: %s vs %s", id, results[i].MutationID)
		}
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "converged" {
		t.Fatalf("concurrent applies corrupted the artifact: %q", got)
	}
}

func TestUndoMutationRestoresExactBytes(t *testing.T) {
	repo := nextTestRepo(t)
	base := writeRepoFile(t, repo, "src/auth.go", "before")
	receipt, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "after", map[string]string{"src/auth.go": base}))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "after" {
		t.Fatalf("setup did not apply: %q", got)
	}
	undone, err := UndoMutation(repo, receipt.MutationID)
	if err != nil {
		t.Fatal(err)
	}
	// Undo is itself a mutation: it lands as an APPLIED inverse receipt whose own
	// inverse is the original post-image (that is what makes redo possible).
	if undone.Status != "APPLIED" {
		t.Fatalf("expected the inverse mutation to be APPLIED, got %q", undone.Status)
	}
	if undone.MutationID == receipt.MutationID {
		t.Fatal("undo did not produce a distinct inverse receipt")
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "before" {
		t.Fatalf("undo did not restore exact bytes: %q", got)
	}
}

func TestUndoMutationRemovesFileThatWasAbsentBefore(t *testing.T) {
	repo := nextTestRepo(t)
	receipt, err := ApplyMutation(repo, singleFileMutation("src/new.go", "created", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UndoMutation(repo, receipt.MutationID); err != nil {
		t.Fatal(err)
	}
	if _, ok := readRepoFile(t, repo, "src/new.go"); ok {
		t.Fatal("undo did not remove a file that was absent before the mutation")
	}
}

// TestUndoMutationIsReversibleAsRedo proves the boundary is closed under
// inversion: undoing an undo receipt restores the original after-image. Because
// undo lands as an ordinary APPLIED mutation, redo needs no separate verb — it is
// just undo of the undo receipt.
func TestUndoMutationIsReversibleAsRedo(t *testing.T) {
	repo := nextTestRepo(t)
	base := writeRepoFile(t, repo, "src/auth.go", "before")
	applied, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "after", map[string]string{"src/auth.go": base}))
	if err != nil {
		t.Fatal(err)
	}
	undone, err := UndoMutation(repo, applied.MutationID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "before" {
		t.Fatalf("undo did not restore the before-image: %q", got)
	}
	// Redo = undo of the undo receipt → the original after-image returns.
	redone, err := UndoMutation(repo, undone.MutationID)
	if err != nil {
		t.Fatal(err)
	}
	if redone.MutationID == undone.MutationID || redone.Status != "APPLIED" {
		t.Fatalf("redo did not land as a distinct APPLIED inverse: %+v", redone)
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "after" {
		t.Fatalf("redo did not restore the after-image: %q", got)
	}
}

// TestUndoMutationReplayIsIdempotent proves that re-issuing the same undo is a
// no-op: the inverse MutationSet is content-addressed, so the second call replays
// the first undo receipt instead of forking a new one or clobbering later state.
func TestUndoMutationReplayIsIdempotent(t *testing.T) {
	repo := nextTestRepo(t)
	base := writeRepoFile(t, repo, "src/auth.go", "before")
	applied, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "after", map[string]string{"src/auth.go": base}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := UndoMutation(repo, applied.MutationID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := UndoMutation(repo, applied.MutationID)
	if err != nil {
		t.Fatal(err)
	}
	if first.MutationID != second.MutationID {
		t.Fatalf("undo replay forked a new receipt: %s vs %s", first.MutationID, second.MutationID)
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "before" {
		t.Fatalf("undo replay disturbed restored state: %q", got)
	}
}

func TestUndoMutationRejectsDivergedState(t *testing.T) {
	repo := nextTestRepo(t)
	base := writeRepoFile(t, repo, "src/auth.go", "before")
	receipt, err := ApplyMutation(repo, singleFileMutation("src/auth.go", "after", map[string]string{"src/auth.go": base}))
	if err != nil {
		t.Fatal(err)
	}
	// A later change touched the same file: undo must refuse rather than clobber.
	writeRepoFile(t, repo, "src/auth.go", "later-work")
	if _, err := UndoMutation(repo, receipt.MutationID); !errors.Is(err, ErrMutationConflict) {
		t.Fatalf("expected conflict refusal, got %v", err)
	}
	if got, _ := readRepoFile(t, repo, "src/auth.go"); got != "later-work" {
		t.Fatalf("undo clobbered diverged state: %q", got)
	}
}
