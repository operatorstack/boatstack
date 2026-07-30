package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: turn-ends-only-at-the-operator-frontier
// (cross-reference: response-contract-is-helper-rendered)
//
// Every prescribed next step is typed by who performs it. A step belongs to
// the operator only when it owes operator knowledge or authority; every other
// step is the agent's, and a working response must not end on it. These tests
// hold the classifier to that boundary (fail-closed to operator), hold the
// renderer to marking agent-owned steps with the delegation line, and pin the
// exported instruction to the delegation reply so a status query stays
// read-only while the operator's next action is always a single key.

const agentStepMarker = "This step is mine to do."

// Per-stage classification through the real resolution path: pre-activation
// stages the agent can drive are agent-owned; stages owing operator knowledge
// (the source-plan path, a feature choice) are operator-owned.
func TestNextActorPerStage(t *testing.T) {
	expectActor := func(t *testing.T, repo, feature string, want NextActor) FlowNext {
		t.Helper()
		status, err := ResolveNext(repo, feature)
		if err != nil {
			t.Fatal(err)
		}
		next, err := nextControlFromStatus(repo, status)
		if err != nil {
			t.Fatal(err)
		}
		if next.Actor != want {
			t.Fatalf("actor = %q, want %q (stage %s)", next.Actor, want, status.ObservedStage)
		}
		return next
	}

	t.Run("not_started_owes_plan_path", func(t *testing.T) {
		expectActor(t, nextTestRepo(t), "", NextActorOperator)
	})

	t.Run("draft_plan_check_is_agents", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		expectActor(t, repo, "", NextActorAgent)
	})

	t.Run("approved_activation_is_agents", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		if err := os.WriteFile(filepath.Join(repo, ".product-loop", "features", "demo", "approval.md"), []byte("approved\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		expectActor(t, repo, "", NextActorAgent)
	})

	t.Run("build_evidence_is_agents", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		next := expectActor(t, repo, feature, NextActorAgent)
		if next.Prescribed == nil || len(next.Prescribed.RequiresHumanInput) == 0 {
			t.Fatal("fixture must owe evidence flags — the point is they do not cross the frontier")
		}
	})

	t.Run("ambiguous_choice_is_operators", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "plan-one")
		writeSavedFeaturePlan(t, repo, "plan-two")
		expectActor(t, repo, "", NextActorOperator)
	})
}

// Boundary: the classifier is fail-closed. Terminal states owe nobody an
// action; publish authority, operator-owed knowledge flags, and unprescribed
// blocks all resolve to the operator — the worst misclassification is
// prescribe-and-stop, never a runaway agent.
func TestNextActorFrontierBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		status NextStatus
		next   FlowNext
		want   NextActor
	}{
		{"feature_complete_is_terminal", NextStatus{ObservedStage: "FEATURE_COMPLETE"}, FlowNext{}, NextActorNone},
		{"merged_publication_is_terminal", NextStatus{ObservedStage: "PUBLISHED", Lifecycle: "PUBLISHED_MERGED"}, FlowNext{}, NextActorNone},
		{"open_pr_review_is_operators", NextStatus{ObservedStage: "PUBLISHED"}, FlowNext{}, NextActorOperator},
		{"unprescribed_block_is_operators", NextStatus{ObservedStage: "INVALID_STATE"}, FlowNext{}, NextActorOperator},
		{"publish_authority_is_operators", NextStatus{ObservedStage: "REVIEW_PASSED"}, FlowNext{
			Prescribed: &PrescribedCommand{Verb: "publish", Transition: PublishTransition},
		}, NextActorOperator},
		{"owed_knowledge_is_operators", NextStatus{ObservedStage: "BUILD"}, FlowNext{
			Prescribed: &PrescribedCommand{Verb: "record-change", RequiresHumanInput: []string{"--message", "--source-stage", "--classification", "--mechanism"}},
		}, NextActorOperator},
		{"owed_evidence_stays_agents", NextStatus{ObservedStage: "BUILD"}, FlowNext{
			Prescribed: &PrescribedCommand{Verb: "record-delivery-gate", RequiresHumanInput: []string{"--status", "--evidence"}, Transition: deliverycontrol.TransitionID("delivery.record_gate_test")},
		}, NextActorAgent},
		{"owed_visual_attach_retry_is_agents", NextStatus{ObservedStage: "PUBLISHED", Lifecycle: "PUBLISHED_OPEN", VisualPublication: "visual_pending"}, FlowNext{}, NextActorAgent},
		{"manual_visual_attachment_is_operators", NextStatus{ObservedStage: "PUBLISHED", Lifecycle: "PUBLISHED_OPEN", VisualPublication: "manual_required"}, FlowNext{}, NextActorOperator},
		{"escaped_pursuit_demotes_despite_owed_attachment", NextStatus{ObservedStage: "PUBLISHED", Lifecycle: "PUBLISHED_OPEN", VisualPublication: "visual_pending", GoalEscape: "pr_closed"}, FlowNext{Terminal: TerminalMerged}, NextActorOperator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNextActor(tc.status, tc.next); got != tc.want {
				t.Fatalf("classifyNextActor = %q, want %q", got, tc.want)
			}
		})
	}
}

// Rendering: the delegation line appears exactly when the step is the
// agent's, and it always names the single-key reply — the operator's next
// action is known even when the work is the agent's.
func TestResponseMarksAgentOwnedSteps(t *testing.T) {
	t.Run("agent_owned_step_carries_delegation", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		_, output := renderedResponse(t, repo)
		if !strings.Contains(output, agentStepMarker) {
			t.Fatalf("agent-owned step must carry the marker: %q", output)
		}
		if !strings.Contains(output, "Reply `g`") {
			t.Fatalf("agent-owned step must name the delegation reply: %q", output)
		}
	})

	t.Run("operator_owned_step_carries_no_delegation", func(t *testing.T) {
		_, output := renderedResponse(t, nextTestRepo(t))
		if strings.Contains(output, agentStepMarker) {
			t.Fatalf("operator-owned step must not claim to be the agent's: %q", output)
		}
	})

	t.Run("ambiguous_block_carries_no_delegation", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "plan-one")
		writeSavedFeaturePlan(t, repo, "plan-two")
		_, output := renderedResponse(t, repo)
		if strings.Contains(output, agentStepMarker) {
			t.Fatalf("a feature choice is a human act, never the agent's: %q", output)
		}
	})
}

// Bypass: the exported boatstack-next instruction carries the frontier rule —
// the delegation reply, the continue-until-operator loop, and the
// no-progress stall guard — so no host prose can quietly turn an agent-owned
// step back into an operator to-do item.
func TestExportedNextInstructionCarriesFrontierRule(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for path, content := range bundle.Files {
		if !strings.Contains(path, "boatstack-next") {
			continue
		}
		inspected++
		text := string(content)
		for _, rule := range []string{
			agentStepMarker[:len(agentStepMarker)-1], // marker phrase, unpunctuated
			"delegation reply g",
			"repetition without progress is a stall",
			"Never end a response by describing work the agent still has to do",
		} {
			if !strings.Contains(text, rule) {
				t.Fatalf("%s must carry the frontier rule %q", path, rule)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("no exported boatstack-next instruction found — the frontier rule would be vacuous")
	}
	workflow := string(bundle.Files[".product-loop/workflow.md"])
	if !strings.Contains(workflow, "### The operator frontier") {
		t.Fatal("workflow.md must document the operator frontier")
	}
}
