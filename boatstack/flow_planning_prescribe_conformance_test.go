package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: prescriptive-closure-every-stage-names-a-runnable-command
//
// `flow next` used to go quiet for every pre-activation stage: the delivery
// oracle deliberately does not model planning, so the advisory printed only a
// soft operation name and no runnable command, and a denied raw write named
// only the cleanup verb. The contract these tests hold: every ObservedStage
// ResolveNext can emit either resolves through the delivery oracle, or is
// prescribed a concrete runnable command by prescribePlanning, or sits on an
// explicit documented exception list — and the prescriptions never fabricate
// human-owed input, never become legal delivery moves, and never auto-execute.

// planningExceptions are the stages that deliberately prescribe nothing.
// Adding a stage to ResolveNext without a prescription rule or an entry here
// fails the totality sweep below.
var planningExceptions = map[string]string{
	"AMBIGUOUS": "choosing between candidate features/deliveries is a human act; candidates surface via Reason/BlockingAmbiguity",
}

// planningStages is one representative synthetic NextStatus per pre-activation
// stage (INVALID_STATE once per NextOperation route ResolveNext or the safety
// finding can carry).
var planningStages = []NextStatus{
	{ObservedStage: "NOT_INITIALIZED", NextOperation: "init"},
	{ObservedStage: "NOT_STARTED", NextOperation: "auto-plan"},
	{ObservedStage: "DRAFT_PLAN", NextOperation: "plan-gate", Feature: "demo"},
	{ObservedStage: "DRAFT_PLAN", NextOperation: "workspace-cut", Feature: "demo"},
	{ObservedStage: "APPROVED", NextOperation: "build", Feature: "demo"},
	{ObservedStage: "APPROVED", NextOperation: "workspace-cut", Feature: "demo"},
	{ObservedStage: "POLICY_READY", NextOperation: "build", Feature: "demo"},
	{ObservedStage: "POLICY_READY", NextOperation: "workspace-cut", Feature: "demo"},
	{ObservedStage: "INVALID_STATE", NextOperation: "doctor"},
	{ObservedStage: "INVALID_STATE", NextOperation: "discard-delivery", BlockingAmbiguity: []string{"stale"}},
	{ObservedStage: "INVALID_STATE", NextOperation: "repair-state", Feature: "demo"},
}

