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
		StateUninitialized, StateFeatureComplete, StateInvalid, StateDiscarded, StateUnresolved,
	}
}
