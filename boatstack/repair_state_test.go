package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMalformedDraft models the taxweave-roles failure: an agent hand-authored a
// feature directory with a prose plan.md but never let the helper register it, so
// there is no plan.lock.json and no delivery state. CheckPlan fails on it, which
// the guard escalates to INVALID_STATE and denies every product mutation.
func writeMalformedDraft(t *testing.T, repo, feature string) string {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.md")
	body := "# " + feature + "\n\nHand-written prose with no structured, marked plan block.\n"
	if err := os.WriteFile(planPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPlan(planPath); err == nil {
		t.Fatalf("fixture is not malformed: CheckPlan unexpectedly passed for %s", feature)
	}
	return directory
}

func TestControlledPhaseTransitionAllowsRepairStateAcrossStages(t *testing.T) {
	command := ".product-loop/bin/boatstack-helper repair-state --repo . --feature stuck"
	for _, stage := range []string{"", "INVALID_STATE", "DRAFT_PLAN", "APPROVED"} {
		if !controlledPhaseTransition(command, stage) {
			t.Fatalf("repair-state was denied at stage %q; recovery must be reachable", stage)
		}
	}
	// The escape hatch must not widen the surface for real transitions or metachars.
	if controlledPhaseTransition(".product-loop/bin/boatstack-helper activate-plan", "INVALID_STATE") {
		t.Fatal("activate-plan escaped the INVALID_STATE interlock")
	}
	if controlledPhaseTransition(".product-loop/bin/boatstack-helper repair-state; rm -rf .", "INVALID_STATE") {
		t.Fatal("chained destruction was allowed to ride on repair-state")
	}
	if controlledPhaseTransition("python scripts/migrate.py", "INVALID_STATE") {
		t.Fatal("an ordinary mutation was allowed at INVALID_STATE")
	}
}

// TestRepairStateClosesTheInvalidStateLoop is the end-to-end contract: with a
// malformed unregistered draft on disk, a product mutation is denied and names
// repair-state, and repair-state itself is then allowed — the recovery the guard
// prescribes is genuinely reachable.
func TestRepairStateClosesTheInvalidStateLoop(t *testing.T) {
	repo := nextTestRepo(t)
	writeMalformedDraft(t, repo, "stuck-feature")

	findings := ClassifyCommand(repo, "python scripts/migrate.py")
	if len(findings) == 0 || findings[0].WorkflowStage != "INVALID_STATE" || findings[0].NextOperation != "repair-state" {
		t.Fatalf("product mutation was not denied with a repair-state prescription: %#v", findings)
	}
	if denied := ClassifyCommand(repo, ".product-loop/bin/boatstack-helper repair-state --repo . --feature stuck-feature"); len(denied) != 0 {
		t.Fatalf("the prescribed recovery was itself denied: %#v", denied)
	}
}

func TestRepairStateQuarantinesUnregisteredMalformedDraft(t *testing.T) {
	repo := nextTestRepo(t)
	directory := writeMalformedDraft(t, repo, "stuck-feature")

	result, err := RepairState(repo, "stuck-feature")
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || result.Action != "quarantined" || result.NextOperation != "auto-plan" {
		t.Fatalf("unexpected repair outcome: %#v", result)
	}
	if dirExists(directory) {
		t.Fatal("the malformed draft directory was not removed from features/")
	}
	if result.QuarantinePath == "" || !dirExists(filepath.Join(repo, filepath.FromSlash(result.QuarantinePath))) {
		t.Fatalf("quarantine copy is missing at %q", result.QuarantinePath)
	}
	if !fileExists(filepath.Join(repo, filepath.FromSlash(result.QuarantinePath), "plan.md")) {
		t.Fatal("quarantine did not preserve the draft plan.md")
	}
	// After recovery the workflow must be able to plan again from a clean slate.
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "NOT_STARTED" {
		t.Fatalf("workflow did not return to a plannable state: %q", status.ObservedStage)
	}
}

func TestRepairStateResolvesSoleCandidateWithoutFeature(t *testing.T) {
	repo := nextTestRepo(t)
	writeMalformedDraft(t, repo, "only-stuck")
	result, err := RepairState(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || result.Feature != "only-stuck" {
		t.Fatalf("sole malformed candidate was not resolved: %#v", result)
	}
}

func TestRepairStateBlocksAmbiguousDrafts(t *testing.T) {
	repo := nextTestRepo(t)
	writeMalformedDraft(t, repo, "stuck-one")
	writeMalformedDraft(t, repo, "stuck-two")
	result, err := RepairState(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" || len(result.Blockers) != 2 {
		t.Fatalf("ambiguous drafts were not refused with both candidates: %#v", result)
	}
}

func TestRepairStateRefusesRegisteredAndPublishedFeatures(t *testing.T) {
	t.Run("valid saved plan", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeValidSavedFeaturePlan(t, repo, "good-feature")
		assertRefused(t, repo, "good-feature")
	})
	t.Run("locked plan", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeMalformedDraft(t, repo, "locked-feature")
		writeDraftFile(t, repo, "locked-feature", "plan.lock.json", "lock\n")
		assertRefused(t, repo, "locked-feature")
	})
	t.Run("published pr", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeMalformedDraft(t, repo, "shipped-feature")
		writeDraftFile(t, repo, "shipped-feature", "pr.md", "# PR\n")
		assertRefused(t, repo, "shipped-feature")
	})
	t.Run("active delivery state", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeNextDelivery(t, repo, "active-feature", "BUILD", 0)
		// Overwrite the registered plan with a malformed one to prove the refusal
		// comes from the delivery state, not from CheckPlan.
		planPath := filepath.Join(repo, ".product-loop", "features", "active-feature", "plan.md")
		if err := os.WriteFile(planPath, []byte("# prose only\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertRefused(t, repo, "active-feature")
	})
	t.Run("tracked directory", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeMalformedDraft(t, repo, "tracked-feature")
		runGit(t, repo, "config", "user.name", "Boatstack Test")
		runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
		runGit(t, repo, "add", ".product-loop/features/tracked-feature/plan.md")
		assertRefused(t, repo, "tracked-feature")
	})
}

func assertRefused(t *testing.T, repo, feature string) {
	t.Helper()
	result, err := RepairState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" || result.Action != "refused" {
		t.Fatalf("repair-state should have refused %s but returned %#v", feature, result)
	}
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if !dirExists(directory) {
		t.Fatalf("refused feature %s was mutated on disk", feature)
	}
}

func writeDraftFile(t *testing.T, repo, feature, name, body string) {
	t.Helper()
	path := filepath.Join(repo, ".product-loop", "features", feature, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRepairStateDoesNotBypassRegisteredMalformedPlan guards against a silent
// escape: a feature with a plan.lock.json is registered even if its plan.md is
// malformed, so it is excluded from candidate resolution and never eligible for
// quarantine by an empty --feature.
func TestRepairStateDoesNotBypassRegisteredMalformedPlan(t *testing.T) {
	repo := nextTestRepo(t)
	writeMalformedDraft(t, repo, "registered-broken")
	writeDraftFile(t, repo, "registered-broken", "plan.lock.json", "lock\n")
	candidates, err := featurePlanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate == "registered-broken" {
			t.Fatal("a locked feature was offered as a repair candidate")
		}
	}
	result, err := RepairState(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action == "quarantined" || strings.Contains(result.QuarantinePath, "registered-broken") {
		t.Fatalf("repair-state quarantined a registered feature: %#v", result)
	}
}
