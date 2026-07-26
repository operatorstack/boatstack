package boatstack

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// flowGoal is the accepted end state of a delivery flow — the sink the oracle
// scores paths toward.
const flowGoal = deliverycontrol.StatePublished

// FlowCheck runs the static conformance + liveness gate over the owned delivery
// model. It reads no repository state — it validates the single declaration and
// its graph — so the CLI gate is deterministic and side-effect free.
func FlowCheck() deliverycontrol.CheckResult {
	return deliverycontrol.Check()
}

// FormatFlowCheck renders a FlowCheck result as human-facing lines. A sound model
// reports PASS; drift lists the registry issues and any deadlocked or
// goal-unreachable states so the fault is actionable, not just a red exit code.
func FormatFlowCheck(result deliverycontrol.CheckResult) string {
	var b strings.Builder
	if result.OK {
		fmt.Fprintf(&b, "PASS: delivery flow model is conformant and live (goal %s)\n", result.Liveness.Goal)
	} else {
		fmt.Fprintf(&b, "BLOCKED: delivery flow model failed the static check (goal %s)\n", result.Liveness.Goal)
	}
	for _, issue := range result.RegistryIssues {
		fmt.Fprintf(&b, "REGISTRY_ISSUE=%s\n", issue)
	}
	for _, s := range result.Liveness.Deadlocks {
		fmt.Fprintf(&b, "DEADLOCK=%s\n", s)
	}
	for _, s := range result.Liveness.GoalUnreachable {
		fmt.Fprintf(&b, "GOAL_UNREACHABLE=%s\n", s)
	}
	fmt.Fprintf(&b, "REACHABLE=%d LIVE=%t\n", len(result.Liveness.Reachable), result.Liveness.Live)
	return b.String()
}

// flowStateFromStage maps a read-only NextStatus.ObservedStage to a
// delivery-flow StateID. It resolves ONLY the concrete slice-lifecycle stages,
// where the position is unambiguous; every planning, ambiguous, or invalid stage
// returns false so callers fall back to existing behavior rather than act on a
// guessed position. This conservative mapping is what keeps flow control from
// ever interfering with pre-activation or ambiguous flows.
func flowStateFromStage(stage string) (deliverycontrol.StateID, bool) {
	switch stage {
	case "BUILD":
		return deliverycontrol.StateBuild, true
	case "TEST_PASSED":
		return deliverycontrol.StateTestPassed, true
	case "REVIEW_PASSED", "PR_PREVIEW":
		return deliverycontrol.StateReviewPassed, true
	case "PUBLISHED", "FEATURE_COMPLETE":
		return deliverycontrol.StatePublished, true
	default:
		return "", false
	}
}

// CurrentFlowState resolves the delivery-flow state of the addressable slice via
// the read-only ResolveNext projection. The boolean is false whenever the
// position cannot be trusted — an error, a non-VERIFIED status (blocked,
// ambiguous, uninitialized), or a stage that is not a concrete slice-lifecycle
// state. Callers must treat false as "unknown" and never fabricate a position.
func CurrentFlowState(repo, feature string) (deliverycontrol.StateID, bool) {
	status, err := ResolveNext(repo, feature)
	if err != nil {
		return "", false
	}
	if status.VerificationStatus != "VERIFIED" {
		return "", false
	}
	return flowStateFromStage(status.ObservedStage)
}

// FlowNext is the advisory answer for `flow next`: the current delivery-flow
// state, the real recommended operation (from ResolveNext — the authoritative
// next-move table), and the oracle's lowest-cost next control plus the remaining
// cost to the goal. It is purely advisory; it changes no command, gate, or
// authority. Resolved is false when the oracle cannot place the flow, in which
// case only the real recommendation is meaningful.
type FlowNext struct {
	Resolved      bool                         `json:"resolved"`
	State         deliverycontrol.StateID      `json:"state,omitempty"`
	Goal          deliverycontrol.StateID      `json:"goal"`
	RecommendedOp string                       `json:"recommended_operation"`
	OracleNext    deliverycontrol.TransitionID `json:"oracle_next_transition,omitempty"`
	RemainingCost int                          `json:"remaining_flow_cost"`
	Reason        string                       `json:"reason"`
	// Prescribed is the exact runnable command for the oracle's lowest-cost next
	// move. It is non-nil ONLY when the flow position resolves and the transition
	// can be assembled faithfully; an unresolved position prescribes nothing rather
	// than a guessed command.
	Prescribed *PrescribedCommand `json:"prescribed,omitempty"`
	// SubAction is the read-only next coding sub-action from the plan's task DAG,
	// surfaced only while the active slice is in BUILD (where "build" is otherwise
	// opaque). It is a pointer into the slice's dependency-ordered tasks; it is nil
	// when the slice is not building or the task graph cannot be read. It carries no
	// completion state and prescribes no command — coding work is never a modeled
	// transition, only an ordered pointer.
	SubAction *FlowTask `json:"sub_action,omitempty"`
}

// PrescribedCommand is the exact next command that makes the oracle's lowest-cost
// move. Args carries only auto-derivable flags (repo/feature/slice/gate/preview
// path/action). RequiresHumanInput names the flags that must be supplied by a
// human/CI and must NEVER be fabricated (evidence, gate status, the human-confirmed
// preview fingerprint, reviewer identity); those flags are deliberately absent from
// Args. AutoDerivable is true exactly when RequiresHumanInput is empty — the only
// commands the opt-in execute driver may run.
type PrescribedCommand struct {
	Verb               string                       `json:"verb"`
	Args               []string                     `json:"args,omitempty"`
	RequiresHumanInput []string                     `json:"requires_human_input,omitempty"`
	AutoDerivable      bool                         `json:"auto_derivable"`
	Transition         deliverycontrol.TransitionID `json:"transition"`
}

