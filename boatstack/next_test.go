package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nextTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func writeNextDelivery(t *testing.T, repo, feature, status string, activeIndex int) {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
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
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: hash,
		ActiveIndex: activeIndex, Slices: []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: status}},
	}); err != nil {
		t.Fatal(err)
	}
}

func writeSavedFeaturePlan(t *testing.T, repo, feature string) {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeIntakePlan(t *testing.T, repo, name string) {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "intake")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNextReportsNotStartedWhenNoFeatureExists(t *testing.T) {
	repo := nextTestRepo(t)
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.ObservedStage != "NOT_STARTED" || status.NextOperation != "auto-plan" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestResolveNextReportsSavedSourcePlan(t *testing.T) {
	repo := nextTestRepo(t)
	writeIntakePlan(t, repo, "feature.md")
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "SOURCE_PLAN_READY" || status.NextOperation != "auto-plan" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestResolveNextPrefersUniqueSourcePlanOverHistoricalPlans(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "historical-one")
	writeSavedFeaturePlan(t, repo, "historical-two")
	writeIntakePlan(t, repo, "current.md")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.ObservedStage != "SOURCE_PLAN_READY" || status.NextOperation != "auto-plan" {
		t.Fatalf("new source plan did not outrank historical plans: %+v", status)
	}
}

func TestResolveNextBlocksMultipleSourcePlansBeforeHistoricalPlans(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "historical-one")
	writeSavedFeaturePlan(t, repo, "historical-two")
	writeIntakePlan(t, repo, "first.md")
	writeIntakePlan(t, repo, "second.md")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".product-loop/intake/first.md", ".product-loop/intake/second.md"}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "AMBIGUOUS" || !reflect.DeepEqual(status.BlockingAmbiguity, want) {
		t.Fatalf("unexpected source-plan ambiguity: %+v", status)
	}
}

func TestResolveNextActiveDeliveryOutranksSourcePlan(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "active-feature", "BUILD", 0)
	writeIntakePlan(t, repo, "new-feature.md")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Feature != "active-feature" || status.ObservedStage != "BUILD" || status.NextOperation != "build" {
		t.Fatalf("source plan displaced active delivery: %+v", status)
	}
}

func TestResolveNextOrphanedEvidenceOutranksSourcePlan(t *testing.T) {
	repo := nextTestRepo(t)
	directory := filepath.Join(repo, ".product-loop", "features", "orphan")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "pr.md"), []byte("# Preview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeIntakePlan(t, repo, "new-feature.md")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "INVALID_STATE" || status.NextOperation != "repair-state" {
		t.Fatalf("source plan bypassed orphaned evidence: %+v", status)
	}
}

func TestResolveNextBlocksHistoricalPlansWithoutSourceIntent(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "historical-one")
	writeSavedFeaturePlan(t, repo, "historical-two")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"historical-one", "historical-two"}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "AMBIGUOUS" || !reflect.DeepEqual(status.BlockingAmbiguity, want) {
		t.Fatalf("unexpected historical-plan ambiguity: %+v", status)
	}
}

