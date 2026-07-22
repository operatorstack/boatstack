package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceGitDo(t *testing.T, dir string, arguments ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, arguments...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, out)
	}
}

// workspaceRepo builds a real git repository with one commit on main and a
// Boatstack project.json carrying the given workspace policy.
func workspaceRepo(t *testing.T, ws Workspace) string {
	t.Helper()
	repo := t.TempDir()
	workspaceGitDo(t, repo, "init", "-b", "main")
	workspaceGitDo(t, repo, "config", "user.name", "Boatstack Test")
	workspaceGitDo(t, repo, "config", "user.email", "boatstack@example.test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", "README.md")
	workspaceGitDo(t, repo, "commit", "-m", "initial")
	config := ProjectConfig{
		SchemaVersion: 1,
		Project:       Project{Name: "test", DefaultBranch: "main", Commands: map[string]string{"test": "go test ./..."}},
		Workflow:      Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true},
		Workspace:     ws,
		Adapters:      []string{"cursor"},
	}
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func withWorkspaceGh(t *testing.T, fn func(string, ...string) (string, error)) {
	t.Helper()
	old := workspaceGh
	workspaceGh = fn
	t.Cleanup(func() { workspaceGh = old })
}

func ghState(state string) func(string, ...string) (string, error) {
	return func(string, ...string) (string, error) { return state, nil }
}

func ghUnavailable() func(string, ...string) (string, error) {
	return func(string, ...string) (string, error) { return "", fmt.Errorf("gh: not found") }
}

func defaultWorkspace() Workspace {
	return Workspace{Enabled: true, Mode: "worktree", Cleanup: "confirm", CleanupAfter: "merge"}
}

func TestResolveWorkspaceAppliesDefaults(t *testing.T) {
	got := resolveWorkspace(Workspace{Enabled: true})
	if got.Mode != "worktree" || got.Cleanup != "confirm" || got.CleanupAfter != "merge" {
		t.Fatalf("unexpected resolved defaults: %+v", got)
	}
	if resolveWorkspace(Workspace{}).Enabled {
		t.Fatal("empty workspace must resolve to disabled")
	}
	explicit := resolveWorkspace(Workspace{Enabled: true, Mode: "branch", Cleanup: "auto", CleanupAfter: "ship"})
	if explicit.Mode != "branch" || explicit.Cleanup != "auto" || explicit.CleanupAfter != "ship" {
		t.Fatalf("explicit values overwritten: %+v", explicit)
	}
}

func TestValidateWorkspaceConfig(t *testing.T) {
	valid := []Workspace{
		{},
		{Enabled: true},
		{Mode: "worktree", Cleanup: "confirm", CleanupAfter: "merge"},
		{Mode: "branch", Cleanup: "off", CleanupAfter: "ship"},
		{Cleanup: "auto"},
	}
	for _, ws := range valid {
		if err := validateWorkspaceConfig(ws); err != nil {
			t.Fatalf("expected %+v valid: %v", ws, err)
		}
	}
	invalid := []Workspace{
		{Mode: "detached"},
		{Cleanup: "prompt"},
		{CleanupAfter: "review"},
	}
	for _, ws := range invalid {
		if err := validateWorkspaceConfig(ws); err == nil {
			t.Fatalf("expected %+v invalid", ws)
		}
	}
}

func TestCutFeatureWorkspaceWorktreeMode(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	result, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "add-widget"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || !result.Created || result.Branch != "feat/add-widget" || result.Mode != "worktree" {
		t.Fatalf("unexpected cut: %+v", result)
	}
	if result.WorktreePath == "" {
		t.Fatal("worktree mode must report a worktree path")
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree directory missing: %v", err)
	}
	if !branchExists(repo, "feat/add-widget") {
		t.Fatal("branch was not created")
	}
}

func TestCutFeatureWorkspaceBranchMode(t *testing.T) {
	ws := defaultWorkspace()
	ws.Mode = "branch"
	repo := workspaceRepo(t, ws)
	result, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Branch: "feat/inline"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || result.Mode != "branch" || result.WorktreePath != "" {
		t.Fatalf("unexpected branch-mode cut: %+v", result)
	}
	current, _ := gitCommand(repo, "branch", "--show-current")
	if strings.TrimSpace(current) != "feat/inline" {
		t.Fatalf("branch mode did not switch to feature branch, on %q", current)
	}
}

func TestCutFeatureWorkspaceRefusesExistingBranch(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	workspaceGitDo(t, repo, "branch", "feat/dupe")
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "dupe"})
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "already exists") {
		t.Fatalf("expected existing-branch block: %+v", result)
	}
}

