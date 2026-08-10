package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// control-law: activation-requires-current-readiness-bound-to-exact-authority
func TestApprovalAndActivationBindSameReadinessFingerprint(t *testing.T) {
	repo := runTestRepo(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, repo, "switch", "-c", "readiness-feature")

	feature := "readiness-feature"
	dir := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source-plan.md"), []byte("# source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature-spec.md"), []byte("# spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["schema_version"] = float64(3)
	plan["feature_id"] = feature
	plan["spec_path"] = "feature-spec.md"
	plan["architecture_facts"] = []any{}
	plan["architecture_unknowns"] = []any{}
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["requires_facts"] = []any{}
	task["affected_paths"] = []any{"README.md"}
	task["rollback_boundary"] = "revert the change"
	task["side_effects"] = []any{}
	plan["journey_evidence"] = map[string]any{
		"relevance": "not_relevant", "reason": "internal control-only change", "oracles": []any{},
	}
	planPath := filepath.Join(dir, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := CheckPlanReadiness(planPath)
	if err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(dir, "approval.md")
	runGit(t, repo, "remote", "rename", "origin", "temporarily-unavailable")
	if err := RecordApproval(ApprovalRecordOptions{
		Repo:     repo,
		PlanPath: planPath, OutputPath: approvalPath, ApprovedBy: "Test Human",
		ApprovedAt: "2026-07-29T12:00:00Z", Fingerprint: check.Fingerprint,
	}); err == nil {
		t.Fatal("missing readiness allowed approval")
	}
	if fileExists(approvalPath) {
		t.Fatal("blocked readiness created an approval artifact")
	}
	runGit(t, repo, "remote", "rename", "temporarily-unavailable", "origin")
	writeApprovalReceipt(t, approvalPath, check.Fingerprint)
	if _, err := CheckApprovalReceipt(approvalPath, check); err == nil {
		t.Fatal("unactivated legacy approval must not authorize a schema-v3 plan")
	}
	if err := RecordApproval(ApprovalRecordOptions{
		Repo:     repo,
		PlanPath: planPath, OutputPath: approvalPath, ApprovedBy: "Test Human",
		ApprovedAt: "2026-07-29T12:00:00Z", Fingerprint: check.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	receipt, err := LoadApprovalReceipt(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != 3 || receipt.Readiness.Fingerprint != readiness.Fingerprint {
		t.Fatalf("approval readiness mismatch: %+v vs %+v", receipt.Readiness, readiness)
	}
	lockPath := filepath.Join(dir, "plan.lock.json")
	if err := ActivatePlan(ActivationOptions{
		Repo:     repo,
		PlanPath: planPath, ApprovalPath: approvalPath,
		OutDir: filepath.Join(dir, "compiled"), OutputPath: lockPath,
	}); err != nil {
		t.Fatal(err)
	}
	lockValue, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(lockValue, &lock); err != nil {
		t.Fatal(err)
	}
	if intValue(lock["schema_version"]) != 3 || stringValue(lock["readiness_fingerprint"]) != readiness.Fingerprint {
		t.Fatalf("activation lock did not preserve approval readiness: %+v", lock)
	}
	manifestPath := filepath.Join(dir, "compiled", "journey-oracles.json")
	manifestValue, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestValue, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["reason"] = "tampered after activation"
	tampered, _ := MarshalJSON(manifest)
	if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalLock(ApprovalOptions{
		Repo:           repo,
		SourcePlanPath: filepath.Join(dir, "source-plan.md"), SpecPath: filepath.Join(dir, "feature-spec.md"),
		PlanPath: planPath, TasksPath: filepath.Join(dir, "compiled", "tasks.json"),
		AuthorizationMode: "human", OutputPath: lockPath,
	}); err == nil {
		t.Fatal("tampered journey manifest bypassed the immutable lock")
	}
}
