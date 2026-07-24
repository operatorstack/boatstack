package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publishedThenBuilding is the canonical partially-delivered state the incident
// hit: slice "a" shipped (PR open or merged), the pointer advanced, slice "b" is
// mid-build. pr_state is a parameter because real projects were observed to leave
// it empty even on merged PRs, so the fix must key "published" off the pointer and
// Status, never pr_state.
func publishedThenBuilding(prState string) DeliveryState {
	return DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion,
		Feature:       "f",
		PlanLockHash:  "old-lock",
		ActiveIndex:   1,
		Slices: []DeliverySlice{
			{ID: "a", Title: "First", Status: "PUBLISHED", PRURL: "https://x/pr/1", PRState: prState, TaskIDs: []string{"T-1"}, AffectedPaths: []string{"a.go"}},
			{ID: "b", Title: "Second", Status: "TEST_PASSED", TaskIDs: []string{"T-2"}, AffectedPaths: []string{"b.go"}},
		},
	}
}

func TestValidateAmendmentPreservesProgressBoundaries(t *testing.T) {
	base := publishedThenBuilding("OPEN")

	t.Run("tail-only amendment is allowed", func(t *testing.T) {
		newSlices := []DeliverySlice{
			{ID: "a", TaskIDs: []string{"T-1"}, AffectedPaths: []string{"a.go"}},
			{ID: "b", TaskIDs: []string{"T-2"}, AffectedPaths: []string{"b.go", "b-extra.go"}},
		}
		if err := validateAmendmentPreservesProgress(base, newSlices); err != nil {
			t.Fatalf("widening the building tail slice must be allowed: %v", err)
		}
	})

	t.Run("nothing published yet is always allowed", func(t *testing.T) {
		fresh := base
		fresh.ActiveIndex = 0
		newSlices := []DeliverySlice{{ID: "z", TaskIDs: []string{"T-9"}, AffectedPaths: []string{"z.go"}}}
		if err := validateAmendmentPreservesProgress(fresh, newSlices); err != nil {
			t.Fatalf("pre-publication amendment must be allowed: %v", err)
		}
	})

	t.Run("changing a published slice is refused", func(t *testing.T) {
		newSlices := []DeliverySlice{
			{ID: "a", TaskIDs: []string{"T-1"}, AffectedPaths: []string{"a.go", "a-widened.go"}},
			{ID: "b", TaskIDs: []string{"T-2"}, AffectedPaths: []string{"b.go"}},
		}
		err := validateAmendmentPreservesProgress(base, newSlices)
		if err == nil || !strings.Contains(err.Error(), "changes published delivery slice a") || !strings.Contains(err.Error(), "corrective child") {
			t.Fatalf("changing a published slice must route to a corrective child: %v", err)
		}
	})

	t.Run("renaming a published slice is refused", func(t *testing.T) {
		newSlices := []DeliverySlice{
			{ID: "renamed", TaskIDs: []string{"T-1"}, AffectedPaths: []string{"a.go"}},
			{ID: "b", TaskIDs: []string{"T-2"}, AffectedPaths: []string{"b.go"}},
		}
		err := validateAmendmentPreservesProgress(base, newSlices)
		if err == nil || !strings.Contains(err.Error(), "reorders or renames published delivery slice a") {
			t.Fatalf("renaming a published slice must be refused: %v", err)
		}
	})

	t.Run("dropping a published slice is refused", func(t *testing.T) {
		newSlices := []DeliverySlice{}
		err := validateAmendmentPreservesProgress(base, newSlices)
		if err == nil || !strings.Contains(err.Error(), "drops published delivery slice a") {
			t.Fatalf("dropping a published slice must be refused: %v", err)
		}
	})

	t.Run("a fully published delivery stays immutable", func(t *testing.T) {
		done := base
		done.ActiveIndex = len(done.Slices)
		err := validateAmendmentPreservesProgress(done, base.Slices)
		if err == nil || !strings.Contains(err.Error(), "is immutable") {
			t.Fatalf("fully published delivery must remain immutable: %v", err)
		}
	})
}

