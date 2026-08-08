package boatstack

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: guard-never-prescribes-what-it-would-deny
// control-law: solution-set-derives-from-guard-declarations
//
// The sibling harness's v2.9 campaign lost trials to exactly this defect
// class: the system compiled an artifact its own laws then rejected. The
// solution set makes that class structurally testable here — every command the
// enumerator presents as a legal pick must be admitted by the guard at the
// exact position that emitted it, and must be a legal or observation move on
// the declared delivery model. These sweeps hold both consumers of the guard
// tables to the same rows.

// substituteOwedFlags replaces <REQUIRED> placeholders with a dummy value so a
// rendered command can be fed back to the guard (which rejects angle brackets
// as shell metacharacters). The closure property is defined over substituted
// lines: what the user runs after filling the owed input.
func substituteOwedFlags(line string) string {
	line = strings.ReplaceAll(line, "--feature '<REQUIRED>'", "--feature demo")
	line = strings.ReplaceAll(line, "--artifact '<REQUIRED>'", "--artifact plan.md")
	line = strings.ReplaceAll(line, "--feature <REQUIRED>", "--feature demo")
	line = strings.ReplaceAll(line, "--artifact <REQUIRED>", "--artifact plan.md")
	return strings.ReplaceAll(line, "<REQUIRED>", "test-value")
}

// solutionOptions is the full pick list of a position: the primary plus the
// alternatives.
func solutionOptions(next FlowNext) []PrescribedCommand {
	var options []PrescribedCommand
	if next.Prescribed != nil {
		options = append(options, *next.Prescribed)
	}
	return append(options, next.Alternatives...)
}

// deliveryStages maps each oracle-resolved stage to a synthetic VERIFIED
// status, mirroring planningStages for the delivery side.
var deliveryStages = []NextStatus{
	{VerificationStatus: "VERIFIED", ObservedStage: "BUILD", NextOperation: "build", Feature: "demo", ActiveSlice: "s1"},
	{VerificationStatus: "VERIFIED", ObservedStage: "TEST_PASSED", NextOperation: "review-gate", Feature: "demo", ActiveSlice: "s1"},
	{VerificationStatus: "VERIFIED", ObservedStage: "REVIEW_PASSED", NextOperation: "ship-gate", Feature: "demo", ActiveSlice: "s1"},
	{VerificationStatus: "VERIFIED", ObservedStage: "PUBLISHED", NextOperation: "none", Feature: "demo", ActiveSlice: "s1"},
}

