package boatstack

import (
	"os"
	"path/filepath"
	"strings"
)

// EngagementMode is the single authority projection used by host hooks and
// command adapters. Repository presence and historical workflow evidence are
// deliberately absent from this vocabulary.
type EngagementMode string

const (
	EngagementDormant EngagementMode = "DORMANT"
	EngagementCommand EngagementMode = "COMMAND"
	EngagementActive  EngagementMode = "ACTIVE"
)

const engagementLeaseSchemaVersion = 1

// EngagementRequest declares whether the current operation is an explicit
// Boatstack command. Command engagement is ephemeral and is never persisted.
type EngagementRequest struct {
	ExplicitCommand bool
}

// EngagementStatus is the canonical answer to whether Boatstack owns the
// current operation. ACTIVE is valid only when the worktree-local lease agrees
// with valid delivery state and the current branch.
type EngagementStatus struct {
	SchemaVersion int            `json:"schema_version"`
	Mode          EngagementMode `json:"mode"`
	RepoRoot      string         `json:"repo_root,omitempty"`
	WorktreeID    string         `json:"worktree_id,omitempty"`
	Branch        string         `json:"branch,omitempty"`
	Feature       string         `json:"feature,omitempty"`
	Slice         string         `json:"slice,omitempty"`
	PlanLockHash  string         `json:"plan_lock_sha256,omitempty"`
	Reason        string         `json:"reason"`
}

type engagementLease struct {
	SchemaVersion int    `json:"schema_version"`
	RepoRoot      string `json:"repo_root"`
	GitDir        string `json:"git_dir"`
	WorktreeID    string `json:"worktree_id,omitempty"`
	Branch        string `json:"branch"`
	Feature       string `json:"feature"`
	Slice         string `json:"slice"`
	PlanLockHash  string `json:"plan_lock_sha256"`
}

// engagementLeasePath is intentionally rooted in the per-worktree Git dir for
// both embedded and detached installations. Generated hook shims can therefore
// prove that no engagement exists before they load or hydrate any runtime.
func engagementLeasePath(repo string) (string, error) {
	gitDir, err := worktreeGitDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, controlDirName, "engagement.json"), nil
}

func dormantEngagement(reason string) EngagementStatus {
	return EngagementStatus{SchemaVersion: engagementLeaseSchemaVersion, Mode: EngagementDormant, Reason: reason}
}

// ResolveEngagement is the sole authority resolver for ambient Boatstack
// behavior. Invalid, stale, terminal, cross-branch, and cross-worktree evidence
// is inert here; explicit Boatstack commands validate and diagnose that evidence
// through their own fail-closed boundaries.
func ResolveEngagement(repoPath string, request EngagementRequest) EngagementStatus {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return dormantEngagement("repository identity is unavailable")
	}
	workspace := WorkspaceFor(repo)
	if request.ExplicitCommand {
		return EngagementStatus{
			SchemaVersion: engagementLeaseSchemaVersion, Mode: EngagementCommand,
			RepoRoot: repo, WorktreeID: workspace.WorktreeID,
			Branch: strings.TrimSpace(gitOutput(repo, "branch", "--show-current")),
			Reason: "an explicit Boatstack command owns only this operation",
		}
	}
	path, err := engagementLeasePath(repo)
	if err != nil {
		return dormantEngagement("no worktree engagement lease exists")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return dormantEngagement("no valid worktree engagement lease exists")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return dormantEngagement("the worktree engagement lease is unreadable")
	}
	var lease engagementLease
	if err := DecodeJSON("load engagement lease", path, value, &lease); err != nil ||
		lease.SchemaVersion != engagementLeaseSchemaVersion ||
		!featureSlugPattern.MatchString(lease.Feature) ||
		!featureSlugPattern.MatchString(lease.Slice) || strings.TrimSpace(lease.PlanLockHash) == "" {
		return dormantEngagement("the worktree engagement lease is invalid")
	}
	gitDir, err := worktreeGitDir(repo)
	if err != nil || canonicalizeExistingAncestor(lease.RepoRoot) != canonicalizeExistingAncestor(repo) ||
		canonicalizeExistingAncestor(lease.GitDir) != canonicalizeExistingAncestor(gitDir) ||
		lease.WorktreeID != workspace.WorktreeID {
		return dormantEngagement("the engagement lease belongs to another worktree")
	}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	if branch == "" || lease.Branch != branch {
		return dormantEngagement("the engagement lease belongs to another branch")
	}
	state, err := LoadDeliveryState(repo, lease.Feature)
	if err != nil || state.PlanLockHash != lease.PlanLockHash || state.ActiveIndex >= len(state.Slices) {
		return dormantEngagement("the engagement lease has no current active delivery")
	}
	if err := checkDeliveryPlanLock(repo, lease.Feature, state); err != nil {
		return dormantEngagement("the engagement lease has no valid plan lock")
	}
	slice := state.Slices[state.ActiveIndex]
	if slice.ID != lease.Slice || slice.Status == StatusPublished || !stateMatchesBranch(state, branch) {
		return dormantEngagement("the engagement lease does not match the current delivery")
	}
	return EngagementStatus{
		SchemaVersion: engagementLeaseSchemaVersion, Mode: EngagementActive,
		RepoRoot: repo, WorktreeID: workspace.WorktreeID, Branch: branch,
		Feature: lease.Feature, Slice: lease.Slice, PlanLockHash: lease.PlanLockHash,
		Reason: "a verified delivery is active in this worktree and branch",
	}
}

func engagementLeaseForState(repo string, state DeliveryState) (engagementLease, bool, error) {
	if state.ActiveIndex < 0 || state.ActiveIndex >= len(state.Slices) {
		return engagementLease{}, false, nil
	}
	slice := state.Slices[state.ActiveIndex]
	if slice.Status == StatusPublished {
		return engagementLease{}, false, nil
	}
	root, err := ResolveRepository(repo)
	if err != nil {
		return engagementLease{}, false, err
	}
	branch := strings.TrimSpace(gitOutput(root, "branch", "--show-current"))
	if branch == "" || !stateMatchesBranch(state, branch) {
		return engagementLease{}, false, nil
	}
	gitDir, err := worktreeGitDir(root)
	if err != nil {
		return engagementLease{}, false, err
	}
	return engagementLease{
		SchemaVersion: engagementLeaseSchemaVersion, RepoRoot: root, GitDir: gitDir,
		WorktreeID: WorkspaceFor(root).WorktreeID, Branch: branch,
		Feature: state.Feature, Slice: slice.ID, PlanLockHash: state.PlanLockHash,
	}, true, nil
}

// syncEngagementLease makes activation and publication the only durable lease
// transitions. Historical delivery files remain intact after release.
func syncEngagementLease(repo string, state DeliveryState) error {
	path, err := engagementLeasePath(repo)
	if err != nil {
		return err
	}
	lease, active, err := engagementLeaseForState(repo, state)
	if err != nil {
		return err
	}
	if !active {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	value, err := MarshalJSON(lease)
	if err != nil {
		return err
	}
	return atomicWriteMode(path, value, 0o600)
}

func clearEngagementLease(repo string) error {
	path, err := engagementLeasePath(repo)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