// TestReconcileAmendedDeliveryStatePreservesPrefixAndPointer is the heart of the
// fix: the published prefix (and everything about it) survives verbatim, the
// pointer does not move, and only the tail adopts the amended definitions — a
// newly added tail slice appears PENDING.
func TestReconcileAmendedDeliveryStatePreservesPrefixAndPointer(t *testing.T) {
	existing := publishedThenBuilding("MERGED")
	newSlices := []DeliverySlice{
		{ID: "a", TaskIDs: []string{"T-1"}, AffectedPaths: []string{"a.go"}},                     // published prefix, unchanged def
		{ID: "b", TaskIDs: []string{"T-2"}, AffectedPaths: []string{"b.go", "b-extra.go"}},        // widened building slice
		{ID: "c", TaskIDs: []string{"T-3"}, AffectedPaths: []string{"c.go"}},                      // freshly added tail slice
	}

	result := reconcileAmendedDeliveryState(existing, newSlices, "new-lock")

	if result.ActiveIndex != 1 {
		t.Fatalf("reconcile moved the BUILD pointer: ActiveIndex=%d want 1", result.ActiveIndex)
	}
	// Published prefix preserved verbatim, including PR bookkeeping.
	if got := result.Slices[0]; got.ID != "a" || got.Status != "PUBLISHED" || got.PRURL != "https://x/pr/1" || got.PRState != "MERGED" {
		t.Fatalf("published prefix not preserved: %#v", got)
	}
	// Building slice adopts the amended definition and restarts at BUILD.
	if got := result.Slices[1]; got.ID != "b" || got.Status != "BUILD" || !equalStrings(got.AffectedPaths, []string{"b.go", "b-extra.go"}) {
		t.Fatalf("building slice not reconciled: %#v", got)
	}
	// New tail slice is PENDING.
	if got := result.Slices[2]; got.ID != "c" || got.Status != "PENDING" {
		t.Fatalf("new tail slice not appended as PENDING: %#v", got)
	}
	if result.PlanLockHash != "new-lock" {
		t.Fatalf("plan lock hash not updated: %q", result.PlanLockHash)
	}
	if !equalStrings(result.PreviousPlanLocks, []string{"old-lock"}) {
		t.Fatalf("previous plan lock not recorded: %#v", result.PreviousPlanLocks)
	}
}

