package deliverycontrol

import "testing"

func TestTallyCodingSumsUnits(t *testing.T) {
	effort := TallyCoding([]CodingSignal{{Units: 3}, {Units: 2}})
	if effort.JCoding != 5 || effort.Signals != 2 {
		t.Errorf("tally = %+v, want J_coding 5 over 2 signals", effort)
	}
}

func TestTallyCodingBareMarkerCountsOne(t *testing.T) {
	effort := TallyCoding([]CodingSignal{{Units: 0}, {Units: -4}})
	if effort.JCoding != 2 {
		t.Errorf("bare/negative markers should each count one unit; J_coding = %d, want 2", effort.JCoding)
	}
}

func TestCodingLogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if signals, err := ReadCodingSignals(dir); err != nil || len(signals) != 0 {
		t.Fatalf("empty coding log must read clean: %v %v", signals, err)
	}
	if err := AppendCodingSignal(dir, CodingSignal{Units: 2, Note: "repair"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendCodingSignal(dir, CodingSignal{Units: 1, Note: "amend"}); err != nil {
		t.Fatal(err)
	}
	signals, err := ReadCodingSignals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 || signals[0].Note != "repair" || signals[1].Units != 1 {
		t.Errorf("round-trip mismatch: %+v", signals)
	}
}

// Coding effort must be reported ALONGSIDE flow regret, never folded into it.
func TestComputeReportWithCodingKeepsRegretFlowOnly(t *testing.T) {
	g := RegistryGraph(DefaultFlowCostWeights())
	// A single friction attempt: J_flow = 3, oracle from BUILD = 3, regret = 0.
	walk := Trajectory{{From: StateBuild, Transition: "delivery.publish", Outcome: OutcomeDenied, CostClass: CostFriction}}
	signals := []CodingSignal{{Units: 7}}

	report := ComputeReportWithCoding(walk, g, DefaultFlowCostWeights(), StatePublished, signals)
	if report.JCoding != 7 {
		t.Errorf("J_coding = %d, want 7", report.JCoding)
	}
	// Regret is derived purely from flow; coding effort must not perturb it.
	bare := ComputeReport(walk, g, DefaultFlowCostWeights(), StatePublished)
	if report.Regret != bare.Regret || report.JFlow != bare.JFlow {
		t.Errorf("coding telemetry leaked into flow: with-coding=%+v flow-only=%+v", report, bare)
	}
}
