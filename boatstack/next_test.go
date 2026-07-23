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

// writeShippedFeatureArtifacts models a feature that was built and shipped, then
// had its worktree (and the per-worktree delivery state.json) removed by cleanup:
// only the committed plan.md, plan.lock.json, and pr.md survive.
func writeShippedFeatureArtifacts(t *testing.T, repo, feature string) {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"plan.md":        "# Plan\n",
		"plan.lock.json": "lock\n",
		"pr.md":          "# PR\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
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

// TestResolveNextIgnoresAmbientPlanFiles is the conformance guard for the
// "no Boatstack context for things we did not ship" contract at the state
// machine: saved, never-shipped plan files sitting in the historically scanned
// directories must never surface as SOURCE_PLAN_READY or AMBIGUOUS. next-status
// reports NOT_STARTED regardless.
func TestResolveNextIgnoresAmbientPlanFiles(t *testing.T) {
	repo := nextTestRepo(t)
	for _, dir := range []string{
		".product-loop/intake",
		".cursor/plans",
		".claude/plans",
		".codex/plans",
	} {
		absolute := filepath.Join(repo, filepath.FromSlash(dir))
		if err := os.MkdirAll(absolute, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(absolute, "unshipped.md"), []byte("# Unshipped plan\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "NOT_STARTED" || status.NextOperation != "auto-plan" {
		t.Fatalf("ambient plan files leaked into next-status: %+v", status)
	}
	if status.ObservedStage == "SOURCE_PLAN_READY" {
		t.Fatal("SOURCE_PLAN_READY must no longer be produced")
	}
}

func TestResolveNextActiveDeliveryIsReported(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "active-feature", "BUILD", 0)

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Feature != "active-feature" || status.ObservedStage != "BUILD" || status.NextOperation != "build" {
		t.Fatalf("active delivery not reported: %+v", status)
	}
}

func TestResolveNextOrphanedEvidenceBlocks(t *testing.T) {
	repo := nextTestRepo(t)
	directory := filepath.Join(repo, ".product-loop", "features", "orphan")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "pr.md"), []byte("# Preview\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "INVALID_STATE" || status.NextOperation != "repair-state" {
		t.Fatalf("orphaned evidence did not block: %+v", status)
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

// TestFeaturePlanCandidatesExcludesLockedAndShippedFeatures is the unit-level
// guard for the shipped-feature ambiguity bug: locked (plan.lock.json) and
// shipped (pr.md) feature dirs must never re-register as open plan candidates
// even after their ephemeral per-worktree state.json was destroyed on cleanup.
func TestFeaturePlanCandidatesExcludesLockedAndShippedFeatures(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "open-feature")

	locked := filepath.Join(repo, ".product-loop", "features", "locked-feature")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "plan.lock.json"), []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeShippedFeatureArtifacts(t, repo, "shipped-feature")

	candidates, err := featurePlanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidates, []string{"open-feature"}) {
		t.Fatalf("locked/shipped features leaked into plan candidates: %v", candidates)
	}
}

// TestResolveNextIgnoresShippedFeatureCandidates reproduces the taxweave scenario
// through ResolveNext: one genuinely open feature plus several shipped dirs whose
// state.json was destroyed by worktree cleanup must resolve to the single open
// candidate, not AMBIGUOUS.
func TestResolveNextIgnoresShippedFeatureCandidates(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "open-feature")
	writeShippedFeatureArtifacts(t, repo, "shipped-one")
	writeShippedFeatureArtifacts(t, repo, "shipped-two")
	writeShippedFeatureArtifacts(t, repo, "shipped-three")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage == "AMBIGUOUS" {
		t.Fatalf("shipped features re-registered as ambiguous candidates: %+v", status)
	}
	if status.VerificationStatus != "VERIFIED" || status.Feature != "open-feature" ||
		status.ObservedStage != "DRAFT_PLAN" || status.NextOperation != "plan-gate" {
		t.Fatalf("single open feature did not resolve cleanly: %+v", status)
	}
}

// TestResolveNextIgnoresShippedFeaturesFromLinkedWorktree reproduces the exact
// reported symptom: from a fresh linked build worktree, shipped feature dirs
// (committed plan.md/plan.lock.json/pr.md, no worktree-local state.json) must not
// re-register as open candidates. Guards the durable-committed-artifact contract
// under real worktree conditions.
func TestResolveNextIgnoresShippedFeaturesFromLinkedWorktree(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")

	if err := os.MkdirAll(filepath.Join(repo, ".product-loop", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	value, err := MarshalJSON(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	writeSavedFeaturePlan(t, repo, "open-feature")
	writeShippedFeatureArtifacts(t, repo, "shipped-one")
	writeShippedFeatureArtifacts(t, repo, "shipped-two")

	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "seed shipped and open features")

	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "build-work", linked)

	status, err := ResolveNext(linked, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage == "AMBIGUOUS" {
		t.Fatalf("shipped features re-registered as ambiguous from linked worktree: %+v", status)
	}
	if status.VerificationStatus != "VERIFIED" || status.Feature != "open-feature" ||
		status.ObservedStage != "DRAFT_PLAN" || status.NextOperation != "plan-gate" {
		t.Fatalf("linked worktree did not resolve to the single open feature: %+v", status)
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
	if err := os.WriteFile(filepath.Join(repo, "source-plan.md"), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = "recovery"
	plan["source_plan_path"] = "../../../source-plan.md"
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

func setIgnoredDeliveries(t *testing.T, repo string, ignored ...string) {
	t.Helper()
	config := testConfig()
	config.Workflow.IgnoredDeliveries = ignored
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNextIgnoredActiveDeliveryClearsAmbiguity(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "first", "BUILD", 0)
	writeNextDelivery(t, repo, "second", "BUILD", 0)
	setIgnoredDeliveries(t, repo, "first")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage == "AMBIGUOUS" {
		t.Fatalf("ignored active delivery did not clear ambiguity: %+v", status)
	}
	if status.Feature != "second" || status.NextOperation != "build" {
		t.Fatalf("remaining active delivery did not resolve uniquely: %+v", status)
	}
}

func TestResolveNextIgnoredPublishedDeliveryClearsAmbiguity(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "published-one", "PUBLISHED", 1)
	writeNextDelivery(t, repo, "published-two", "PUBLISHED", 1)
	setIgnoredDeliveries(t, repo, "published-one")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus == "BLOCKED" || status.ObservedStage == "AMBIGUOUS" {
		t.Fatalf("ignored published delivery did not clear ambiguity: %+v", status)
	}
	if status.Feature != "published-two" {
		t.Fatalf("remaining published delivery did not resolve uniquely: %+v", status)
	}
}

func TestResolveNextNewUnignoredActiveDeliveryStillBlocks(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "first", "BUILD", 0)
	writeNextDelivery(t, repo, "second", "BUILD", 0)
	// Ignoring an unrelated slug must not clear a genuinely ambiguous pair.
	setIgnoredDeliveries(t, repo, "unrelated")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "AMBIGUOUS" || !reflect.DeepEqual(status.BlockingAmbiguity, []string{"first", "second"}) {
		t.Fatalf("un-ignored ambiguous deliveries should still block: %+v", status)
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
