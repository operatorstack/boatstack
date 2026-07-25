package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

func TestRecordCodingEffortRoundTrips(t *testing.T) {
	repo := prTestRepo(t)

	RecordCodingEffort(repo, 2, "implementation_repair")
	RecordCodingEffort(repo, 0, "amend") // bare marker -> one unit

	dir, err := flowLogDirectory(repo)
	if err != nil {
		t.Fatal(err)
	}
	signals, err := deliverycontrol.ReadCodingSignals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := deliverycontrol.TallyCoding(signals); got.JCoding != 3 || got.Signals != 2 {
		t.Errorf("recorded coding effort = %+v, want J_coding 3 over 2 signals", got)
	}
}

func TestRecordCodingEffortHonorsKillSwitch(t *testing.T) {
	t.Setenv(flowTraceKillSwitch, "0")
	repo := prTestRepo(t)

	RecordCodingEffort(repo, 5, "should-not-write")

	dir, err := flowLogDirectory(repo)
	if err != nil {
		t.Fatal(err)
	}
	signals, err := deliverycontrol.ReadCodingSignals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 {
		t.Errorf("kill switch must suppress coding telemetry; got %+v", signals)
	}
}
