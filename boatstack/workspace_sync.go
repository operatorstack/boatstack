package boatstack

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const workspaceSyncSchemaVersion = 1

var workspaceSyncNow = time.Now

// WorkspaceSyncOptions identifies one local branch and one remote branch that
// should become identical. Empty Branch means the branch checked out in Repo.
type WorkspaceSyncOptions struct {
	Repo   string
	Branch string
	Source string
}

// WorkspaceSync reports the verified result and the Git refs that retain the
// pre-sync branch and worktree state.
type WorkspaceSync struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"` // NO_CHANGE | SYNCED | BLOCKED
	Branch        string `json:"branch,omitempty"`
	Source        string `json:"source,omitempty"`
	WorktreePath  string `json:"worktree_path,omitempty"`
	OldCommit     string `json:"old_commit,omitempty"`
	NewCommit     string `json:"new_commit,omitempty"`
	RecoveryRef   string `json:"recovery_ref,omitempty"`
	CheckpointRef string `json:"checkpoint_ref,omitempty"`
	Reason        string `json:"reason"`
}

func blockedWorkspaceSync(result WorkspaceSync, reason string) WorkspaceSync {
	result.SchemaVersion = workspaceSyncSchemaVersion
	result.Status = "BLOCKED"
	result.Reason = reason
	return result
}

func normalizeRemoteSource(repo, source string) (string, string, string, error) {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "refs/remotes/")
	parts := strings.SplitN(source, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("source must name a remote branch such as origin/main")
	}
	remote, branch := parts[0], parts[1]
	ref := "refs/remotes/" + remote + "/" + branch
	if _, err := workspaceGit(repo, "check-ref-format", ref); err != nil {
		return "", "", "", fmt.Errorf("source %q is not a valid remote branch", source)
	}
	if _, err := workspaceGit(repo, "remote", "get-url", remote); err != nil {
		return "", "", "", fmt.Errorf("remote %q does not exist", remote)
	}
	return remote, branch, ref, nil
}

func activeDeliveryOwningBranch(repo, branch string) (string, error) {
	paths := []string{repo}
	if output, err := workspaceGit(repo, "worktree", "list", "--porcelain"); err == nil {
		paths = paths[:0]
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "worktree ") {
				paths = append(paths, strings.TrimSpace(strings.TrimPrefix(line, "worktree ")))
			}
		}
	}
	seen := map[string]bool{}
	for _, path := range paths {
		active, err := ActiveManagedDeliveries(path)
		if err != nil {
			return "", err
		}
		for _, feature := range active {
			key := path + "\x00" + feature
			if seen[key] {
				continue
			}
			seen[key] = true
			state, loadErr := LoadDeliveryState(path, feature)
			if loadErr != nil {
				return "", loadErr
			}
			if stateMatchesBranch(state, branch) {
				return feature, nil
			}
		}
	}
	return "", nil
}

func syncRecoveryRefs(branch, oldCommit string) (string, string) {
	now := workspaceSyncNow().UTC()
	fingerprint := SHA256Bytes([]byte(branch + "\x00" + oldCommit + "\x00" + now.Format(time.RFC3339Nano)))
	base := "refs/boatstack/recovery/workspace-sync/" + now.Format("20060102T150405Z") + "-" + fingerprint[:12]
	return base + "/head", base + "/worktree"
}

func rollbackWorkspaceCheckpoint(worktreePath, recoveryRef string, checkpointCreated bool) {
	if checkpointCreated {
		_, _ = workspaceGit(worktreePath, "stash", "pop", "--index")
	}
	if recoveryRef != "" {
		_, _ = workspaceGit(worktreePath, "update-ref", "-d", recoveryRef)
	}
}

