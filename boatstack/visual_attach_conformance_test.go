package boatstack

import (
	"strings"
	"testing"
)

// control-law: prescriptive-closure-every-stage-names-a-runnable-command
// control-law: turn-ends-only-at-the-operator-frontier
//
// A published-open slice with an owed visual publication never resolves to a
// dark prescription: visual_pending prescribes the agent-owned attach-evidence
// retry, manual_required prescribes recording the operator-attached comment,
// and both fire under BOTH terminals because the attachment completes the
// publication itself — it is not merge pursuit.

func publishedOpenStatus(visualPublication string) NextStatus {
	return NextStatus{
		VerificationStatus: "VERIFIED",
		ObservedStage:      "PUBLISHED", Lifecycle: "PUBLISHED_OPEN", Feature: "demo",
		PRURL: "https://github.com/example/repo/pull/7", VisualPublication: visualPublication,
	}
}

func TestOwedVisualAttachmentNeverResolvesDark(t *testing.T) {
	t.Run("visual_pending_prescribes_the_retry", func(t *testing.T) {
		cmd, followUp := prescribeVisualAttach(".", publishedOpenStatus("visual_pending"))
		if cmd == nil || cmd.Verb != "attach-evidence" || !cmd.AutoDerivable {
			t.Fatalf("visual_pending did not prescribe the derivable retry: %+v", cmd)
		}
		if strings.Join(cmd.Args, " ") != "--feature demo" {
			t.Fatalf("retry arguments are not state-derived: %v", cmd.Args)
		}
		if cmd.Transition != MarkerPublishedAttach {
			t.Fatalf("retry must carry the attach marker, got %s", cmd.Transition)
		}
		if followUp == "" {
			t.Fatal("the retry prescription owes its manual-fallback follow-up")
		}
	})

	t.Run("manual_required_prescribes_the_recording", func(t *testing.T) {
		cmd, _ := prescribeVisualAttach(".", publishedOpenStatus("manual_required"))
		if cmd == nil || cmd.Verb != "record-pr-visual-publication" {
			t.Fatalf("manual_required did not prescribe the recording: %+v", cmd)
		}
		if cmd.AutoDerivable || strings.Join(cmd.RequiresHumanInput, " ") != "--comment-url" {
			t.Fatalf("the observed comment URL must be owed to the operator: %+v", cmd)
		}
		if !strings.Contains(strings.Join(cmd.Args, " "), "--pr-url https://github.com/example/repo/pull/7") {
			t.Fatalf("the recorded PR URL is state-derived and must be in Args: %v", cmd.Args)
		}
	})

	t.Run("attach_fires_under_the_published_default_terminal", func(t *testing.T) {
		repo := nextTestRepo(t)
		next, err := nextControlFromStatus(repo, publishedOpenStatus("visual_pending"))
		if err != nil {
			t.Fatal(err)
		}
		if next.Terminal != TerminalPublished {
			t.Fatalf("fixture must exercise the published default, got %s", next.Terminal)
		}
		if next.Prescribed == nil || next.Prescribed.Verb != "attach-evidence" {
			t.Fatalf("owed attachment resolved dark under the published terminal: %+v", next.Prescribed)
		}
		if next.Actor != NextActorAgent {
			t.Fatalf("the derivable retry is the agent's step, got %s", next.Actor)
		}
	})

	t.Run("no_owed_attachment_prescribes_nothing", func(t *testing.T) {
		if cmd, _ := prescribeVisualAttach(".", publishedOpenStatus("")); cmd != nil {
			t.Fatalf("nothing is owed but something was prescribed: %+v", cmd)
		}
	})

	t.Run("goal_escape_still_demotes_and_stops", func(t *testing.T) {
		status := publishedOpenStatus("visual_pending")
		status.GoalEscape = "pr_closed"
		if cmd, _ := prescribeVisualAttach(".", status); cmd != nil {
			t.Fatalf("a fired escape must prescribe nothing: %+v", cmd)
		}
	})

	t.Run("attach_marker_is_never_auto_driven", func(t *testing.T) {
		cmd, _ := prescribeVisualAttach(".", publishedOpenStatus("visual_pending"))
		if canAutoDrive(cmd, autoDrivableTransitions) {
			t.Fatal("the attach retry must be prescribed, never driven")
		}
	})
}
