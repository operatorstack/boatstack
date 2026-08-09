package boatstack

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// FlowReport reads the session's shadow trajectory and coding-effort logs for a
// repo and measures them against the oracle toward a published delivery. It is
// read-only and best-effort on the logs — a missing log is an empty session, not
// an error — so the report is safe to request at any time. The result keeps
// J_flow, its oracle baseline J_flow*, the regret between them, and J_coding as
// independent figures; regret is derived purely from flow navigation.
func FlowReport(repo string) (deliverycontrol.FlowTrajectoryReport, error) {
	return FlowReportFor(repo, "")
}

// FlowReportFor scopes command telemetry to one feature when provided. The
// delivery trajectory remains the per-worktree walk because its legacy records
// predate feature correlation; command evidence is never silently attributed.
func FlowReportFor(repo, feature string) (deliverycontrol.FlowTrajectoryReport, error) {
	dir, err := flowLogDirectory(repo)
	if err != nil {
		return deliverycontrol.FlowTrajectoryReport{}, err
	}
	trajectory, err := deliverycontrol.ReadTrajectory(dir)
	if err != nil {
		return deliverycontrol.FlowTrajectoryReport{}, err
	}
	signals, err := deliverycontrol.ReadCodingSignals(dir)
	if err != nil {
		return deliverycontrol.FlowTrajectoryReport{}, err
	}
	weights := deliverycontrol.DefaultFlowCostWeights()
	graph := deliverycontrol.RegistryGraph(weights)
	report := deliverycontrol.ComputeReportWithCoding(trajectory, graph, weights, flowGoal, signals)
	report.Feature = strings.TrimSpace(feature)
	events, err := deliverycontrol.ReadCommandEvents(dir)
	if err != nil {
		return deliverycontrol.FlowTrajectoryReport{}, err
	}
	filtered := make([]deliverycontrol.CommandEvent, 0, len(events))
	for _, event := range events {
		if report.Feature == "" || event.Feature == report.Feature {
			filtered = append(filtered, event)
		}
	}
	applyCommandReport(&report, filtered)
	return report, nil
}

func applyCommandReport(report *deliverycontrol.FlowTrajectoryReport, events []deliverycontrol.CommandEvent) {
	report.CommandCoverageStatus = "NO_EVENTS"
	if len(events) == 0 {
		return
	}
	report.CommandCoverageStatus = "SCOPED_COMPLETE"
	report.CommandEvents = len(events)
	report.CommandFailureByCategory = map[string]int{}
	var first, last time.Time
	for _, event := range events {
		report.ObservedCommandMS += event.DurationMS
		started, _ := time.Parse(time.RFC3339Nano, event.StartedAt)
		finished, _ := time.Parse(time.RFC3339Nano, event.FinishedAt)
		if first.IsZero() || started.Before(first) {
			first = started
		}
		if last.IsZero() || finished.After(last) {
			last = finished
		}
		if event.ExitCode != 0 {
			report.CommandFailures++
			report.CommandFailureByCategory[event.Category]++
		}
	}
	if report.CommandFailures == 0 {
		report.CommandFailureByCategory = nil
	}
	report.FirstCommandAt = first.UTC().Format(time.RFC3339Nano)
	report.LastCommandAt = last.UTC().Format(time.RFC3339Nano)
	report.CommandWallSpanMS = last.Sub(first).Milliseconds()
}

// FormatFlowReport renders a session flow report as human-facing lines. When the
// oracle cannot place the session's start against the goal the regret line is
// withheld rather than fabricated, and coding effort is always shown as a
// separate figure so it is never read as part of the flow regret.
func FormatFlowReport(report deliverycontrol.FlowTrajectoryReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Flow report: %d steps, start %s -> goal %s\n", report.Steps, startLabel(report.Start), report.Goal)
	if report.Resolution == deliverycontrol.Resolved {
		fmt.Fprintf(&b, "J_flow=%d J_flow*=%d regret=%d\n", report.JFlow, report.JFlowStar, report.Regret)
	} else {
		fmt.Fprintf(&b, "J_flow=%d regret=unresolved (no oracle baseline for this start)\n", report.JFlow)
	}
	fmt.Fprintf(&b, "J_coding=%d (telemetry, separate from flow)\n", report.JCoding)
	fmt.Fprintf(&b, "command_coverage=%s events=%d failures=%d observed_ms=%d wall_span_ms=%d\n", report.CommandCoverageStatus, report.CommandEvents, report.CommandFailures, report.ObservedCommandMS, report.CommandWallSpanMS)
	if report.Feature != "" {
		fmt.Fprintf(&b, "feature=%s\n", report.Feature)
	}
	if len(report.PositiveGapByCategory) > 0 {
		categories := make([]string, 0, len(report.PositiveGapByCategory))
		for category := range report.PositiveGapByCategory {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		for _, category := range categories {
			fmt.Fprintf(&b, "positive_gap[%s]=%d\n", category, report.PositiveGapByCategory[category])
		}
	}
	if len(report.CommandFailureByCategory) > 0 {
		categories := make([]string, 0, len(report.CommandFailureByCategory))
		for category := range report.CommandFailureByCategory {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		for _, category := range categories {
			fmt.Fprintf(&b, "command_failure[%s]=%d\n", category, report.CommandFailureByCategory[category])
		}
	}
	return b.String()
}

func startLabel(start deliverycontrol.StateID) string {
	if start == "" {
		return "(none)"
	}
	return string(start)
}