// SyncWorkspace aligns one local branch with a freshly fetched remote branch.
// It creates recovery refs before changing the branch or its owning worktree.
func SyncWorkspace(options WorkspaceSyncOptions) (WorkspaceSync, error) {
	result := WorkspaceSync{SchemaVersion: workspaceSyncSchemaVersion}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return blockedWorkspaceSync(result, err.Error()), nil
	}

	branch := strings.TrimSpace(options.Branch)
	if branch == "" {
		branch, _ = workspaceGit(repo, "branch", "--show-current")
		branch = strings.TrimSpace(branch)
	}
	if branch == "" {
		return blockedWorkspaceSync(result, "A named branch is required when the current worktree is detached."), nil
	}
	result.Branch = branch
	if _, err := workspaceGit(repo, "check-ref-format", "refs/heads/"+branch); err != nil {
		return blockedWorkspaceSync(result, fmt.Sprintf("Branch %q is not a valid local branch.", branch)), nil
	}

	remote, remoteBranch, sourceRef, err := normalizeRemoteSource(repo, options.Source)
	if err != nil {
		return blockedWorkspaceSync(result, err.Error()), nil
	}
	result.Source = strings.TrimPrefix(sourceRef, "refs/remotes/")

	if owner, ownerErr := activeDeliveryOwningBranch(repo, branch); ownerErr != nil {
		return blockedWorkspaceSync(result, "Boatstack could not verify managed delivery ownership: "+ownerErr.Error()), nil
	} else if owner != "" {
		return blockedWorkspaceSync(result, fmt.Sprintf("Branch %q belongs to active managed delivery %q; use repair instead.", branch, owner)), nil
	}

	localRef := "refs/heads/" + branch
	oldCommit, err := workspaceGit(repo, "rev-parse", "--verify", localRef+"^{commit}")
	if err != nil {
		return blockedWorkspaceSync(result, fmt.Sprintf("Local branch %q does not exist.", branch)), nil
	}
	result.OldCommit = strings.TrimSpace(oldCommit)

	if _, err := workspaceGit(repo, "fetch", remote, "+refs/heads/"+remoteBranch+":"+sourceRef); err != nil {
		return blockedWorkspaceSync(result, "Could not fetch the requested source: "+err.Error()), nil
	}
	newCommit, err := workspaceGit(repo, "rev-parse", "--verify", sourceRef+"^{commit}")
	if err != nil {
		return blockedWorkspaceSync(result, fmt.Sprintf("Fetched source %q does not resolve to a commit.", result.Source)), nil
	}
	result.NewCommit = strings.TrimSpace(newCommit)

	worktreePath := worktreePathForBranch(repo, branch)
	if worktreePath != "" {
		absolute, absErr := filepath.Abs(worktreePath)
		if absErr != nil {
			return blockedWorkspaceSync(result, "Could not resolve the branch's owning worktree."), nil
		}
		result.WorktreePath = filepath.Clean(absolute)
	}
	dirty := ""
	if result.WorktreePath != "" {
		dirty, _ = workspaceGit(result.WorktreePath, "status", "--porcelain=v1", "--untracked-files=all")
	}
	if result.OldCommit == result.NewCommit && strings.TrimSpace(dirty) == "" {
		result.Status = "NO_CHANGE"
		result.Reason = fmt.Sprintf("Branch %q already matches %s and its worktree is clean.", branch, result.Source)
		return result, nil
	}

	recoveryRef, checkpointRef := syncRecoveryRefs(branch, result.OldCommit)
	if _, err := workspaceGit(repo, "update-ref", recoveryRef, result.OldCommit); err != nil {
		return blockedWorkspaceSync(result, "Could not create the branch recovery reference: "+err.Error()), nil
	}
	result.RecoveryRef = recoveryRef
	if verified, verifyErr := workspaceGit(repo, "rev-parse", "--verify", recoveryRef+"^{commit}"); verifyErr != nil || strings.TrimSpace(verified) != result.OldCommit {
		_, _ = workspaceGit(repo, "update-ref", "-d", recoveryRef)
		result.RecoveryRef = ""
		return blockedWorkspaceSync(result, "Could not verify the branch recovery reference."), nil
	}

	checkpointCreated := false
	if strings.TrimSpace(dirty) != "" {
		beforeStash, _ := workspaceGit(result.WorktreePath, "rev-parse", "--verify", "refs/stash^{commit}")
		label := fmt.Sprintf("Boatstack workspace-sync %s from %s", branch, result.OldCommit)
		if _, err := workspaceGit(result.WorktreePath, "stash", "push", "--include-untracked", "--message", label); err != nil {
			rollbackWorkspaceCheckpoint(result.WorktreePath, recoveryRef, false)
			result.RecoveryRef = ""
			return blockedWorkspaceSync(result, "Could not checkpoint the dirty worktree: "+err.Error()), nil
		}
		stashCommit, stashErr := workspaceGit(result.WorktreePath, "rev-parse", "--verify", "refs/stash^{commit}")
		checkpointCreated = stashErr == nil && strings.TrimSpace(stashCommit) != "" && strings.TrimSpace(stashCommit) != strings.TrimSpace(beforeStash)
		if !checkpointCreated {
			rollbackWorkspaceCheckpoint(result.WorktreePath, recoveryRef, false)
			result.RecoveryRef = ""
			return blockedWorkspaceSync(result, "Could not verify the dirty-worktree checkpoint."), nil
		}
		if _, err := workspaceGit(repo, "update-ref", checkpointRef, strings.TrimSpace(stashCommit)); err != nil {
			rollbackWorkspaceCheckpoint(result.WorktreePath, recoveryRef, true)
			result.RecoveryRef = ""
			return blockedWorkspaceSync(result, "Could not preserve the dirty-worktree checkpoint: "+err.Error()), nil
		}
		result.CheckpointRef = checkpointRef
		if verified, verifyErr := workspaceGit(repo, "rev-parse", "--verify", checkpointRef+"^{commit}"); verifyErr != nil || strings.TrimSpace(verified) != strings.TrimSpace(stashCommit) {
			_, _ = workspaceGit(repo, "update-ref", "-d", checkpointRef)
			result.CheckpointRef = ""
			rollbackWorkspaceCheckpoint(result.WorktreePath, recoveryRef, true)
			result.RecoveryRef = ""
			return blockedWorkspaceSync(result, "Could not verify the dirty-worktree recovery reference."), nil
		}
	}

	if result.WorktreePath != "" {
		if _, err := workspaceGit(result.WorktreePath, "reset", "--hard", result.NewCommit); err != nil {
			return blockedWorkspaceSync(result, "The recovery checkpoint was preserved, but branch alignment failed: "+err.Error()), nil
		}
	} else if _, err := workspaceGit(repo, "update-ref", localRef, result.NewCommit, result.OldCommit); err != nil {
		return blockedWorkspaceSync(result, "The recovery reference was preserved, but branch alignment failed: "+err.Error()), nil
	}

	actual, actualErr := workspaceGit(repo, "rev-parse", "--verify", localRef+"^{commit}")
	if actualErr != nil || strings.TrimSpace(actual) != result.NewCommit {
		return blockedWorkspaceSync(result, "Branch alignment could not be verified; recovery references were preserved."), nil
	}
	if result.WorktreePath != "" {
		status, statusErr := workspaceGit(result.WorktreePath, "status", "--porcelain=v1", "--untracked-files=all")
		if statusErr != nil || strings.TrimSpace(status) != "" {
			return blockedWorkspaceSync(result, "The target worktree is not clean after alignment; recovery references were preserved."), nil
		}
	}
	if checkpointCreated {
		if _, err := workspaceGit(repo, "rev-parse", "--verify", result.CheckpointRef+"^{commit}"); err != nil {
			return blockedWorkspaceSync(result, "The worktree checkpoint became unreadable after alignment."), nil
		}
	}

	result.Status = "SYNCED"
	result.Reason = fmt.Sprintf("Branch %q now matches %s; prior state is retained under %s.", branch, result.Source, result.RecoveryRef)
	return result, nil
}
