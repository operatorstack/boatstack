package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const workspaceSchemaVersion = 1

// workspaceGit and workspaceGh are indirected so tests can substitute
// deterministic git and GitHub CLI behavior. They default to the same helpers
// the rest of the package uses.
var (
	workspaceGit = gitCommand
	workspaceGh  = func(repo string, arguments ...string) (string, error) {
		return commandOutput(repo, "gh", arguments...)
	}
)

// ResolvedWorkspace is the workspace policy with empty fields filled from the
// documented defaults. Enabled is never defaulted: a config without a workspace
// block, or with enabled=false, keeps Boatstack's prior hands-off behavior.
type ResolvedWorkspace struct {
	Enabled      bool
	Mode         string
	Cleanup      string
	CleanupAfter string
}

func resolveWorkspace(workspace Workspace) ResolvedWorkspace {
	resolved := ResolvedWorkspace{
		Enabled:      workspace.Enabled,
		Mode:         workspace.Mode,
		Cleanup:      workspace.Cleanup,
		CleanupAfter: workspace.CleanupAfter,
	}
	if resolved.Mode == "" {
		resolved.Mode = "worktree"
	}
	if resolved.Cleanup == "" {
		resolved.Cleanup = "confirm"
	}
	if resolved.CleanupAfter == "" {
		resolved.CleanupAfter = "merge"
	}
	return resolved
}

// workspaceEnabled reports whether workspace management is on, swallowing config
// errors as "off" so read-only callers never fail on a malformed project file.
func workspaceEnabled(repo string) bool {
	policy, err := loadWorkspacePolicy(repo)
	if err != nil {
		return false
	}
	return policy.Enabled
}

// needsFreshCut reports whether an approved feature still has to be moved off the
// base branch onto its own fresh workspace. It is a local-only check: true only
// when the feature has no existing branch or worktree and the working tree is
// still on the default branch.
func needsFreshCut(repo, feature string) bool {
	branch := branchForFeature(feature)
	if branch == "" {
		return false
	}
	if branchExists(repo, branch) || worktreePathForBranch(repo, branch) != "" {
		return false
	}
	current, _ := workspaceGit(repo, "branch", "--show-current")
	return strings.TrimSpace(current) == defaultPRBase(repo)
}

func loadWorkspacePolicy(repo string) (ResolvedWorkspace, error) {
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		return ResolvedWorkspace{}, err
	}
	return resolveWorkspace(config.Workspace), nil
}

// branchForFeature derives the branch name for a feature slug when the caller
// does not supply an explicit branch.
func branchForFeature(feature string) string {
	slug := previewSlug(feature)
	if slug == "" {
		return ""
	}
	return "feat/" + slug
}

// WorkspaceCutOptions requests a fresh per-feature workspace cut from the
// up-to-date default branch.
type WorkspaceCutOptions struct {
	Repo    string
	Feature string
	Branch  string
}

// WorkspaceCut is the deterministic result of a fresh-cut request.
type WorkspaceCut struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"`
	Mode               string `json:"mode,omitempty"`
	BaseBranch         string `json:"base_branch,omitempty"`
	Branch             string `json:"branch,omitempty"`
	WorktreePath       string `json:"worktree_path,omitempty"`
	Created            bool   `json:"created"`
	Reason             string `json:"reason"`
}

