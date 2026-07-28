package boatstack

// control-law: watch-observes-and-exits-never-acts
//
// `flow watch` is a bounded observe-compare loop over the read-only frontier:
// each tick re-observes, and the loop exits on the first signature change, on
// an all-terminal frontier, or at the deadline. It never executes a
// transition and never writes — across any number of ticks the delivery
// ledger stays byte-identical. Time is injected through seams so the law is
// provable without real waiting.
//
// Test classes: positive (an external phase change ends the wait and names
// the changed row), negative (no change → timeout outcome, frontier intact),
// bypass (zero writes across many ticks), failure-state (gh failing every
// tick degrades to Unknown rows and the loop still terminates at the
// deadline; an all-terminal frontier refuses to wait at all).

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWatchClock replaces the time seams: sleeping advances a virtual clock,
// so deadlines fire deterministically and instantly.
func fakeWatchClock(t *testing.T) *atomic.Int64 {
	t.Helper()
	var virtual atomic.Int64
	previousNow, previousSleep := flowWatchNow, flowWatchSleep
	flowWatchNow = func() time.Time { return time.Unix(0, virtual.Load()) }
	flowWatchSleep = func(d time.Duration) { virtual.Add(int64(d)) }
	t.Cleanup(func() { flowWatchNow, flowWatchSleep = previousNow, previousSleep })
	return &virtual
}

func watchRepoWithOpenPR(t *testing.T) string {
	t.Helper()
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	return repo
}

// Positive: the PR's checks finish between ticks; the watch exits with
// outcome "changed" and names the row that moved.
func TestWatchExitsWhenTheFrontierChanges(t *testing.T) {
	fakeWatchClock(t)
	repo := watchRepoWithOpenPR(t)
	var calls atomic.Int64
	withRecoveryGh(t, func(_ string, args ...string) (string, error) {
		if calls.Add(1) <= 1 {
			return phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunPending)(repo, args...)
		}
		return phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunFail)(repo, args...)
	})
	result, err := WatchFrontier(FlowWatchOptions{Repo: repo, Interval: time.Minute, Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != WatchOutcomeChanged || result.Ticks != 1 {
		t.Fatalf("unexpected watch result: %#v", result)
	}
	if len(result.ChangedRows) != 1 || result.ChangedRows[0] != "shipped/delivery" {
		t.Fatalf("changed row not named: %#v", result.ChangedRows)
	}
	if result.Final.Rows[0].PRPhase != string(PRPhaseChecksFailing) {
		t.Fatalf("final frontier does not carry the new observation: %#v", result.Final.Rows)
	}
}

// Negative: nothing changes; the watch times out with the frontier intact and
// the CLI-visible timeout outcome.
func TestWatchTimesOutWhenNothingChanges(t *testing.T) {
	fakeWatchClock(t)
	repo := watchRepoWithOpenPR(t)
	withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunPending))
	result, err := WatchFrontier(FlowWatchOptions{Repo: repo, Interval: 10 * time.Minute, Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != WatchOutcomeTimeout {
		t.Fatalf("unexpected outcome: %#v", result)
	}
	if result.Ticks < 5 || result.Ticks > 7 {
		t.Fatalf("unexpected tick count for 1h/10m: %d", result.Ticks)
	}
	if result.Final.Rows[0].PRPhase != string(PRPhaseChecksPending) {
		t.Fatalf("frontier drifted without a change: %#v", result.Final.Rows)
	}
}

// Bypass: across many ticks — including a tick that observes a terminal
// MERGED lifecycle — the watch writes nothing. The change is reported, never
// recorded.
func TestWatchWritesNothingAcrossTicks(t *testing.T) {
	fakeWatchClock(t)
	repo := watchRepoWithOpenPR(t)
	statePath, err := deliveryStatePath(repo, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	withRecoveryGh(t, func(_ string, args ...string) (string, error) {
		if calls.Add(1) <= 3 {
			return phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunPending)(repo, args...)
		}
		return phaseObservationPayload("MERGED", "", "", "")(repo, args...)
	})
	result, err := WatchFrontier(FlowWatchOptions{Repo: repo, Interval: time.Minute, Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != WatchOutcomeChanged {
		t.Fatalf("unexpected outcome: %#v", result)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("watch mutated the delivery ledger")
	}
}

// Failure-state 1: gh fails on every tick — rows degrade to Unknown, no
// crash, and the loop still ends at the deadline.
func TestWatchStaysBoundedWhenObservationFails(t *testing.T) {
	fakeWatchClock(t)
	repo := watchRepoWithOpenPR(t)
	withRecoveryGh(t, func(string, ...string) (string, error) { return "", errors.New("gh unavailable") })
	result, err := WatchFrontier(FlowWatchOptions{Repo: repo, Interval: 15 * time.Minute, Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != WatchOutcomeTimeout {
		t.Fatalf("unexpected outcome: %#v", result)
	}
	if result.Final.Rows[0].PRPhase != string(PRPhaseUnknown) {
		t.Fatalf("degraded observation not fail-closed: %#v", result.Final.Rows)
	}
}

// Failure-state 2: when nothing on the frontier can move, the watch refuses
// to wait at all.
func TestWatchRefusesToWaitOnTerminalFrontier(t *testing.T) {
	fakeWatchClock(t)
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "done", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "done", "feat/phase", "https://example.invalid/pr/9", "")
	withRecoveryGh(t, phaseObservationPayload("MERGED", "", "", ""))
	result, err := WatchFrontier(FlowWatchOptions{Repo: repo, Interval: time.Minute, Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != WatchOutcomeTerminal || result.Ticks != 0 {
		t.Fatalf("unexpected result on a terminal frontier: %#v", result)
	}
}