// CommandLine renders the auto-derivable part of the prescribed command as a
// runnable string. Human-required flags are appended as explicit <REQUIRED>
// placeholders so the rendering is never a fabricated, runnable-as-is command
// when input is still owed.
func (p PrescribedCommand) CommandLine() string {
	parts := append([]string{"boatstack-helper", p.Verb}, p.Args...)
	for _, flag := range p.RequiresHumanInput {
		parts = append(parts, flag, "<REQUIRED>")
	}
	return strings.Join(parts, " ")
}

// prescribeCommand assembles the runnable command for a forward delivery
// transition. It returns (nil, false) for any transition it cannot assemble
// faithfully — so the caller emits nothing rather than a guessed command. The
// emitted verb is always the registry CLIVerb of the transition (single source),
// and human-owed inputs are listed, never filled.
func prescribeCommand(repo, feature string, status NextStatus, transition deliverycontrol.TransitionID) (*PrescribedCommand, bool) {
	desc, ok := deliverycontrol.Transition(transition)
	if !ok || desc.CLIVerb == "" {
		return nil, false
	}
	cmd := &PrescribedCommand{Verb: desc.CLIVerb, Transition: transition}
	var repoArgs []string
	if repo != "" && repo != "." {
		repoArgs = []string{"--repo", repo}
	}
	switch transition {
	case deliverycontrol.TransitionID("delivery.record_gate_test"):
		cmd.Args = append(repoArgs, "--feature", feature, "--slice", status.ActiveSlice, "--gate", "test")
		cmd.RequiresHumanInput = []string{"--status", "--evidence"}
	case deliverycontrol.TransitionID("delivery.record_gate_review"):
		cmd.Args = append(repoArgs, "--feature", feature, "--slice", status.ActiveSlice, "--gate", "review")
		cmd.RequiresHumanInput = []string{"--status", "--evidence", "--reviewer-identity", "--review-method"}
	case PublishTransition:
		preview := filepath.Join(WorkspaceFor(repo).GeneratedRoot(), "features", feature, "pr.md")
		cmd.Args = append(repoArgs, "--preview", preview, "--action", "open")
		cmd.RequiresHumanInput = []string{"--preview-fingerprint"}
	default:
		// Recovery/observe/rework transitions are not prescribed as a forward move;
		// emit nothing rather than a command whose arguments we cannot derive.
		return nil, false
	}
	cmd.AutoDerivable = len(cmd.RequiresHumanInput) == 0
	return cmd, true
}

// NextControl composes the authoritative read-only recommendation (ResolveNext)
// with the deterministic oracle to advise the lowest-cost next move toward a
// published delivery. It performs no mutation and is safe to call at any time.
func NextControl(repo, feature string) (FlowNext, error) {
	status, err := ResolveNext(repo, feature)
	if err != nil {
		return FlowNext{}, err
	}
	out := FlowNext{
		Goal:          flowGoal,
		RecommendedOp: status.NextOperation,
		Reason:        status.Reason,
	}

	var state deliverycontrol.StateID
	resolved := false
	if status.VerificationStatus == "VERIFIED" {
		state, resolved = flowStateFromStage(status.ObservedStage)
	}
	if !resolved {
		return out, nil
	}
	out.State = state

	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	advice := graph.Advise(state, flowGoal)
	if advice.Resolution == deliverycontrol.Resolved {
		out.Resolved = true
		out.OracleNext = advice.NextTransition
		out.RemainingCost = advice.RemainingCost
		if advice.NextTransition != "" {
			if prescribed, ok := prescribeCommand(repo, feature, status, advice.NextTransition); ok {
				out.Prescribed = prescribed
			}
		}
		// While the slice is building, "build" is opaque; surface the read-only
		// dependency-ordered next sub-action from the plan's task DAG as a hint.
		if state == deliverycontrol.StateBuild {
			if tasks, err := FlowTasksForActiveSlice(repo, feature); err == nil && tasks.Resolved && len(tasks.Ordered) > 0 {
				hint := tasks.Ordered[0]
				out.SubAction = &hint
			}
		}
	}
	return out, nil
}

// FormatFlowNext renders a FlowNext advisory as human-facing lines. The
// recommended operation always comes from the authoritative next-move table; the
// oracle line is shown only when the flow position resolves, and is explicitly
// labeled advisory so it is never mistaken for a gate.
func FormatFlowNext(next FlowNext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recommended: %s\n", next.RecommendedOp)
	if next.Reason != "" {
		fmt.Fprintf(&b, "Reason: %s\n", next.Reason)
	}
	if next.Resolved {
		fmt.Fprintf(&b, "Flow state: %s -> goal %s\n", next.State, next.Goal)
		fmt.Fprintf(&b, "Advisory (flow oracle): next %s, remaining cost %d\n", next.OracleNext, next.RemainingCost)
		if next.Prescribed != nil {
			fmt.Fprintf(&b, "Run: %s\n", next.Prescribed.CommandLine())
			if next.Prescribed.AutoDerivable {
				fmt.Fprintf(&b, "  (auto-derivable — all arguments follow from state)\n")
			} else {
				fmt.Fprintf(&b, "  You must supply: %s (never auto-filled)\n", strings.Join(next.Prescribed.RequiresHumanInput, " "))
			}
		}
		if next.SubAction != nil {
			title := next.SubAction.Title
			if title != "" {
				title = " — " + title
			}
			fmt.Fprintf(&b, "Next sub-action: %s%s (from the plan task DAG; see `flow tasks`)\n", next.SubAction.ID, title)
		}
	} else {
		fmt.Fprintf(&b, "Flow state: unresolved (no oracle advisory)\n")
	}
	return b.String()
}
