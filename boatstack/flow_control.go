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
	// Post-publish markers (merged terminal only). The delivery machine
	// deliberately models nothing past PUBLISHED — merging is not a Boatstack
	// verb and FEATURE_COMPLETE is entered by observation — so the post-publish
	// steps are markers like the planning ones: self-describing provenance,
	// never legal registry transitions, never on the auto-drive allowlist.
	// control-law: merged-terminal-prescribes-merge-never-executes-it
	MarkerPublishedWatch = deliverycontrol.TransitionID("published.watch_checks")
	MarkerPublishedMerge = deliverycontrol.TransitionID("published.merge")
	// MarkerPublishedAttach names the owed-attachment retry of a published
	// PR's visual-evidence comment. Unlike the merged-terminal markers above
	// it fires under BOTH terminals: attaching evidence completes the
	// publication itself, it is not merge pursuit.
	MarkerPublishedAttach = deliverycontrol.TransitionID("published.attach_evidence")
)

// NextActor names who performs the prescribed next step. The operator owns a
// step only when it owes operator knowledge or authority — an approval, a
// publish decision, a feature choice, a plan path, a correction fact. The
// agent owns every other step, including steps whose owed inputs are evidence
// the agent produces by doing the work (test runs, the review protocol). A
// working response may end only on an operator-owned step or a terminal
// state; that boundary is the operator frontier.
// control-law: turn-ends-only-at-the-operator-frontier
type NextActor string

const (
	// NextActorAgent — the coding agent performs this step now. A working
	// response never ends on an agent-owned step; the read-only status view
	// renders it as a one-key delegation instead of executing it.
	NextActorAgent NextActor = "agent"
	// NextActorOperator — the step owes operator knowledge or authority; the
	// response may end here.
	NextActorOperator NextActor = "operator"
	// NextActorNone — terminal; nobody owes an action.
	NextActorNone NextActor = "none"
)

// operatorOwedFlags are the prescribed-command inputs that carry operator
// knowledge or authority rather than work-derivable evidence. A prescription
// owing any of these belongs to the operator. The evidence flags (--status,
// --evidence, --reviewer-identity, --review-method) are deliberately absent:
// the agent obtains those by doing the work — never by fabrication — so owing
// them does not move the step across the frontier.
// control-law: turn-ends-only-at-the-operator-frontier
var operatorOwedFlags = map[string]bool{
	"--plan":                true, // which source plan: product knowledge
	"--feature":             true, // which delivery: the operator names the slug
	"--mutation":            true, // which receipt to reverse: an operator decision
	"--preview-fingerprint": true, // publish authority is human-confirmed
	"--message":             true, // correction facts are human knowledge
	"--source-stage":        true,
	"--classification":      true,
	"--mechanism":           true,
}

// classifyNextActor types the next step by who must act. Fail-closed: anything
// it cannot place returns operator, which preserves prescribe-and-stop — the
// worst misclassification is today's behavior, never a runaway agent.
// control-law: turn-ends-only-at-the-operator-frontier
func classifyNextActor(status NextStatus, next FlowNext) NextActor {
	switch {
	case status.ObservedStage == "FEATURE_COMPLETE",
		status.ObservedStage == "PUBLISHED" && status.Lifecycle == "PUBLISHED_MERGED":
		return NextActorNone
	case status.ObservedStage == "PUBLISHED":
		// Both the current visual_pending state and the legacy manual_required
		// state are work-derivable external-host retries.
		// control-law: turn-ends-only-at-the-operator-frontier
		if status.Lifecycle == "PUBLISHED_OPEN" && status.GoalEscape == "" && (status.VisualPublication == "visual_pending" || status.VisualPublication == "manual_required") {
			return NextActorAgent
		}
		// Under the default published terminal, reviewing the open pull
		// request is the operator's act — unchanged. Under the merged
		// terminal, the frontier extends: the phases whose next step is
		// work-derivable (watch running checks, fix failing checks from the
		// check logs, run the prescribed merge of an eligible PR) are the
		// agent's; every phase owing operator authority or knowledge — a
		// review approval, a changes-requested verdict, a closed PR, an
		// unknown position — stays the operator's. Fail-closed: the zero
		// Terminal behaves as published.
		// control-law: turn-ends-only-at-the-operator-frontier
		// A fired goal escape demotes unconditionally: the pursuit contract
		// ended, so no phase can hand the step back to the agent.
		// control-law: goal-escape-demotes-to-operator-and-stops
		if next.Terminal == TerminalMerged && status.GoalEscape == "" {
			switch PRPhase(status.PRPhase) {
			case PRPhaseChecksPending, PRPhaseChecksFailing, PRPhaseMergeEligible:
				return NextActorAgent
			}
		}
		return NextActorOperator
	case next.Prescribed == nil:
		// Ambiguity and unprescribed blocks resolve only by operator choice.
		return NextActorOperator
	case next.Prescribed.Transition == PublishTransition:
		// Opening or updating a PR is operator-confirmed (`o`/`u`), regardless
		// of which flags happen to be owed.
		return NextActorOperator
	}
	for _, flag := range next.Prescribed.RequiresHumanInput {
		if operatorOwedFlags[flag] {
			return NextActorOperator
		}
	}
	return NextActorAgent
}

