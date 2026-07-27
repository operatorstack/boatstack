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
// returns false so the oracle never scores a guessed position. The oracle stays
// delivery-only; pre-activation stages are covered instead by prescribePlanning,
// which names the exact runnable command without ever claiming a flow state.
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
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

// Non-registry prescription markers. These name pre-activation and recovery
// moves the delivery oracle deliberately does not model (deliverycontrol shadows
// the DELIVERY machine only). They are never legal registry transitions, never
// allowlisted for auto-drive, and exist so a prescription's provenance is
// self-describing in JSON and telemetry.
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
const (
	MarkerPlanningInit        = deliverycontrol.TransitionID("planning.init")
	MarkerPlanningCheckSource = deliverycontrol.TransitionID("planning.check_source_plan")
	MarkerPlanningCheckPlan   = deliverycontrol.TransitionID("planning.check_plan")
	MarkerPlanningActivate    = deliverycontrol.TransitionID("planning.activate")
	MarkerPlanningWorkspace   = deliverycontrol.TransitionID("planning.workspace_cut")
	MarkerPlanningWrite       = deliverycontrol.TransitionID("planning.planning_write")
	MarkerPlanningApproval    = deliverycontrol.TransitionID("planning.record_approval")
	MarkerRecoveryDoctor      = deliverycontrol.TransitionID("recovery.doctor")
	MarkerRecoveryDiscard     = deliverycontrol.TransitionID("recovery.discard_delivery")
	MarkerRecoveryRepair      = deliverycontrol.TransitionID("recovery.repair_state")
)

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
	// Prescribed is the exact runnable command for the next move. When the flow
	// position resolves it is the oracle's lowest-cost transition; when it does
	// not, it is the pre-activation prescription for the observed stage (marked by
	// a planning./recovery. Transition). It is non-nil ONLY when the command can
	// be assembled faithfully; otherwise nothing is prescribed rather than a
	// guessed command.
	Prescribed *PrescribedCommand `json:"prescribed,omitempty"`
	// FollowUp names the step after the prescribed pre-activation command, set
	// only by the planning prescription layer (e.g. record approval after
	// check-plan; re-author via planning-write after repair-state). Empty for
	// oracle moves.
	FollowUp string `json:"follow_up,omitempty"`
	// SubAction is the read-only next coding sub-action from the plan's task DAG,
	// surfaced only while the active slice is in BUILD (where "build" is otherwise
	// opaque). It is a pointer into the slice's dependency-ordered tasks; it is nil
	// when the slice is not building or the task graph cannot be read. It carries no
	// completion state and prescribes no command — coding work is never a modeled
	// transition, only an ordered pointer.
	SubAction *FlowTask `json:"sub_action,omitempty"`
	// Alternatives are the other admissible next commands from this position —
	// the computed solution set minus the single Prescribed primary. They let a
	// caller PICK a legal move instead of deriving one from the law's prose.
	// Advisory, never a second primary: the rendering keeps exactly one Run line.
	// control-law: solution-set-derives-from-guard-declarations
	Alternatives []PrescribedCommand `json:"alternatives,omitempty"`
}

// PrescribedCommand is the exact next command that makes the oracle's lowest-cost
// move. Args carries only auto-derivable flags (repo/feature/slice/gate/preview
// path/action). RequiresHumanInput names the flags that must be supplied by a
// human/CI and must NEVER be fabricated (evidence, gate status, the human-confirmed
// preview fingerprint, reviewer identity); those flags are deliberately absent from
// Args. AutoDerivable is true exactly when RequiresHumanInput is empty — the only
// commands the opt-in execute driver may run. Transition is the registry
// TransitionID of an oracle move, or a planning./recovery.-prefixed marker for a
// pre-activation prescription outside the delivery model; markers never pass the
// auto-drive allowlist, so a marked prescription is always prescribe-and-stop.
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
	case deliverycontrol.TransitionID("delivery.record_change"):
		if feature == "" {
			return nil, false
		}
		// Rework: the correction facts (what changed, where it was observed, and
		// its classification) are human knowledge; owe them, never fabricate them.
		cmd.Args = append(repoArgs, "--feature", feature)
		if status.ActiveSlice != "" {
			cmd.Args = append(cmd.Args, "--slice", status.ActiveSlice)
		}
		cmd.RequiresHumanInput = []string{"--message", "--source-stage", "--classification"}
	case deliverycontrol.TransitionID("delivery.undo"):
		// The mutation id names WHICH receipt to reverse — a human decision.
		cmd.Args = repoArgs
		cmd.RequiresHumanInput = []string{"--mutation"}
	case deliverycontrol.TransitionID("delivery.discard_delivery"):
		if feature == "" {
			return nil, false
		}
		cmd.Args = append(repoArgs, "--feature", feature)
	default:
		// Recovery/observe transitions outside the set above are not prescribed as
		// a forward move; emit nothing rather than a command whose arguments we
		// cannot derive.
		return nil, false
	}
	cmd.AutoDerivable = len(cmd.RequiresHumanInput) == 0
	return cmd, true
}