func TestResolveNextPlanningStates(t *testing.T) {
	for _, test := range []struct {
		name, approval, stage, next string
	}{
		{name: "draft", stage: "DRAFT_PLAN", next: "plan-gate"},
		{name: "approved", approval: "approved", stage: "APPROVED", next: "build"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := nextTestRepo(t)
			directory := filepath.Join(repo, ".product-loop", "features", "recovery")
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if test.approval != "" {
				if err := os.WriteFile(filepath.Join(directory, "approval.md"), []byte("# Approval\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			status, err := ResolveNext(repo, "")
			if err != nil {
				t.Fatal(err)
			}
			if status.ObservedStage != test.stage || status.NextOperation != test.next || status.Feature != "recovery" {
				t.Fatalf("unexpected status: %+v", status)
			}
		})
	}
}

func TestResolveNextRoutesPolicyAuthorizedPlanToBuild(t *testing.T) {
	repo := nextTestRepo(t)
	configPath := filepath.Join(repo, ".product-loop", "project.json")
	config, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config.Workflow.HumanPlanApproval = false
	value, _ := MarshalJSON(config)
	if err := os.WriteFile(configPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	writeSavedFeaturePlan(t, repo, "policy-ready")
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "POLICY_READY" || status.NextOperation != "build" || status.Feature != "policy-ready" {
		t.Fatalf("policy-authorized plan did not route to build: %+v", status)
	}
}

func TestResolveNextDeliveryTransitions(t *testing.T) {
	for _, test := range []struct{ state, next string }{
		{state: "BUILD", next: "build"},
		{state: "TEST_PASSED", next: "review-gate"},
		{state: "REVIEW_PASSED", next: "ship-gate"},
	} {
		t.Run(test.state, func(t *testing.T) {
			repo := nextTestRepo(t)
			writeNextDelivery(t, repo, "recovery", test.state, 0)
			status, err := ResolveNext(repo, "")
			if err != nil {
				t.Fatal(err)
			}
			if status.ObservedStage != test.state || status.NextOperation != test.next || status.ActiveSlice != "delivery" {
				t.Fatalf("unexpected status: %+v", status)
			}
		})
	}
}

func TestResolveNextReportsPublishedUnknownWithoutPRVerification(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "recovery", "PUBLISHED", 1)
	intake := filepath.Join(repo, ".product-loop", "intake")
	if err := os.MkdirAll(intake, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intake, "source-plan.md"), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = "recovery"
	plan["source_plan_path"] = "../../intake/source-plan.md"
	writeMarkdownPlan(t, filepath.Join(repo, ".product-loop", "features", "recovery", "plan.md"), plan, true)
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.SchemaVersion != 2 || status.ObservedStage != "PUBLISHED" || status.Lifecycle != "PUBLISHED_UNKNOWN" || status.NextOperation != "none" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Feature != "recovery" {
		t.Fatalf("expected recovery feature to be marked complete: %+v", status)
	}
	if status.ActiveSlice != "delivery" {
		t.Fatalf("expected final delivery slice to be surfaced: %+v", status)
	}
}

func TestResolveNextReportsFeatureCompleteOnlyAfterVerifiedMerge(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "recovery", "PUBLISHED", 1)
	state, err := LoadDeliveryState(repo, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	state.Slices[0].HeadBranch = "feat/recovery"
	state.Slices[0].PRURL = "https://example.invalid/pr/1"
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	withRecoveryGh(t, recoveryPR("MERGED", "feat/recovery", "abc123"))
	status, err := ResolveNext(repo, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "FEATURE_COMPLETE" || status.Lifecycle != "PUBLISHED_MERGED" || status.NextOperation != "none" {
		t.Fatalf("unexpected merged status: %+v", status)
	}
}

func TestFormatNextStatusStillRendersSchemaV1Values(t *testing.T) {
	value := FormatNextStatus(NextStatus{SchemaVersion: 1, VerificationStatus: "VERIFIED", Feature: "legacy", ObservedStage: "FEATURE_COMPLETE", NextOperation: "none", Reason: "Legacy published state."})
	for _, expected := range []string{"Feature: legacy", "Boatstack stage: FEATURE_COMPLETE", "Next: none"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("legacy status rendering omitted %q: %s", expected, value)
		}
	}
}

func TestResolveNextPrefersNewDraftOverCompletedHistory(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
	directory := filepath.Join(repo, ".product-loop", "features", "new-feature")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "DRAFT_PLAN" || status.Feature != "new-feature" || status.NextOperation != "plan-gate" {
		t.Fatalf("completed history masked newer work: %+v", status)
	}
}

func TestResolveNextBlocksMultipleActiveFeaturesWithoutMutation(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "first", "BUILD", 0)
	writeNextDelivery(t, repo, "second", "BUILD", 0)
	before, err := os.ReadFile(filepath.Join(repo, ".git", "boatstack", "deliveries", "first", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(repo, ".git", "boatstack", "deliveries", "first", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || !reflect.DeepEqual(status.BlockingAmbiguity, []string{"first", "second"}) {
		t.Fatalf("unexpected ambiguity: %+v", status)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only next inspection changed delivery state")
	}
}

func TestResolveNextBlocksStaleManagedState(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "recovery", "BUILD", 0)
	lockPath := filepath.Join(repo, ".product-loop", "features", "recovery", "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "INVALID_STATE" || status.NextOperation != "repair-state" {
		t.Fatalf("stale managed state was accepted: %+v", status)
	}
}

func TestResolveNextBlocksMissingLockAndOrphanPreview(t *testing.T) {
	for _, test := range []struct {
		name      string
		withState bool
	}{
		{name: "managed state missing lock", withState: true},
		{name: "orphan preview", withState: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := nextTestRepo(t)
			directory := filepath.Join(repo, ".product-loop", "features", "orphan")
			if test.withState {
				writeNextDelivery(t, repo, "orphan", "BUILD", 0)
				if err := os.Remove(filepath.Join(directory, "plan.lock.json")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "pr.md"), []byte("# Preview\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(directory, "pr.md"))
			if err != nil {
				t.Fatal(err)
			}
			status, err := ResolveNext(repo, "")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(directory, "pr.md"))
			if err != nil {
				t.Fatal(err)
			}
			if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "INVALID_STATE" || status.NextOperation != "repair-state" {
				t.Fatalf("unexpected invalid state: %+v", status)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("invalid-state inspection modified the orphan preview")
			}
		})
	}
}
