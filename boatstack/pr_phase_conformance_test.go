package boatstack

// control-law: pr-phase-derives-only-from-live-observation
//
// The post-publish PR phase (checks pending/failing, changes requested,
// review required, merge eligible, merged, closed) is derived exclusively
// from one live GitHub observation at read-only resolution time. It is never
// persisted, never accepted from an agent's text, and every observation the
// derivation does not understand with certainty degrades to PR_UNKNOWN.
// Companion law re-pinned here: gate resolution stays network-free — a
// non-terminal observation must leave the delivery ledger byte-identical
// (persistObservedTerminalPRState caches terminal lifecycles only).
//
// Test classes: positive (each understood observation → its phase, through
// the real ResolveNext path, over both statusCheckRollup shapes), negative
// (degraded/malformed/unrecognized observations → PR_UNKNOWN), bypass (a
// non-terminal observation writes nothing), failure-state (an older gh that
// rejects the enriched field list still yields the legacy lifecycle).

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	rollupCheckRunPass    = `{"__typename":"CheckRun","name":"unit","status":"COMPLETED","conclusion":"SUCCESS"}`
	rollupCheckRunFail    = `{"__typename":"CheckRun","name":"unit","status":"COMPLETED","conclusion":"FAILURE"}`
	rollupCheckRunPending = `{"__typename":"CheckRun","name":"unit","status":"IN_PROGRESS","conclusion":""}`
	rollupContextPass     = `{"__typename":"StatusContext","context":"ci/lint","state":"SUCCESS"}`
	rollupContextFail     = `{"__typename":"StatusContext","context":"ci/lint","state":"FAILURE"}`
	rollupContextPending  = `{"__typename":"StatusContext","context":"ci/lint","state":"PENDING"}`
	rollupUnrecognized    = `{"__typename":"CheckRun","name":"novel","status":"COMPLETED","conclusion":"SOMETHING_NEW"}`
)

func phaseObservationPayload(prState, reviewDecision, mergeState, rollup string) func(string, ...string) (string, error) {
	return func(_ string, _ ...string) (string, error) {
		return fmt.Sprintf(
			`{"state":%q,"headRefName":"feat/phase","headRefOid":"head1","url":"https://example.invalid/pr/9","baseRefName":"main","mergeable":"MERGEABLE","mergeStateStatus":%q,"reviewDecision":%q,"statusCheckRollup":[%s]}`,
			prState, mergeState, reviewDecision, rollup), nil
	}
}

func publishedPhaseRepo(t *testing.T) string {
	t.Helper()
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "phased", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "phased", "feat/phase", "https://example.invalid/pr/9", "")
	return repo
}

