package boatstack

import "testing"

// Regression for the published-slice correction trap: after a slice publishes and
// the BUILD pointer advances, the actuator layer (resolveAddressableSlice) keeps
// the published-but-open slice correctable in place, but the advisor/router/safety
// layer used to decide purely on the whole-delivery pointer (ActiveIndex <
// len(Slices)) and mis-routed a correction for the published slice to the active
// slice — repairing the wrong slice and reporting the fix branch as unrelated.
//
// These tests pin the advisor layer to the SAME addressable set the actuators use.

// publishOpenFirstSlice activates a two-slice delivery, gates and publishes the
// first slice, and returns the repo/feature with the pointer advanced to the
// still-building second slice. The first slice is PUBLISHED with an open PR; the
// test repo's git branch (feat/reviewer-ready) is the first slice's head branch.
func publishOpenFirstSlice(t *testing.T) (string, string) {
	t.Helper()
	repo, feature := activateTwoSliceDelivery(t)
	gateSlice(t, repo, feature, "phase-one")
	if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1"); err != nil {
		t.Fatalf("publish phase-one: %v", err)
	}
	state := loadDelivery(t, repo, feature)
	if state.ActiveIndex != 1 || state.Slices[0].Status != "PUBLISHED" || state.Slices[0].PRState != "OPEN" {
		t.Fatalf("setup did not leave phase-one published-open with the pointer on phase-two: %#v", state)
	}
	return repo, feature
}

func TestRecoveryRoutesPublishedOpenSliceInPlace(t *testing.T) {
	repo, feature := publishOpenFirstSlice(t)

	// The correction is reported from phase-one's own branch (feat/reviewer-ready),
	// which is the test repo's current branch — no explicit --feature is needed.
	status, err := ResolveRecovery(RecoveryStatusOptions{
		Repo: repo, Feature: feature,
		Message: "required checks failed on the published PR", SourceStage: "ci",
	})
	if err != nil {
		t.Fatalf("resolve recovery: %v", err)
	}
	if status.NextOperation != "repair_published_slice" {
		t.Fatalf("correction for a published-open slice was not routed in place: %#v", status)
	}
	if status.Slice != "phase-one" {
		t.Fatalf("router named the wrong slice: got %q, want phase-one", status.Slice)
	}
	if status.Lifecycle != "PUBLISHED_OPEN" {
		t.Fatalf("router did not mark the published-open lifecycle: %#v", status)
	}
}

func TestRecordChangeBindsPublishedOpenSliceWithoutDisturbingActive(t *testing.T) {
	assertInPlace := func(t *testing.T, options ChangeObservationOptions) {
		t.Helper()
		repo, feature := publishOpenFirstSlice(t)
		options.Repo = repo
		options.Feature = feature
		options.Message = "required checks failed on the published PR"
		options.SourceStage = "ci"
		options.Classification = "verification_repair"

		observation, _, err := RecordChangeObservation(options)
		if err != nil {
			t.Fatalf("record change: %v", err)
		}
		if observation.Outcome != "RESUME_PUBLISHED_SLICE" {
			t.Fatalf("change bound to the wrong path: %#v", observation)
		}
		if observation.SliceID != "phase-one" {
			t.Fatalf("change bound to the wrong slice: got %q, want phase-one", observation.SliceID)
		}
		got := loadDelivery(t, repo, feature)
		// The active slice's build loop must be untouched.
		if got.ActiveIndex != 1 {
			t.Fatalf("recording a published-slice correction moved the pointer: %#v", got)
		}
		if got.Slices[1].Status != "BUILD" {
			t.Fatalf("active slice was disturbed: %#v", got.Slices[1])
		}
		if got.RepairAttempt != 0 {
			t.Fatalf("active repair budget was consumed by a published-slice correction: %d", got.RepairAttempt)
		}
		if got.Mode == "REWORK" {
			t.Fatalf("published-slice correction hijacked the delivery mode: %q", got.Mode)
		}
		// The published slice is driven back to an in-place re-gate; its open PR
		// identity is preserved so publish-pr --action update can target it.
		if got.Slices[0].Status != "BUILD" {
			t.Fatalf("published slice was not reset for an in-place re-gate: %#v", got.Slices[0])
		}
		if got.Slices[0].PRState != "OPEN" {
			t.Fatalf("in-place re-gate dropped the published PR identity: %#v", got.Slices[0])
		}
	}

	t.Run("explicit --slice", func(t *testing.T) {
		assertInPlace(t, ChangeObservationOptions{SliceID: "phase-one"})
	})
	t.Run("resolved from the correction branch", func(t *testing.T) {
		assertInPlace(t, ChangeObservationOptions{})
	})
}

