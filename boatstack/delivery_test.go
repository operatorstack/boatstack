package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twoSlicePlan() map[string]any {
	plan := validPlan()
	plan["acceptance_criteria"] = []any{
		map[string]any{"id": "AC-1", "text": "first observable result"},
		map[string]any{"id": "AC-2", "text": "second observable result"},
	}
	first := plan["tasks"].([]any)[0].(map[string]any)
	first["affected_paths"] = []any{"feature.go"}
	second := map[string]any{
		"id": "T-2", "title": "implement second result", "depends_on": []any{"T-1"},
		"acceptance_criteria": []any{"AC-2"}, "affected_paths": []any{"second.go"},
		"validation": []any{map[string]any{
			"criteria": []any{"AC-2"}, "run": "go test ./...", "origin": "AC-2",
			"oracle": "second contract assertion", "independence": "contract-derived",
		}},
	}
	plan["tasks"] = []any{first, second}
	plan["delivery_slices"] = []any{
		map[string]any{"id": "phase-one", "title": "First reviewer outcome", "task_ids": []any{"T-1"}},
		map[string]any{"id": "phase-two", "title": "Second reviewer outcome", "task_ids": []any{"T-2"}},
	}
	return plan
}

func TestDeliverySlicesPartitionTasksAndRejectForwardDependencies(t *testing.T) {
	plan := twoSlicePlan()
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("valid two-slice plan rejected: %v", err)
	}
	plan["delivery_slices"].([]any)[1].(map[string]any)["task_ids"] = []any{"T-1", "T-2"}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "assigned") {
		t.Fatalf("duplicate task assignment did not block: %v", err)
	}
	plan = twoSlicePlan()
	plan["tasks"].([]any)[0].(map[string]any)["depends_on"] = []any{"T-2"}
	plan["tasks"].([]any)[1].(map[string]any)["depends_on"] = []any{}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "future slice") {
		t.Fatalf("forward delivery dependency did not block: %v", err)
	}
}

func activateTwoSliceDelivery(t *testing.T) (string, string) {
	t.Helper()
	repo := prTestRepo(t)
	feature := "phased-feature"
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := twoSlicePlan()
	plan["feature_id"] = feature
	plan["spec_path"] = "feature-spec.md"
	if err := os.WriteFile(filepath.Join(directory, "source-plan.md"), []byte("# Two PR proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "feature-spec.md"), []byte("# Accepted phased feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(directory, "approval.md")
	writeApprovalReceipt(t, approvalPath, check.Fingerprint)
	if err := ActivatePlan(ActivationOptions{
		PlanPath: planPath, ApprovalPath: approvalPath, OutDir: filepath.Join(directory, "compiled"),
		OutputPath: filepath.Join(directory, "plan.lock.json"), SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := "# Evidence ledger\n\n- Test gate (phase-one): `PASS`\n- Review gate (phase-one): `PASS`\n- Test gate (phase-two): `BLOCKED`\n- Review gate (phase-two): `BLOCKED`\n"
	if err := os.WriteFile(filepath.Join(directory, "evidence.md"), []byte(evidence), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".product-loop/features/"+feature)
	runGit(t, repo, "commit", "-m", "activate phased delivery")
	return repo, feature
}

func TestDeliveryGateReceiptsBindTheActiveSliceAndAdvanceOnce(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "review", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "test gate") {
		t.Fatalf("review passed without a test receipt: %v", err)
	}
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package fixture\n\nconst PhaseOne = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feature.go")
	runGit(t, repo, "commit", "-m", "change phase after test")
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "review", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "changed after the test gate") {
		t.Fatalf("review accepted a diff not covered by the test receipt: %v", err)
	}
	for _, gate := range []string{"test", "review"} {
		if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: gate, Status: "PASS"}); err != nil {
			t.Fatalf("record %s gate: %v", gate, err)
		}
	}
	if err := MarkDeliveryPublished(repo, feature, "phase-one", "https://example.invalid/pr/1"); err != nil {
		t.Fatal(err)
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveIndex != 1 || state.Slices[0].Status != "PUBLISHED" || state.Slices[1].Status != "BUILD" {
		t.Fatalf("publication advanced the wrong state: %#v", state)
	}
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := ActivatePlan(ActivationOptions{
		PlanPath: filepath.Join(directory, "plan.md"), ApprovalPath: filepath.Join(directory, "approval.md"),
		OutDir: filepath.Join(directory, "compiled"), OutputPath: filepath.Join(directory, "plan.lock.json"),
		SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	}); err != nil {
		t.Fatalf("idempotent build activation failed: %v", err)
	}
	state, err = LoadDeliveryState(repo, feature)
	if err != nil || state.ActiveIndex != 1 {
		t.Fatalf("rerunning build reset delivery progress: state=%#v err=%v", state, err)
	}
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "current slice is phase-two") {
		t.Fatalf("prior slice receipt reused after publication: %v", err)
	}
}

func TestDeliveryGateRejectsChangesOwnedByALaterSlice(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	if err := os.WriteFile(filepath.Join(repo, "second.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "second.go")
	runGit(t, repo, "commit", "-m", "implement future slice early")
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "outside its affected_paths") {
		t.Fatalf("active slice accepted a later slice's file: %v", err)
	}
}

func TestDeliveryGateRejectsStateFromAnotherPlanLock(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	state.PlanLockHash = strings.Repeat("b", 64)
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "stale for the current plan lock") {
		t.Fatalf("delivery state crossed plan locks: %v", err)
	}
}

func TestManagedDeliveryHookDeniesDirectPublicationRoutes(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "phased-feature", PlanLockHash: strings.Repeat("a", 64),
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "phase-one", Title: "First", Status: "BUILD"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"git push origin feature", "git -C " + repo + " push origin feature", "gh pr create --title phase-one",
		"gh api repos/example/project/pulls --method POST", "hub pull-request -m phase-one",
	} {
		findings := ClassifyCommand(repo, command)
		if len(findings) == 0 || findings[0].Category != "workflow-publication-bypass" {
			t.Fatalf("direct publication was not denied for %q: %#v", command, findings)
		}
	}
	findings := ClassifyTool(repo, "github_create_pull_request", map[string]any{"title": "phase one"})
	if len(findings) == 0 || findings[0].Category != "workflow-publication-bypass" {
		t.Fatalf("GitHub tool publication was not denied: %#v", findings)
	}
	if findings := ClassifyCommand(repo, "git status --short"); len(findings) != 0 {
		t.Fatalf("read-only Git was unexpectedly denied: %#v", findings)
	}
	statePath, err := deliveryStatePath(repo, "phased-feature")
	if err != nil {
		t.Fatal(err)
	}
	findings = ClassifyCommand(repo, "rm "+statePath)
	if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
		t.Fatalf("direct delivery-state mutation was not denied: %#v", findings)
	}
	if err := os.WriteFile(statePath, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings = ClassifyCommand(repo, "git push origin feature")
	if len(findings) == 0 || findings[0].Category != "workflow-state-invalid" {
		t.Fatalf("corrupt delivery state failed open: %#v", findings)
	}
}

func TestManagedDeliveryStateDoesNotBlockUnrelatedWorktrees(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "base")
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "phased-feature", PlanLockHash: strings.Repeat("a", 64),
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "phase-one", Title: "First", Status: "BUILD"}},
	}); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "other-work", linked)
	active, err := ActiveManagedDeliveries(linked)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("delivery state leaked into unrelated worktree: %v", active)
	}
	if findings := ClassifyCommand(linked, "git push origin other-work"); len(findings) != 0 {
		t.Fatalf("unrelated worktree publication was denied: %#v", findings)
	}
}