// Positive: every understood live observation maps to exactly one phase,
// through the real ResolveNext path, over both rollup shapes.
func TestResolveNextDerivesPRPhaseFromLiveObservation(t *testing.T) {
	for _, test := range []struct {
		name           string
		prState        string
		reviewDecision string
		mergeState     string
		rollup         string
		wantPhase      PRPhase
		wantStage      string
		reasonContains string
	}{
		{"green_approved_clean_is_merge_eligible", "OPEN", "APPROVED", "CLEAN", rollupCheckRunPass + "," + rollupContextPass, PRPhaseMergeEligible, "PUBLISHED", "clean merge state"},
		{"no_required_review_green_clean_is_merge_eligible", "OPEN", "", "CLEAN", rollupCheckRunPass, PRPhaseMergeEligible, "PUBLISHED", "clean merge state"},
		{"no_checks_configured_green_by_absence", "OPEN", "APPROVED", "CLEAN", "", PRPhaseMergeEligible, "PUBLISHED", "clean merge state"},
		{"has_hooks_is_merge_eligible", "OPEN", "APPROVED", "HAS_HOOKS", rollupCheckRunPass, PRPhaseMergeEligible, "PUBLISHED", "clean merge state"},
		{"failing_check_run", "OPEN", "APPROVED", "CLEAN", rollupCheckRunFail + "," + rollupContextPass, PRPhaseChecksFailing, "PUBLISHED", "failing (unit)"},
		{"failing_status_context", "OPEN", "", "CLEAN", rollupCheckRunPass + "," + rollupContextFail, PRPhaseChecksFailing, "PUBLISHED", "failing (ci/lint)"},
		{"pending_check_run", "OPEN", "", "CLEAN", rollupCheckRunPending, PRPhaseChecksPending, "PUBLISHED", "still running"},
		{"pending_status_context", "OPEN", "", "CLEAN", rollupContextPending, PRPhaseChecksPending, "PUBLISHED", "still running"},
		{"review_required_after_green", "OPEN", "REVIEW_REQUIRED", "BLOCKED", rollupCheckRunPass, PRPhaseReviewRequired, "PUBLISHED", "required review approval"},
		{"changes_requested_outranks_failing_checks", "OPEN", "CHANGES_REQUESTED", "CLEAN", rollupCheckRunFail, PRPhaseChangesRequested, "PUBLISHED", "requested changes"},
		{"merged_pr_is_terminal", "MERGED", "", "", "", PRPhaseMerged, "FEATURE_COMPLETE", "is merged"},
		{"closed_pr_is_terminal", "CLOSED", "", "", "", PRPhaseClosed, "PUBLISHED", "closed without a verified merge"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := publishedPhaseRepo(t)
			withRecoveryGh(t, phaseObservationPayload(test.prState, test.reviewDecision, test.mergeState, test.rollup))
			status, err := ResolveNext(repo, "")
			if err != nil {
				t.Fatal(err)
			}
			if status.PRPhase != string(test.wantPhase) {
				t.Fatalf("phase = %q, want %q (%#v)", status.PRPhase, test.wantPhase, status)
			}
			if status.ObservedStage != test.wantStage {
				t.Fatalf("stage = %q, want %q", status.ObservedStage, test.wantStage)
			}
			if !strings.Contains(status.Reason, test.reasonContains) {
				t.Fatalf("reason %q does not mention %q", status.Reason, test.reasonContains)
			}
			if status.NextOperation != "none" {
				t.Fatalf("phase observation must not change the prescribed operation yet: %q", status.NextOperation)
			}
		})
	}
}

// Positive: the failing-check names ride along for status output, bounded by
// the cap so a huge check matrix cannot flood a rendered response.
func TestFailingCheckNamesSurfaceBounded(t *testing.T) {
	repo := publishedPhaseRepo(t)
	entries := make([]string, 0, prFailingChecksCap+4)
	for i := 0; i < prFailingChecksCap+4; i++ {
		entries = append(entries, fmt.Sprintf(`{"__typename":"CheckRun","name":"job-%02d","status":"COMPLETED","conclusion":"FAILURE"}`, i))
	}
	withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", strings.Join(entries, ",")))
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.PRPhase != string(PRPhaseChecksFailing) {
		t.Fatalf("phase = %q", status.PRPhase)
	}
	if len(status.PRFailingChecks) != prFailingChecksCap {
		t.Fatalf("failing names = %d, want cap %d", len(status.PRFailingChecks), prFailingChecksCap)
	}
	if status.PRFailingChecks[0] != "job-00" {
		t.Fatalf("unexpected first failing check: %v", status.PRFailingChecks)
	}
}