func markPrefixPublished(t *testing.T, repo, feature, sliceID, url, prState string) {
	t.Helper()
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	found := -1
	for i := range state.Slices {
		if state.Slices[i].ID == sliceID {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("slice %s not found in state", sliceID)
	}
	state.Slices[found].Status = "PUBLISHED"
	state.Slices[found].PRURL = url
	state.Slices[found].PRState = prState
	state.ActiveIndex = found + 1
	if state.ActiveIndex < len(state.Slices) {
		state.Slices[state.ActiveIndex].Status = "BUILD"
	}
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
}

func reactivateWithAmendedPlan(t *testing.T, repo, feature string, mutate func(plan map[string]any)) error {
	t.Helper()
	dir := filepath.Join(repo, ".product-loop", "features", feature)
	plan := twoSlicePlan()
	plan["feature_id"] = feature
	plan["spec_path"] = "feature-spec.md"
	if mutate != nil {
		mutate(plan)
	}
	planPath := filepath.Join(dir, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatalf("amended plan is invalid: %v", err)
	}
	approvalPath := filepath.Join(dir, "approval.md")
	writeApprovalReceipt(t, approvalPath, check.Fingerprint)
	return ActivatePlan(ActivationOptions{
		PlanPath:     planPath,
		ApprovalPath: approvalPath,
		OutDir:       filepath.Join(dir, "compiled"),
		OutputPath:   filepath.Join(dir, "plan.lock.json"),
		SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	})
}

// TestReactivationPreservesPublishedPrefixThroughActivatePlan reproduces the
// exact incident end to end: slice one is published/merged, the pointer is at
// slice two, and an approved amendment widens slice two's scope. Before the fix,
// re-activation reset the pointer to 0 and dropped slice one back to BUILD.
func TestReactivationPreservesPublishedPrefixThroughActivatePlan(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	markPrefixPublished(t, repo, feature, "phase-one", "https://example.invalid/pr/328", "MERGED")

	err := reactivateWithAmendedPlan(t, repo, feature, func(plan map[string]any) {
		second := plan["tasks"].([]any)[1].(map[string]any)
		second["affected_paths"] = []any{"second.go", "second_extra.go"}
	})
	if err != nil {
		t.Fatalf("amending the building tail slice must succeed: %v", err)
	}

	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveIndex != 1 {
		t.Fatalf("re-activation reset the pointer: ActiveIndex=%d want 1", state.ActiveIndex)
	}
	if s := state.Slices[0]; s.ID != "phase-one" || s.Status != "PUBLISHED" || s.PRURL != "https://example.invalid/pr/328" || s.PRState != "MERGED" {
		t.Fatalf("published+merged slice one was not preserved: %#v", s)
	}
	if s := state.Slices[1]; s.ID != "phase-two" || s.Status != "BUILD" {
		t.Fatalf("building slice two not preserved as active: %#v", s)
	}
	if !containsString(state.Slices[1].AffectedPaths, "second_extra.go") {
		t.Fatalf("amended scope not adopted by the tail slice: %#v", state.Slices[1].AffectedPaths)
	}
	if len(state.PreviousPlanLocks) != 1 {
		t.Fatalf("previous plan lock not recorded on amendment: %#v", state.PreviousPlanLocks)
	}
}

// TestReactivationRefusalLeavesPriorStateIntact proves the guard runs before the
// transactional promote, so refusing to alter a published slice leaves both the
// plan lock and the delivery state byte-for-byte unchanged (no half-apply).
func TestReactivationRefusalLeavesPriorStateIntact(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	markPrefixPublished(t, repo, feature, "phase-one", "https://example.invalid/pr/328", "OPEN")

	dir := filepath.Join(repo, ".product-loop", "features", feature)
	lockPath := filepath.Join(dir, "plan.lock.json")
	statePath, err := deliveryStatePath(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = reactivateWithAmendedPlan(t, repo, feature, func(plan map[string]any) {
		first := plan["tasks"].([]any)[0].(map[string]any)
		first["affected_paths"] = []any{"feature.go", "feature_widened.go"} // touches the PUBLISHED slice
	})
	if err == nil || !strings.Contains(err.Error(), "corrective child") {
		t.Fatalf("amending a published slice must be refused with a corrective-child directive: %v", err)
	}

	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(lockBefore) != string(lockAfter) {
		t.Fatalf("refused re-activation half-applied the plan lock:\nbefore=%s\nafter=%s", lockBefore, lockAfter)
	}
	if string(stateBefore) != string(stateAfter) {
		t.Fatalf("refused re-activation mutated delivery state:\nbefore=%s\nafter=%s", stateBefore, stateAfter)
	}
}

// TestReactivationIdempotentAndBenignPrePublish covers the two non-destructive
// paths that must keep working: re-activating the identical plan is a no-op, and
// amending before anything is published simply recomputes at the head.
func TestReactivationIdempotentAndBenignPrePublish(t *testing.T) {
	t.Run("identical re-activation is a no-op", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		statePath, err := deliveryStatePath(repo, feature)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := reactivateWithAmendedPlan(t, repo, feature, nil); err != nil {
			t.Fatalf("identical re-activation errored: %v", err)
		}
		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("identical re-activation changed state:\nbefore=%s\nafter=%s", before, after)
		}
	})

	t.Run("pre-publication amendment recomputes at the head", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		if err := reactivateWithAmendedPlan(t, repo, feature, func(plan map[string]any) {
			second := plan["tasks"].([]any)[1].(map[string]any)
			second["affected_paths"] = []any{"second.go", "second_extra.go"}
		}); err != nil {
			t.Fatalf("pre-publication amendment must succeed: %v", err)
		}
		state, err := LoadDeliveryState(repo, feature)
		if err != nil {
			t.Fatal(err)
		}
		if state.ActiveIndex != 0 || state.Slices[0].Status != "BUILD" {
			t.Fatalf("pre-publication amendment mis-seated the head: %#v", state)
		}
		if !containsString(state.Slices[1].AffectedPaths, "second_extra.go") {
			t.Fatalf("amended tail scope not adopted: %#v", state.Slices[1].AffectedPaths)
		}
	})
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
