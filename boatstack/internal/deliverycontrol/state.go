package deliverycontrol

const (
	// Slice-status states — these string values mirror DeliverySlice.Status
	// literals in the real state machine (delivery.go, next.go). Kept in sync by
	// the parity conformance test (control-law: registry-mirrors-real-transitions).
	StatePending      StateID = "PENDING"
	StateBuild        StateID = "BUILD"
	StateTestPassed   StateID = "TEST_PASSED"
	StateReviewPassed StateID = "REVIEW_PASSED"
	StatePublished    StateID = "PUBLISHED"

	// Composite exceptional states. These are real durable delivery positions,
	// not presentation aliases: DeliveryState.Mode removes the ordinary gate
	// actuators even while the active slice still says BUILD. Keeping them out of
	// this vocabulary made the old liveness proof project a deadlocked delivery
	// back to BUILD and therefore prove the wrong machine.
	StateAmendmentRequired StateID = "AMENDMENT_REQUIRED"
	StateAmendmentDrafted  StateID = "AMENDMENT_DRAFTED"
	StateAmendmentApproved StateID = "AMENDMENT_APPROVED"
	StatePlanInvalid       StateID = "PLAN_INVALID"

	// Boundary states — needed to describe transitions faithfully; not stored as
	// a slice Status.
	StateUninitialized   StateID = "UNINITIALIZED"    // no managed delivery yet
	StateFeatureComplete StateID = "FEATURE_COMPLETE" // published + merged (ActiveIndex >= len(Slices))
	StateInvalid         StateID = "INVALID"          // malformed / blocked delivery
	StateDiscarded       StateID = "DISCARDED"        // archived via discard-delivery

	// StateUnresolved is the sentinel an oracle returns for unknown/invalid
	// state. A controller must never fabricate a path; it returns UNRESOLVED.
	StateUnresolved StateID = "UNRESOLVED"
)

// SliceStatusStates returns the states that mirror real DeliverySlice.Status
// literals, in lifecycle order. The parity test pins this set to the strings the
// real state machine actually uses.
func SliceStatusStates() []StateID {
	return []StateID{StatePending, StateBuild, StateTestPassed, StateReviewPassed, StatePublished}
}

// States returns every declared state.
func States() []StateID {
	return []StateID{
		StatePending, StateBuild, StateTestPassed, StateReviewPassed, StatePublished,
		StateAmendmentRequired, StateAmendmentDrafted, StateAmendmentApproved, StatePlanInvalid,
		StateUninitialized, StateFeatureComplete, StateInvalid, StateDiscarded, StateUnresolved,
	}
}
