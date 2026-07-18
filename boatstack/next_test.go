package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), []byte("{}\n"), 0o644); err != nil {
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

func TestResolveNextReportsFeatureCompleteWhenNothingIsActive(t *testing.T) {
	repo := nextTestRepo(t)
	status, err := ResolveNext(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.ObservedStage != "FEATURE_COMPLETE" || status.NextOperation != "none" {
		t.Fatalf("unexpected status: %+v", status)
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
			status, err := ResolveNext(repo)
			if err != nil {
				t.Fatal(err)
			}
			if status.ObservedStage != test.stage || status.NextOperation != test.next || status.Feature != "recovery" {
				t.Fatalf("unexpected status: %+v", status)
			}
		})
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
			status, err := ResolveNext(repo)
			if err != nil {
				t.Fatal(err)
			}
			if status.ObservedStage != test.state || status.NextOperation != test.next || status.ActiveSlice != "delivery" {
				t.Fatalf("unexpected status: %+v", status)
			}
		})
	}
}

func TestResolveNextReportsFeatureCompleteAfterPublication(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "recovery", "PUBLISHED", 1)
	status, err := ResolveNext(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "FEATURE_COMPLETE" || status.NextOperation != "none" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Feature != "recovery" {
		t.Fatalf("expected recovery feature to be marked complete: %+v", status)
	}
	if status.ActiveSlice != "delivery" {
		t.Fatalf("expected final delivery slice to be surfaced: %+v", status)
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
	status, err := ResolveNext(repo)
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

func TestResolveNextRejectsStaleManagedState(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "recovery", "BUILD", 0)
	lockPath := filepath.Join(repo, ".product-loop", "features", "recovery", "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveNext(repo); err == nil {
		t.Fatal("stale managed state was accepted")
	}
}