// prescribePlanning assembles the exact runnable command for a stage the
// delivery oracle deliberately does not model: the pre-activation planning
// stages and the blocked recovery stages. It closes the prescriptive loop —
// every reachable pre-activation stage names at least one concrete command —
// without adding planning states to deliverycontrol, whose declared scope is
// the DELIVERY machine only. It returns (nil, "") exactly for the documented
// exceptions: AMBIGUOUS (choosing a feature is a human act, and the candidates
// already surface via Reason/BlockingAmbiguity) and unknown stages (never
// guess). AutoDerivable here is a rendering fact ("all arguments follow from
// state"), not an execution grant: markers are off the auto-drive allowlist
// and have no executor, so the driver always prescribes-and-stops on them.
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
// planningFeatureDir is the single joined form of a feature's planning
// directory used by the prescription layer and the solution-set enumerator.
func planningFeatureDir(repo, feature string) string {
	return filepath.Join(repo, ".product-loop", "features", feature)
}

func prescribePlanning(repo string, status NextStatus) (*PrescribedCommand, string) {
	var repoArgs []string
	if repo != "" && repo != "." {
		repoArgs = []string{"--repo", repo}
	}
	featureDir := planningFeatureDir(repo, status.Feature)
	finish := func(cmd *PrescribedCommand, followUp string) (*PrescribedCommand, string) {
		cmd.AutoDerivable = len(cmd.RequiresHumanInput) == 0
		return cmd, followUp
	}
	switch status.ObservedStage {
	case "NOT_INITIALIZED":
		return finish(&PrescribedCommand{
			Verb: "init", Args: repoArgs, Transition: MarkerPlanningInit,
		}, "")
	case "NOT_STARTED":
		// The host plan path is knowable only to the host conversation; owe it.
		return finish(&PrescribedCommand{
			Verb: "check-source-plan", Args: repoArgs,
			RequiresHumanInput: []string{"--plan"},
			Transition:         MarkerPlanningCheckSource,
		}, "Then run auto-plan with the validated SOURCE_PLAN path; author every feature artifact through `boatstack-helper planning-write` (document on stdin).")
	case "DRAFT_PLAN":
		return finish(&PrescribedCommand{
			Verb:       "check-plan",
			Args:       []string{"--plan", filepath.Join(featureDir, "plan.md")},
			Transition: MarkerPlanningCheckPlan,
		}, "After the check passes, present the plan for approval and record it with `record-approval` using the exact PLAN_FINGERPRINT it printed.")
	case "APPROVED", "POLICY_READY":
		// ResolveNext already ordered the move: a fresh workspace cut when one is
		// needed, otherwise activation. "build" is an operation name, not a verb;
		// activate-plan is the build operation's first concrete command.
		if status.NextOperation == "workspace-cut" {
			return finish(buildWorkspaceCut(repoArgs, status.Feature),
				"Then activate the plan from the fresh workspace with `activate-plan`.")
		}
		return finish(buildActivatePlan(featureDir, status.ObservedStage), "")
	case "INVALID_STATE":
		switch status.NextOperation {
		case "doctor":
			return finish(&PrescribedCommand{
				Verb: "doctor", Args: repoArgs, Transition: MarkerRecoveryDoctor,
			}, "")
		case "discard-delivery":
			cmd := &PrescribedCommand{Verb: "discard-delivery", Args: repoArgs, Transition: MarkerRecoveryDiscard}
			if len(status.BlockingAmbiguity) == 1 {
				cmd.Args = append(cmd.Args, "--feature", status.BlockingAmbiguity[0])
			} else {
				cmd.RequiresHumanInput = []string{"--feature"}
			}
			return finish(cmd, "")
		case "repair-state":
			// ResolveNext never routes here today; the safety finding does. Keep the
			// case so any carrier of the repair-state operation gets the full loop:
			// quarantine, then re-author through the owned channel.
			cmd := &PrescribedCommand{Verb: "repair-state", Args: repoArgs, Transition: MarkerRecoveryRepair}
			slug := status.Feature
			if slug == "" && len(status.BlockingAmbiguity) == 1 {
				slug = status.BlockingAmbiguity[0]
			}
			if slug != "" {
				cmd.Args = append(cmd.Args, "--feature", slug)
			} else {
				slug = "<feature>"
			}
			return finish(cmd, fmt.Sprintf("After repair, re-author the planning Markdown through the owned channel: `boatstack-helper planning-write --repo . --feature %s --artifact <name>` with the document on stdin.", slug))
		}
		return nil, ""
	default:
		// AMBIGUOUS and anything unrecognized: no prescription, never a guess.
		return nil, ""
	}
}

