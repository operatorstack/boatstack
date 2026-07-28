package boatstack

// control-law: frontier-reports-never-mutates
//
// The frontier dashboard is a pure projection: it reports every managed
// delivery's position and owing actor while performing ZERO writes — not even
// the best-effort terminal PR-state cache the next/recovery resolvers
// maintain. A report that mutates is a report that can lie about what it
// found. Companion laws exercised here:
// stale-delivery-cannot-block-unrelated-feature (one corrupt delivery is one
// blocked row, never a poisoned view) and
// turn-ends-only-at-the-operator-frontier (a frontier row's actor equals the
// flow advisor's actor for the same delivery — one classification path).
//
// Test classes: positive (a multi-delivery store renders every slice with a
// typed actor and live PR position), negative (a corrupt delivery yields one
// blocked row while healthy rows survive), bypass (state files are
// byte-identical after a frontier run, even under a terminal MERGED
// observation), relation (frontier actor == flow next actor per delivery).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func frontierStateBytes(t *testing.T, repo string, features ...string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	for _, feature := range features {
		path, err := deliveryStatePath(repo, feature)
		if err != nil {
			t.Fatal(err)
		}
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[feature] = string(value)
	}
	return snapshot
}

// Positive + relation: every delivery renders with a typed actor, the
// published delivery carries its live PR phase, and each row's actor matches
// the flow advisor's actor for the same feature.
func TestFrontierRendersEveryDeliveryWithTypedActor(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "building", "BUILD", 0)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunFail))

	frontier, err := ResolveFrontier(repo)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]FrontierRow{}
	for _, row := range frontier.Rows {
		rows[row.Feature] = row
	}
	building, ok := rows["building"]
	if !ok || building.Stage != "BUILD" || building.Actor != string(NextActorAgent) {
		t.Fatalf("unexpected building row: %#v", building)
	}
	if building.Prescribed == "" {
		t.Fatal("an agent-owned row must carry its prescribed command")
	}
	shipped, ok := rows["shipped"]
	if !ok || shipped.Stage != "PUBLISHED" || shipped.PRPhase != string(PRPhaseChecksFailing) {
		t.Fatalf("unexpected shipped row: %#v", shipped)
	}
	if shipped.Actor != string(NextActorOperator) {
		t.Fatalf("published-open step belongs to the operator today: %#v", shipped)
	}
	if frontier.AgentSteps != 1 || frontier.OperatorSteps != 1 || frontier.BlockedRows != 0 {
		t.Fatalf("unexpected summary: %#v", frontier)
	}

	// Relation: the frontier's actor for each feature equals the advisor's.
	for _, feature := range []string{"building", "shipped"} {
		next, nextErr := NextControl(repo, feature)
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if string(next.Actor) != rows[feature].Actor {
			t.Fatalf("frontier actor %q disagrees with flow next actor %q for %s", rows[feature].Actor, next.Actor, feature)
		}
	}

	rendered := FormatFlowFrontier(frontier)
	if !strings.Contains(rendered, "PR_CHECKS_FAILING") || !strings.Contains(rendered, "failing: unit") {
		t.Fatalf("rendered frontier hides the live PR position:\n%s", rendered)
	}
}

// Positive: an active delivery with an earlier published-but-open slice shows
// both balls — the building active slice and the open PR of the earlier slice.
func TestFrontierShowsEarlierPublishedOpenSlices(t *testing.T) {
	repo := nextTestRepo(t)
	directory := filepath.Join(repo, ".product-loop", "features", "layered")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := SHA256File(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "layered", PlanLockHash: hash,
		ActiveIndex: 1, Slices: []DeliverySlice{
			{ID: "first", Title: "First", Status: "PUBLISHED", PRURL: "https://example.invalid/pr/9", HeadBranch: "feat/phase", PRState: "OPEN"},
			{ID: "second", Title: "Second", Status: "BUILD"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunFail))

	frontier, frontierErr := ResolveFrontier(repo)
	if frontierErr != nil {
		t.Fatal(frontierErr)
	}
	if len(frontier.Rows) != 2 {
		t.Fatalf("want active + earlier published rows, got %#v", frontier.Rows)
	}
	var earlier *FrontierRow
	for i := range frontier.Rows {
		if frontier.Rows[i].Slice == "first" {
			earlier = &frontier.Rows[i]
		}
	}
	if earlier == nil || earlier.PRPhase != string(PRPhaseChecksFailing) || earlier.Actor != string(NextActorOperator) {
		t.Fatalf("earlier published-open slice not surfaced: %#v", frontier.Rows)
	}
}

// Negative: one corrupt delivery becomes one blocked row; the healthy
// delivery's row survives untouched.
func TestFrontierPartitionsCorruptDeliveries(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "healthy", "BUILD", 0)
	writeNextDelivery(t, repo, "corrupt", "BUILD", 0)
	statePath, err := deliveryStatePath(repo, "corrupt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	frontier, err := ResolveFrontier(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Rows) != 2 || frontier.BlockedRows != 1 || frontier.AgentSteps != 1 {
		t.Fatalf("unexpected partition: %#v", frontier)
	}
	for _, row := range frontier.Rows {
		if row.Feature == "corrupt" && (!row.Blocked || row.NextOperation != "discard-delivery") {
			t.Fatalf("corrupt delivery not routed to its remedy: %#v", row)
		}
		if row.Feature == "healthy" && (row.Blocked || row.Actor != string(NextActorAgent)) {
			t.Fatalf("healthy delivery poisoned by corrupt neighbor: %#v", row)
		}
	}
}

// Bypass: the frontier performs zero writes — the delivery ledger is
// byte-identical after a run, even when the live observation is terminal
// (MERGED), which next/recovery WOULD cache. The report never mutates.
func TestFrontierWritesNothingEvenOnTerminalObservation(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "building", "BUILD", 0)
	writeNextDelivery(t, repo, "shipped", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "shipped", "feat/phase", "https://example.invalid/pr/9", "")
	withRecoveryGh(t, phaseObservationPayload("MERGED", "", "", ""))

	before := frontierStateBytes(t, repo, "building", "shipped")
	frontier, err := ResolveFrontier(repo)
	if err != nil {
		t.Fatal(err)
	}
	after := frontierStateBytes(t, repo, "building", "shipped")
	for feature, value := range before {
		if after[feature] != value {
			t.Fatalf("frontier mutated delivery state for %q", feature)
		}
	}
	var shipped FrontierRow
	for _, row := range frontier.Rows {
		if row.Feature == "shipped" {
			shipped = row
		}
	}
	if shipped.Actor != string(NextActorNone) || shipped.Stage != "FEATURE_COMPLETE" {
		t.Fatalf("terminal observation misclassified: %#v", shipped)
	}
}

// Failure-state: an uninitialized repository reports an empty, unblocked
// frontier rather than an error.
func TestFrontierOnUninitializedRepository(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	frontier, err := ResolveFrontier(repo)
	if err != nil {
		t.Fatal(err)
	}
	if frontier.Initialized || len(frontier.Rows) != 0 {
		t.Fatalf("unexpected frontier: %#v", frontier)
	}
	if rendered := FormatFlowFrontier(frontier); !strings.Contains(rendered, "not tracking") {
		t.Fatalf("unexpected rendering: %s", rendered)
	}
}
