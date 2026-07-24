package boatstack

import (
	"strings"
	"testing"
)

// TestSupervisoryControlNeverDeadlocks is the conformance model for the
// nonblocking-supervisory-control invariant:
//
//	Every durable supervisory transition that removes an actuator must leave a
//	bounded actuator reachable in the resulting state that reaches the next valid
//	state (or reverses the transition).
//
// The canonical violation it guards against is a durable pointer/state that
// advances on request-success and thereby revokes the correction actuator for a
// target whose postcondition has not yet been observed — exactly the delivery
// publication bug that stranded a just-published slice. Each subtest names the
// property it asserts and drives the exported delivery/recovery functions
// directly, in the black-box style of TestMutationBoundaryPreservesALegalTrajectory.
func TestSupervisoryControlNeverDeadlocks(t *testing.T) {
	// Property: publication advances the BUILD pointer to the next slice, but the
	// published slice remains re-gateable in place because its PR postcondition is
	// not yet terminal. Advancing "which slice builds next" must not revoke "which
	// slices may still be corrected".
	t.Run("published-open slice stays correctable after the pointer advances", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		gateSlice(t, repo, feature, "phase-one")
		if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1"); err != nil {
			t.Fatalf("publish phase-one: %v", err)
		}

		state := loadDelivery(t, repo, feature)
		if state.ActiveIndex != 1 || state.Slices[1].Status != "BUILD" {
			t.Fatalf("publication did not advance the BUILD pointer: %#v", state)
		}
		if state.Slices[0].Status != "PUBLISHED" || state.Slices[0].PRState != "OPEN" {
			t.Fatalf("published slice did not record an open PR: %#v", state.Slices[0])
		}

		// The bounded correction actuator is still reachable: re-gate phase-one in
		// place through BOTH gates (its changelog baseline anchors to phase-one, not
		// the active slice), and the BUILD pointer must not move.
		if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"}); err != nil {
			t.Fatalf("published-open slice was not re-gateable (test) in place: %v", err)
		}
		if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "review", Status: "PASS"}); err != nil {
			t.Fatalf("published-open slice was not re-gateable (review) in place: %v", err)
		}
		got := loadDelivery(t, repo, feature)
		if got.ActiveIndex != 1 {
			t.Fatalf("re-gating a published-open slice advanced the pointer: %#v", got)
		}
		if got.Slices[0].Status != "REVIEW_PASSED" || got.Slices[0].PRState != "OPEN" {
			t.Fatalf("in-place re-gate did not restore the corrected slice: %#v", got.Slices[0])
		}
	})

	// Property: re-publishing a published-open slice (an --action update of its PR)
	// is idempotent on the BUILD pointer — the advance happens once, on the first
	// REVIEW_PASSED -> PUBLISHED transition, never again.
	t.Run("re-publication does not double-advance the pointer", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		gateSlice(t, repo, feature, "phase-one")
		if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1"); err != nil {
			t.Fatalf("publish phase-one: %v", err)
		}
		// Re-publishing the same already-PUBLISHED, open slice is the --action update
		// path (e.g. after a corrective in-place change): it refreshes the recorded
		// PR URL but must not advance the BUILD pointer a second time.
		if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1?rev=2"); err != nil {
			t.Fatalf("re-publish phase-one: %v", err)
		}
		state := loadDelivery(t, repo, feature)
		if state.ActiveIndex != 1 {
			t.Fatalf("re-publication double-advanced the pointer: ActiveIndex=%d", state.ActiveIndex)
		}
		if state.Slices[0].PRURL != "https://example.invalid/pr/1?rev=2" {
			t.Fatalf("re-publication did not refresh the PR URL: %#v", state.Slices[0])
		}
		if state.Slices[1].Status != "BUILD" {
			t.Fatalf("re-publication disturbed the active slice: %#v", state.Slices[1])
		}
	})

	// Property: once a published slice's PR reaches a terminal state (merged/closed)
	// the in-place actuator is correctly removed, but a bounded FORWARD actuator —
	// the corrective child delivery — remains reachable. Removal of one actuator is
	// only legal because it exposes another; a terminal PR is never a deadlock.
	t.Run("terminal PR refuses in-place correction but offers a corrective child", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		gateSlice(t, repo, feature, "phase-one")
		if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1"); err != nil {
			t.Fatalf("publish phase-one: %v", err)
		}
		// Drive the delivery to fully published directly: phase-two's real gate flow
		// needs per-slice committed diffs this conformance property does not exercise.
		state := loadDelivery(t, repo, feature)
		state.ActiveIndex = 2
		state.Slices[1].Status = "PUBLISHED"
		state.Slices[1].PRState = "OPEN"
		state.Slices[1].PRURL = "https://example.invalid/pr/2"
		if err := saveDeliveryState(repo, state); err != nil {
			t.Fatalf("persist fully-published state: %v", err)
		}

		// The whole delivery is published; simulate the merge observation the
		// recovery/next resolver would make against GitHub.
		restore := stubRecoveryGh(t, "MERGED", "https://example.invalid/pr/2", branchForFeature(feature))
		defer restore()

		status, err := ResolveRecovery(RecoveryStatusOptions{
			Repo: repo, Feature: feature,
			Message: "required checks failed after merge", SourceStage: "ci",
		})
		if err != nil {
			t.Fatalf("resolve recovery: %v", err)
		}
		if status.NextOperation != "draft_corrective_child" {
			t.Fatalf("terminal PR did not route to the bounded forward actuator: %#v", status)
		}

		// The terminal observation is now cached on the slice, so the network-free
		// gate resolver refuses an in-place re-gate and points at the corrective child.
		reloaded := loadDelivery(t, repo, feature)
		if _, _, err := resolveAddressableSlice(reloaded, "phase-two"); err == nil || !strings.Contains(err.Error(), "corrective child") {
			t.Fatalf("terminal slice was still addressable in place: %v", err)
		}
	})

	// Property: the addressable-slice resolver both redirects (pr-context/gate to a
	// named published-open slice) and guards (refuses future, terminal, or unknown
	// slices with a message that names the correct actuator). This is the shared
	// lookup behind record-delivery-gate --slice and pr-context --slice.
	t.Run("addressable resolver redirects to correctable slices and guards the rest", func(t *testing.T) {
		state := DeliveryState{
			ActiveIndex: 1,
			Slices: []DeliverySlice{
				{ID: "phase-one", Status: "PUBLISHED", PRState: "OPEN"},
				{ID: "phase-two", Status: "BUILD"},
				{ID: "phase-three", Status: "PENDING"},
			},
		}
		cases := []struct {
			name      string
			sliceID   string
			wantIndex int
			wantErr   string
		}{
			{"empty resolves to the active slice", "", 1, ""},
			{"active slice by name", "phase-two", 1, ""},
			{"published-open slice redirects in place", "phase-one", 0, ""},
			{"future slice is refused", "phase-three", -1, "not active"},
			{"unknown slice is refused", "phase-nine", -1, "does not exist"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				index, slice, err := resolveAddressableSlice(state, tc.sliceID)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if index != tc.wantIndex || slice.ID != state.Slices[tc.wantIndex].ID {
					t.Fatalf("resolved index=%d slice=%s, want index=%d", index, slice.ID, tc.wantIndex)
				}
			})
		}

		// A terminal published slice is refused in place and routed to a corrective child.
		terminal := DeliveryState{
			ActiveIndex: 1,
			Slices: []DeliverySlice{
				{ID: "phase-one", Status: "PUBLISHED", PRState: "MERGED"},
				{ID: "phase-two", Status: "BUILD"},
			},
		}
		if _, _, err := resolveAddressableSlice(terminal, "phase-one"); err == nil || !strings.Contains(err.Error(), "corrective child") {
			t.Fatalf("terminal published slice was addressable in place: %v", err)
		}
	})

	// Property: the safety guard that removes ordinary command authority at
	// exceptional stages still admits a bounded recovery verb at EVERY stage, so no
	// stage transition is a deadlock. (Reinforces
	// TestControlledPhaseTransitionAllowsBoundedRecoveryVerbs as an invariant, not
	// an enumerated fixture.)
	t.Run("a bounded recovery verb is admitted at every stage", func(t *testing.T) {
		stages := []string{"BUILD", "DELIVERY", "SHIP_GATE", "PR_OPEN", "INVALID_STATE", "PUBLISHED", ""}
		for _, stage := range stages {
			admitted := controlledPhaseTransition("boatstack-helper repair-state --repo .", stage) ||
				controlledPhaseTransition("boatstack-helper undo --repo . --mutation abc", stage)
			if !admitted {
				t.Fatalf("stage %q admitted no bounded recovery verb — a deadlock", stage)
			}
		}
	})
}

func gateSlice(t *testing.T, repo, feature, sliceID string) {
	t.Helper()
	for _, gate := range []string{"test", "review"} {
		if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: sliceID, Gate: gate, Status: "PASS"}); err != nil {
			t.Fatalf("record %s gate for %s: %v", gate, sliceID, err)
		}
	}
}

func loadDelivery(t *testing.T, repo, feature string) DeliveryState {
	t.Helper()
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatalf("load delivery state: %v", err)
	}
	return state
}

// stubRecoveryGh replaces the package gh shim with a fixed `gh pr view`
// observation so recovery/next resolution stays network-free and deterministic
// in tests. It returns a restore function.
func stubRecoveryGh(t *testing.T, state, url, branch string) func() {
	t.Helper()
	previous := recoveryGh
	recoveryGh = func(repo string, arguments ...string) (string, error) {
		payload, err := MarshalJSON(map[string]any{
			"state": state, "headRefName": branch, "headRefOid": "deadbeef", "url": url,
		})
		if err != nil {
			return "", err
		}
		return string(payload), nil
	}
	return func() { recoveryGh = previous }
}
