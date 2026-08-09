package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Boundary: validated plan package -> feature workspace.
// Control law: the transition preserves the exact plan fingerprint, repository
// identity, and fresh base branch or leaves the source authoritative unchanged.

func addWorkspaceOrigin(t *testing.T, repo string) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	workspaceGitDo(t, repo, "remote", "add", "origin", remote)
	workspaceGitDo(t, repo, "push", "-u", "origin", "main")
	return remote
}

func commitWorkspaceController(t *testing.T, repo string) {
	t.Helper()
	workspaceGitDo(t, repo, "add", ".product-loop/project.json")
	workspaceGitDo(t, repo, "commit", "-m", "install controller fixture")
}

func writeWorkspacePlanPackage(t *testing.T, repo, feature string) (string, string) {
	t.Helper()
	directory := WorkspaceFor(repo).FeatureDir(feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "source-plan.md"), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "spec.md"), []byte("# Feature spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = feature
	planPath := filepath.Join(directory, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	return planPath, check.Fingerprint
}

func writeWorkspaceSchema3Package(t *testing.T, repo, feature string) (string, string) {
	t.Helper()
	directory := WorkspaceFor(repo).FeatureDir(feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "source-plan.md"), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "spec.md"), []byte("# Feature spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["schema_version"] = float64(3)
	plan["feature_id"] = feature
	plan["architecture_facts"] = []any{}
	plan["architecture_unknowns"] = []any{}
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["requires_facts"] = []any{}
	task["affected_paths"] = []any{"README.md"}
	task["side_effects"] = []any{}
	task["rollback_boundary"] = "revert the workspace fixture"
	plan["journey_evidence"] = map[string]any{
		"relevance": "not_relevant", "reason": "workspace control-only fixture", "oracles": []any{},
	}
	planPath := filepath.Join(directory, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	return planPath, check.Fingerprint
}

func withWorkspaceTransitionSeams(t *testing.T) {
	t.Helper()
	copyFn := workspacePackageCopy
	removeFn := workspaceSourcePackageRemove
	aliasFn := workspaceDetachedAlias
	afterDestinationFn := workspaceAfterDestination
	afterAliasFn := workspaceAfterDetachedAlias
	t.Cleanup(func() {
		workspacePackageCopy = copyFn
		workspaceSourcePackageRemove = removeFn
		workspaceDetachedAlias = aliasFn
		workspaceAfterDestination = afterDestinationFn
		workspaceAfterDetachedAlias = afterAliasFn
	})
}

// control-law: workspace-transition-preserves-plan-authority
func TestWorkspaceTransitionMovesValidatedPackageFromBase(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	sourcePlan, fingerprint := writeWorkspacePlanPackage(t, repo, "feature-one")

	status, err := ResolveNext(repo, "")
	if err != nil || status.NextOperation != "workspace-cut" || status.ObservedStage != "DRAFT_PLAN" {
		t.Fatalf("valid draft did not route through workspace transition: %+v (%v)", status, err)
	}
	result, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("transition failed: %+v (%v)", result, err)
	}
	if result.Outcome != "created" || result.PlanFingerprint != fingerprint || result.DestinationRepo == "" || result.BaseCommit == "" {
		t.Fatalf("transition result lacks verified destination facts: %+v", result)
	}
	if fileExists(sourcePlan) {
		t.Fatal("source package remained authoritative after verified relocation")
	}
	destinationPlan := filepath.Join(WorkspaceFor(result.DestinationRepo).FeatureDir("feature-one"), "plan.md")
	check, err := CheckPlan(destinationPlan)
	if err != nil || check.Fingerprint != fingerprint {
		t.Fatalf("destination plan fingerprint changed: %v %#v", err, check)
	}
	next, err := ResolveNext(result.DestinationRepo, "")
	if err != nil || next.NextOperation != "plan-gate" || next.ObservedStage != "DRAFT_PLAN" {
		t.Fatalf("destination did not continue at approval boundary: %+v (%v)", next, err)
	}
}

// control-law: workspace-transition-preserves-plan-authority
func TestWorkspaceTransitionAcceptsDetachedHEAD(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	workspaceGitDo(t, repo, "checkout", "--detach")
	_, fingerprint := writeWorkspacePlanPackage(t, repo, "feature-one")

	result, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if err != nil || result.VerificationStatus != "VERIFIED" || result.PlanFingerprint != fingerprint {
		t.Fatalf("detached HEAD did not reach a verified workspace: %+v (%v)", result, err)
	}
	branch, _ := workspaceGit(result.DestinationRepo, "branch", "--show-current")
	if strings.TrimSpace(branch) != "feat/feature-one" {
		t.Fatalf("destination branch = %q", branch)
	}
}

// control-law: activation-requires-current-readiness-bound-to-exact-authority
func TestWorkspaceTransitionPrecedesSchema3ApprovalAndActivation(t *testing.T) {
	previousHealth := runInstallationHealth
	runInstallationHealth = func(string) error { return nil }
	t.Cleanup(func() { runInstallationHealth = previousHealth })
	repo := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	planPath, fingerprint := writeWorkspaceSchema3Package(t, repo, "feature-one")
	if _, err := CheckPlanReadiness(planPath); err == nil || !strings.Contains(err.Error(), "workspace-cut") {
		t.Fatalf("pre-transition readiness did not name workspace-cut: %v", err)
	}
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if result.VerificationStatus != "VERIFIED" || result.PlanFingerprint != fingerprint {
		t.Fatalf("workspace transition failed: %+v", result)
	}
	destinationPlan := filepath.Join(WorkspaceFor(result.DestinationRepo).FeatureDir("feature-one"), "plan.md")
	check, err := CheckPlan(destinationPlan)
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := CheckPlanReadiness(destinationPlan)
	if err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(filepath.Dir(destinationPlan), "approval.md")
	if err := RecordApproval(ApprovalRecordOptions{
		PlanPath: destinationPlan, OutputPath: approvalPath, ApprovedBy: "Test Human",
		ApprovedAt: "2026-08-09T12:00:00Z", Fingerprint: check.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	receipt, err := LoadApprovalReceipt(approvalPath)
	if err != nil || receipt.Readiness.Fingerprint != readiness.Fingerprint || receipt.Readiness.HeadBranch != "feat/feature-one" {
		t.Fatalf("approval did not bind destination readiness: %+v (%v)", receipt, err)
	}
	if err := ActivatePlan(ActivationOptions{
		PlanPath: destinationPlan, ApprovalPath: approvalPath,
		OutDir:     filepath.Join(filepath.Dir(destinationPlan), "compiled"),
		OutputPath: filepath.Join(filepath.Dir(destinationPlan), "plan.lock.json"),
	}); err != nil {
		t.Fatalf("destination approval did not activate: %v", err)
	}
}

// control-law: autonomy-receipt-binds-policy-activation-to-plan-repository-and-branch
func TestWorkspaceTransitionPrecedesAutonomyTargets(t *testing.T) {
	for _, target := range []RunTarget{RunTargetVerified, RunTargetPR} {
		t.Run(string(target), func(t *testing.T) {
			previousAction := autonomyRecommendedPRAction
			autonomyRecommendedPRAction = func(string) (string, string, error) { return "open", "", nil }
			t.Cleanup(func() { autonomyRecommendedPRAction = previousAction })
			repo := workspaceRepo(t, defaultWorkspace())
			commitWorkspaceController(t, repo)
			addWorkspaceOrigin(t, repo)
			planPath, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
			if _, err := RecordAutonomy(AutonomyRecordOptions{Repo: repo, PlanPath: planPath, Target: target}); err == nil || !strings.Contains(err.Error(), "workspace-cut") {
				t.Fatalf("pre-transition autonomy did not name workspace-cut: %v", err)
			}
			result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
			if result.VerificationStatus != "VERIFIED" {
				t.Fatalf("workspace transition failed: %+v", result)
			}
			destinationPlan := filepath.Join(WorkspaceFor(result.DestinationRepo).FeatureDir("feature-one"), "plan.md")
			receipt, err := RecordAutonomy(AutonomyRecordOptions{Repo: result.DestinationRepo, PlanPath: destinationPlan, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Branch != "feat/feature-one" || receipt.IssuingBranch != "feat/feature-one" || receipt.PlanFingerprint != result.PlanFingerprint {
				t.Fatalf("autonomy did not bind destination: %+v", receipt)
			}
		})
	}
}

// control-law: workspace-transition-adopts-only-pristine-base
func TestWorkspaceTransitionAdoptsOnlyExactBaseBranch(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	workspaceGitDo(t, repo, "branch", "feat/feature-one")
	_, fingerprint := writeWorkspacePlanPackage(t, repo, "feature-one")
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if result.VerificationStatus != "VERIFIED" || result.Outcome != "adopted" || result.PlanFingerprint != fingerprint {
		t.Fatalf("exact-base branch was not adopted: %+v", result)
	}

	other := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, other)
	addWorkspaceOrigin(t, other)
	workspaceGitDo(t, other, "switch", "-c", "feat/feature-one")
	if err := os.WriteFile(filepath.Join(other, "diverged.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, other, "add", "diverged.txt")
	workspaceGitDo(t, other, "commit", "-m", "diverge feature branch")
	workspaceGitDo(t, other, "switch", "main")
	sourcePlan, _ := writeWorkspacePlanPackage(t, other, "feature-one")
	blocked, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: other, Feature: "feature-one"})
	if blocked.VerificationStatus != "BLOCKED" || !strings.Contains(blocked.Reason, "diverges") || !fileExists(sourcePlan) {
		t.Fatalf("divergent branch changed source authority: %+v", blocked)
	}
}

// control-law: workspace-transition-adopts-only-pristine-base
func TestWorkspaceTransitionRejectsDirtyOwnedAndConflictingDestinations(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		repo := workspaceRepo(t, defaultWorkspace())
		commitWorkspaceController(t, repo)
		addWorkspaceOrigin(t, repo)
		workspaceGitDo(t, repo, "branch", "feat/feature-one")
		path := filepath.Join(repo, ".product-loop", "worktrees", "existing-feature-one")
		workspaceGitDo(t, repo, "worktree", "add", path, "feat/feature-one")
		return repo, path
	}

	t.Run("dirty", func(t *testing.T) {
		repo, destination := setup(t)
		sourcePlan, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
		if err := os.WriteFile(filepath.Join(destination, "unrelated.txt"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
		if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "changes outside") || !fileExists(sourcePlan) {
			t.Fatalf("dirty destination changed source authority: %+v", result)
		}
	})

	t.Run("owned", func(t *testing.T) {
		repo, destination := setup(t)
		sourcePlan, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
		if err := saveDeliveryState(destination, DeliveryState{
			SchemaVersion: deliveryStateSchemaVersion, Feature: "other-delivery", ActiveIndex: 0,
			Slices: []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: StatusBuild, HeadBranch: "feat/feature-one"}},
		}); err != nil {
			t.Fatal(err)
		}
		result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
		if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "owned") || !fileExists(sourcePlan) {
			t.Fatalf("owned destination changed source authority: %+v", result)
		}
	})

	t.Run("conflicting_plan", func(t *testing.T) {
		repo, destination := setup(t)
		sourcePlan, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
		destinationDir := WorkspaceFor(destination).FeatureDir("feature-one")
		if err := os.MkdirAll(destinationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destinationDir, "source-plan.md"), []byte("# Other source\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destinationDir, "spec.md"), []byte("# Other spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		plan := validPlan()
		plan["feature_id"] = "feature-one"
		plan["acceptance_criteria"].([]any)[0].(map[string]any)["text"] = "different result"
		writeMarkdownPlan(t, filepath.Join(destinationDir, "plan.md"), plan, true)
		result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
		if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "conflicting") || !fileExists(sourcePlan) {
			t.Fatalf("conflicting destination changed source authority: %+v", result)
		}
	})
}

// control-law: workspace-transition-preserves-plan-authority
func TestWorkspaceTransitionReusesMatchingWorktree(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	sourcePlan, fingerprint := writeWorkspacePlanPackage(t, repo, "feature-one")
	workspaceGitDo(t, repo, "branch", "feat/feature-one")
	destination := filepath.Join(repo, ".product-loop", "worktrees", "existing-feature-one")
	workspaceGitDo(t, repo, "worktree", "add", destination, "feat/feature-one")
	destinationDir := WorkspaceFor(destination).FeatureDir("feature-one")
	if err := copyFeaturePackage(filepath.Dir(sourcePlan), destinationDir); err != nil {
		t.Fatal(err)
	}
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	expectedDestination, err := filepath.EvalSymlinks(destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || result.Outcome != "current" || !samePath(result.DestinationRepo, expectedDestination) || result.PlanFingerprint != fingerprint {
		t.Fatalf("matching worktree was not reused: %+v", result)
	}
	if fileExists(sourcePlan) {
		t.Fatal("matching current worktree left the embedded source package authoritative")
	}
}

// control-law: workspace-transition-preserves-plan-authority
func TestWorkspaceTransitionRollbackPreservesSource(t *testing.T) {
	tests := []struct {
		name string
		fail func()
	}{
		{"copy", func() {
			workspacePackageCopy = func(string, string) error { return fmt.Errorf("injected copy failure") }
		}},
		{"copied_package_drift", func() {
			workspacePackageCopy = func(source, destination string) error {
				if err := copyFeaturePackage(source, destination); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(destination, "spec.md"), []byte("# Drifted spec\n"), 0o644)
			}
		}},
		{"source_cleanup", func() {
			workspaceSourcePackageRemove = func(string) error { return fmt.Errorf("injected cleanup failure") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withWorkspaceTransitionSeams(t)
			repo := workspaceRepo(t, defaultWorkspace())
			commitWorkspaceController(t, repo)
			addWorkspaceOrigin(t, repo)
			sourcePlan, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
			test.fail()
			result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
			if result.VerificationStatus != "BLOCKED" || !fileExists(sourcePlan) {
				t.Fatalf("failure did not preserve source package: %+v", result)
			}
			if branchExists(repo, "feat/feature-one") || worktreePathForBranch(repo, "feat/feature-one") != "" {
				t.Fatal("failed transition left branch or worktree authority")
			}
		})
	}
}

// control-law: workspace-transition-preserves-plan-authority
func TestBranchWorkspaceTransitionRollbackRestoresOriginalHead(t *testing.T) {
	withWorkspaceTransitionSeams(t)
	repo := workspaceRepo(t, Workspace{Enabled: true, Mode: "branch"})
	commitWorkspaceController(t, repo)
	addWorkspaceOrigin(t, repo)
	sourcePlan, _ := writeWorkspacePlanPackage(t, repo, "feature-one")
	originalHead, err := workspaceGit(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	workspaceAfterDestination = func(string) error { return fmt.Errorf("injected post-branch failure") }

	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if result.VerificationStatus != "BLOCKED" || !fileExists(sourcePlan) {
		t.Fatalf("branch failure did not preserve source authority: %+v", result)
	}
	current, err := workspaceGit(repo, "branch", "--show-current")
	if err != nil {
		t.Fatal(err)
	}
	if current = strings.TrimSpace(current); current != "main" {
		t.Fatalf("rollback left caller on %q", current)
	}
	head, err := workspaceGit(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if head = strings.TrimSpace(head); head != strings.TrimSpace(originalHead) {
		t.Fatalf("rollback changed original head: got %s want %s", head, originalHead)
	}
	if branchExists(repo, "feat/feature-one") {
		t.Fatal("branch rollback left partial feature authority")
	}
}

// control-law: detached-state-controls-only-its-bound-repository
func TestDetachedWorkspaceTransitionReusesControllerIdentity(t *testing.T) {
	repo := detachedTestRepo(t, "")
	remote := addWorkspaceOrigin(t, repo)
	_ = remote
	attached, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || attached.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach failed: %+v (%v)", attached, err)
	}
	_, fingerprint := writeWorkspacePlanPackage(t, repo, "feature-one")
	result, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if err != nil || result.VerificationStatus != "VERIFIED" || result.ControllerMode != "detached" || result.PlanFingerprint != fingerprint {
		t.Fatalf("detached transition failed: %+v (%v)", result, err)
	}
	destination := WorkspaceFor(result.DestinationRepo)
	if destination.Mode != SupervisionDetached || destination.RepoID != attached.RepoID || destination.WorktreeID == WorkspaceFor(repo).WorktreeID {
		t.Fatalf("destination controller identity drifted: source=%+v destination=%+v", WorkspaceFor(repo), destination)
	}
}

// control-law: detached-state-controls-only-its-bound-repository
func TestDetachedWorkspaceRegistrationRollsBack(t *testing.T) {
	withWorkspaceTransitionSeams(t)
	repo := detachedTestRepo(t, "")
	addWorkspaceOrigin(t, repo)
	attached, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || attached.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach failed: %+v (%v)", attached, err)
	}
	writeWorkspacePlanPackage(t, repo, "feature-one")
	workspaceAfterDetachedAlias = func(string) error { return fmt.Errorf("injected post-registration failure") }
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if result.VerificationStatus != "BLOCKED" {
		t.Fatalf("post-registration failure was accepted: %+v", result)
	}
	if branchExists(repo, "feat/feature-one") || worktreePathForBranch(repo, "feat/feature-one") != "" {
		t.Fatal("detached registration failure left branch or worktree authority")
	}
	registry, err := loadRegistry(filepath.Join(os.Getenv(stateRootEnv), "boatstack"))
	if err != nil || len(registry.Repositories) != 1 {
		t.Fatalf("detached alias rollback changed source binding: %+v (%v)", registry, err)
	}
}
