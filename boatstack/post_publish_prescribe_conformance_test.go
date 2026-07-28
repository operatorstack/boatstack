package boatstack

// control-law: merged-terminal-prescribes-merge-never-executes-it
// control-law: turn-ends-only-at-the-operator-frontier (post-publish extension)
//
// Under delivery.terminal "merged", the flow keeps prescribing past publish —
// from the live PR observation only: watch running checks (agent), fix
// failing checks through the existing record-change transition (agent; the
// failure facts are work-derivable from check logs), and merge an eligible PR
// via a prescribed `gh pr merge` (agent, PRESCRIBE-ONLY). Boatstack itself
// can never execute the merge: the command carries a foreign Program, which
// canAutoDrive refuses categorically before the allowlist is consulted, its
// transition is a marker (never allowlisted), and executePrescribed has no
// executor. Every phase owing operator authority — review approval, changes
// requested, closed, unknown — prescribes nothing and ends at the operator
// frontier. Under the published default, nothing changes at all.
//
// Test classes: positive (per-phase prescription + actor under merged,
// through the real ResolveNext path), negative (published default: no
// post-publish prescription, operator actor — behavior preserved), bypass
// (the merge prescription cannot be driven even with a hostile allowlist;
// the driver decision is prescribe-and-stop), failure-state (unknown phase →
// operator, nothing prescribed; merged observation → none, nothing owed).

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

func mergedTerminalRepo(t *testing.T) string {
	t.Helper()
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	writeTerminalConfig(t, repo, "merged")
	return repo
}

// Positive: each observed phase maps to exactly one prescription and one
// actor under the merged terminal.
func TestMergedTerminalPrescribesPostPublishSteps(t *testing.T) {
	for _, test := range []struct {
		name           string
		payload        func(string, ...string) (string, error)
		wantActor      NextActor
		wantVerb       string
		wantProgram    string
		wantTransition string
		wantInCommand  string
		wantOwed       []string
	}{
		{
			name:           "checks_pending_prescribes_watch",
			payload:        phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunPending),
			wantActor:      NextActorAgent,
			wantVerb:       "flow",
			wantTransition: string(MarkerPublishedWatch),
			wantInCommand:  "boatstack-helper flow watch",
		},
		{
			name:           "checks_failing_prescribes_ci_correction",
			payload:        phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunFail),
			wantActor:      NextActorAgent,
			wantVerb:       "record-change",
			wantTransition: "delivery.record_change",
			wantInCommand:  "--source-stage ci",
			wantOwed:       []string{"--message", "--classification"},
		},
		{
			name:           "merge_eligible_prescribes_gh_merge",
			payload:        phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunPass),
			wantActor:      NextActorAgent,
			wantVerb:       "pr",
			wantProgram:    "gh",
			wantTransition: string(MarkerPublishedMerge),
			wantInCommand:  "gh pr merge https://example.invalid/pr/9 --squash",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := mergedTerminalRepo(t)
			withRecoveryGh(t, test.payload)
			next, err := NextControl(repo, "shipped")
			if err != nil {
				t.Fatal(err)
			}
			if next.Actor != test.wantActor {
				t.Fatalf("actor = %q, want %q (%#v)", next.Actor, test.wantActor, next)
			}
			if next.Prescribed == nil {
				t.Fatalf("nothing prescribed: %#v", next)
			}
			if next.Prescribed.Verb != test.wantVerb || next.Prescribed.Program != test.wantProgram {
				t.Fatalf("prescribed %q/%q, want %q/%q", next.Prescribed.Program, next.Prescribed.Verb, test.wantProgram, test.wantVerb)
			}
			if string(next.Prescribed.Transition) != test.wantTransition {
				t.Fatalf("transition = %q, want %q", next.Prescribed.Transition, test.wantTransition)
			}
			if !strings.Contains(next.Prescribed.CommandLine(), test.wantInCommand) {
				t.Fatalf("command %q does not contain %q", next.Prescribed.CommandLine(), test.wantInCommand)
			}
			if strings.Join(next.Prescribed.RequiresHumanInput, " ") != strings.Join(test.wantOwed, " ") {
				t.Fatalf("owed inputs %v, want %v", next.Prescribed.RequiresHumanInput, test.wantOwed)
			}
		})
	}
}

