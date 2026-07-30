package boatstack

import (
	"fmt"
	"strings"
)

// The frontier report answers "where is every ball, and whose is it" in one
// read-only view: every managed delivery — active, published, or invalid —
// becomes a row carrying its observed position and the actor who owes the
// next step. It is pure presentation over the same resolution the flow oracle
// uses, so a frontier row can never disagree with `flow next` for the same
// delivery; and unlike next/recovery it performs ZERO writes — not even the
// best-effort terminal PR-state cache — because a report that mutates is a
// report that can lie about what it found.
// control-law: frontier-reports-never-mutates
const flowFrontierSchemaVersion = 1

// FrontierRow is one delivery slice's position on the operator frontier.
type FrontierRow struct {
	Feature         string   `json:"feature"`
	Slice           string   `json:"slice,omitempty"`
	SliceIndex      int      `json:"slice_index,omitempty"`
	TotalSlices     int      `json:"total_slices,omitempty"`
	Stage           string   `json:"stage"`
	Lifecycle       string   `json:"lifecycle,omitempty"`
	GoalEscape      string   `json:"goal_escape,omitempty"`
	PRPhase         string   `json:"pr_phase,omitempty"`
	PRFailingChecks []string `json:"pr_failing_checks,omitempty"`
	PRURL           string   `json:"pr_url,omitempty"`
	Actor           string   `json:"next_actor"`
	NextOperation   string   `json:"next_operation"`
	Prescribed      string   `json:"prescribed,omitempty"`
	Reason          string   `json:"reason"`
	Blocked         bool     `json:"blocked,omitempty"`
}

// FlowFrontier is the full cross-delivery dashboard.
type FlowFrontier struct {
	SchemaVersion int           `json:"schema_version"`
	Initialized   bool          `json:"initialized"`
	Rows          []FrontierRow `json:"rows"`
	AgentSteps    int           `json:"agent_steps"`
	OperatorSteps int           `json:"operator_steps"`
	TerminalRows  int           `json:"terminal_rows"`
	BlockedRows   int           `json:"blocked_rows"`
}

// ResolveFrontier builds the frontier report. Faults are partitioned, never
// propagated: one invalid delivery becomes one blocked row instead of
// poisoning the view of every healthy delivery (the same partition law the
// read-only recovery boundary uses).
// control-law: frontier-reports-never-mutates
// control-law: stale-delivery-cannot-block-unrelated-feature
func ResolveFrontier(repoPath string) (FlowFrontier, error) {
	frontier := FlowFrontier{SchemaVersion: flowFrontierSchemaVersion}
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return frontier, err
	}
	if !fileExists(WorkspaceFor(repo).ProjectConfigPath()) {
		return frontier, nil
	}
	frontier.Initialized = true
	config, _, configErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if configErr != nil {
		return frontier, fmt.Errorf("boatstack project configuration is invalid; fix the config file (doctor diagnoses): %w", configErr)
	}
	states, invalid, err := allManagedDeliveryStates(repo)
	if err != nil {
		return frontier, err
	}
	states = withoutIgnoredDeliveryStates(states, config.Workflow.IgnoredDeliveries)
	invalid = withoutIgnoredDeliveries(invalid, config.Workflow.IgnoredDeliveries)

	for _, slug := range invalid {
		frontier.Rows = append(frontier.Rows, FrontierRow{
			Feature: slug, Stage: "INVALID_STATE", Actor: string(NextActorOperator),
			NextOperation: "discard-delivery", Blocked: true,
			Reason: "This managed delivery state cannot be verified; restore its evidence, ignore it, or discard it.",
		})
	}
	for _, state := range states {
		if state.ActiveIndex < len(state.Slices) {
			frontier.Rows = append(frontier.Rows, activeDeliveryRows(repo, state)...)
			continue
		}
		branch, _, prURL := deliveryBranchAndSlice(state)
		status := publishedNextStatus(state, observePRTarget(repo, prURL, branch), resolveDeliveryTerminal(repo, state.Feature), observeVisualPublication(repo, state.Feature))
		frontier.Rows = append(frontier.Rows, frontierRowFromStatus(repo, status))
	}
	for _, row := range frontier.Rows {
		switch {
		case row.Blocked:
			frontier.BlockedRows++
		case row.Actor == string(NextActorAgent):
			frontier.AgentSteps++
		case row.Actor == string(NextActorNone):
			frontier.TerminalRows++
		default:
			frontier.OperatorSteps++
		}
	}
	return frontier, nil
}