// FlowNext is the advisory answer for `flow next`: the current delivery-flow
// state, the real recommended operation (from ResolveNext — the authoritative
// next-move table), and the oracle's lowest-cost next control plus the remaining
// cost to the goal. It is purely advisory; it changes no command, gate, or
// authority. Resolved is false when the oracle cannot place the flow, in which
// case only the real recommendation is meaningful.
type FlowNext struct {
	Resolved bool                    `json:"resolved"`
	State    deliverycontrol.StateID `json:"state,omitempty"`
	Goal     deliverycontrol.StateID `json:"goal"`
	// Terminal is the standing goal of this delivery ("published" or
	// "merged"), resolved state-then-config-then-default. Goal above remains
	// the ORACLE's sink (always StatePublished — the delivery machine has no
	// modeled transition past it); Terminal is the operator-facing setpoint
	// that decides whether anything is still owed after publish. In this
	// slice it is surfaced only; post-publish prescriptions follow.
	// control-law: terminal-goal-defaults-to-published-and-hydrates-from-state-then-config
	Terminal      DeliveryTerminal             `json:"terminal"`
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
	// Actor names who performs the next step: "agent" when the step is the
	// coding agent's to do now, "operator" when it owes operator knowledge or
	// authority (the response may end there — the operator frontier), "none"
	// when the flow is terminal.
	// control-law: turn-ends-only-at-the-operator-frontier
	Actor NextActor `json:"next_actor"`
}

// PrescribedCommand is the exact next command that makes the oracle's lowest-cost
// move. Args carries only auto-derivable flags (repo/feature/slice/gate/preview
// path/action). RequiresHumanInput names flags or bounded content that must be
// supplied by a human/CI/authoring agent and must NEVER be fabricated (evidence,
// gate status, the planning document, the human-confirmed preview fingerprint,
// reviewer identity); those inputs are deliberately absent from Args.
// AutoDerivable is true exactly when RequiresHumanInput is empty — the only
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
	// Program names the executable when the prescribed step is honestly NOT a
	// boatstack-helper verb (today: `gh`, for the operator-frontier merge).
	// Empty means boatstack-helper, exactly as before. A foreign-program
	// command is rendering-only by construction: canAutoDrive refuses it
	// categorically and executePrescribed has no executor for it, so the
	// execute driver can never run a program that is not the helper.
	// control-law: merged-terminal-prescribes-merge-never-executes-it
	Program string `json:"program,omitempty"`
}

const planningMarkdownInput = "stdin:markdown"