// Positive: the rendered response marks post-publish agent steps with the
// delegation line — the frontier extension reuses the one rendering path.
func TestResponseDelegatesPostPublishAgentSteps(t *testing.T) {
	repo := mergedTerminalRepo(t)
	withRecoveryGh(t, phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunPass))
	status, err := ResolveNext(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderNextStatusResponse(repo, status)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "This step is mine to do.") {
		t.Fatalf("agent-owned merge step not delegated:\n%s", rendered)
	}
	if !strings.Contains(rendered, "gh pr merge https://example.invalid/pr/9 --squash") {
		t.Fatalf("rendered response lacks the exact merge command:\n%s", rendered)
	}
}

// Negative: the operator-owed phases prescribe nothing and end the turn at
// the frontier, merged terminal or not.
func TestOperatorPhasesPrescribeNothing(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload func(string, ...string) (string, error)
	}{
		{"review_required", phaseObservationPayload("OPEN", "REVIEW_REQUIRED", "BLOCKED", rollupCheckRunPass)},
		{"changes_requested", phaseObservationPayload("OPEN", "CHANGES_REQUESTED", "CLEAN", rollupCheckRunFail)},
		{"closed_pr", phaseObservationPayload("CLOSED", "", "", "")},
		{"unknown_phase", phaseObservationPayload("OPEN", "APPROVED", "DIRTY", rollupCheckRunPass)},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := mergedTerminalRepo(t)
			withRecoveryGh(t, test.payload)
			next, err := NextControl(repo, "shipped")
			if err != nil {
				t.Fatal(err)
			}
			if next.Actor != NextActorOperator {
				t.Fatalf("actor = %q, want operator", next.Actor)
			}
			if next.Prescribed != nil {
				t.Fatalf("operator phase must not carry a prescription: %#v", next.Prescribed)
			}
		})
	}
}

// Negative: under the published default nothing is prescribed post-publish
// and the actor stays operator — the frontier does not move without the
// explicit setpoint.
func TestPublishedDefaultKeepsPostPublishBehavior(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	withRecoveryGh(t, phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunPass))
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Actor != NextActorOperator || next.Prescribed != nil {
		t.Fatalf("published default drifted: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
	}
}

// Bypass: the prescribed merge can NEVER be executed by Boatstack — not by
// the pure drive decision, not with a hostile allowlist naming its marker,
// and not by the CLI executor.
func TestMergePrescriptionIsCategoricallyUndrivable(t *testing.T) {
	repo := mergedTerminalRepo(t)
	withRecoveryGh(t, phaseObservationPayload("OPEN", "APPROVED", "CLEAN", rollupCheckRunPass))
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Prescribed == nil || next.Prescribed.Program != "gh" {
		t.Fatalf("fixture did not produce the merge prescription: %#v", next.Prescribed)
	}
	decision := DecideDrive(next, true, false)
	if decision.Action != DrivePrescribe {
		t.Fatalf("drive decision = %q, want prescribe-and-stop", decision.Action)
	}
	hostileAllowlist := map[deliverycontrol.TransitionID]bool{MarkerPublishedMerge: true}
	if canAutoDrive(next.Prescribed, hostileAllowlist) {
		t.Fatal("a foreign-program command passed canAutoDrive despite the categorical refusal")
	}
}

// Failure-state: a merged observation owes nobody anything.
func TestMergedObservationEndsTheFlow(t *testing.T) {
	repo := mergedTerminalRepo(t)
	withRecoveryGh(t, phaseObservationPayload("MERGED", "", "", ""))
	next, err := NextControl(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	if next.Actor != NextActorNone || next.Prescribed != nil {
		t.Fatalf("merged flow still owes something: actor=%q prescribed=%#v", next.Actor, next.Prescribed)
	}
}