// activeDeliveryRows renders an active delivery: one row for the active slice
// via the authoritative resolution, plus one row for every earlier slice that
// is published with a still-open PR — those are live balls too (their checks
// can be failing while the active slice builds), and they are exactly the
// addressable set the actuators can still re-gate in place.
func activeDeliveryRows(repo string, state DeliveryState) []FrontierRow {
	rows := []FrontierRow{}
	status, err := nextForDelivery(repo, state.Feature)
	if err != nil {
		rows = append(rows, FrontierRow{
			Feature: state.Feature, Stage: "INVALID_STATE", Actor: string(NextActorOperator),
			NextOperation: "discard-delivery", Blocked: true,
			Reason: "The active managed delivery cannot be verified: " + err.Error(),
		})
	} else {
		rows = append(rows, frontierRowFromStatus(repo, status))
	}
	limit := state.ActiveIndex
	if limit > len(state.Slices) {
		limit = len(state.Slices)
	}
	for i := 0; i < limit; i++ {
		slice := state.Slices[i]
		if slice.Status != "PUBLISHED" || strings.TrimSpace(slice.PRState) == "" || isTerminalPRState(slice.PRState) {
			continue
		}
		observation := observePRTarget(repo, slice.PRURL, slice.HeadBranch)
		sliceStatus := NextStatus{
			SchemaVersion: nextStatusSchemaVersion, VerificationStatus: "VERIFIED",
			Feature: state.Feature, ActiveSlice: slice.ID, SliceIndex: i + 1,
			TotalSlices: len(state.Slices), ObservedStage: "PUBLISHED", NextOperation: "none",
			Lifecycle: observation.Lifecycle, PRURL: observation.URL, HeadBranch: observation.Branch,
			PRPhase: string(observation.Phase), PRReviewDecision: observation.ReviewDecision,
			PRMergeState: observation.MergeState, PRFailingChecks: observation.FailingChecks,
			Reason: fmt.Sprintf("Slice %q is published with an open pull request while a later slice is active.", slice.ID),
		}
		if resolveDeliveryTerminal(repo, state.Feature) == TerminalMerged && observation.Lifecycle != "PUBLISHED_MERGED" {
			sliceStatus.GoalEscape = evaluateGoalEscape(slice, observation)
		}
		rows = append(rows, frontierRowFromStatus(repo, sliceStatus))
	}
	return rows
}

// frontierRowFromStatus projects one resolved status through the SAME actor
// classification and prescription layer `flow next` uses — one resolution
// path, so the dashboard and the advisor can never name different owners for
// the same step.
func frontierRowFromStatus(repo string, status NextStatus) FrontierRow {
	row := FrontierRow{
		Feature: status.Feature, Slice: status.ActiveSlice,
		SliceIndex: status.SliceIndex, TotalSlices: status.TotalSlices,
		Stage: status.ObservedStage, Lifecycle: status.Lifecycle,
		GoalEscape: status.GoalEscape,
		PRPhase:    status.PRPhase, PRFailingChecks: status.PRFailingChecks,
		PRURL: status.PRURL, NextOperation: status.NextOperation,
		Reason:  status.Reason,
		Blocked: status.VerificationStatus == "BLOCKED",
	}
	next, err := nextControlFromStatus(repo, status)
	if err != nil {
		row.Actor = string(NextActorOperator)
		row.Blocked = true
		return row
	}
	row.Actor = string(next.Actor)
	if next.Prescribed != nil {
		row.Prescribed = next.Prescribed.CommandLine()
	}
	return row
}

// frontierPosition is the one-word position column: the observed PR phase when
// it is positively known, the stage otherwise.
func frontierPosition(row FrontierRow) string {
	if row.PRPhase != "" && row.PRPhase != string(PRPhaseUnknown) {
		return row.PRPhase
	}
	return row.Stage
}

// FormatFlowFrontier renders the dashboard as fixed-width human-facing lines.
func FormatFlowFrontier(frontier FlowFrontier) string {
	var b strings.Builder
	if !frontier.Initialized {
		b.WriteString("Boatstack is not tracking anything here yet.\n")
		return b.String()
	}
	if len(frontier.Rows) == 0 {
		b.WriteString("Frontier: no managed deliveries.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Frontier: %d for you, %d for the agent, %d complete, %d blocked\n",
		frontier.OperatorSteps, frontier.AgentSteps, frontier.TerminalRows, frontier.BlockedRows)
	nameWidth, positionWidth := len("FEATURE"), len("POSITION")
	for _, row := range frontier.Rows {
		if len(frontierLabel(row)) > nameWidth {
			nameWidth = len(frontierLabel(row))
		}
		if len(frontierPosition(row)) > positionWidth {
			positionWidth = len(frontierPosition(row))
		}
	}
	fmt.Fprintf(&b, "%-*s  %-*s  %-8s  %s\n", nameWidth, "FEATURE", positionWidth, "POSITION", "ACTOR", "NEXT")
	for _, row := range frontier.Rows {
		next := row.NextOperation
		if len(row.PRFailingChecks) > 0 {
			next += " (failing: " + strings.Join(row.PRFailingChecks, ", ") + ")"
		}
		fmt.Fprintf(&b, "%-*s  %-*s  %-8s  %s\n", nameWidth, frontierLabel(row), positionWidth, frontierPosition(row), row.Actor, next)
	}
	return b.String()
}

// frontierLabel names a row: the feature, with the slice id appended when the
// delivery has more than one slice so two rows of one delivery stay distinct.
func frontierLabel(row FrontierRow) string {
	if row.TotalSlices > 1 && row.Slice != "" {
		return row.Feature + "/" + row.Slice
	}
	return row.Feature
}