// buildWorkspaceCut and buildActivatePlan are the single assembly points for
// their commands, shared by prescribePlanning (the primary) and the solution-set
// enumerator (the alternatives) so the two can never drift apart.
// control-law: solution-set-derives-from-guard-declarations
func buildWorkspaceCut(repoArgs []string, feature string) *PrescribedCommand {
	return &PrescribedCommand{
		Verb:       "workspace-cut",
		Args:       append(append([]string{}, repoArgs...), "--feature", feature),
		Transition: MarkerPlanningWorkspace,
	}
}

func buildActivatePlan(featureDir, stage string) *PrescribedCommand {
	args := []string{
		"--plan", filepath.Join(featureDir, "plan.md"),
		"--out-dir", filepath.Join(featureDir, "compiled"),
		"--output", filepath.Join(featureDir, "plan.lock.json"),
	}
	if stage == "APPROVED" {
		args = append(args, "--approval", filepath.Join(featureDir, "approval.md"))
	}
	return &PrescribedCommand{Verb: "activate-plan", Args: args, Transition: MarkerPlanningActivate}
}

// NextControl composes the authoritative read-only recommendation (ResolveNext)
// with the deterministic oracle to advise the lowest-cost next move toward a
// published delivery. It performs no mutation and is safe to call at any time.
func NextControl(repo, feature string) (FlowNext, error) {
	status, err := ResolveNext(repo, feature)
	if err != nil {
		return FlowNext{}, err
	}
	return nextControlFromStatus(repo, status)
}

// nextControlFromStatus is NextControl on an already-resolved status, so a
// caller that renders both the friendly phrase and the prescription (the
// response contract) observes state exactly once — one resolution, no drift.
func nextControlFromStatus(repo string, status NextStatus) (FlowNext, error) {
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
		// Pre-activation and blocked stages sit outside the delivery oracle, but
		// they still name their exact runnable command. Resolved stays false: the
		// flow-state conservativeness contract is untouched.
		if cmd, followUp := prescribePlanning(repo, status); cmd != nil {
			out.Prescribed = cmd
			out.FollowUp = followUp
		}
		out.Alternatives = alternativesFor(repo, status, out)
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
			if prescribed, ok := prescribeCommand(repo, status.Feature, status, advice.NextTransition); ok {
				out.Prescribed = prescribed
			}
		}
		// While the slice is building, "build" is opaque; surface the read-only
		// dependency-ordered next sub-action from the plan's task DAG as a hint.
		if state == deliverycontrol.StateBuild {
			if tasks, err := FlowTasksForActiveSlice(repo, status.Feature); err == nil && tasks.Resolved && len(tasks.Ordered) > 0 {
				hint := tasks.Ordered[0]
				out.SubAction = &hint
			}
		}
	}
	out.Alternatives = alternativesFor(repo, status, out)
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
			writePrescribed(&b, next.Prescribed)
		}
		if next.SubAction != nil {
			title := next.SubAction.Title
			if title != "" {
				title = " — " + title
			}
			fmt.Fprintf(&b, "Next sub-action: %s%s (from the plan task DAG; see `flow tasks`)\n", next.SubAction.ID, title)
		}
	} else if next.Prescribed != nil {
		fmt.Fprintf(&b, "Flow state: pre-activation (delivery oracle not engaged)\n")
		writePrescribed(&b, next.Prescribed)
		if next.FollowUp != "" {
			fmt.Fprintf(&b, "Then: %s\n", next.FollowUp)
		}
	} else {
		fmt.Fprintf(&b, "Flow state: unresolved (no oracle advisory; follow the recommended operation above)\n")
	}
	writeAlternatives(&b, next.Alternatives)
	return b.String()
}

// writeAlternatives renders the solution set's other legal moves as ONE line of
// verbs with a short purpose gloss — never a second Run line, so the response
// contract's single primary action holds.
// control-law: solution-set-derives-from-guard-declarations
func writeAlternatives(b *strings.Builder, alternatives []PrescribedCommand) {
	if len(alternatives) == 0 {
		return
	}
	shown := alternatives
	if len(shown) > solutionSetTextCap {
		shown = shown[:solutionSetTextCap]
	}
	labels := make([]string, 0, len(shown))
	for _, alt := range shown {
		labels = append(labels, alt.Verb+" ("+solutionGloss(alt.Transition)+")")
	}
	fmt.Fprintf(b, "Also legal from here: %s\n", strings.Join(labels, ", "))
	if len(alternatives) > len(shown) {
		fmt.Fprintf(b, "  (%d more in `flow next --json` under alternatives)\n", len(alternatives)-len(shown))
	}
}

// writePrescribed renders the Run line and its owed-input annotation for a
// prescribed command, shared by the oracle and pre-activation branches.
func writePrescribed(b *strings.Builder, p *PrescribedCommand) {
	fmt.Fprintf(b, "Run: %s\n", p.CommandLine())
	if p.AutoDerivable {
		fmt.Fprintf(b, "  (auto-derivable — all arguments follow from state)\n")
	} else {
		fmt.Fprintf(b, "  You must supply: %s (never auto-filled)\n", strings.Join(p.RequiresHumanInput, " "))
	}
}