// Positive/Relation: every pre-activation option is admitted by
// controlledPhaseTransition at the emitting stage — the guard admits its own
// solution set, verb for verb, from the same tables.
func TestPlanningSolutionSetIsClosedUnderGuardAdmission(t *testing.T) {
	for _, status := range planningStages {
		next, err := nextControlFromStatus(".", status)
		if err != nil {
			t.Fatal(err)
		}
		options := solutionOptions(next)
		if len(options) == 0 {
			t.Errorf("%s/%s: the solution set must never be empty at a pre-activation stage", status.ObservedStage, status.NextOperation)
		}
		for _, option := range options {
			line := substituteOwedFlags(option.CommandLine())
			if !controlledPhaseTransition(line, status.ObservedStage) {
				t.Errorf("%s/%s: enumerated %q but the guard denies it at that stage", status.ObservedStage, status.NextOperation, line)
			}
		}
	}
	// AMBIGUOUS keeps its documented no-primary exception, but the solution set
	// still names the legal observations — a weak model always has a pick.
	next, err := nextControlFromStatus(".", NextStatus{ObservedStage: "AMBIGUOUS"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Prescribed != nil {
		t.Fatalf("AMBIGUOUS must not gain a fabricated primary: %+v", next.Prescribed)
	}
	if len(next.Alternatives) == 0 {
		t.Fatal("AMBIGUOUS must still enumerate read-only picks (next-status, doctor)")
	}
	for _, option := range next.Alternatives {
		if !controlledPhaseTransition(substituteOwedFlags(option.CommandLine()), "AMBIGUOUS") {
			t.Errorf("AMBIGUOUS pick %q must be guard-admitted", option.CommandLine())
		}
	}
}

// Positive/Relation: every delivery-state option is either a legal move on the
// registry graph from that exact state, or an observation row that accepts the
// state. Nothing outside the declared model is ever offered.
func TestDeliverySolutionSetIsLegalOnTheDeclaredModel(t *testing.T) {
	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	for _, status := range deliveryStages {
		next, err := nextControlFromStatus(".", status)
		if err != nil {
			t.Fatal(err)
		}
		if !next.Resolved {
			t.Fatalf("%s must resolve a flow state", status.ObservedStage)
		}
		options := solutionOptions(next)
		if len(options) == 0 {
			t.Errorf("%s: the solution set must never be empty at a resolved state", status.ObservedStage)
		}
		for _, option := range options {
			descriptor, isRegistry := deliverycontrol.Transition(option.Transition)
			if !isRegistry {
				t.Errorf("%s: delivery option %s carries a non-registry transition %s", status.ObservedStage, option.Verb, option.Transition)
				continue
			}
			if descriptor.To != "" {
				if !graph.IsLegalMove(next.State, option.Transition) {
					t.Errorf("%s: %s is not a legal move from %s", status.ObservedStage, option.Transition, next.State)
				}
				continue
			}
			if descriptor.From != nil && !transitionAccepts(descriptor, next.State) {
				t.Errorf("%s: observation %s does not accept state %s", status.ObservedStage, option.Transition, next.State)
			}
		}
	}
}

// Bypass: no rendered option, after owed-input substitution, may trip the
// text-level guard laws — the managed-state path law and the destruction
// classifier. This closes the publish-update-pr regression class end to end:
// the guard denying its own prescribed command line.
func TestSolutionSetCommandsPassTheTextGuards(t *testing.T) {
	statuses := append(append([]NextStatus{}, planningStages...), deliveryStages...)
	for _, status := range statuses {
		next, err := nextControlFromStatus(".", status)
		if err != nil {
			t.Fatal(err)
		}
		for _, option := range solutionOptions(next) {
			line := substituteOwedFlags(option.CommandLine())
			if deliveryStatePathPattern.MatchString(line) && !isPureReadOnlyCommand(line) && !approvedUpdatePublisherPattern.MatchString(line) {
				t.Errorf("%s: prescribed %q names managed state the guard would deny", status.ObservedStage, line)
			}
			if findings := classifySafetyText(line, "command", commandExecutesLiveSQL(line)); len(findings) > 0 {
				t.Errorf("%s: prescribed %q trips the text guard: %+v", status.ObservedStage, line, findings)
			}
		}
	}
}

// End to end: in a real DRAFT_PLAN repository, every enumerated option passes
// the full command classifier — zero findings, not just the phase interlock.
func TestDraftPlanSolutionSetPassesFullClassifier(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "demo")
	next, err := NextControl(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	options := solutionOptions(next)
	if len(options) < 3 {
		t.Fatalf("DRAFT_PLAN should enumerate the primary plus alternatives, got %d: %+v", len(options), options)
	}
	for _, option := range options {
		line := substituteOwedFlags(option.CommandLine())
		if findings := ClassifyCommand(repo, line); len(findings) > 0 {
			t.Errorf("guard denies its own prescription %q: %+v", line, findings)
		}
	}
}

// Failure-state/Invariants: dedup identity holds, the cap holds, the primary
// never reappears in Alternatives, and owed flags never leak into Args.
func TestSolutionSetInvariants(t *testing.T) {
	statuses := append(append([]NextStatus{}, planningStages...), deliveryStages...)
	for _, status := range statuses {
		next, err := nextControlFromStatus(".", status)
		if err != nil {
			t.Fatal(err)
		}
		if len(next.Alternatives) > solutionSetCap {
			t.Errorf("%s: alternatives exceed the cap: %d", status.ObservedStage, len(next.Alternatives))
		}
		seen := map[string]bool{}
		if next.Prescribed != nil {
			seen[prescriptionKey(*next.Prescribed)] = true
		}
		for _, option := range next.Alternatives {
			key := prescriptionKey(option)
			if seen[key] {
				t.Errorf("%s: duplicate or primary-shadowing option %q", status.ObservedStage, option.CommandLine())
			}
			seen[key] = true
			if option.AutoDerivable != (len(option.RequiresHumanInput) == 0) {
				t.Errorf("%s: AutoDerivable must equal owed-input emptiness: %+v", status.ObservedStage, option)
			}
			for _, owed := range option.RequiresHumanInput {
				for _, arg := range option.Args {
					if arg == owed {
						t.Errorf("%s: owed flag %s fabricated into Args: %+v", status.ObservedStage, owed, option)
					}
				}
			}
		}
	}
}

// Rendering: the text carriers keep exactly one primary Run line; alternatives
// are one line (flow) or one sentence (response), capped.
func TestSolutionSetRenderingKeepsOnePrimary(t *testing.T) {
	status := deliveryStages[1] // TEST_PASSED: gate, rework, abandon all legal
	next, err := nextControlFromStatus(".", status)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatFlowNext(next)
	if got := strings.Count(out, "Run: "); got != 1 {
		t.Fatalf("flow rendering must keep exactly one Run line, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "Also legal from here: ") {
		t.Fatalf("flow rendering must name the other legal moves:\n%s", out)
	}
	line := ""
	for _, candidate := range strings.Split(out, "\n") {
		if strings.HasPrefix(candidate, "Also legal from here: ") {
			line = candidate
		}
	}
	if got := strings.Count(line, ","); got > solutionSetTextCap-1 {
		t.Fatalf("text rendering must cap at %d entries: %q", solutionSetTextCap, line)
	}
}
