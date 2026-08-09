package boatstack

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: helper-dispatch-produces-secret-free-shadow-event
func TestRecordCommandEventIsSecretFreeAndBestEffort(t *testing.T) {
	repo := prTestRepo(t)
	started := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	RecordCommandEvent(CommandTraceInput{
		Repo: repo, Verb: "run-preflight", Category: "readiness", Feature: "demo",
		StartedAt: started, FinishedAt: started.Add(1500 * time.Millisecond), ExitCode: 1,
	})
	dir, err := flowLogDirectory(repo)
	if err != nil {
		t.Fatal(err)
	}
	events, err := deliverycontrol.ReadCommandEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Outcome != "failed" || events[0].DurationMS != 1500 {
		t.Fatalf("events = %+v", events)
	}
	raw, _ := json.Marshal(events[0])
	for _, forbidden := range []string{"arguments", "stdin", "stdout", "stderr", "environment"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("secret-bearing field %q entered event: %s", forbidden, raw)
		}
	}

	t.Setenv(flowTraceKillSwitch, "0")
	RecordCommandEvent(CommandTraceInput{
		Repo: repo, Verb: "doctor", Category: "readiness",
		StartedAt: started, FinishedAt: started, ExitCode: 0,
	})
	events, err = deliverycontrol.ReadCommandEvents(dir)
	if err != nil || len(events) != 1 {
		t.Fatalf("kill switch changed log: events=%v err=%v", events, err)
	}

	// Invalid repositories and malformed timestamps are silent no-ops.
	RecordCommandEvent(CommandTraceInput{Repo: t.TempDir(), Verb: "doctor", Category: "readiness"})
}

func TestFlowReportScopesCommandEvidenceByFeature(t *testing.T) {
	repo := prTestRepo(t)
	dir, _ := flowLogDirectory(repo)
	for _, event := range []deliverycontrol.CommandEvent{
		{Verb: "check-plan", Category: "planning", Feature: "one", StartedAt: "2026-08-09T10:00:00Z", FinishedAt: "2026-08-09T10:00:01Z", DurationMS: 1000, Outcome: "succeeded"},
		{Verb: "publish-pr", Category: "publication", Feature: "two", StartedAt: "2026-08-09T10:00:02Z", FinishedAt: "2026-08-09T10:00:04Z", DurationMS: 2000, ExitCode: 1, Outcome: "failed"},
	} {
		if err := deliverycontrol.AppendCommandEvent(dir, event); err != nil {
			t.Fatal(err)
		}
	}
	report, err := FlowReportFor(repo, "two")
	if err != nil {
		t.Fatal(err)
	}
	if report.CommandCoverageStatus != "SCOPED_COMPLETE" || report.CommandEvents != 1 || report.CommandFailures != 1 || report.ObservedCommandMS != 2000 {
		t.Fatalf("report = %+v", report)
	}
	if report.CommandFailureByCategory["publication"] != 1 {
		t.Fatalf("failure categories = %+v", report.CommandFailureByCategory)
	}
}
