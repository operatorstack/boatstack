package deliverycontrol

import (
	"path/filepath"
	"testing"
)

func TestTrajectoryLogRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flow")

	// A first read before any write is an empty trajectory, not an error.
	if traj, err := ReadTrajectory(dir); err != nil || len(traj) != 0 {
		t.Fatalf("empty read: got %v (%d), err %v", traj, len(traj), err)
	}

	want := Trajectory{
		{Sequence: 0, From: StateBuild, Transition: "delivery.record_gate_test", Outcome: OutcomeAllowed, CostClass: CostMutation},
		{Sequence: 1, From: StateBuild, Transition: "delivery.undo", Outcome: OutcomeDenied, CostClass: CostFriction, Note: "gate receipt exists"},
	}
	for _, attempt := range want {
		if err := AppendAttempt(dir, attempt); err != nil {
			t.Fatalf("append %s: %v", attempt.Transition, err)
		}
	}

	got, err := ReadTrajectory(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d attempts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("attempt %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// Order is preserved and the walk cost reflects the friction on the denial.
	if cost := got.WalkCost(DefaultFlowCostWeights()); cost != 4 {
		t.Errorf("walk cost = %d, want 4 (1 move + 3 friction)", cost)
	}
}

func TestAppendAttemptRejectsEmptyDir(t *testing.T) {
	if err := AppendAttempt("", TransitionAttempt{Transition: "x"}); err == nil {
		t.Error("expected an error for an empty directory")
	}
}