func TestCutFeatureWorkspaceRefusesBaseBranch(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Branch: "main"})
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "base branch") {
		t.Fatalf("expected base-branch block: %+v", result)
	}
}

func TestCutFeatureWorkspaceDisabled(t *testing.T) {
	repo := workspaceRepo(t, Workspace{Enabled: false})
	result, _ := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "x"})
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "disabled") {
		t.Fatalf("expected disabled block: %+v", result)
	}
}

func TestWorkspaceMergeStatusPrefersGh(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	withWorkspaceGh(t, ghState("MERGED"))
	if merged, source := workspaceMergeStatus(repo, "feat/x", "main"); !merged || source != "gh" {
		t.Fatalf("gh MERGED not honored: merged=%v source=%s", merged, source)
	}
	withWorkspaceGh(t, ghState("OPEN"))
	if merged, source := workspaceMergeStatus(repo, "feat/x", "main"); merged || source != "gh" {
		t.Fatalf("gh OPEN not honored: merged=%v source=%s", merged, source)
	}
}

func TestWorkspaceMergeStatusFallsBackToGit(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	withWorkspaceGh(t, ghUnavailable())
	// Merged: branch is an ancestor of main.
	workspaceGitDo(t, repo, "branch", "feat/landed")
	if merged, source := workspaceMergeStatus(repo, "feat/landed", "main"); !merged || source != "git" {
		t.Fatalf("git ancestry merged not detected: merged=%v source=%s", merged, source)
	}
	// Not merged: branch has a commit main does not contain.
	workspaceGitDo(t, repo, "switch", "-c", "feat/ahead")
	if err := os.WriteFile(filepath.Join(repo, "ahead.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", "ahead.txt")
	workspaceGitDo(t, repo, "commit", "-m", "ahead")
	workspaceGitDo(t, repo, "switch", "main")
	if merged, _ := workspaceMergeStatus(repo, "feat/ahead", "main"); merged {
		t.Fatal("branch with unmerged commit reported as merged")
	}
}

func TestCleanupBlocksWhenNotMerged(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "open-feature"}); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("OPEN"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/open-feature", Confirm: true})
	if result.VerificationStatus != "BLOCKED" || result.Merged || !strings.Contains(result.Reason, "not merged") {
		t.Fatalf("expected not-merged block: %+v", result)
	}
	if !branchExists(repo, "feat/open-feature") {
		t.Fatal("blocked cleanup must not delete the branch")
	}
}

func TestCleanupNeedsConfirmation(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "ready"}); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/ready", Confirm: false})
	if result.VerificationStatus != "NEEDS_CONFIRMATION" {
		t.Fatalf("expected confirmation gate: %+v", result)
	}
	if !branchExists(repo, "feat/ready") {
		t.Fatal("confirmation gate must not delete anything")
	}
}

func TestCleanupRemovesMergedWorktree(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "done"})
	if err != nil {
		t.Fatal(err)
	}
	// A committed change in the worktree keeps it clean but non-empty.
	if err := os.WriteFile(filepath.Join(cut.WorktreePath, "done.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, cut.WorktreePath, "add", "done.txt")
	workspaceGitDo(t, cut.WorktreePath, "commit", "-m", "done")
	withWorkspaceGh(t, ghState("MERGED"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/done", Confirm: true})
	if result.VerificationStatus != "VERIFIED" || !result.WorktreeRemoved || !result.BranchDeleted {
		t.Fatalf("expected full cleanup: %+v", result)
	}
	if _, err := os.Stat(cut.WorktreePath); !os.IsNotExist(err) {
		t.Fatal("worktree directory was not removed")
	}
	if branchExists(repo, "feat/done") {
		t.Fatal("branch was not deleted")
	}
}

func TestCleanupDirtyWorktreeBlocked(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "dirty"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cut.WorktreePath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/dirty", Confirm: true})
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "uncommitted") {
		t.Fatalf("expected dirty block: %+v", result)
	}
	if _, err := os.Stat(cut.WorktreePath); err != nil {
		t.Fatal("blocked cleanup must not remove a dirty worktree")
	}
}

func TestCleanupForceDiscardsDirtyUnmerged(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "abandon"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cut.WorktreePath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("OPEN"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/abandon", Force: true})
	if result.VerificationStatus != "VERIFIED" || !result.WorktreeRemoved || !result.BranchDeleted {
		t.Fatalf("force cleanup should discard everything: %+v", result)
	}
}

func TestCleanupDisabled(t *testing.T) {
	ws := defaultWorkspace()
	ws.Cleanup = "off"
	repo := workspaceRepo(t, ws)
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "keep"}); err != nil {
		t.Fatal(err)
	}
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/keep", Confirm: true})
	if result.VerificationStatus != "BLOCKED" || !strings.Contains(result.Reason, "disabled") {
		t.Fatalf("expected cleanup-off block: %+v", result)
	}
}

func TestCleanupNothingToClean(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/ghost", Confirm: true})
	if result.VerificationStatus != "VERIFIED" || !strings.Contains(result.Reason, "nothing to clean") {
		t.Fatalf("expected idempotent no-op: %+v", result)
	}
}

func TestCleanupAutoModeSkipsConfirmation(t *testing.T) {
	ws := defaultWorkspace()
	ws.Cleanup = "auto"
	repo := workspaceRepo(t, ws)
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "auto-clean"}); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	result, _ := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: "feat/auto-clean", Confirm: false})
	if result.VerificationStatus != "VERIFIED" || !result.BranchDeleted {
		t.Fatalf("auto cleanup should not require confirmation: %+v", result)
	}
}