// Positive: each pre-activation stage reached through the real read-only
// projection (NextControl end to end) yields the exact runnable command.
func TestNextControlPrescribesPreActivationStages(t *testing.T) {
	t.Run("not_initialized", func(t *testing.T) {
		repo := t.TempDir()
		if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		if next.Resolved {
			t.Fatalf("pre-activation must not resolve a flow state: %+v", next)
		}
		if next.Prescribed == nil || next.Prescribed.Verb != "init" {
			t.Fatalf("NOT_INITIALIZED must prescribe init: %+v", next.Prescribed)
		}
	})

	t.Run("not_started_owes_plan_path", func(t *testing.T) {
		repo := nextTestRepo(t)
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		p := next.Prescribed
		if p == nil || p.Verb != "check-source-plan" {
			t.Fatalf("NOT_STARTED must prescribe check-source-plan: %+v", p)
		}
		if p.AutoDerivable || len(p.RequiresHumanInput) != 1 || p.RequiresHumanInput[0] != "--plan" {
			t.Fatalf("the host plan path is unknowable and must be owed, never fabricated: %+v", p)
		}
		if !strings.Contains(next.FollowUp, "planning-write") {
			t.Fatalf("the planning follow-up must name the owned authoring channel: %q", next.FollowUp)
		}
	})

	t.Run("draft_plan_prescribes_check_plan_on_real_path", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		p := next.Prescribed
		if p == nil || p.Verb != "check-plan" || !p.AutoDerivable {
			t.Fatalf("DRAFT_PLAN must prescribe an auto-derivable check-plan: %+v", p)
		}
		planPath := ""
		for i, arg := range p.Args {
			if arg == "--plan" && i+1 < len(p.Args) {
				planPath = p.Args[i+1]
			}
		}
		if planPath == "" {
			t.Fatalf("check-plan prescription carries no --plan: %+v", p.Args)
		}
		if _, err := os.Stat(planPath); err != nil {
			t.Fatalf("prescribed --plan does not point at the saved plan: %v", err)
		}
		if !strings.Contains(next.FollowUp, "record-approval") {
			t.Fatalf("DRAFT_PLAN follow-up must route to record-approval: %q", next.FollowUp)
		}
	})

	t.Run("approved_prescribes_activate_plan_with_approval", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		approval := filepath.Join(repo, ".product-loop", "features", "demo", "approval.md")
		if err := os.WriteFile(approval, []byte("approved\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		p := next.Prescribed
		if p == nil || p.Verb != "activate-plan" || !p.AutoDerivable {
			t.Fatalf("APPROVED must prescribe an auto-derivable activate-plan: %+v", p)
		}
		joined := strings.Join(p.Args, " ")
		for _, want := range []string{"--plan", "--out-dir", "--output", "--approval"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("activate-plan prescription missing %s: %q", want, joined)
			}
		}
	})

	t.Run("policy_ready_prescribes_activate_plan_without_approval", func(t *testing.T) {
		repo := nextTestRepo(t)
		config := testConfig()
		config.Workflow.HumanPlanApproval = false
		value, err := MarshalJSON(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
			t.Fatal(err)
		}
		writeSavedFeaturePlan(t, repo, "demo")
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		p := next.Prescribed
		if p == nil || p.Verb != "activate-plan" {
			t.Fatalf("POLICY_READY must prescribe activate-plan: %+v", p)
		}
		if strings.Contains(strings.Join(p.Args, " "), "--approval") {
			t.Fatalf("policy activation must not reference an approval receipt: %+v", p.Args)
		}
	})

	t.Run("invalid_state_prescribes_discard_delivery", func(t *testing.T) {
		repo := nextTestRepo(t)
		directory := filepath.Join(repo, ".product-loop", "features", "orphan")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "pr.md"), []byte("# Preview\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		p := next.Prescribed
		if p == nil || p.Verb != "discard-delivery" {
			t.Fatalf("INVALID_STATE orphan must prescribe discard-delivery: %+v", p)
		}
		if !strings.Contains(strings.Join(p.Args, " "), "--feature orphan") {
			t.Fatalf("a unique blocker derives --feature: %+v", p.Args)
		}
	})
}

// Relation/Totality: every ObservedStage ResolveNext can emit is oracle-owned,
// prescribed, or a documented exception. A new stage fails here until it gets a
// rule or an exception entry.
func TestEveryObservedStageIsPrescribedOrExcepted(t *testing.T) {
	prescribed := map[string]bool{}
	for _, status := range planningStages {
		if cmd, _ := prescribePlanning(".", status); cmd != nil {
			prescribed[status.ObservedStage] = true
		}
	}
	allStages := []string{
		"NOT_INITIALIZED", "NOT_STARTED", "DRAFT_PLAN", "APPROVED", "POLICY_READY",
		"AMBIGUOUS", "INVALID_STATE",
		"BUILD", "TEST_PASSED", "REVIEW_PASSED", "PR_PREVIEW", "PUBLISHED", "FEATURE_COMPLETE",
	}
	for _, stage := range allStages {
		if _, oracle := flowStateFromStage(stage); oracle {
			continue
		}
		if prescribed[stage] {
			continue
		}
		if reason, ok := planningExceptions[stage]; ok {
			if reason == "" {
				t.Fatalf("exception for %s must carry a reason", stage)
			}
			continue
		}
		t.Errorf("stage %s is neither oracle-owned, prescribed, nor a documented exception", stage)
	}
}