func blockedCut(reason string) WorkspaceCut {
	return WorkspaceCut{SchemaVersion: workspaceSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

// CutFeatureWorkspace creates a fresh branch (and, in worktree mode, a linked
// worktree) rooted at the freshly-fetched default branch. It never switches an
// existing branch's history, never deletes anything, and refuses to reuse a
// branch name that already exists.
func CutFeatureWorkspace(options WorkspaceCutOptions) (WorkspaceCut, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return blockedCut(err.Error()), nil
	}
	if !fileExists(filepath.Join(repo, ".product-loop", "project.json")) {
		return blockedCut("This repository has no Boatstack project installation."), nil
	}
	policy, err := loadWorkspacePolicy(repo)
	if err != nil {
		return blockedCut("Boatstack could not read the workspace policy: " + err.Error()), nil
	}
	if !policy.Enabled {
		return blockedCut("Workspace management is disabled (workspace.enabled=false)."), nil
	}

	branch := strings.TrimSpace(options.Branch)
	if branch == "" {
		branch = branchForFeature(options.Feature)
	}
	if branch == "" {
		return blockedCut("A feature slug or explicit branch is required to cut a workspace."), nil
	}

	base := defaultPRBase(repo)
	if branch == base {
		return blockedCut(fmt.Sprintf("Refusing to cut a workspace named after the base branch %q.", base)), nil
	}

	// Freshen the base from origin when a remote is available; a local-only
	// repository is still cuttable from its local base.
	if _, originErr := workspaceGit(repo, "remote", "get-url", "origin"); originErr == nil {
		if _, fetchErr := workspaceGit(repo, "fetch", "origin"); fetchErr != nil {
			return blockedCut("Boatstack could not fetch origin before cutting: " + fetchErr.Error()), nil
		}
	}
	baseCommit, err := resolveBaseCommit(repo, base)
	if err != nil {
		return blockedCut(err.Error()), nil
	}

	if _, existsErr := workspaceGit(repo, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"); existsErr == nil {
		return blockedCut(fmt.Sprintf("Branch %q already exists; choose a new feature or clean up the old workspace first.", branch)), nil
	}

	result := WorkspaceCut{
		SchemaVersion:      workspaceSchemaVersion,
		VerificationStatus: "VERIFIED",
		Mode:               policy.Mode,
		BaseBranch:         base,
		Branch:             branch,
		Created:            true,
	}

	switch policy.Mode {
	case "branch":
		if _, err := workspaceGit(repo, "switch", "-c", branch, baseCommit); err != nil {
			return blockedCut("Boatstack could not create the branch: " + err.Error()), nil
		}
		result.Reason = fmt.Sprintf("Cut fresh branch %q from %s.", branch, base)
	default: // "worktree"
		path := filepath.Join(repo, ".product-loop", "worktrees", previewSlug(branch))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return blockedCut("Boatstack could not prepare the worktree directory: " + err.Error()), nil
		}
		if _, err := workspaceGit(repo, "worktree", "add", "-b", branch, path, baseCommit); err != nil {
			return blockedCut("Boatstack could not add the worktree: " + err.Error()), nil
		}
		result.WorktreePath = path
		result.Reason = fmt.Sprintf("Cut fresh worktree for branch %q from %s at %s.", branch, base, path)
	}
	return result, nil
}

// workspaceMergeStatus reports whether the branch's work has landed. It prefers
// the GitHub CLI's authoritative PR state and falls back to local ancestry when
// gh is unavailable, always reporting which source answered.
func workspaceMergeStatus(repo, branch, base string) (bool, string) {
	if out, err := workspaceGh(repo, "pr", "view", branch, "--json", "state", "-q", ".state"); err == nil {
		return strings.EqualFold(strings.TrimSpace(out), "MERGED"), "gh"
	}
	for _, target := range []string{"refs/remotes/origin/" + base, "refs/heads/" + base, base} {
		if _, err := workspaceGit(repo, "merge-base", "--is-ancestor", "refs/heads/"+branch, target); err == nil {
			return true, "git"
		}
	}
	return false, "git"
}

// worktreePathForBranch returns the linked worktree path checked out on branch,
// or "" when the branch is not checked out in a separate worktree.
func worktreePathForBranch(repo, branch string) string {
	out, err := workspaceGit(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	current := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			if strings.TrimSpace(strings.TrimPrefix(line, "branch ")) == "refs/heads/"+branch {
				return current
			}
		}
	}
	return ""
}