// Negative: degraded, malformed, or partially understood observations all
// land on PR_UNKNOWN and keep the pre-phase behavior intact.
func TestPRPhaseFailsClosedToUnknown(t *testing.T) {
	for _, test := range []struct {
		name          string
		gh            func(string, ...string) (string, error)
		wantLifecycle string
	}{
		{"gh_unavailable", func(string, ...string) (string, error) { return "", errors.New("not authenticated") }, "PUBLISHED_UNKNOWN"},
		{"malformed_payload", func(string, ...string) (string, error) { return "not json", nil }, "PUBLISHED_UNKNOWN"},
		{"unrecognized_rollup_entry", phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupUnrecognized), "PUBLISHED_OPEN"},
		{"unrecognized_review_decision", phaseObservationPayload("OPEN", "SOMETHING_NEW", "CLEAN", rollupCheckRunPass), "PUBLISHED_OPEN"},
		{"dirty_merge_state_is_not_guessed", phaseObservationPayload("OPEN", "APPROVED", "DIRTY", rollupCheckRunPass), "PUBLISHED_OPEN"},
		{"behind_merge_state_is_not_guessed", phaseObservationPayload("OPEN", "APPROVED", "BEHIND", rollupCheckRunPass), "PUBLISHED_OPEN"},
		{"draft_merge_state_is_not_guessed", phaseObservationPayload("OPEN", "", "DRAFT", ""), "PUBLISHED_OPEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := publishedPhaseRepo(t)
			withRecoveryGh(t, test.gh)
			status, err := ResolveNext(repo, "")
			if err != nil {
				t.Fatal(err)
			}
			if status.PRPhase != string(PRPhaseUnknown) {
				t.Fatalf("phase = %q, want PR_UNKNOWN", status.PRPhase)
			}
			if status.Lifecycle != test.wantLifecycle {
				t.Fatalf("lifecycle = %q, want %q", status.Lifecycle, test.wantLifecycle)
			}
			if status.NextOperation != "none" {
				t.Fatalf("unexpected operation %q", status.NextOperation)
			}
			if rendered := FormatNextStatus(status); strings.Contains(rendered, "PR phase:") {
				t.Fatalf("an Unknown phase must not earn a rendered line:\n%s", rendered)
			}
		})
	}
}

// Bypass: a non-terminal observation — however rich — must leave the delivery
// ledger byte-identical. Only a terminal lifecycle is cached, exactly as
// before the enrichment.
func TestNonTerminalPhaseObservationWritesNothing(t *testing.T) {
	repo := publishedPhaseRepo(t)
	statePath, err := deliveryStatePath(repo, "phased")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	withRecoveryGh(t, phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunFail))
	if _, err := ResolveNext(repo, ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a non-terminal observation modified the delivery ledger")
	}

	// Relation: the terminal cache write still happens after the enrichment.
	withRecoveryGh(t, phaseObservationPayload("MERGED", "", "", ""))
	if _, err := ResolveNext(repo, ""); err != nil {
		t.Fatal(err)
	}
	state, err := LoadDeliveryState(repo, "phased")
	if err != nil {
		t.Fatal(err)
	}
	if state.Slices[len(state.Slices)-1].PRState != "PUBLISHED_MERGED" {
		t.Fatalf("terminal lifecycle was not cached: %#v", state.Slices)
	}
}

// Failure-state: an older gh that rejects the enriched field list must not
// cost the basic lifecycle observation — the observer falls back to the
// legacy field list and the phase stays Unknown.
func TestObservationFallsBackToLegacyFieldsOnOlderGh(t *testing.T) {
	repo := publishedPhaseRepo(t)
	var requested []string
	withRecoveryGh(t, func(_ string, args ...string) (string, error) {
		fields := args[len(args)-1]
		requested = append(requested, fields)
		if strings.Contains(fields, "statusCheckRollup") {
			return "", errors.New("unknown JSON field: statusCheckRollup")
		}
		return `{"state":"OPEN","headRefName":"feat/phase","headRefOid":"head1","url":"https://example.invalid/pr/9"}`, nil
	})
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle != "PUBLISHED_OPEN" {
		t.Fatalf("legacy lifecycle lost: %q", status.Lifecycle)
	}
	if status.PRPhase != string(PRPhaseUnknown) {
		t.Fatalf("phase = %q, want PR_UNKNOWN on a legacy observation", status.PRPhase)
	}
	if len(requested) != 2 || requested[0] != publishedPRFields || requested[1] != publishedPRLegacyFields {
		t.Fatalf("unexpected field negotiation: %v", requested)
	}
}