// Negative: prescriptions never fabricate human-owed input, and AutoDerivable
// is exactly the absence of owed input.
func TestPlanningPrescriptionsNeverFabricateHumanInput(t *testing.T) {
	for _, status := range planningStages {
		cmd, _ := prescribePlanning(".", status)
		if cmd == nil {
			continue
		}
		if cmd.AutoDerivable != (len(cmd.RequiresHumanInput) == 0) {
			t.Errorf("%s/%s: AutoDerivable must equal owed-input emptiness: %+v", status.ObservedStage, status.NextOperation, cmd)
		}
		for _, owed := range cmd.RequiresHumanInput {
			for _, arg := range cmd.Args {
				if arg == owed {
					t.Errorf("%s/%s: owed flag %s must never appear in Args: %+v", status.ObservedStage, status.NextOperation, owed, cmd.Args)
				}
			}
		}
	}
	// A non-unique blocker cannot derive --feature; it must be owed.
	cmd, _ := prescribePlanning(".", NextStatus{
		ObservedStage: "INVALID_STATE", NextOperation: "discard-delivery",
		BlockingAmbiguity: []string{"one", "two"},
	})
	if cmd == nil || cmd.AutoDerivable || len(cmd.RequiresHumanInput) != 1 || cmd.RequiresHumanInput[0] != "--feature" {
		t.Fatalf("ambiguous discard-delivery must owe --feature: %+v", cmd)
	}
}

// Bypass: planning/recovery markers can never reach the delivery machine or
// the execute driver — a marked prescription is always prescribe-and-stop.
func TestPlanningMarkersCannotReachDeliveryMachineOrDriver(t *testing.T) {
	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())
	states := []deliverycontrol.StateID{
		deliverycontrol.StateUninitialized, deliverycontrol.StatePending,
		deliverycontrol.StateBuild, deliverycontrol.StateTestPassed,
		deliverycontrol.StateReviewPassed, deliverycontrol.StatePublished,
	}
	for _, status := range planningStages {
		cmd, _ := prescribePlanning(".", status)
		if cmd == nil {
			continue
		}
		if _, ok := deliverycontrol.Transition(cmd.Transition); ok {
			t.Errorf("marker %s must not be a registry transition", cmd.Transition)
		}
		for _, state := range states {
			if graph.IsLegalMove(state, cmd.Transition) {
				t.Errorf("marker %s must not be a legal move from %s", cmd.Transition, state)
			}
		}
		decision := decideDrive(FlowNext{Prescribed: cmd}, true, false, autoDrivableTransitions)
		if decision.Action != DrivePrescribe {
			t.Errorf("marker %s must prescribe-and-stop under --execute, got %s", cmd.Transition, decision.Action)
		}
	}
}

// Failure-state: the repair loop names the owned authoring channel; ambiguous
// and unknown stages prescribe nothing rather than a guess, and the rendering
// still routes to the recommended operation.
func TestPlanningPrescriptionFailureStates(t *testing.T) {
	t.Run("repair_follow_up_names_planning_write", func(t *testing.T) {
		cmd, followUp := prescribePlanning(".", NextStatus{
			ObservedStage: "INVALID_STATE", NextOperation: "repair-state", Feature: "demo",
		})
		if cmd == nil || cmd.Verb != "repair-state" {
			t.Fatalf("repair-state route must be prescribed: %+v", cmd)
		}
		if !strings.Contains(followUp, "planning-write --repo . --feature demo") {
			t.Fatalf("repair follow-up must name planning-write for the feature: %q", followUp)
		}
	})

	t.Run("unknown_and_ambiguous_prescribe_nothing", func(t *testing.T) {
		for _, stage := range []string{"AMBIGUOUS", "SOMETHING_NEW", ""} {
			if cmd, followUp := prescribePlanning(".", NextStatus{ObservedStage: stage}); cmd != nil || followUp != "" {
				t.Fatalf("stage %q must prescribe nothing: %+v %q", stage, cmd, followUp)
			}
		}
	})

	t.Run("ambiguous_render_keeps_operation_fallback", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "plan-one")
		writeSavedFeaturePlan(t, repo, "plan-two")
		next, err := NextControl(repo, "")
		if err != nil {
			t.Fatal(err)
		}
		if next.Resolved || next.Prescribed != nil {
			t.Fatalf("AMBIGUOUS must stay unresolved and unprescribed: %+v", next)
		}
		out := FormatFlowNext(next)
		if !strings.Contains(out, "follow the recommended operation above") {
			t.Fatalf("ambiguous rendering must route to the recommended operation: %q", out)
		}
		if strings.Contains(out, "Run: ") {
			t.Fatalf("ambiguous rendering must not fabricate a command: %q", out)
		}
	})
}
