package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func planningRepo(t *testing.T) string {
	t.Helper()
	previousHealth := planningInstallationHealth
	planningInstallationHealth = func(string) error { return nil }
	t.Cleanup(func() { planningInstallationHealth = previousHealth })
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	return repo
}

func TestPlanningWriteBlocksBeforeArtifactWhenInstallationIsUnhealthy(t *testing.T) {
	repo := planningRepo(t)
	planningInstallationHealth = func(string) error { return fmt.Errorf("generated state drift") }
	_, err := WritePlanningArtifact(PlanningWriteOptions{Repo: repo, Feature: "blocked-plan", Artifact: "plan.md", Content: []byte("# Plan\n")})
	if err == nil || !strings.Contains(err.Error(), "generated state drift") {
		t.Fatalf("unhealthy installation did not block precisely: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".product-loop", "features", "blocked-plan")); !os.IsNotExist(statErr) {
		t.Fatalf("planning artifact directory exists after failed health check: %v", statErr)
	}
}

func TestPlanningWriteIsBoundedMarkdownOnly(t *testing.T) {
	repo := planningRepo(t)
	path, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "account-recovery", Artifact: "questions.md",
		Content: []byte("# Questions\n\nQ-1 remains open.\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != ".product-loop/features/account-recovery/questions.md" {
		t.Fatalf("unexpected planning path: %s", path)
	}
	value, _ := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
	if !strings.Contains(string(value), "Q-1") {
		t.Fatal("planning write lost content")
	}

	cases := []PlanningWriteOptions{
		{Repo: repo, Feature: "../escape", Artifact: "plan.md", Content: []byte("# bad\n")},
		{Repo: repo, Feature: "account-recovery", Artifact: "plan.json", Content: []byte("{}")},
		{Repo: repo, Feature: "account-recovery", Artifact: "../README.md", Content: []byte("# bad\n")},
		{Repo: repo, Feature: "account-recovery", Artifact: "plan.md", Content: []byte(" \n")},
		{Repo: repo, Feature: "account-recovery", Artifact: "plan.md", Content: []byte{0xff, 0xfe}},
	}
	for _, options := range cases {
		if _, err := WritePlanningArtifact(options); err == nil {
			t.Fatalf("expected bounded writer to reject %#v", options)
		}
	}
}

func TestPlanningWriteNormalizesPowerShellUTF8BOM(t *testing.T) {
	repo := planningRepo(t)
	path, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "powershell-transport", Artifact: "plan.md",
		Content: append([]byte{0xef, 0xbb, 0xbf}, []byte("# Plan\r\n")...),
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "# Plan\r\n" {
		t.Fatalf("PowerShell transport BOM reached the Markdown artifact: %q", written)
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "powershell-transport", Artifact: "questions.md",
		Content: []byte{0xef, 0xbb, 0xbf},
	}); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("a BOM without Markdown must remain empty input: %v", err)
	}
}

// Discoverability: the natural guess for an artifact token is "plan" — but the
// tokens carry a .md suffix, so it is always wrong. The rejection must name the
// accepted set and the transform at the point of failure, so the caller is not
// forced to discover them out of band (the failure that burned a planning session).
func TestPlanningWriteUnsupportedArtifactErrorIsDiscoverable(t *testing.T) {
	repo := planningRepo(t)
	_, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "account-recovery", Artifact: "plan", Content: []byte("# Plan\n"),
	})
	if err == nil {
		t.Fatal("expected the bare token to be rejected")
	}
	for _, want := range []string{"plan.md", "source-plan.md", "test-plan.md", ".md suffix"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unsupported-artifact error omitted %q: %v", want, err)
		}
	}
}

func TestPlanningWriteRejectsSymlinksAndPreservesExistingContentOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated Windows permissions")
	}
	repo := planningRepo(t)
	outside := t.TempDir()
	productLoop := filepath.Join(repo, ".product-loop")
	if err := os.Symlink(outside, productLoop); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "feature", Artifact: "plan.md", Content: []byte("# plan\n"),
	}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "features", "feature", "plan.md")); !os.IsNotExist(err) {
		t.Fatal("bounded writer followed a symlink")
	}

	if err := os.Remove(productLoop); err != nil {
		t.Fatal(err)
	}
	destination, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "feature", Artifact: "plan.md", Content: []byte("# known good\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "feature", Artifact: "plan.md", Content: []byte("\n"),
	}); err == nil {
		t.Fatal("expected invalid replacement to fail")
	}
	value, _ := os.ReadFile(filepath.Join(repo, filepath.FromSlash(destination)))
	if string(value) != "# known good\n" {
		t.Fatal("failed planning write damaged the previous artifact")
	}
}

