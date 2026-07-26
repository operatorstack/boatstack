package boatstack

import (
	"os"
	"path/filepath"
	"testing"
)

// These are boundary-conformance tests for the post-merge reap sweep. Each test
// names the control law it proves (see AGENTS.md "Map tests to control laws"):
//
//	control-law: reap-removes-only-terminal-boatstack-workspaces
//	  Reap removes a worktree/branch only when it is Boatstack-owned AND confirmed
//	  merged or explicitly abandoned; never unmerged/open, never a non-Boatstack
//	  worktree, never the base or current worktree.
//	control-law: reap-never-discards-unlanded-work
//	  Reap never removes a worktree with uncommitted or unmerged work without Force.
//	control-law: reap-prompts-before-destroying-in-confirm-mode
//	  confirm returns NEEDS_CONFIRMATION without removing; auto removes; off blocks.
//	control-law: reap-preserves-per-worktree-delivery-isolation
//	  Reap enumerates via git worktree list and the merge oracle; reaping one
//	  worktree never disturbs an unrelated worktree or its per-worktree state.

// ghStateByBranch mocks `gh pr view <branch> --json state -q .state`, returning a
// per-branch state and defaulting unknown branches to OPEN so the merge oracle is
// deterministic (it never falls through to local ancestry, which would treat a
// commitless freshly-cut branch as merged).
func ghStateByBranch(states map[string]string) func(string, ...string) (string, error) {
	return func(_ string, arguments ...string) (string, error) {
		branch := ""
		for i := 0; i+1 < len(arguments); i++ {
			if arguments[i] == "view" {
				branch = arguments[i+1]
				break
			}
		}
		if state, ok := states[branch]; ok {
			return state, nil
		}
		return "OPEN", nil
	}
}

func cutWorktree(t *testing.T, repo, feature string) (branch, path string) {
	t.Helper()
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: feature})
	if err != nil || cut.VerificationStatus != "VERIFIED" {
		t.Fatalf("cut %q failed: %+v (%v)", feature, cut, err)
	}
	return cut.Branch, cut.WorktreePath
}

// commitInWorktree gives a worktree a real, unmerged commit so it is genuinely not
// an ancestor of the base branch.
func commitInWorktree(t *testing.T, path, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, name), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, path, "add", name)
	workspaceGitDo(t, path, "commit", "-m", "work in "+name)
}

// writeWorkspaceProjectConfig rewrites project.json with a workspace policy and an
// optional set of abandoned (ignored) feature slugs. It mirrors the config that
// workspaceRepo writes.
func writeWorkspaceProjectConfig(t *testing.T, repo string, ws Workspace, ignored ...string) {
	t.Helper()
	config := ProjectConfig{
		SchemaVersion: 1,
		Project:       Project{Name: "test", DefaultBranch: "main", Commands: map[string]string{"test": "go test ./..."}},
		Workflow:      Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true, IgnoredDeliveries: ignored},
		Workspace:     ws,
		Adapters:      []string{"cursor"},
	}
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func autoReapWorkspace() Workspace {
	ws := defaultWorkspace()
	ws.Reap = "auto"
	return ws
}

