package deliverycontrol

import (
	"path/filepath"
	"testing"
)

func TestCommandLogRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flow")
	event := NewCommandEvent(CommandEvent{
		Verb: "publish-pr", Category: "publication", Feature: "demo", Slice: "delivery",
		Transition: "delivery.publish", StartedAt: "2026-08-09T10:00:00Z",
		FinishedAt: "2026-08-09T10:00:01Z", DurationMS: 1000, ExitCode: 0,
		Outcome: "succeeded", AuthorityFingerprint: "authority", OperationFingerprint: "operation",
	})
	if err := AppendCommandEvent(dir, event); err != nil {
		t.Fatal(err)
	}
	events, err := ReadCommandEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0] != event {
		t.Fatalf("events = %+v, want %+v", events, event)
	}
}

func TestCommandLogRejectsMalformedOrSecretBearingShape(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flow")
	bad := CommandEvent{Verb: "x", Category: "test", StartedAt: "bad", FinishedAt: "bad", Outcome: "succeeded"}
	if err := AppendCommandEvent(dir, bad); err == nil {
		t.Fatal("malformed timing was accepted")
	}
	if events, err := ReadCommandEvents(dir); err != nil || len(events) != 0 {
		t.Fatalf("failed append changed log: events=%v err=%v", events, err)
	}
}