// TestActiveSliceCorrectionStillRoutesToRepairActive guards the ordinary path: a
// correction on the active slice's own branch, with no earlier published-open
// slice, must still route to repair_active unchanged.
func TestActiveSliceCorrectionStillRoutesToRepairActive(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	// Gate (but do not publish) phase-one so it becomes the branch-matched ACTIVE
	// slice; the pointer stays at index 0.
	gateSlice(t, repo, feature, "phase-one")

	status, err := ResolveRecovery(RecoveryStatusOptions{
		Repo: repo, Feature: feature,
		Message: "a review note on the active slice", SourceStage: "review",
	})
	if err != nil {
		t.Fatalf("resolve recovery: %v", err)
	}
	if status.NextOperation != "repair_active" {
		t.Fatalf("active-slice correction no longer routes to repair_active: %#v", status)
	}
}

// TestSafetyFindingNamesPublishedOpenSlice pins the publication-bypass finding to
// the addressable slice the branch owns: a denied push on the published slice's
// branch must name that slice and read as the current branch, not the active slice
// with relation=unrelated.
func TestSafetyFindingNamesPublishedOpenSlice(t *testing.T) {
	repo, _ := publishOpenFirstSlice(t)

	finding, blocked := publicationBypassFinding(repo, "denied direct push", "tool-input")
	if !blocked {
		t.Fatalf("expected a publication-bypass finding for the active delivery")
	}
	if finding.BranchRelation != "current_branch" {
		t.Fatalf("published-slice fix branch read as %q, want current_branch", finding.BranchRelation)
	}
	if finding.BlockingSlice != "phase-one" {
		t.Fatalf("finding named the wrong slice: got %q, want phase-one", finding.BlockingSlice)
	}
}

// TestAdvisorAndActuatorResolveSameSlice is the anti-drift invariant: for a given
// branch, the advisor resolver (by branch) and the actuator resolver (by id) must
// select the same slice. This is the structural guarantee that the advisor layer
// cannot silently diverge from the actuator layer again.
func TestAdvisorAndActuatorResolveSameSlice(t *testing.T) {
	state := DeliveryState{
		ActiveIndex: 1,
		Slices: []DeliverySlice{
			{ID: "phase-one", Status: "PUBLISHED", PRState: "OPEN", HeadBranch: "feat/phase-one"},
			{ID: "phase-two", Status: "BUILD", HeadBranch: "feat/phase-two"},
		},
	}
	for _, slice := range state.Slices {
		byBranch, addressable, ok := resolveAddressableSliceByBranch(state, slice.HeadBranch)
		if !ok {
			t.Fatalf("advisor resolver could not resolve branch %q", slice.HeadBranch)
		}
		byID, _, err := resolveAddressableSlice(state, slice.ID)
		if err != nil {
			t.Fatalf("actuator resolver rejected slice %q: %v", slice.ID, err)
		}
		if byBranch != byID {
			t.Fatalf("advisor/actuator drift for %q: branch->%d, id->%d", slice.ID, byBranch, byID)
		}
		if addressable.ID != slice.ID {
			t.Fatalf("advisor resolver returned %q for branch %q", addressable.ID, slice.HeadBranch)
		}
	}
}