func writeApprovedFeature(t *testing.T, repo, feature string) {
	t.Helper()
	dir := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approval.md"), []byte("# Approval\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNextRoutesToWorkspaceCutWhenApprovedOnBase(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	writeApprovedFeature(t, repo, "newthing")
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "APPROVED" || status.NextOperation != "workspace-cut" {
		t.Fatalf("expected workspace-cut routing: %+v", status)
	}
}

func TestResolveNextApprovedBuildsWhenWorkspaceExists(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	writeApprovedFeature(t, repo, "cutdone")
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "cutdone"}); err != nil {
		t.Fatal(err)
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.NextOperation != "build" {
		t.Fatalf("expected build once workspace exists: %+v", status)
	}
}

func TestResolveNextApprovedBuildsWhenWorkspaceDisabled(t *testing.T) {
	repo := workspaceRepo(t, Workspace{Enabled: false})
	writeApprovedFeature(t, repo, "plainfeat")
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.NextOperation != "build" {
		t.Fatalf("disabled workspace must go straight to build: %+v", status)
	}
}

func writeCompletedDelivery(t *testing.T, repo, feature, headBranch string) {
	t.Helper()
	dir := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := SHA256File(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: hash,
		ActiveIndex: 1,
		Slices:      []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: "PUBLISHED", HeadBranch: headBranch}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNextRoutesToWorkspaceCleanupAfterPublication(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "shipped"}); err != nil {
		t.Fatal(err)
	}
	writeCompletedDelivery(t, repo, "shipped", "feat/shipped")
	withRecoveryGh(t, recoveryPR("MERGED", "feat/shipped", "head"))
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "FEATURE_COMPLETE" || status.NextOperation != "workspace-cleanup" {
		t.Fatalf("expected cleanup routing: %+v", status)
	}
}

func TestResolveNextFeatureCompleteStaysNoneWithoutWorktree(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	writeCompletedDelivery(t, repo, "shipped", "feat/no-worktree")
	withRecoveryGh(t, recoveryPR("MERGED", "feat/no-worktree", "head"))
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedStage != "FEATURE_COMPLETE" || status.NextOperation != "none" {
		t.Fatalf("expected none without a live worktree: %+v", status)
	}
}

func TestResolveNextFeatureCompleteStaysNoneWhenWorkspaceDisabled(t *testing.T) {
	repo := workspaceRepo(t, Workspace{Enabled: false})
	// A worktree exists on disk, but management is off, so cleanup is not surfaced.
	workspaceGitDo(t, repo, "worktree", "add", "-b", "feat/manual", filepath.Join(repo, "wt-manual"))
	writeCompletedDelivery(t, repo, "shipped", "feat/manual")
	withRecoveryGh(t, recoveryPR("MERGED", "feat/manual", "head"))
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.NextOperation != "none" {
		t.Fatalf("disabled workspace must not route to cleanup: %+v", status)
	}
}

func TestFeatureWorkspaceStatus(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	if _, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "reportable"}); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("OPEN"))
	open, err := FeatureWorkspaceStatus(repo, "feat/reportable")
	if err != nil {
		t.Fatal(err)
	}
	if !open.Exists || open.Merged || open.CleanupDue {
		t.Fatalf("open workspace status wrong: %+v", open)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	merged, err := FeatureWorkspaceStatus(repo, "feat/reportable")
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Exists || !merged.Merged || !merged.CleanupDue {
		t.Fatalf("merged workspace status wrong: %+v", merged)
	}
	missing, err := FeatureWorkspaceStatus(repo, "feat/never")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Exists {
		t.Fatalf("missing workspace reported as existing: %+v", missing)
	}
}
