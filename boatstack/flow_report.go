package boatstack

import (
	"fmt"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// FlowReport reads the session's shadow trajectory and coding-effort logs for a
// repo and measures them against the oracle toward a published delivery. It is
// read-only and best-effort on the logs — a missing log is an empty session, not
// an error — so the report is safe to request at any time. The result keeps
// J_flow, its oracle baseline J_flow*, the regret between them, and J_coding as
// independent figures; regret is derived purely from flow navigation.
func FlowReport(repo string) (deliverycontrol.FlowTrajectoryReport, error) {
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
	return deliverycontrol.ComputeReportWithCoding(trajectory, graph, weights, flowGoal, signals), nil
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
	return b.String()
}

func startLabel(start deliverycontrol.StateID) string {
	if start == "" {
		return "(none)"
	}
	return string(start)
}
