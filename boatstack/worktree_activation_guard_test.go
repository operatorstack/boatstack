package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Conformance for the worktree-discipline guard.
//
//	control-law: managed-delivery-activates-in-its-cut-worktree
//	Once a feature's workspace worktree is cut, activation must happen inside it,
//	never from the main worktree on the base branch (which would strand compiled
//	artifacts + a competing per-worktree delivery ledger on the base branch).
//	Activation inside the worktree, or when no workspace is cut, is unaffected.

func workspaceActivationConfig() ProjectConfig {
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Workspace = Workspace{Enabled: true, Mode: "worktree", Cleanup: "confirm", CleanupAfter: "merge"}
	return config
}

// control-law: managed-delivery-activates-in-its-cut-worktree
func TestActivationGuardBlocksMainWorktreeWhenWorkspaceCut(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "sample-feature"})
	if err != nil || cut.VerificationStatus != "VERIFIED" || cut.WorktreePath == "" {
		t.Fatalf("failed to cut workspace: %+v (%v)", cut, err)
	}
	config := workspaceActivationConfig()

	// From the main worktree on the base branch → blocked, naming the worktree.
	err = guardManagedActivationWorktree(repo, config, "sample-feature")
	if err == nil {
		t.Fatal("activation from the main worktree after a cut must be blocked")
	}
	// The guard's path comes from `git worktree list` (always forward slashes);
	// cut.WorktreePath comes from filepath.Join (OS separator). Normalize before
	// comparing so this holds on Windows too.
	if !strings.Contains(filepath.ToSlash(err.Error()), filepath.ToSlash(cut.WorktreePath)) || !strings.Contains(err.Error(), "cut worktree") {
		t.Fatalf("block message should name the worktree and the rule: %v", err)
	}

	// From inside the cut worktree → allowed.
	if err := guardManagedActivationWorktree(cut.WorktreePath, config, "sample-feature"); err != nil {
		t.Fatalf("activation inside the cut worktree must be allowed: %v", err)
	}
}

// control-law: managed-delivery-activates-in-its-cut-worktree
func TestActivationGuardIsInertWithoutWorktreeMode(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())

	// No workspace cut yet → the normal pre-cut path is allowed.
	if err := guardManagedActivationWorktree(repo, workspaceActivationConfig(), "sample-feature"); err != nil {
		t.Fatalf("pre-cut activation must be allowed: %v", err)
	}

	// Cut a workspace, then confirm disabled/branch-mode policies do not guard.
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "sample-feature"}); err != nil {
		t.Fatal(err)
	}
	if err := guardManagedActivationWorktree(repo, ProjectConfig{Workspace: Workspace{Enabled: false}}, "sample-feature"); err != nil {
		t.Fatalf("disabled workspace management must not guard: %v", err)
	}
	if err := guardManagedActivationWorktree(repo, ProjectConfig{Workspace: Workspace{Enabled: true, Mode: "branch"}}, "sample-feature"); err != nil {
		t.Fatalf("branch mode must not guard: %v", err)
	}
}

// control-law: managed-delivery-activates-in-its-cut-worktree
// End-to-end: ActivatePlan refuses to run in the main worktree after a cut, and
// writes no lock/delivery state on the base branch.
func TestActivatePlanBlockedFromMainWorktreeAfterCut(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, false) // policy mode: no approval needed
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	config := workspaceActivationConfig()
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record planning inputs")

	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: root, Feature: "feature-one"}); err != nil {
		t.Fatal(err)
	}

	lock := filepath.Join(root, "plan.lock.json")
	options := ActivationOptions{PlanPath: planPath, OutDir: filepath.Join(root, "compiled"), OutputPath: lock, SourceCommit: "test"}
	err = ActivatePlan(options)
	if err == nil || !strings.Contains(err.Error(), "cut worktree") {
		t.Fatalf("expected activation blocked from the main worktree, got %v", err)
	}
	if fileExists(lock) {
		t.Fatal("guard was bypassed: a plan lock was written on the base branch")
	}
	deliveries, _ := deliveryStateDirectory(root)
	if _, statErr := os.Stat(filepath.Join(deliveries, "feature-one")); statErr == nil {
		t.Fatal("guard was bypassed: delivery state was written on the base branch")
	}
}