func branchExists(repo, branch string) bool {
	_, err := workspaceGit(repo, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	return err == nil
}

// WorkspaceCleanupOptions requests removal of a finished per-feature workspace.
type WorkspaceCleanupOptions struct {
	Repo    string
	Branch  string
	Confirm bool // the human supplied the cleanup confirmation
	Force   bool // override the merge gate and discard uncommitted/unmerged work
}

// WorkspaceCleanup is the deterministic result of a cleanup request.
type WorkspaceCleanup struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"` // VERIFIED | BLOCKED | NEEDS_CONFIRMATION
	Branch             string `json:"branch,omitempty"`
	Mode               string `json:"mode,omitempty"`
	Merged             bool   `json:"merged"`
	MergeSource        string `json:"merge_source,omitempty"`
	WorktreeRemoved    bool   `json:"worktree_removed"`
	BranchDeleted      bool   `json:"branch_deleted"`
	Reason             string `json:"reason"`
}

func blockedCleanup(branch, reason string) WorkspaceCleanup {
	return WorkspaceCleanup{SchemaVersion: workspaceSchemaVersion, VerificationStatus: "BLOCKED", Branch: branch, Reason: reason}
}

// CleanupFeatureWorkspace removes a finished workspace only when it is safe: the
// PR must be merged (unless cleanup_after is "ship" or Force overrides), there
// must be no uncommitted or unmerged work (unless Force), and confirm-mode must
// receive the human confirmation before anything is deleted.
func CleanupFeatureWorkspace(options WorkspaceCleanupOptions) (WorkspaceCleanup, error) {
	branch := strings.TrimSpace(options.Branch)
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return blockedCleanup(branch, err.Error()), nil
	}
	if branch == "" {
		return blockedCleanup(branch, "A branch is required to clean up a workspace."), nil
	}
	if !fileExists(filepath.Join(repo, ".product-loop", "project.json")) {
		return blockedCleanup(branch, "This repository has no Boatstack project installation."), nil
	}
	policy, err := loadWorkspacePolicy(repo)
	if err != nil {
		return blockedCleanup(branch, "Boatstack could not read the workspace policy: "+err.Error()), nil
	}
	if policy.Cleanup == "off" && !options.Force {
		return blockedCleanup(branch, "Workspace cleanup is disabled (workspace.cleanup=off)."), nil
	}

	worktreePath := worktreePathForBranch(repo, branch)
	if !branchExists(repo, branch) && worktreePath == "" {
		return WorkspaceCleanup{
			SchemaVersion: workspaceSchemaVersion, VerificationStatus: "VERIFIED", Branch: branch,
			Mode: policy.Mode, Reason: fmt.Sprintf("No workspace for branch %q; nothing to clean up.", branch),
		}, nil
	}

	base := defaultPRBase(repo)
	if branch == base {
		return blockedCleanup(branch, fmt.Sprintf("Refusing to clean up the base branch %q.", base)), nil
	}
	if current, _ := workspaceGit(repo, "branch", "--show-current"); strings.TrimSpace(current) == branch && worktreePath == "" {
		return blockedCleanup(branch, fmt.Sprintf("Branch %q is the current branch; switch away before cleaning it up.", branch)), nil
	}

	merged, source := workspaceMergeStatus(repo, branch, base)
	result := WorkspaceCleanup{
		SchemaVersion: workspaceSchemaVersion, Branch: branch, Mode: policy.Mode,
		Merged: merged, MergeSource: source,
	}

	if policy.CleanupAfter == "merge" && !merged && !options.Force {
		result.VerificationStatus = "BLOCKED"
		result.Reason = fmt.Sprintf("PR for %q is not merged yet; keeping the workspace. Re-run with force to clean up early.", branch)
		return result, nil
	}

	// Refuse to discard work the user has not landed unless explicitly forced.
	if !options.Force {
		if worktreePath != "" {
			if dirty, _ := workspaceGit(worktreePath, "status", "--porcelain"); strings.TrimSpace(dirty) != "" {
				result.VerificationStatus = "BLOCKED"
				result.Reason = fmt.Sprintf("Workspace %q has uncommitted changes; commit or discard them, or force cleanup.", branch)
				return result, nil
			}
		}
		if !merged {
			for _, target := range []string{"refs/remotes/origin/" + base, "refs/heads/" + base, base} {
				if _, err := workspaceGit(repo, "merge-base", "--is-ancestor", "refs/heads/"+branch, target); err == nil {
					merged = true
					break
				}
			}
			if !merged && policy.CleanupAfter != "ship" {
				result.VerificationStatus = "BLOCKED"
				result.Reason = fmt.Sprintf("Branch %q has commits not merged into %s; force cleanup to discard them.", branch, base)
				return result, nil
			}
		}
	}
	result.Merged = merged

	if policy.Cleanup == "confirm" && !options.Confirm && !options.Force {
		result.VerificationStatus = "NEEDS_CONFIRMATION"
		result.Reason = fmt.Sprintf("Ready to remove the workspace for %q. Confirm cleanup to proceed.", branch)
		return result, nil
	}

	if worktreePath != "" {
		removeArgs := []string{"worktree", "remove", worktreePath}
		if options.Force {
			removeArgs = append(removeArgs, "--force")
		}
		if _, err := workspaceGit(repo, removeArgs...); err != nil {
			return blockedCleanup(branch, "Boatstack could not remove the worktree: "+err.Error()), nil
		}
		result.WorktreeRemoved = true
	}
	if branchExists(repo, branch) {
		// Once the merge/safety gates above have cleared, force-delete so a
		// squash- or rebase-merged PR (whose local ref is not a local ancestor
		// of the base) is still removable.
		deleteFlag := "-d"
		if options.Force || result.Merged {
			deleteFlag = "-D"
		}
		if _, err := workspaceGit(repo, "branch", deleteFlag, branch); err != nil {
			return blockedCleanup(branch, "Boatstack could not delete the branch: "+err.Error()), nil
		}
		result.BranchDeleted = true
	}
	result.VerificationStatus = "VERIFIED"
	result.Reason = fmt.Sprintf("Cleaned up the workspace for %q.", branch)
	return result, nil
}

