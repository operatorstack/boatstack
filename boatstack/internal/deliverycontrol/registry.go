package deliverycontrol

// registry is the single declaration of Boatstack's delivery transitions,
// mirroring ../../notes/delivery-control-inventory.md. Each HandlerRef names a
// real exported function in package boatstack, and each CLIVerb names a real
// dispatch verb in cmd/boatstack-helper; the conformance tests keep both
// faithful in each direction. The registry is the authoritative projection
// source: the flow commands consume it at runtime (NextControl resolves the
// prescribed command through Transition(id).CLIVerb), and the coverage
// conformance test asserts it covers exactly the real delivery machine — no real
// mutation verb without a row, no row without a real verb.
var registry = []TransitionDescriptor{
	{
		ID: "delivery.activate", From: []StateID{StateUninitialized}, To: StateBuild,
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "ActivatePlan", CLIVerb: "activate-plan",
		Note: "Requires CheckPlan + repository-safety PASS and a human/policy approval receipt; writes the plan lock via the mutation boundary. Reversible via delivery.undo while no gate receipt exists.",
	},
	{
		ID: "delivery.record_gate_test", From: []StateID{StateBuild}, To: StateTestPassed,
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "RecordDeliveryGate", CLIVerb: "record-delivery-gate",
		Note: "Records the test gate (PASS/PASS_WITH_GAPS) against a validated plan lock and a matching evidence ledger. Reversible via record-change re-gate.",
	},
	{
		ID: "delivery.record_gate_review", From: []StateID{StateTestPassed}, To: StateReviewPassed,
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "RecordDeliveryGate", CLIVerb: "record-delivery-gate",
		Note: "Records the review gate; requires prior TEST_PASSED with a matching diff and reviewer identity/method on high-risk paths. Clears rework mode.",
	},
	{
		ID: "delivery.record_change", From: []StateID{StateTestPassed, StateReviewPassed, StatePublished}, To: StateBuild,
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "RecordChangeObservation", CLIVerb: "record-change",
		Note: "Rework resets the addressable slice to BUILD (bounded by RepairAttempt<3); amendment/plan-invalid set Mode; a fully-published delivery emits a corrective child with no state mutation.",
	},
	{
		ID: "delivery.publish", From: []StateID{StateReviewPassed, StatePublished}, To: StatePublished,
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: false,
		HandlerRef: "PublishPR", CLIVerb: "publish-pr",
		Note: "Publishes the reviewed slice behind a human-confirmed preview fingerprint and advances ActiveIndex to the next slice. Re-publishing a PUBLISHED-open slice is idempotent and does not advance the pointer.",
	},
	{
		ID: "delivery.undo", From: []StateID{StateBuild}, To: StateUninitialized,
		Kind: KindReversibleMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "UndoManagedMutation", CLIVerb: "undo",
		Note: "Reverses a plan-activation/compiled-plan mutation through the boundary (closed under inversion, so redo is undo of the returned receipt). Refused once any gate receipt exists (would strand delivery state).",
	},
	{
		ID: "delivery.discard_delivery", From: []StateID{StatePending, StateBuild, StateTestPassed, StateReviewPassed, StatePublished}, To: StateDiscarded,
		Kind: KindReversibleMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "DiscardDelivery", CLIVerb: "discard-delivery",
		Note: "Archives (never deletes) the delivery state directory. Refuses a slice with a set PRState unless --force; preserves published authority and git/lock/merged history.",
	},
	{
		ID: "delivery.repair_state", From: []StateID{StateInvalid}, To: StateUninitialized,
		Kind: KindRecovery, CostClass: CostRecovery, Reversible: true,
		HandlerRef: "RepairState", CLIVerb: "repair-state",
		Note: "Quarantines a malformed unregistered feature draft by moving it aside; refuses when a plan lock, pr.md, managed state, tracked files, or an active/published delivery is present. Typed and bounded — never a generic bypass.",
	},
	{
		ID: "delivery.ignore_delivery", From: []StateID{StatePending, StateBuild, StateTestPassed, StateReviewPassed, StatePublished}, To: "",
		Kind: KindCommittedMutation, CostClass: CostMutation, Reversible: true,
		HandlerRef: "IgnoreDelivery", CLIVerb: "ignore-delivery",
		Note: "Appends the feature to project.json workflow.ignored_deliveries, filtering it from ResolveNext and publication authority. Changes no slice status; reversible by removing the entry.",
	},
	{
		ID: "delivery.status", From: []StateID{StateBuild, StateTestPassed, StateReviewPassed, StatePublished}, To: "",
		Kind: KindObserve, CostClass: CostObserve, Reversible: false,
		HandlerRef: "CurrentDeliveryState", CLIVerb: "delivery-status",
		Note: "Reads delivery state and validates the plan lock. No state change.",
	},
	{
		ID: "delivery.check_ship", From: []StateID{StateReviewPassed, StatePublished}, To: "",
		Kind: KindQuery, CostClass: CostQuery, Reversible: false,
		HandlerRef: "CheckDeliveryReadyForShip", CLIVerb: "pr-context",
		Note: "Re-checks receipt freshness and gate policy for the addressable slice and returns its PR sources via pr-context. No state change.",
	},
	{
		ID: "delivery.next", From: nil, To: "",
		Kind: KindObserve, CostClass: CostObserve, Reversible: false,
		HandlerRef: "ResolveNext", CLIVerb: "next-status",
		Note: "Derives the recommended next move. Read-only, except that the published branch caches an observed terminal PRState — and, under the merged terminal, a fired goal-escape demotion — as a best-effort side effect (a known bypass, modeled not fixed).",
	},
	{
		ID: "delivery.recovery_status", From: []StateID{StateBuild, StateTestPassed, StateReviewPassed, StatePublished}, To: "",
		Kind: KindObserve, CostClass: CostObserve, Reversible: false,
		HandlerRef: "ResolveRecovery", CLIVerb: "recovery-status",
		Note: "Derives a correction decision; carries no edit/approve/publish authority. Read-only, except the same best-effort terminal-PRState cache.",
	},
}

// Transitions returns a copy of the declared transition set.
func Transitions() []TransitionDescriptor {
	out := make([]TransitionDescriptor, len(registry))
	copy(out, registry)
	return out
}

// Transition returns the descriptor with the given ID.
func Transition(id TransitionID) (TransitionDescriptor, bool) {
	for _, t := range registry {
		if t.ID == id {
			return t, true
		}
	}
	return TransitionDescriptor{}, false
}

// HandlerRefs returns the distinct real function names the registry declares.
func HandlerRefs() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range registry {
		if !seen[t.HandlerRef] {
			seen[t.HandlerRef] = true
			out = append(out, t.HandlerRef)
		}
	}
	return out
}