func posixPlanningWord(value string) string {
	if value != "" {
		safe := true
		for _, char := range []byte(value) {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("_@%+=:,./-", rune(char))) {
				safe = false
				break
			}
		}
		if safe {
			return value
		}
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// CommandLine renders the auto-derivable part of the prescribed command as a
// runnable string. Human-required inputs use explicit <REQUIRED> placeholders;
// planning Markdown is placed inside the same literal envelope the hook admits.
// The rendering is never fabricated or runnable as-is while input is still owed.
func (p PrescribedCommand) CommandLine() string {
	literalPlanningInput := false
	for _, input := range p.RequiresHumanInput {
		if input == planningMarkdownInput {
			literalPlanningInput = true
			break
		}
	}
	program := p.Program
	if program == "" {
		if literalPlanningInput {
			program = projectLocalHelperCommand()
		} else {
			program = "boatstack-helper"
		}
	}
	parts := append([]string{program, p.Verb}, p.Args...)
	for _, flag := range p.RequiresHumanInput {
		if flag == planningMarkdownInput {
			literalPlanningInput = true
			continue
		}
		parts = append(parts, flag, "<REQUIRED>")
	}
	line := strings.Join(parts, " ")
	if literalPlanningInput {
		for index := range parts {
			parts[index] = posixPlanningWord(parts[index])
		}
		line = strings.Join(parts, " ")
		return line + " <<'BOATSTACK_PLAN_EOF'\n<REQUIRED>\nBOATSTACK_PLAN_EOF"
	}
	return line
}

// prescribeCommand assembles the runnable command for a forward delivery
// transition. It returns (nil, false) for any transition it cannot assemble
// faithfully — so the caller emits nothing rather than a guessed command. The
// emitted verb is always the registry CLIVerb of the transition (single source),
// and human-owed inputs are listed, never filled.
var deriveAutonomousPRPublish = func(repo, feature, previewPath string) (string, string, string, bool) {
	preview, _, err := CheckPRPreview(repo, previewPath)
	if err != nil {
		return "", "", "", false
	}
	action, _, err := RecommendedPRAction(repo)
	if err != nil || (action != "open" && action != "update") {
		return "", "", "", false
	}
	planPath := filepath.Join(WorkspaceFor(repo).FeatureDir(feature), "plan.md")
	check, err := CheckPlan(planPath)
	if err != nil {
		return "", "", "", false
	}
	autonomyPath := filepath.Join(filepath.Dir(planPath), "autonomy.md")
	receipt, err := CheckAutonomyReceipt(autonomyPath, check, repo, RunTargetPR, action)
	if err != nil || receipt.PRAction != action {
		return "", "", "", false
	}
	return action, preview.Fingerprint, autonomyPath, true
}

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
		if action, fingerprint, autonomyPath, authorized := deriveAutonomousPRPublish(repo, feature, preview); authorized {
			cmd.Args = append(repoArgs, "--preview", preview, "--action", action, "--preview-fingerprint", fingerprint, "--autonomy", autonomyPath)
		} else {
			cmd.Args = append(repoArgs, "--preview", preview, "--action", "open")
			cmd.RequiresHumanInput = []string{"--preview-fingerprint"}
		}
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
		cmd.RequiresHumanInput = []string{"--message", "--source-stage", "--classification", "--mechanism"}
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
	return WorkspaceFor(repo).FeatureDir(feature)
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
		}, fmt.Sprintf("Then run auto-plan with the validated SOURCE_PLAN path; author every feature artifact through one complete literal `%s planning-write` envelope from `%s`.", projectLocalHelperCommand(), generatedWorkflowReference()))
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
			return finish(cmd, fmt.Sprintf("After repair, re-author the planning Markdown through the owned channel: one complete literal `%s planning-write --repo . --feature %s --artifact <name>` envelope from `%s`.", projectLocalHelperCommand(), slug, generatedWorkflowReference()))
		}
		return nil, ""
	default:
		// AMBIGUOUS and anything unrecognized: no prescription, never a guess.
		return nil, ""
	}
}

// prescribeVisualAttach closes the owed-attachment gap of a published-open
// PR so the flow never goes dark on visual_pending or manual_required. It
// fires under BOTH terminals — the attachment completes publication, it is
// not merge pursuit. Both current and legacy owed states prescribe the same
// externally hosted attach-evidence retry. A fired goal escape prescribes
// nothing, exactly like the post-publish layer.
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
func prescribeVisualAttach(repo string, status NextStatus) (*PrescribedCommand, string) {
	if status.ObservedStage != "PUBLISHED" || status.Lifecycle != "PUBLISHED_OPEN" || status.Feature == "" || status.GoalEscape != "" {
		return nil, ""
	}
	var repoArgs []string
	if repo != "" && repo != "." {
		repoArgs = []string{"--repo", repo}
	}
	switch status.VisualPublication {
	case "visual_pending", "manual_required":
		cmd := &PrescribedCommand{
			Verb: "attach-evidence", Args: append(repoArgs, "--feature", status.Feature),
			AutoDerivable: true, Transition: MarkerPublishedAttach,
		}
		return cmd, "The PR is open; only its externally hosted Boatstack visual-evidence comment is owed. Retry the same evidence fingerprint and comment after host access recovers."
	}
	return nil, ""
}

