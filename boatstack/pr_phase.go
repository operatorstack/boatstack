package boatstack

import "strings"

// PRPhase is the observed position of a published pull request between
// publication and merge. It is derived ONLY from a live GitHub observation
// (checks, review decision, merge state) at the moment of a read-only
// resolution; it is never persisted, recorded by an agent, or accepted from
// text. Anything the derivation cannot classify with certainty degrades to
// PRPhaseUnknown, which downstream classification treats as the operator's.
// control-law: pr-phase-derives-only-from-live-observation
type PRPhase string

const (
	// PRPhaseUnknown: the observation is missing, partial, or names a
	// combination this derivation does not understand. Fail-closed default.
	PRPhaseUnknown PRPhase = "PR_UNKNOWN"
	// PRPhaseChecksPending: the PR is open and at least one check has not finished.
	PRPhaseChecksPending PRPhase = "PR_CHECKS_PENDING"
	// PRPhaseChecksFailing: the PR is open and at least one check concluded badly.
	PRPhaseChecksFailing PRPhase = "PR_CHECKS_FAILING"
	// PRPhaseChangesRequested: a reviewer requested changes. This outranks check
	// status: a human review verdict is a stronger signal than CI and hands the
	// step to the operator regardless of what the checks are doing.
	PRPhaseChangesRequested PRPhase = "PR_CHANGES_REQUESTED"
	// PRPhaseReviewRequired: checks are green but a required review approval is
	// still owed. Granting approval is never Boatstack's or the agent's to do.
	PRPhaseReviewRequired PRPhase = "PR_REVIEW_REQUIRED"
	// PRPhaseMergeEligible: checks green, review satisfied, and GitHub reports
	// the branch cleanly mergeable.
	PRPhaseMergeEligible PRPhase = "PR_MERGE_ELIGIBLE"
	// PRPhaseMerged / PRPhaseClosed: terminal, mirrors the PR lifecycle.
	PRPhaseMerged PRPhase = "PR_MERGED"
	PRPhaseClosed PRPhase = "PR_CLOSED"
)

// prStatusCheck is one element of gh's statusCheckRollup array. GitHub emits
// two shapes — CheckRun (Actions/checks API: status+conclusion+name) and
// StatusContext (legacy commit status: state+context) — and this struct holds
// the union so one decode covers both.
type prStatusCheck struct {
	TypeName   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

// prCheckSummary aggregates a statusCheckRollup. Unrecognized is sticky: one
// entry the tables below cannot classify poisons the whole summary, because a
// phase derived from a partially understood rollup would be a guess.
type prCheckSummary struct {
	Total        int
	Passed       int
	Failed       int
	Pending      int
	Failing      []string
	Unrecognized bool
}

// prFailingChecksCap bounds the failing-check name list carried into status
// output so one enormous check matrix cannot flood a rendered response.
const prFailingChecksCap = 8

func summarizeCheckRollup(entries []prStatusCheck) prCheckSummary {
	summary := prCheckSummary{Total: len(entries)}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = strings.TrimSpace(entry.Context)
		}
		switch classifyStatusCheck(entry) {
		case "passed":
			summary.Passed++
		case "pending":
			summary.Pending++
		case "failed":
			summary.Failed++
			if name != "" && len(summary.Failing) < prFailingChecksCap {
				summary.Failing = append(summary.Failing, name)
			}
		default:
			summary.Unrecognized = true
		}
	}
	return summary
}

// classifyStatusCheck maps one rollup entry to passed/pending/failed, or ""
// when the entry's vocabulary is not in the tables. The typename is trusted
// first; when absent, the populated field set identifies the shape.
func classifyStatusCheck(entry prStatusCheck) string {
	shape := strings.TrimSpace(entry.TypeName)
	if shape == "" {
		switch {
		case entry.State != "" || entry.Context != "":
			shape = "StatusContext"
		case entry.Status != "" || entry.Conclusion != "":
			shape = "CheckRun"
		}
	}
	switch shape {
	case "CheckRun":
		if !strings.EqualFold(strings.TrimSpace(entry.Status), "COMPLETED") {
			return "pending"
		}
		switch strings.ToUpper(strings.TrimSpace(entry.Conclusion)) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			return "passed"
		case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE":
			return "failed"
		}
	case "StatusContext":
		switch strings.ToUpper(strings.TrimSpace(entry.State)) {
		case "SUCCESS":
			return "passed"
		case "PENDING", "EXPECTED":
			return "pending"
		case "FAILURE", "ERROR":
			return "failed"
		}
	}
	return ""
}

// derivePRPhase turns one live observation into a PRPhase. The derivation is
// pure and total: every input lands somewhere, and everything outside the
// explicitly understood combinations lands on PRPhaseUnknown. Notably absent
// on purpose: DIRTY/BEHIND/BLOCKED/DRAFT merge states (conflicts, stale base,
// branch protection this derivation cannot see, drafts) all stay Unknown so
// they reach the operator instead of being guessed at.
func derivePRPhase(prState string, checks prCheckSummary, reviewDecision, mergeState string) PRPhase {
	switch strings.ToUpper(strings.TrimSpace(prState)) {
	case "MERGED":
		return PRPhaseMerged
	case "CLOSED":
		return PRPhaseClosed
	case "OPEN":
	default:
		return PRPhaseUnknown
	}
	if checks.Unrecognized {
		return PRPhaseUnknown
	}
	decision := strings.ToUpper(strings.TrimSpace(reviewDecision))
	if decision == "CHANGES_REQUESTED" {
		return PRPhaseChangesRequested
	}
	if checks.Failed > 0 {
		return PRPhaseChecksFailing
	}
	if checks.Pending > 0 {
		return PRPhaseChecksPending
	}
	switch decision {
	case "REVIEW_REQUIRED":
		return PRPhaseReviewRequired
	case "", "APPROVED":
	default:
		return PRPhaseUnknown
	}
	// An empty rollup means no checks are configured; green-by-absence is
	// acceptable only because merge eligibility still requires GitHub itself
	// to report the branch cleanly mergeable below.
	switch strings.ToUpper(strings.TrimSpace(mergeState)) {
	case "CLEAN", "HAS_HOOKS":
		return PRPhaseMergeEligible
	}
	return PRPhaseUnknown
}
