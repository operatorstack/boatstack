package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

func readFlowTrajectory(t *testing.T, repo string) deliverycontrol.Trajectory {
	t.Helper()
	dir, err := flowLogDirectory(repo)
	if err != nil {
		t.Fatalf("flow log directory: %v", err)
	}
	traj, err := deliverycontrol.ReadTrajectory(dir)
	if err != nil {
		t.Fatalf("read trajectory: %v", err)
	}
	return traj
}

// An allowed mutation and a denied one are both recorded, and the denial is
// billed as friction — the recorder is a faithful, cheap witness to real
// command outcomes.
func TestRecordFlowTransitionCapturesOutcomes(t *testing.T) {
	repo := prTestRepo(t)

	RecordFlowTransition(repo, "delivery.record_gate_test", deliverycontrol.StateBuild, true)
	RecordFlowTransition(repo, "delivery.undo", deliverycontrol.StateBuild, false)

	traj := readFlowTrajectory(t, repo)
	if len(traj) != 2 {
		t.Fatalf("recorded %d attempts, want 2", len(traj))
	}
	if traj[0].Outcome != deliverycontrol.OutcomeAllowed || traj[0].CostClass != deliverycontrol.CostMutation {
		t.Errorf("allowed gate: got %+v", traj[0])
	}
	if traj[1].Outcome != deliverycontrol.OutcomeDenied || traj[1].CostClass != deliverycontrol.CostFriction {
		t.Errorf("denied undo should be friction: got %+v", traj[1])
	}
	if cost := traj.WalkCost(deliverycontrol.DefaultFlowCostWeights()); cost != 4 {
		t.Errorf("walk cost = %d, want 4 (1 move + 3 friction)", cost)
	}
}

// The recorder is best-effort: an unresolvable repo, an unknown transition, and
// the kill switch each leave command behavior untouched and write nothing —
// without erroring or panicking.
func TestRecordFlowTransitionIsBestEffort(t *testing.T) {
	// A non-git directory cannot resolve a flow-log location; must be a silent no-op.
	RecordFlowTransition(t.TempDir(), "delivery.record_gate_test", deliverycontrol.StateBuild, true)

	repo := prTestRepo(t)

	// An unknown transition is ignored.
	RecordFlowTransition(repo, "delivery.does_not_exist", deliverycontrol.StateBuild, true)
	if traj := readFlowTrajectory(t, repo); len(traj) != 0 {
		t.Errorf("unknown transition should record nothing; got %d", len(traj))
	}

	// The kill switch disables recording entirely.
	t.Setenv(flowTraceKillSwitch, "0")
	RecordFlowTransition(repo, "delivery.record_gate_test", deliverycontrol.StateBuild, true)
	if traj := readFlowTrajectory(t, repo); len(traj) != 0 {
		t.Errorf("kill switch should suppress recording; got %d", len(traj))
	}
}