// control-law: reap-removes-only-terminal-boatstack-workspaces
// Positive + relation conformance: a sweep reclaims every merged worktree at once
// and keeps the still-open one.
func TestReapSweepsAllMergedWorktrees(t *testing.T) {
	repo := workspaceRepo(t, autoReapWorkspace())
	alphaBranch, alphaPath := cutWorktree(t, repo, "alpha")
	betaBranch, betaPath := cutWorktree(t, repo, "beta")
	gammaBranch, gammaPath := cutWorktree(t, repo, "gamma")
	commitInWorktree(t, gammaPath, "gamma.txt") // genuinely unmerged
	withWorkspaceGh(t, ghStateByBranch(map[string]string{
		alphaBranch: "MERGED", betaBranch: "MERGED", gammaBranch: "OPEN",
	}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || result.ReapedCount != 2 {
		t.Fatalf("expected 2 merged worktrees reaped: %+v", result)
	}
	for _, path := range []string{alphaPath, betaPath} {
		if dirExists(path) {
			t.Fatalf("merged worktree was not removed: %s", path)
		}
	}
	if branchExists(repo, alphaBranch) || branchExists(repo, betaBranch) {
		t.Fatal("merged branches were not deleted")
	}
	if !dirExists(gammaPath) || !branchExists(repo, gammaBranch) {
		t.Fatal("open worktree must be kept")
	}
}

// control-law: reap-removes-only-terminal-boatstack-workspaces
// Positive conformance: an explicitly abandoned (ignored) delivery is reclaimed
// even though its branch is unmerged, because the operator authorized disposal.
func TestReapReclaimsAbandonedIgnoredDelivery(t *testing.T) {
	ws := autoReapWorkspace()
	repo := workspaceRepo(t, ws)
	branch, path := cutWorktree(t, repo, "delta")
	commitInWorktree(t, path, "delta.txt") // unmerged commit
	writeWorkspaceProjectConfig(t, repo, ws, "delta")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "OPEN"}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReapedCount != 1 || dirExists(path) || branchExists(repo, branch) {
		t.Fatalf("abandoned delivery was not reclaimed: %+v", result)
	}
}

// control-law: reap-removes-only-terminal-boatstack-workspaces
// Negative + bypass conformance: a human-created worktree outside
// .product-loop/worktrees is never reaped, even when it is merged; a real
// Boatstack worktree alongside it still is, proving the sweep ran.
func TestReapSkipsNonBoatstackWorktree(t *testing.T) {
	repo := workspaceRepo(t, autoReapWorkspace())
	manualBranch := "feat/manual"
	manualPath := filepath.Join(repo, "manual-wt")
	workspaceGitDo(t, repo, "worktree", "add", "-b", manualBranch, manualPath)
	realBranch, realPath := cutWorktree(t, repo, "real")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{
		manualBranch: "MERGED", realBranch: "MERGED",
	}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !dirExists(manualPath) || !branchExists(repo, manualBranch) {
		t.Fatal("non-Boatstack worktree must never be reaped")
	}
	if dirExists(realPath) || branchExists(repo, realBranch) {
		t.Fatal("Boatstack worktree should have been reaped")
	}
	if result.ReapedCount != 1 {
		t.Fatalf("expected exactly one reaped worktree: %+v", result)
	}
}

// control-law: reap-removes-only-terminal-boatstack-workspaces
// Negative conformance: an open, unmerged, non-abandoned worktree is never in the
// reclaimable set — Force overrides discard gates, not terminality.
func TestReapNeverReapsOpenUnmergedEvenWithForce(t *testing.T) {
	repo := workspaceRepo(t, autoReapWorkspace())
	branch, path := cutWorktree(t, repo, "openpr")
	commitInWorktree(t, path, "openpr.txt")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "OPEN"}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo, Confirm: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReapedCount != 0 || !dirExists(path) || !branchExists(repo, branch) {
		t.Fatalf("open unmerged branch must never be reaped, even with force: %+v", result)
	}
}