// WorkspaceStatus reports whether a branch's workspace still exists and whether
// it is safe to clean up, so the flow can surface cleanup without forcing it.
type WorkspaceStatus struct {
	SchemaVersion int    `json:"schema_version"`
	Branch        string `json:"branch,omitempty"`
	Exists        bool   `json:"exists"`
	WorktreePath  string `json:"worktree_path,omitempty"`
	Merged        bool   `json:"merged"`
	MergeSource   string `json:"merge_source,omitempty"`
	CleanupDue    bool   `json:"cleanup_due"`
	Reason        string `json:"reason"`
}

// FeatureWorkspaceStatus inspects one branch's workspace. It is read-only.
func FeatureWorkspaceStatus(repoPath, branch string) (WorkspaceStatus, error) {
	branch = strings.TrimSpace(branch)
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return WorkspaceStatus{}, err
	}
	if branch == "" {
		return WorkspaceStatus{}, fmt.Errorf("workspace status requires a branch")
	}
	status := WorkspaceStatus{SchemaVersion: workspaceSchemaVersion, Branch: branch}
	worktreePath := worktreePathForBranch(repo, branch)
	status.WorktreePath = worktreePath
	status.Exists = worktreePath != "" || branchExists(repo, branch)
	if !status.Exists {
		status.Reason = fmt.Sprintf("No workspace exists for branch %q.", branch)
		return status, nil
	}
	base := defaultPRBase(repo)
	status.Merged, status.MergeSource = workspaceMergeStatus(repo, branch, base)
	policy, policyErr := loadWorkspacePolicy(repo)
	requireMerged := true
	if policyErr == nil {
		requireMerged = policy.CleanupAfter != "ship"
	}
	status.CleanupDue = status.Merged || !requireMerged
	if status.CleanupDue {
		status.Reason = fmt.Sprintf("Workspace for %q is ready to clean up.", branch)
	} else {
		status.Reason = fmt.Sprintf("Workspace for %q is still open (PR not merged).", branch)
	}
	return status, nil
}
