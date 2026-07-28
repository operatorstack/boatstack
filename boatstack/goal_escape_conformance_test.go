package boatstack

// control-law: goal-escape-demotes-to-operator-and-stops
//
// The merged-terminal pursuit is bounded by an explicit contract: it runs
// only while the fix budget holds, no reviewer has requested changes, and the
// branch merges cleanly. Any of those disturbances fires a goal escape, and a
// fired escape demotes unconditionally — the actor becomes operator, nothing
// further is prescribed (demote-and-stop, never demote-and-suggest), and the
// demotion persists best-effort so it holds OFFLINE in a fresh session. The
// only reset is the next explicitly recorded correction cycle. Under the
// published default no escape is ever evaluated or written.
//
// Test classes: positive (each escape condition → operator + no
// prescription + explanatory reason), relation (a persisted escape demotes
// with gh unavailable — sticky offline; recording a correction clears it and
// restarts the cycle at one), negative (a budget not yet spent does not
// escape; the default terminal records nothing), bypass (an escaped delivery
// never gets the merge prescribed again until reset, even under a live
// merge-eligible observation).

import (
	"errors"
	"strings"
	"testing"
)

func recordCIObservation(t *testing.T, repo, feature string) {
	t.Helper()
	if _, _, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "check failed", SourceStage: "ci",
		Classification: "implementation_repair",
	}); err != nil {
		t.Fatal(err)
	}
}

// Positive: each live disturbance fires its escape — operator actor, no
// prescription, and a reason that explains the pause.
func TestLiveDisturbancesFireEscapes(t *testing.T) {
	for _, test := range []struct {
		name       string
		payload    func(string, ...string) (string, error)
		wantEscape string
	}{
		{"changes_requested", phaseObservationPayload("OPEN", "CHANGES_REQUESTED", "CLEAN", rollupCheckRunPass), EscapeChangesRequested},
		{"base_conflicts", phaseObservationPayload("OPEN", "APPROVED", "DIRTY", rollupCheckRunPass), EscapeBaseConflicts},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := mergedTerminalRepo(t)
			withRecoveryGh(t, test.payload)
			status, err := ResolveNext(repo, "shipped")
			if err != nil {
				t.Fatal(err)
			}
			if status.GoalEscape != test.wantEscape {
				t.Fatalf("escape = %q, want %q", status.GoalEscape, test.wantEscape)
			}
			if !strings.Contains(status.Reason, "paused") {
				t.Fatalf("reason does not explain the pause: %q", status.Reason)
			}
			next, err := NextControl(repo, "shipped")
			if err != nil {
				t.Fatal(err)
			}
			if next.Actor != NextActorOperator || next.Prescribed != nil {
				t.Fatalf("escape did not demote-and-stop: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
			}
		})
	}
}

// Positive + relation: the fix budget is offline — three recorded cycles
// exhaust it with no live signal at all, the demotion is sticky with gh
// unavailable, and the next recorded correction is the reset that starts a
// fresh cycle at one.
func TestFixBudgetExhaustsDemotesOfflineAndResets(t *testing.T) {
	repo := mergedTerminalRepo(t)
	for i := 0; i < postPublishFixBudget; i++ {
		recordCIObservation(t, repo, "shipped")
	}
	state, err := LoadDeliveryState(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	last := state.Slices[len(state.Slices)-1]
	if last.PostPublishFixAttempts != postPublishFixBudget {
		t.Fatalf("attempts = %d, want %d", last.PostPublishFixAttempts, postPublishFixBudget)
	}

	// gh is unavailable: the escape must fire from the persisted counter alone.
	withRecoveryGh(t, func(string, ...string) (string, error) { return "", errors.New("offline") })
	status, err := ResolveNext(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if status.GoalEscape != EscapeFixAttemptsExhausted {
		t.Fatalf("offline escape = %q", status.GoalEscape)
	}
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Actor != NextActorOperator || next.Prescribed != nil {
		t.Fatalf("offline demotion failed: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
	}
	// The fired escape was cached; a fresh load shows it without any observation.
	state, err = LoadDeliveryState(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if state.Slices[len(state.Slices)-1].GoalEscape != EscapeFixAttemptsExhausted {
		t.Fatalf("escape not persisted: %#v", state.Slices)
	}

	// The reset: recording the next correction clears the escape and starts a
	// new cycle at one.
	recordCIObservation(t, repo, "shipped")
	state, err = LoadDeliveryState(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	last = state.Slices[len(state.Slices)-1]
	if last.GoalEscape != "" || last.PostPublishFixAttempts != 1 {
		t.Fatalf("reset failed: %#v", last)
	}
}

// Negative: a budget not yet spent does not escape, and the pursuit still
// prescribes the fix.
func TestUnspentBudgetKeepsPrescribing(t *testing.T) {
	repo := mergedTerminalRepo(t)
	for i := 0; i < postPublishFixBudget-1; i++ {
		recordCIObservation(t, repo, "shipped")
	}
	withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunFail))
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Actor != NextActorAgent || next.Prescribed == nil || next.Prescribed.Verb != "record-change" {
		t.Fatalf("unspent budget stopped prescribing: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
	}
}

// Negative: the published default never evaluates, surfaces, or writes an
// escape — its state files stay byte-stable through a correction cycle.
func TestPublishedDefaultRecordsNoEscapeState(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	withRecoveryGh(t, phaseObservationPayload("OPEN", "CHANGES_REQUESTED", "DIRTY", rollupCheckRunFail))

	status, err := ResolveNext(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if status.GoalEscape != "" {
		t.Fatalf("default terminal surfaced an escape: %q", status.GoalEscape)
	}
	recordCIObservation(t, repo, "shipped")
	state, err := LoadDeliveryState(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	last := state.Slices[len(state.Slices)-1]
	if last.PostPublishFixAttempts != 0 || last.GoalEscape != "" {
		t.Fatalf("default terminal wrote pursuit bookkeeping: %#v", last)
	}
}

// Bypass: once escaped, even a live merge-eligible observation cannot get the
// merge prescribed again — the contract stays ended until the recorded reset.
func TestEscapedDeliveryNeverGetsMergePrescribed(t *testing.T) {
	repo := mergedTerminalRepo(t)
	// Fire and persist an escape.
	withRecoveryGh(t, phaseObservationPayload("OPEN", "CHANGES_REQUESTED", "CLEAN", rollupCheckRunPass))
	if _, err := ResolveNext(repo, "shipped"); err != nil {
		t.Fatal(err)
	}
	// The world now looks perfect — but the escape is sticky.
	withRecoveryGh(t, phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunPass))
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Actor != NextActorOperator || next.Prescribed != nil {
		t.Fatalf("sticky escape bypassed: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
	}
	// After the recorded reset, the pursuit re-arms from a fresh observation.
	recordCIObservation(t, repo, "shipped")
	next, err = NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Prescribed == nil || next.Prescribed.Program != "gh" {
		t.Fatalf("reset did not re-arm the pursuit: %#v", next.Prescribed)
	}
}