func TestRecordApprovalChecksFingerprintAndWritesOnlyReceipt(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record planning inputs")
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	approval := filepath.Join(root, "approval.md")
	if err := RecordApproval(ApprovalRecordOptions{
		PlanPath: planPath, OutputPath: approval, ApprovedBy: "Test Human",
		ApprovedAt: "2026-07-16T12:00:00Z", Fingerprint: "wrong",
	}); err == nil {
		t.Fatal("expected stale fingerprint to block approval")
	}
	if _, err := os.Stat(approval); !os.IsNotExist(err) {
		t.Fatal("failed approval created a receipt")
	}
	if err := RecordApproval(ApprovalRecordOptions{
		PlanPath: planPath, ApprovedBy: "Test Human",
		ApprovedAt: "2026-07-16T12:00:00Z", Fingerprint: check.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckApprovalReceipt(approval, check); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			t.Fatalf("approval wrote machine state before build: %s", entry.Name())
		}
	}
}

func TestApprovalBindsAndPreservesExistingProductBaseline(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record planning baseline")
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("pre-existing operator edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := PlanningBaselineForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.DiffSHA256 == "" || len(baseline.ChangedPaths) != 1 || baseline.ChangedPaths[0] != "app.ts" {
		t.Fatalf("product baseline was not exposed for approval: %+v", baseline)
	}
	approval := filepath.Join(root, "approval.md")
	if err := RecordApproval(ApprovalRecordOptions{
		PlanPath: planPath, ApprovedBy: "Test Human", ApprovedAt: "2026-07-16T12:00:00Z",
		Fingerprint: check.Fingerprint, BaselineDiffSHA256: baseline.DiffSHA256,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckApprovalReceipt(approval, check); err != nil {
		t.Fatalf("unchanged product baseline invalidated approval: %v", err)
	}
	writeActivationConfig(t, root, true)
	compiled := filepath.Join(root, ".product-loop", "features", "feature-one", "compiled")
	lockPath := filepath.Join(root, ".product-loop", "features", "feature-one", "plan.lock.json")
	if err := ActivatePlan(ActivationOptions{PlanPath: planPath, ApprovalPath: approval, OutDir: compiled, OutputPath: lockPath, SourceCommit: "test"}); err != nil {
		t.Fatalf("unchanged pre-existing product diff blocked activation: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "app.ts"))
	if err != nil || string(content) != "pre-existing operator edit\n" {
		t.Fatalf("activation rewrote the pre-existing product diff: %q %v", content, err)
	}
	lockValue, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock := map[string]any{}
	if err := json.Unmarshal(lockValue, &lock); err != nil {
		t.Fatal(err)
	}
	if stringValue(lock["baseline_diff_sha256"]) != baseline.DiffSHA256 {
		t.Fatalf("plan lock lost baseline provenance: %#v", lock)
	}
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("drift after approval\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckApprovalReceipt(approval, check); err == nil || !strings.Contains(err.Error(), "baseline product diff changed") {
		t.Fatalf("product drift did not invalidate approval: %v", err)
	}
}

func TestDoctorDetectsMissingConfigAdapterAndVersionDrift(t *testing.T) {
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(repo, ".cursor", "commands", "ship-gate.md")
	if err := os.Remove(command); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err == nil || !strings.Contains(err.Error(), "missing .cursor/commands/ship-gate.md") {
		t.Fatalf("expected missing adapter diagnosis, got %v", err)
	}
	config, raw, _ := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	bundle, _ := BuildExportBundle(filepath.Join(repo, ".boatstack-project.json"), config, raw, "boatstack")
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	claudeSkill := filepath.Join(repo, ".claude", "skills", "auto-plan", "SKILL.md")
	if err := os.Remove(claudeSkill); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err == nil || !strings.Contains(err.Error(), "missing .claude/skills/auto-plan/SKILL.md") {
		t.Fatalf("expected missing Claude skill diagnosis, got %v", err)
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	lockValue, _ := os.ReadFile(lockPath)
	var lock map[string]any
	if err := json.Unmarshal(lockValue, &lock); err != nil {
		t.Fatal(err)
	}
	lock["boatstack_version"] = "v0.0.0"
	updated, _ := MarshalJSON(lock)
	if err := os.WriteFile(lockPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err == nil || !strings.Contains(err.Error(), "version drift") {
		t.Fatalf("expected helper version diagnosis, got %v", err)
	}
	if err := os.Remove(filepath.Join(repo, ".boatstack-project.json")); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err == nil || !strings.Contains(err.Error(), ".boatstack-project.json") {
		t.Fatalf("expected missing config diagnosis, got %v", err)
	}
}

func TestDoctorRejectsUnsafeHelperPaths(t *testing.T) {
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true}); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	lockValue, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(lockValue, &lock); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{
		filepath.Join(repo, ".product-loop", "bin", "boatstack-helper"),
		"../outside/boatstack-helper",
	} {
		lock["binary_path"] = unsafe
		updated, err := MarshalJSON(lock)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, updated, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Doctor(repo); err == nil || !strings.Contains(err.Error(), "invalid Boatstack helper path") {
			t.Fatalf("expected unsafe helper path %q to be rejected, got %v", unsafe, err)
		}
	}
}