// control-law: reap-never-discards-unlanded-work
// Negative + override conformance: a merged worktree with uncommitted changes is
// blocked without Force and reclaimed with it.
func TestReapRefusesDirtyMergedWorktreeWithoutForce(t *testing.T) {
	repo := workspaceRepo(t, autoReapWorkspace())
	branch, path := cutWorktree(t, repo, "dirtymerged")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "MERGED"}))
	if err := os.WriteFile(filepath.Join(path, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	blocked, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.ReapedCount != 0 || !dirExists(path) {
		t.Fatalf("dirty worktree must not be reaped without force: %+v", blocked)
	}
	sawBlocked := false
	for _, candidate := range blocked.Candidates {
		if candidate.Branch == branch && candidate.Action == "blocked" {
			sawBlocked = true
		}
	}
	if !sawBlocked {
		t.Fatalf("dirty worktree should surface as a blocked candidate: %+v", blocked)
	}

	forced, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if forced.ReapedCount != 1 || dirExists(path) {
		t.Fatalf("force must reclaim the dirty merged worktree: %+v", forced)
	}
}

// control-law: reap-prompts-before-destroying-in-confirm-mode
// Positive conformance: confirm mode reports the reclaimable set without removing
// anything, then reclaims after the confirmation.
func TestReapNeedsConfirmationInConfirmMode(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace()) // reap defaults to "confirm"
	branch, path := cutWorktree(t, repo, "confirmme")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "MERGED"}))

	pending, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if pending.VerificationStatus != "NEEDS_CONFIRMATION" || pending.ReclaimableCount != 1 || !dirExists(path) {
		t.Fatalf("confirm mode must prompt without removing: %+v", pending)
	}

	done, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if done.VerificationStatus != "VERIFIED" || done.ReapedCount != 1 || dirExists(path) {
		t.Fatalf("confirmed reap must remove the workspace: %+v", done)
	}
}

// control-law: reap-prompts-before-destroying-in-confirm-mode
// Negative conformance: reap=off blocks the sweep and removes nothing.
func TestReapDisabledWhenReapOff(t *testing.T) {
	ws := defaultWorkspace()
	ws.Reap = "off"
	repo := workspaceRepo(t, ws)
	branch, path := cutWorktree(t, repo, "keepme")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "MERGED"}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" || !dirExists(path) || !branchExists(repo, branch) {
		t.Fatalf("reap=off must block and remove nothing: %+v", result)
	}
}

// control-law: reap-removes-only-terminal-boatstack-workspaces
// Failure-state conformance: reaping from inside a worktree never removes the
// worktree it is standing in; it is reported as skipped instead.
func TestReapSkipsCurrentWorktree(t *testing.T) {
	ws := autoReapWorkspace()
	repo := workspaceRepo(t, ws)
	// Commit the installation so the linked worktree carries project.json.
	workspaceGitDo(t, repo, "add", ".product-loop")
	workspaceGitDo(t, repo, "commit", "-m", "install boatstack")
	branch, path := cutWorktree(t, repo, "current")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{branch: "MERGED"}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: path})
	if err != nil {
		t.Fatal(err)
	}
	if !dirExists(path) {
		t.Fatal("reap must never remove the current worktree")
	}
	if result.ReapedCount != 0 {
		t.Fatalf("current worktree must not be reaped: %+v", result)
	}
	skipped := false
	for _, candidate := range result.Candidates {
		if candidate.Branch == branch && candidate.Action == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("current worktree should be a skipped candidate: %+v", result)
	}
}

// control-law: reap-preserves-per-worktree-delivery-isolation
// Relation conformance: reaping one merged worktree leaves an unrelated open
// worktree — and thus its per-worktree runtime/delivery state under its own git
// dir — fully intact and still registered.
func TestReapPreservesUnrelatedWorktree(t *testing.T) {
	repo := workspaceRepo(t, autoReapWorkspace())
	mergedBranch, mergedPath := cutWorktree(t, repo, "landed")
	openBranch, openPath := cutWorktree(t, repo, "inflight")
	commitInWorktree(t, openPath, "inflight.txt")
	withWorkspaceGh(t, ghStateByBranch(map[string]string{
		mergedBranch: "MERGED", openBranch: "OPEN",
	}))

	result, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReapedCount != 1 || dirExists(mergedPath) {
		t.Fatalf("the merged worktree should have been reaped: %+v", result)
	}
	if !dirExists(openPath) || !branchExists(repo, openBranch) {
		t.Fatal("reap must not disturb an unrelated worktree")
	}
	if worktreePathForBranch(repo, openBranch) == "" {
		t.Fatal("the unrelated worktree must remain a registered linked worktree")
	}
}