// prescribePostPublish closes the prescriptive loop past publish, but ONLY
// under the merged terminal: with the published default this function returns
// nothing and post-publish behavior is exactly what it always was. The
// delivery oracle is at its sink at PUBLISHED, so these prescriptions derive
// from the live PR observation instead of the registry graph:
//
//	checks running   -> flow watch (agent; read-only wait, exits on change)
//	checks failing   -> record-change --source-stage ci (agent; the failure
//	                    facts are work-derivable from the failing check logs,
//	                    so this branch's owed flags do not cross the frontier)
//	merge eligible   -> gh pr merge <url> --squash (agent, PRESCRIBE-ONLY:
//	                    Program!="" is categorically undrivable and the agent
//	                    runs gh under its own authority, never Boatstack's)
//	everything else  -> nothing; approvals, changes-requested verdicts, closed
//	                    PRs, and unknown positions are the operator's.
//
// FEATURE_COMPLETE and a merged lifecycle prescribe nothing: the goal is met.
// control-law: merged-terminal-prescribes-merge-never-executes-it
// control-law: prescriptive-closure-every-stage-names-a-runnable-command
func prescribePostPublish(repo string, status NextStatus, terminal DeliveryTerminal) (*PrescribedCommand, string) {
	if terminal != TerminalMerged || status.ObservedStage != "PUBLISHED" || status.Lifecycle == "PUBLISHED_MERGED" {
		return nil, ""
	}
	// A fired escape prescribes nothing: demote-and-stop, never
	// demote-and-suggest. control-law: goal-escape-demotes-to-operator-and-stops
	if status.GoalEscape != "" {
		return nil, ""
	}
	var repoArgs []string
	if repo != "" && repo != "." {
		repoArgs = []string{"--repo", repo}
	}
	switch PRPhase(status.PRPhase) {
	case PRPhaseChecksPending:
		cmd := &PrescribedCommand{
			Verb:       "flow",
			Args:       append([]string{"watch"}, repoArgs...),
			Transition: MarkerPublishedWatch,
		}
		cmd.AutoDerivable = true
		return cmd, "When the watch exits, resolve the flow again and continue from the fresh state."
	case PRPhaseChecksFailing:
		if status.Feature == "" {
			return nil, ""
		}
		desc, ok := deliverycontrol.Transition(deliverycontrol.TransitionID("delivery.record_change"))
		if !ok || desc.CLIVerb == "" {
			return nil, ""
		}
		// The registry transition IS the fix path — no new machinery. The
		// source stage is derivable (this observation is the CI failure); the
		// message and classification are owed, to be derived from the failing
		// check logs, never fabricated.
		cmd := &PrescribedCommand{Verb: desc.CLIVerb, Transition: deliverycontrol.TransitionID("delivery.record_change")}
		cmd.Args = append(append([]string{}, repoArgs...), "--feature", status.Feature)
		if status.ActiveSlice != "" {
			cmd.Args = append(cmd.Args, "--slice", status.ActiveSlice)
		}
		cmd.Args = append(cmd.Args, "--source-stage", "ci")
		cmd.RequiresHumanInput = []string{"--message", "--classification", "--mechanism"}
		followUp := "Read the failing check logs"
		if len(status.PRFailingChecks) > 0 {
			followUp += " (" + strings.Join(status.PRFailingChecks, ", ") + ")"
		}
		followUp += " to derive the exact message, classification, and changed mechanism; after the correction re-passes its gates, republish with publish-pr --action update."
		return cmd, followUp
	case PRPhaseMergeEligible:
		if strings.TrimSpace(status.PRURL) == "" {
			return nil, ""
		}
		cmd := &PrescribedCommand{
			Program:    "gh",
			Verb:       "pr",
			Args:       []string{"merge", status.PRURL, "--squash"},
			Transition: MarkerPublishedMerge,
		}
		cmd.AutoDerivable = len(cmd.RequiresHumanInput) == 0
		return cmd, "Run it exactly as rendered — this merge is prescribed only from the live merge-eligible observation, never with --admin, and never for a different pull request."
	default:
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
		Terminal:      resolveDeliveryTerminal(repo, status.Feature),
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
		out.Actor = classifyNextActor(status, out)
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
	// Past publish the oracle sits at its sink and prescribes nothing. An
	// owed visual attachment is consulted first and under BOTH terminals —
	// it completes the publication itself — then, under the merged terminal
	// only, the observation-derived post-publish layer. Each fills only an
	// empty prescription — neither can override an oracle move.
	if out.Prescribed == nil {
		if cmd, followUp := prescribeVisualAttach(repo, status); cmd != nil {
			out.Prescribed = cmd
			out.FollowUp = followUp
		}
	}
	if out.Prescribed == nil {
		if cmd, followUp := prescribePostPublish(repo, status, out.Terminal); cmd != nil {
			out.Prescribed = cmd
			out.FollowUp = followUp
		}
	}
	out.Alternatives = alternativesFor(repo, status, out)
	out.Actor = classifyNextActor(status, out)
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
	if next.Actor != "" {
		fmt.Fprintf(&b, "Next actor: %s\n", next.Actor)
	}
	// The published default renders exactly as before; only the widened goal
	// earns a line, so opting in is visible and not opting in changes nothing.
	if next.Terminal == TerminalMerged {
		fmt.Fprintf(&b, "Terminal goal: merged (delivery.terminal)\n")
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
