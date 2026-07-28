package boatstack

import "strings"

// Goal escapes bound the merged-terminal pursuit: pursuing a merge
// autonomously is trustworthy only while the world stays inside the contract
// the goal was granted under. When a disturbance ends that contract — the fix
// budget is spent, a reviewer requested changes, the base conflicts — the
// pursuit DEMOTES to the operator and stops: the actor becomes operator,
// nothing further is prescribed, and the demotion is persisted best-effort so
// it holds offline in a fresh session. An escape is cleared only by the next
// explicit correction cycle (record-change), which is an operator-authorized
// act in the protocol. Escapes exist only under the merged terminal; the
// published default never evaluates or records them.
// control-law: goal-escape-demotes-to-operator-and-stops
const (
	EscapeFixAttemptsExhausted = "fix_attempts_exhausted"
	EscapeChangesRequested     = "changes_requested"
	EscapeBaseConflicts        = "base_conflicts"
)

// postPublishFixBudget bounds how many post-publish correction cycles a
// published slice may consume before the pursuit hands back to the operator.
// It mirrors the active-slice repair budget (RepairAttempt < 3).
const postPublishFixBudget = 3

// evaluateGoalEscape derives the escape for one published slice from its
// persisted counters and one live observation. Pure and offline-safe: a
// previously persisted escape is sticky regardless of what gh says now (a
// re-approval without a recorded correction does not silently re-arm the
// pursuit), and the attempts bound needs no network at all.
func evaluateGoalEscape(slice DeliverySlice, pr publishedPRObservation) string {
	if persisted := strings.TrimSpace(slice.GoalEscape); persisted != "" {
		return persisted
	}
	if slice.PostPublishFixAttempts >= postPublishFixBudget {
		return EscapeFixAttemptsExhausted
	}
	if strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "CHANGES_REQUESTED") {
		return EscapeChangesRequested
	}
	// DIRTY is GitHub's "the branch conflicts with the base". BEHIND is
	// deliberately not an escape: it is often auto-resolvable and already
	// classifies to an operator-owned Unknown phase without stickiness.
	if strings.EqualFold(strings.TrimSpace(pr.MergeState), "DIRTY") {
		return EscapeBaseConflicts
	}
	return ""
}

// goalEscapeReason renders one escape as the operator-facing explanation.
func goalEscapeReason(escape string) string {
	switch escape {
	case EscapeFixAttemptsExhausted:
		return "the post-publish fix budget is spent"
	case EscapeChangesRequested:
		return "a reviewer requested changes"
	case EscapeBaseConflicts:
		return "the branch conflicts with its base"
	default:
		return "the pursuit contract ended"
	}
}

// persistGoalEscape caches a fired escape on the slice it belongs to, exactly
// like persistObservedTerminalPRState caches a terminal lifecycle: a bounded,
// best-effort write of an already-derived fact, so the demotion is sticky in
// a fresh offline session. Failures are swallowed — the demotion holds for
// this resolution regardless.
func persistGoalEscape(repo string, state DeliveryState, escape string) {
	if strings.TrimSpace(escape) == "" {
		return
	}
	for i := len(state.Slices) - 1; i >= 0; i-- {
		slice := state.Slices[i]
		if slice.Status != "PUBLISHED" {
			continue
		}
		if strings.TrimSpace(slice.GoalEscape) == escape {
			return
		}
		state.Slices[i].GoalEscape = escape
		_ = saveDeliveryState(repo, state)
		return
	}
}

// bumpPostPublishFixCycle advances the per-slice correction-cycle bookkeeping
// when a post-publish correction is explicitly recorded. Recording a
// correction after an escape is the operator's reset: the escape clears and
// the new cycle starts at one. Without an escape, the cycle count advances
// toward the budget.
func bumpPostPublishFixCycle(slice *DeliverySlice) {
	if strings.TrimSpace(slice.GoalEscape) != "" {
		slice.GoalEscape = ""
		slice.PostPublishFixAttempts = 1
		return
	}
	slice.PostPublishFixAttempts++
}
