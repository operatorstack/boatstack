package boatstack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const workspaceSchemaVersion = 2

// workspaceGit and workspaceGh are indirected so tests can substitute
// deterministic git and GitHub CLI behavior. They default to the same helpers
// the rest of the package uses.
var (
	workspaceGit = gitCommand
	workspaceGh  = func(repo string, arguments ...string) (string, error) {
		return commandOutput(repo, "gh", arguments...)
	}
	workspacePackageCopy         = copyFeaturePackage
	workspaceSourcePackageRemove = os.RemoveAll
	workspaceDetachedAlias       = registerDetachedWorkspaceAlias
	workspaceAfterDestination    = func(string) error { return nil }
	workspaceAfterDetachedAlias  = func(string) error { return nil }
)

// ResolvedWorkspace is the workspace policy with empty fields filled from the
// documented defaults. Enabled is never defaulted: a config without a workspace
// block, or with enabled=false, keeps Boatstack's prior hands-off behavior.
type ResolvedWorkspace struct {
	Enabled      bool
	Mode         string
	Cleanup      string
	CleanupAfter string
	Reap         string
}

func resolveWorkspace(workspace Workspace) ResolvedWorkspace {
	resolved := ResolvedWorkspace{
		Enabled:      workspace.Enabled,
		Mode:         workspace.Mode,
		Cleanup:      workspace.Cleanup,
		CleanupAfter: workspace.CleanupAfter,
		Reap:         workspace.Reap,
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
	if resolved.Reap == "" {
		resolved.Reap = "confirm"
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

// reapEnabled reports whether the post-merge reap sweep is active — workspace
// management is on and workspace.reap is not "off". It swallows config errors as
// "off" so the read-only next surface never fails on a malformed project file.
func reapEnabled(repo string) bool {
	policy, err := loadWorkspacePolicy(repo)
	if err != nil {
		return false
	}
	return policy.Enabled && policy.Reap != "off"
}

// needsFreshCut reports whether the caller still has to enter the feature's
// branch workspace. Existence is not readiness: a detached HEAD, the base branch,
// another feature branch, or a sibling worktree all require the managed
// transition so the planning package and execution directory move together.
func needsFreshCut(repo, feature string) bool {
	branch := branchForFeature(feature)
	if branch == "" {
		return false
	}
	current, _ := workspaceGit(repo, "branch", "--show-current")
	return strings.TrimSpace(current) != branch
}

// isMainWorktree reports whether repo is checked out in the repository's main
// worktree, whose Git directory aliases the common directory. A linked worktree's
// Git directory is .git/worktrees/<name>, so the two differ there.
func isMainWorktree(repo string) bool {
	gitDir, err := worktreeGitDir(repo)
	if err != nil {
		return false
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return false
	}
	return gitDir == common
}

// guardManagedActivationWorktree refuses to activate a managed delivery from the
// main worktree once the feature already has a cut workspace. In worktree mode the
// delivery must be built inside its cut worktree; activating on the base branch in
// the main worktree strands compiled artifacts and a competing per-worktree
// delivery ledger on the base branch (the split-brain this guards against). It is
// inert unless workspace management is on in worktree mode, a workspace for the
// feature already exists, and the caller is on the base branch in the main
// worktree — so the normal flow (cut first, then activate inside the worktree) is
// unaffected.
func guardManagedActivationWorktree(repo string, config ProjectConfig, feature string) error {
	policy := resolveWorkspace(config.Workspace)
	if !policy.Enabled || policy.Mode != "worktree" {
		return nil
	}
	branch := branchForFeature(feature)
	if branch == "" {
		return nil
	}
	worktreePath := worktreePathForBranch(repo, branch)
	if worktreePath == "" && !branchExists(repo, branch) {
		return nil // no workspace cut yet — this is the normal pre-cut path
	}
	if !isMainWorktree(repo) {
		return nil // already inside a linked worktree
	}
	base := defaultPRBase(repo)
	if current, _ := workspaceGit(repo, "branch", "--show-current"); strings.TrimSpace(current) != base {
		return nil // not on the base branch
	}
	if worktreePath != "" {
		return fmt.Errorf("feature %q already has a managed workspace at %s; activate and build there, not on the base branch %q. Run: cd %s and re-run — managed delivery must run in its cut worktree", feature, worktreePath, base, worktreePath)
	}
	return fmt.Errorf("feature %q already has a managed branch %q; activate and build in its worktree, not on the base branch %q — managed delivery must run in its cut worktree", feature, branch, base)
}

func loadWorkspacePolicy(repo string) (ResolvedWorkspace, error) {
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
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
	ControllerMode     string `json:"controller_mode,omitempty"`
	Outcome            string `json:"outcome,omitempty"` // created | adopted | current
	BaseBranch         string `json:"base_branch,omitempty"`
	BaseCommit         string `json:"base_commit,omitempty"`
	Branch             string `json:"branch,omitempty"`
	WorktreePath       string `json:"worktree_path,omitempty"`
	SourceRepository   string `json:"source_repository,omitempty"`
	DestinationRepo    string `json:"destination_repository,omitempty"`
	PlanFingerprint    string `json:"plan_fingerprint,omitempty"`
	Created            bool   `json:"created"`
	Reason             string `json:"reason"`
}

func blockedCut(reason string) WorkspaceCut {
	return WorkspaceCut{SchemaVersion: workspaceSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

type workspaceTransition struct {
	branchCreated   bool
	branchSwitched  bool
	worktreeCreated bool
	detachedAlias   bool
	originalBranch  string
	originalHead    string
}

func rollbackWorkspaceTransition(repo, branch, worktreePath string, transition workspaceTransition) {
	if transition.detachedAlias {
		_ = unregisterDetachedWorkspaceAlias(worktreePath)
	}
	if transition.worktreeCreated && worktreePath != "" {
		_, _ = workspaceGit(repo, "worktree", "remove", "--force", worktreePath)
	}
	if transition.branchSwitched {
		if transition.originalBranch != "" {
			_, _ = workspaceGit(repo, "switch", transition.originalBranch)
		} else if transition.originalHead != "" {
			_, _ = workspaceGit(repo, "checkout", "--detach", transition.originalHead)
		}
	}
	if transition.branchCreated {
		_, _ = workspaceGit(repo, "branch", "-D", branch)
	}
}

func featurePackageFingerprint(repo, directory string) (string, error) {
	planPath := filepath.Join(directory, "plan.md")
	if !fileExists(planPath) {
		return "", nil
	}
	check, err := CheckPlanForRepository(repo, planPath)
	if err != nil {
		return "", err
	}
	return check.Fingerprint, nil
}

func featurePackageDigest(directory string) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil || relative == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("planning package contains unsupported entry %s", relative)
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		_, _ = digest.Write([]byte(kind + "\x00" + filepath.ToSlash(relative) + "\x00"))
		if info.IsDir() {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = digest.Write(value)
		_, _ = digest.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func copyFeaturePackage(source, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".boatstack-workspace-transfer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(temporary, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("planning package contains unsupported entry %s", relative)
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, value, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}

func dirtyOutsideFeature(repo, feature string) (bool, error) {
	output, err := workspaceGit(repo, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	prefix := filepath.ToSlash(filepath.Join(productLoopDirName, "features", feature)) + "/"
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		path = strings.Trim(path, "\"")
		normalized := filepath.ToSlash(path)
		if !strings.HasPrefix(normalized, prefix) {
			return true, nil
		}
	}
	return false, nil
}

// transferFeaturePackage moves a validated embedded planning package into the
// destination worktree. The source remains authoritative until the destination
// fingerprint verifies. A failed source cleanup removes the new copy, so the
// boundary never leaves two authoritative packages.
func transferFeaturePackage(sourceRepo, destinationRepo, feature string, controllerMode SupervisionMode) (string, error) {
	if feature == "" {
		return "", nil
	}
	source := WorkspaceFor(sourceRepo).FeatureDir(feature)
	if source == "" || !dirExists(source) {
		destination := WorkspaceFor(destinationRepo).FeatureDir(feature)
		if destination == "" || !dirExists(destination) {
			return "", nil
		}
		return featurePackageFingerprint(destinationRepo, destination)
	}
	sourceFingerprint, err := featurePackageFingerprint(sourceRepo, source)
	if err != nil {
		return "", fmt.Errorf("source planning package is invalid: %w", err)
	}
	if sourceFingerprint == "" {
		return "", nil
	}
	sourceDigest, err := featurePackageDigest(source)
	if err != nil {
		return "", fmt.Errorf("source planning package cannot be fingerprinted: %w", err)
	}
	destination := WorkspaceFor(destinationRepo).FeatureDir(feature)
	if filepath.Clean(source) == filepath.Clean(destination) {
		return sourceFingerprint, nil
	}
	if dirExists(destination) {
		destinationFingerprint, fingerprintErr := featurePackageFingerprint(destinationRepo, destination)
		destinationDigest, digestErr := featurePackageDigest(destination)
		if fingerprintErr != nil || digestErr != nil || destinationFingerprint != sourceFingerprint || destinationDigest != sourceDigest {
			return "", fmt.Errorf("destination workspace contains a conflicting planning package")
		}
		if controllerMode == SupervisionEmbedded {
			if err := workspaceSourcePackageRemove(source); err != nil {
				return "", fmt.Errorf("remove transferred source package: %w", err)
			}
		}
		return sourceFingerprint, nil
	}
	if err := workspacePackageCopy(source, destination); err != nil {
		return "", fmt.Errorf("copy planning package: %w", err)
	}
	destinationFingerprint, err := featurePackageFingerprint(destinationRepo, destination)
	destinationDigest, digestErr := featurePackageDigest(destination)
	if err != nil || digestErr != nil || destinationFingerprint != sourceFingerprint || destinationDigest != sourceDigest {
		_ = os.RemoveAll(destination)
		return "", fmt.Errorf("destination planning package fingerprint did not verify")
	}
	if controllerMode == SupervisionEmbedded {
		if err := workspaceSourcePackageRemove(source); err != nil {
			_ = os.RemoveAll(destination)
			return "", fmt.Errorf("remove transferred source package: %w", err)
		}
	}
	return sourceFingerprint, nil
}

// CutFeatureWorkspace establishes the feature's execution workspace and carries
// its validated planning package across the boundary. Existing branches are
// adopted only when unowned and byte-identical to the fetched base.
func CutFeatureWorkspace(options WorkspaceCutOptions) (WorkspaceCut, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return blockedCut(err.Error()), nil
	}
	if !fileExists(WorkspaceFor(repo).ProjectConfigPath()) {
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

	sourceContext, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return blockedCut(err.Error()), nil
	}
	result := WorkspaceCut{
		SchemaVersion:      workspaceSchemaVersion,
		VerificationStatus: "VERIFIED",
		Mode:               policy.Mode,
		ControllerMode:     string(sourceContext.Mode),
		BaseBranch:         base,
		BaseCommit:         strings.TrimSpace(baseCommit),
		Branch:             branch,
		SourceRepository:   repo,
	}
	originalBranch, _ := workspaceGit(repo, "branch", "--show-current")
	originalHead, _ := workspaceGit(repo, "rev-parse", "HEAD^{commit}")
	transition := workspaceTransition{
		originalBranch: strings.TrimSpace(originalBranch),
		originalHead:   strings.TrimSpace(originalHead),
	}
	destinationRepo := repo

	switch policy.Mode {
	case "branch":
		current, _ := workspaceGit(repo, "branch", "--show-current")
		if strings.TrimSpace(current) == branch {
			headCommit, _ := workspaceGit(repo, "rev-parse", "HEAD^{commit}")
			if strings.TrimSpace(headCommit) != strings.TrimSpace(baseCommit) {
				return blockedCut(fmt.Sprintf("Current branch %q diverges from the fetched base.", branch)), nil
			}
			result.Outcome = "current"
		} else if branchExists(repo, branch) {
			branchCommit, _ := workspaceGit(repo, "rev-parse", "refs/heads/"+branch+"^{commit}")
			if strings.TrimSpace(branchCommit) != strings.TrimSpace(baseCommit) {
				return blockedCut(fmt.Sprintf("Branch %q diverges from the fetched base and cannot be adopted.", branch)), nil
			}
			owner, ownerErr := activeDeliveryOwningBranch(repo, branch)
			if ownerErr != nil {
				return blockedCut(fmt.Sprintf("Boatstack could not verify whether branch %q has an active owner: %v", branch, ownerErr)), nil
			}
			if owner != "" {
				return blockedCut(fmt.Sprintf("Branch %q is owned by an active delivery and cannot be adopted.", branch)), nil
			}
			if _, err := workspaceGit(repo, "switch", branch); err != nil {
				return blockedCut("Boatstack could not adopt the branch: " + err.Error()), nil
			}
			transition.branchSwitched = true
			result.Outcome = "adopted"
		} else {
			if _, err := workspaceGit(repo, "switch", "-c", branch, baseCommit); err != nil {
				return blockedCut("Boatstack could not create the branch: " + err.Error()), nil
			}
			transition.branchCreated = true
			transition.branchSwitched = true
			result.Created = true
			result.Outcome = "created"
		}
	default: // "worktree"
		path := worktreePathForBranch(repo, branch)
		if path != "" {
			branchCommit, _ := workspaceGit(repo, "rev-parse", "refs/heads/"+branch+"^{commit}")
			if strings.TrimSpace(branchCommit) != strings.TrimSpace(baseCommit) {
				return blockedCut(fmt.Sprintf("Branch %q diverges from the fetched base and cannot be reused.", branch)), nil
			}
			owner, ownerErr := activeDeliveryOwningBranch(repo, branch)
			if ownerErr != nil {
				return blockedCut(fmt.Sprintf("Boatstack could not verify whether branch %q has an active owner: %v", branch, ownerErr)), nil
			}
			if owner != "" {
				return blockedCut(fmt.Sprintf("Branch %q is owned by an active delivery and cannot be reused.", branch)), nil
			}
			destinationRepo = path
			result.Outcome = "current"
		} else {
			path = filepath.Join(repo, ".product-loop", "worktrees", previewSlug(branch))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return blockedCut("Boatstack could not prepare the worktree directory: " + err.Error()), nil
			}
			if branchExists(repo, branch) {
				branchCommit, _ := workspaceGit(repo, "rev-parse", "refs/heads/"+branch+"^{commit}")
				if strings.TrimSpace(branchCommit) != strings.TrimSpace(baseCommit) {
					return blockedCut(fmt.Sprintf("Branch %q diverges from the fetched base and cannot be adopted.", branch)), nil
				}
				owner, ownerErr := activeDeliveryOwningBranch(repo, branch)
				if ownerErr != nil {
					return blockedCut(fmt.Sprintf("Boatstack could not verify whether branch %q has an active owner: %v", branch, ownerErr)), nil
				}
				if owner != "" {
					return blockedCut(fmt.Sprintf("Branch %q is owned by an active delivery and cannot be adopted.", branch)), nil
				}
				if _, err := workspaceGit(repo, "worktree", "add", path, branch); err != nil {
					return blockedCut("Boatstack could not adopt the branch into a worktree: " + err.Error()), nil
				}
				result.Outcome = "adopted"
			} else {
				if _, err := workspaceGit(repo, "worktree", "add", "-b", branch, path, baseCommit); err != nil {
					return blockedCut("Boatstack could not add the worktree: " + err.Error()), nil
				}
				transition.branchCreated = true
				result.Created = true
				result.Outcome = "created"
			}
			transition.worktreeCreated = true
			destinationRepo = path
		}
		result.WorktreePath = path
	}

	if err := workspaceAfterDestination(destinationRepo); err != nil {
		rollbackWorkspaceTransition(repo, branch, result.WorktreePath, transition)
		return blockedCut("Boatstack could not verify the destination workspace: " + err.Error()), nil
	}
	if dirty, dirtyErr := dirtyOutsideFeature(destinationRepo, options.Feature); dirtyErr != nil || dirty {
		rollbackWorkspaceTransition(repo, branch, result.WorktreePath, transition)
		return blockedCut("Destination workspace contains changes outside the managed feature package."), nil
	}
	if sourceContext.Mode == SupervisionDetached && filepath.Clean(destinationRepo) != filepath.Clean(repo) {
		added, err := workspaceDetachedAlias(repo, destinationRepo)
		if err != nil {
			rollbackWorkspaceTransition(repo, branch, result.WorktreePath, transition)
			return blockedCut("Boatstack could not bind detached controller state to the destination: " + err.Error()), nil
		}
		transition.detachedAlias = added
		if err := workspaceAfterDetachedAlias(destinationRepo); err != nil {
			rollbackWorkspaceTransition(repo, branch, result.WorktreePath, transition)
			return blockedCut("Boatstack could not verify detached controller state after registration: " + err.Error()), nil
		}
	}
	fingerprint, err := transferFeaturePackage(repo, destinationRepo, options.Feature, sourceContext.Mode)
	if err != nil {
		rollbackWorkspaceTransition(repo, branch, result.WorktreePath, transition)
		return blockedCut("Boatstack could not transfer the planning package: " + err.Error()), nil
	}
	result.PlanFingerprint = fingerprint
	result.DestinationRepo = destinationRepo
	result.Reason = fmt.Sprintf("Workspace %q is ready at %s; continue Boatstack from that repository path.", branch, destinationRepo)
	return result, nil
}

type workspaceLifecyclePhase string

const (
	workspaceActive            workspaceLifecyclePhase = "ACTIVE"
	workspacePublished         workspaceLifecyclePhase = "PUBLISHED"
	workspaceLanded            workspaceLifecyclePhase = "LANDED"
	workspaceAbandoned         workspaceLifecyclePhase = "ABANDONED"
	workspaceAttentionRequired workspaceLifecyclePhase = "ATTENTION_REQUIRED"
)

// workspaceLifecycleAssessment is the single authority boundary for workspace
// completion. Published and Landed are deliberately separate: Git ancestry may
// confirm landing only after a PR or completed managed delivery proves that the
// branch entered the publication lifecycle.
type workspaceLifecycleAssessment struct {
	Phase     workspaceLifecyclePhase
	Source    string
	Published bool
	Landed    bool
	Reason    string
}

func workspaceFeatureForBranch(branch string) string {
	feature := strings.TrimPrefix(strings.TrimSpace(branch), "feat/")
	if feature == branch || !featureSlugPattern.MatchString(feature) {
		return ""
	}
	return feature
}

func workspaceBranchLanded(repo, branch, base string) bool {
	if strings.TrimSpace(branch) == "" {
		return false
	}
	for _, target := range []string{"refs/remotes/origin/" + base, "refs/heads/" + base, base} {
		if _, err := workspaceGit(repo, "merge-base", "--is-ancestor", "refs/heads/"+branch, target); err == nil {
			return true
		}
	}
	return false
}

func managedWorkspaceLifecycle(repo, branch, base string) (workspaceLifecycleAssessment, bool) {
	feature := workspaceFeatureForBranch(branch)
	if feature == "" {
		return workspaceLifecycleAssessment{}, false
	}
	owner := repo
	if worktree := worktreePathForBranch(repo, branch); worktree != "" {
		owner = worktree
	}
	statePath, err := deliveryStatePath(owner, feature)
	if err != nil || !fileExists(statePath) {
		return workspaceLifecycleAssessment{}, false
	}
	state, err := CurrentDeliveryState(owner, feature)
	if err != nil || !stateMatchesBranch(state, branch) {
		return workspaceLifecycleAssessment{
			Phase: workspaceAttentionRequired, Source: "delivery",
			Reason: "Managed delivery evidence is present but cannot be verified for this branch.",
		}, true
	}
	if len(state.Slices) == 0 || state.ActiveIndex < len(state.Slices) {
		return workspaceLifecycleAssessment{
			Phase: workspaceActive, Source: "delivery",
			Reason: "Managed delivery work is still active.",
		}, true
	}
	for _, slice := range state.Slices {
		if slice.Status != StatusPublished || strings.TrimSpace(slice.PRURL) == "" {
			return workspaceLifecycleAssessment{
				Phase: workspaceAttentionRequired, Source: "delivery",
				Reason: "Completed delivery state lacks durable publication evidence.",
			}, true
		}
		if strings.EqualFold(strings.TrimSpace(slice.PRState), "CLOSED") {
			return workspaceLifecycleAssessment{
				Phase: workspaceAttentionRequired, Source: "delivery", Published: true,
				Reason: "A published pull request closed without a verified merge.",
			}, true
		}
	}
	allLanded := true
	for _, slice := range state.Slices {
		if strings.EqualFold(strings.TrimSpace(slice.PRState), "MERGED") {
			continue
		}
		head := strings.TrimSpace(slice.HeadBranch)
		if head == "" {
			head = branch
		}
		if !workspaceBranchLanded(repo, head, base) {
			allLanded = false
			break
		}
	}
	if allLanded {
		return workspaceLifecycleAssessment{
			Phase: workspaceLanded, Source: "git-after-publication", Published: true, Landed: true,
			Reason: "Every published delivery branch is contained in the base branch.",
		}, true
	}
	if resolveDeliveryTerminal(owner, feature) == TerminalMerged {
		return workspaceLifecycleAssessment{
			Phase: workspaceActive, Source: "delivery", Published: true,
			Reason: "Every delivery slice is published, but the configured merged terminal is unfinished.",
		}, true
	}
	return workspaceLifecycleAssessment{
		Phase: workspacePublished, Source: "delivery", Published: true,
		Reason: "Every delivery slice is published, but landing is not verified.",
	}, true
}

func assessWorkspaceLifecycle(repo, branch, base string, abandoned bool) workspaceLifecycleAssessment {
	if abandoned {
		return workspaceLifecycleAssessment{
			Phase: workspaceAbandoned, Source: "operator", Reason: "The operator explicitly abandoned this delivery.",
		}
	}
	if managed, ok := managedWorkspaceLifecycle(repo, branch, base); ok {
		return managed
	}
	if out, err := workspaceGh(repo, "pr", "view", branch, "--json", "state", "-q", ".state"); err == nil {
		switch strings.ToUpper(strings.TrimSpace(out)) {
		case "MERGED":
			return workspaceLifecycleAssessment{
				Phase: workspaceLanded, Source: "gh", Published: true, Landed: true,
				Reason: "GitHub reports the pull request merged.",
			}
		case "OPEN":
			return workspaceLifecycleAssessment{
				Phase: workspacePublished, Source: "gh", Published: true,
				Reason: "GitHub reports an open pull request.",
			}
		case "CLOSED":
			return workspaceLifecycleAssessment{
				Phase: workspaceAttentionRequired, Source: "gh", Published: true,
				Reason: "GitHub reports the pull request closed without a merge.",
			}
		default:
			return workspaceLifecycleAssessment{
				Phase: workspaceAttentionRequired, Source: "gh",
				Reason: "GitHub returned an unsupported pull request state.",
			}
		}
	}
	return workspaceLifecycleAssessment{
		Phase: workspaceActive, Source: "unpublished",
		Reason: "No durable publication evidence exists; preserving the active workspace.",
	}
}

func (assessment workspaceLifecycleAssessment) cleanupEligible(cleanupAfter string) bool {
	if assessment.Phase == workspaceAbandoned {
		return true
	}
	if cleanupAfter == "ship" {
		return assessment.Phase == workspacePublished || assessment.Phase == workspaceLanded
	}
	return assessment.Phase == workspaceLanded
}

// workspaceMergeStatus is retained as the narrow merge projection used by
// existing callers and tests. It can no longer promote bare ancestry into merge
// authority: the full lifecycle assessment owns that decision.
func workspaceMergeStatus(repo, branch, base string) (bool, string) {
	assessment := assessWorkspaceLifecycle(repo, branch, base, false)
	return assessment.Landed, assessment.Source
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

// workspaceRemovalPlan is the outcome of the safety gates shared by cleanup and
// reap: whether the named workspace may be removed, and the merge state resolved
// while deciding (which selects force-deletion of a squash/rebase-merged branch).
type workspaceRemovalPlan struct {
	Removable bool
	Status    string // BLOCKED when !Removable
	Reason    string
	Merged    bool
}

// planWorkspaceRemoval applies the base-branch, current-branch, merge, dirty, and
// unmerged-commit gates that govern removing a single feature workspace. It never
// consults cleanup/reap mode or human confirmation and never mutates the
// repository; callers own policy and confirmation. cleanupAfter="ship" permits
// removing an unmerged branch after the lifecycle boundary has already proved
// publication (used for published and explicitly abandoned workspaces).
func planWorkspaceRemoval(repo, base, branch, worktreePath, cleanupAfter string, merged, force bool) workspaceRemovalPlan {
	if branch == base {
		return workspaceRemovalPlan{Status: "BLOCKED", Reason: fmt.Sprintf("Refusing to clean up the base branch %q.", base)}
	}
	if current, _ := workspaceGit(repo, "branch", "--show-current"); strings.TrimSpace(current) == branch && worktreePath == "" {
		return workspaceRemovalPlan{Status: "BLOCKED", Reason: fmt.Sprintf("Branch %q is the current branch; switch away before cleaning it up.", branch)}
	}
	if cleanupAfter == "merge" && !merged && !force {
		return workspaceRemovalPlan{Status: "BLOCKED", Reason: fmt.Sprintf("PR for %q is not merged yet; keeping the workspace. Re-run with force to clean up early.", branch)}
	}
	// Refuse to discard work the user has not landed unless explicitly forced.
	if !force {
		if worktreePath != "" {
			if dirty, _ := workspaceGit(worktreePath, "status", "--porcelain"); strings.TrimSpace(dirty) != "" {
				return workspaceRemovalPlan{Status: "BLOCKED", Reason: fmt.Sprintf("Workspace %q has uncommitted changes; commit or discard them, or force cleanup.", branch)}
			}
		}
		if !merged && cleanupAfter != "ship" {
			return workspaceRemovalPlan{Status: "BLOCKED", Reason: fmt.Sprintf("Branch %q has commits not merged into %s; force cleanup to discard them.", branch, base)}
		}
	}
	return workspaceRemovalPlan{Removable: true, Status: "VERIFIED", Merged: merged}
}

// performWorkspaceRemoval removes the linked worktree (if any) and deletes the
// local branch in-process. It assumes planWorkspaceRemoval already cleared the
// safety gates; merged/force select force-deletion so a squash- or rebase-merged
// branch (whose local ref is not a local ancestor of the base) is still removable.
// The git it runs is the helper's own subprocess, never an agent shell command, so
// it is the sanctioned actuator the guard allows.
func performWorkspaceRemoval(repo, branch, worktreePath string, merged, force bool) (worktreeRemoved, branchDeleted bool, reason string, err error) {
	if worktreePath != "" {
		removeArgs := []string{"worktree", "remove", worktreePath}
		if force {
			removeArgs = append(removeArgs, "--force")
		}
		if _, e := workspaceGit(repo, removeArgs...); e != nil {
			return false, false, "Boatstack could not remove the worktree: " + e.Error(), e
		}
		worktreeRemoved = true
	}
	if branchExists(repo, branch) {
		deleteFlag := "-d"
		if force || merged {
			deleteFlag = "-D"
		}
		if _, e := workspaceGit(repo, "branch", deleteFlag, branch); e != nil {
			return worktreeRemoved, false, "Boatstack could not delete the branch: " + e.Error(), e
		}
		branchDeleted = true
	}
	return worktreeRemoved, branchDeleted, "", nil
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
	if !fileExists(WorkspaceFor(repo).ProjectConfigPath()) {
		return blockedCleanup(branch, "This repository has no Boatstack project installation."), nil
	}
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		return blockedCleanup(branch, "Boatstack could not read the workspace policy: "+err.Error()), nil
	}
	policy := resolveWorkspace(config.Workspace)
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
	abandoned := false
	for _, feature := range config.Workflow.IgnoredDeliveries {
		if branchForFeature(feature) == branch {
			abandoned = true
			break
		}
	}
	lifecycle := assessWorkspaceLifecycle(repo, branch, base, abandoned)
	result := WorkspaceCleanup{
		SchemaVersion: workspaceSchemaVersion, Branch: branch, Mode: policy.Mode,
		Merged: lifecycle.Landed, MergeSource: lifecycle.Source,
	}
	if !options.Force && !lifecycle.cleanupEligible(policy.CleanupAfter) {
		result.VerificationStatus = "BLOCKED"
		result.Reason = lifecycle.Reason
		return result, nil
	}

	cleanupAfter := policy.CleanupAfter
	if lifecycle.Phase == workspaceAbandoned {
		cleanupAfter = "ship"
	}
	plan := planWorkspaceRemoval(repo, base, branch, worktreePath, cleanupAfter, lifecycle.Landed, options.Force)
	if !plan.Removable {
		result.VerificationStatus = plan.Status
		result.Reason = plan.Reason
		return result, nil
	}
	result.Merged = plan.Merged

	if policy.Cleanup == "confirm" && !options.Confirm && !options.Force {
		result.VerificationStatus = "NEEDS_CONFIRMATION"
		result.Reason = fmt.Sprintf("Ready to remove the workspace for %q. Confirm cleanup to proceed.", branch)
		return result, nil
	}

	worktreeRemoved, branchDeleted, failReason, removeErr := performWorkspaceRemoval(repo, branch, worktreePath, plan.Merged, options.Force)
	if removeErr != nil {
		return blockedCleanup(branch, failReason), nil
	}
	result.WorktreeRemoved = worktreeRemoved
	result.BranchDeleted = branchDeleted
	result.VerificationStatus = "VERIFIED"
	result.Reason = fmt.Sprintf("Cleaned up the workspace for %q.", branch)
	return result, nil
}

// worktreeEntry is one linked worktree and the branch it has checked out.
type worktreeEntry struct {
	Path   string
	Branch string
}

// boatstackWorktrees lists the linked worktrees Boatstack owns — those created
// under .product-loop/worktrees/. The main worktree, human-created worktrees, and
// detached worktrees are excluded so reap can never remove work Boatstack did not
// create. Paths are absolute (git reports them so), which lets reap run from any
// worktree.
func boatstackWorktrees(repo string) []worktreeEntry {
	out, err := workspaceGit(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var entries []worktreeEntry
	var current worktreeEntry
	flush := func() {
		if current.Path != "" && current.Branch != "" && strings.Contains(filepath.ToSlash(current.Path), "/.product-loop/worktrees/") {
			entries = append(entries, current)
		}
		current = worktreeEntry{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "branch ")), "refs/heads/")
		}
	}
	flush()
	return entries
}

// WorkspaceReapOptions requests a sweep of all terminal Boatstack workspaces.
type WorkspaceReapOptions struct {
	Repo    string
	Confirm bool // the operator approved the aggregate reap prompt
	Force   bool // override the merge/dirty/unmerged gates and discard unlanded work
}

// WorkspaceReapItem is the per-workspace outcome within a reap sweep.
type WorkspaceReapItem struct {
	Branch          string `json:"branch"`
	WorktreePath    string `json:"worktree_path,omitempty"`
	Merged          bool   `json:"merged"`
	MergeSource     string `json:"merge_source,omitempty"`
	Abandoned       bool   `json:"abandoned,omitempty"`
	Action          string `json:"action"` // reaped | reclaimable | skipped | blocked
	WorktreeRemoved bool   `json:"worktree_removed,omitempty"`
	BranchDeleted   bool   `json:"branch_deleted,omitempty"`
	Reason          string `json:"reason"`
}

// WorkspaceReap is the deterministic result of a reap sweep.
type WorkspaceReap struct {
	SchemaVersion      int                 `json:"schema_version"`
	VerificationStatus string              `json:"verification_status"` // VERIFIED | NEEDS_CONFIRMATION | BLOCKED
	Mode               string              `json:"mode,omitempty"`
	Candidates         []WorkspaceReapItem `json:"candidates,omitempty"`
	ReclaimableCount   int                 `json:"reclaimable_count"`
	ReapedCount        int                 `json:"reaped_count"`
	Reason             string              `json:"reason"`
}

func blockedReap(reason string) WorkspaceReap {
	return WorkspaceReap{SchemaVersion: workspaceSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

// samePath reports whether two filesystem paths denote the same location,
// resolving symlinks first (macOS temp dirs live under a /private symlink, so a
// raw string compare of git's toplevel against a recorded worktree path can
// spuriously differ) and falling back to a lexical comparison.
func samePath(a, b string) bool {
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// reclaimableScan enumerates the Boatstack worktrees and classifies each as
// skipped or reclaimable without mutating the repository. It returns the skipped
// candidates (for reporting) and the reclaimable subset (merged or abandoned,
// excluding the base branch, the current worktree, and non-Boatstack worktrees).
func reclaimableScan(repo, base, cleanupAfter string, ignored []string) (skipped, reapable []WorkspaceReapItem) {
	abandonedBranches := map[string]bool{}
	for _, slug := range ignored {
		if branch := branchForFeature(slug); branch != "" {
			abandonedBranches[branch] = true
		}
	}
	currentTop := ""
	if top, topErr := workspaceGit(repo, "rev-parse", "--show-toplevel"); topErr == nil {
		currentTop = strings.TrimSpace(top)
	}
	for _, wt := range boatstackWorktrees(repo) {
		branch := wt.Branch
		if branch == "" || branch == base {
			continue
		}
		item := WorkspaceReapItem{Branch: branch, WorktreePath: wt.Path}
		if currentTop != "" && samePath(wt.Path, currentTop) {
			item.Action = "skipped"
			item.Reason = "This is the current worktree; reap it from another location."
			skipped = append(skipped, item)
			continue
		}
		abandoned := abandonedBranches[branch]
		lifecycle := assessWorkspaceLifecycle(repo, branch, base, abandoned)
		item.Merged = lifecycle.Landed
		item.MergeSource = lifecycle.Source
		item.Abandoned = abandoned
		if !lifecycle.cleanupEligible(cleanupAfter) {
			item.Action = "skipped"
			item.Reason = lifecycle.Reason
			skipped = append(skipped, item)
			continue
		}
		item.Action = "reclaimable"
		reapable = append(reapable, item)
	}
	return skipped, reapable
}

// CountReclaimableWorkspaces reports how many terminal Boatstack workspaces reap
// would reclaim right now. It is read-only so boatstack-next can surface the reap
// prompt with an accurate count. It returns 0 when workspace management is off.
func CountReclaimableWorkspaces(repoPath string) int {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return 0
	}
	config, _, cfgErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if cfgErr != nil || !resolveWorkspace(config.Workspace).Enabled {
		return 0
	}
	policy := resolveWorkspace(config.Workspace)
	_, reapable := reclaimableScan(repo, defaultPRBase(repo), policy.CleanupAfter, config.Workflow.IgnoredDeliveries)
	return len(reapable)
}

// ReapWorkspaces sweeps every terminal Boatstack workspace — one whose branch is
// confirmed merged (via gh, else local ancestry) or explicitly abandoned (its
// feature slug is in workflow.ignored_deliveries) — and reclaims the local
// worktree and branch. It never touches a non-Boatstack worktree, the base branch,
// the current worktree, or an unmerged/open workspace, and it never discards
// uncommitted or unmerged work without Force. In confirm mode it returns the
// reclaimable set as NEEDS_CONFIRMATION without removing anything; in auto mode (or
// after Confirm/Force) it removes them through the in-process actuator.
func ReapWorkspaces(options WorkspaceReapOptions) (WorkspaceReap, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return blockedReap(err.Error()), nil
	}
	if !fileExists(WorkspaceFor(repo).ProjectConfigPath()) {
		return blockedReap("This repository has no Boatstack project installation."), nil
	}
	config, _, cfgErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if cfgErr != nil {
		return blockedReap("Boatstack could not read the workspace policy: " + cfgErr.Error()), nil
	}
	policy := resolveWorkspace(config.Workspace)
	result := WorkspaceReap{SchemaVersion: workspaceSchemaVersion, Mode: policy.Reap}
	if !policy.Enabled {
		result.VerificationStatus = "BLOCKED"
		result.Reason = "Workspace management is disabled (workspace.enabled=false)."
		return result, nil
	}
	if policy.Reap == "off" && !options.Force {
		result.VerificationStatus = "BLOCKED"
		result.Reason = "Workspace reaping is disabled (workspace.reap=off)."
		return result, nil
	}

	base := defaultPRBase(repo)
	skipped, reapable := reclaimableScan(repo, base, policy.CleanupAfter, config.Workflow.IgnoredDeliveries)
	result.Candidates = append(result.Candidates, skipped...)
	result.ReclaimableCount = len(reapable)

	if len(reapable) == 0 {
		result.VerificationStatus = "VERIFIED"
		result.Reason = "No merged or abandoned Boatstack workspaces to reclaim."
		return result, nil
	}

	if policy.Reap == "confirm" && !options.Confirm && !options.Force {
		result.Candidates = append(result.Candidates, reapable...)
		result.VerificationStatus = "NEEDS_CONFIRMATION"
		result.Reason = fmt.Sprintf("%d Boatstack worktree(s)/branch(es) are merged or abandoned and reclaimable. Confirm reap to remove them.", len(reapable))
		return result, nil
	}

	for _, item := range reapable {
		// An explicitly abandoned but unmerged branch is operator-authorized
		// disposal, so it is removed like cleanup_after="ship" (unmerged allowed);
		// the dirty gate below still protects uncommitted work unless forced.
		effectiveAfter := policy.CleanupAfter
		if item.Abandoned && !item.Merged {
			effectiveAfter = "ship"
		}
		plan := planWorkspaceRemoval(repo, base, item.Branch, item.WorktreePath, effectiveAfter, item.Merged, options.Force)
		if !plan.Removable {
			item.Action = "blocked"
			item.Reason = plan.Reason
			result.Candidates = append(result.Candidates, item)
			continue
		}
		// A terminal branch — merged or operator-abandoned — is safe to
		// force-delete: an abandoned branch is intentionally not merged into base,
		// so `git branch -d` would refuse it. The gates above already cleared it.
		forceDelete := plan.Merged || item.Abandoned
		worktreeRemoved, branchDeleted, failReason, removeErr := performWorkspaceRemoval(repo, item.Branch, item.WorktreePath, forceDelete, options.Force)
		if removeErr != nil {
			item.Action = "blocked"
			item.Reason = failReason
			result.Candidates = append(result.Candidates, item)
			continue
		}
		item.Merged = plan.Merged
		item.WorktreeRemoved = worktreeRemoved
		item.BranchDeleted = branchDeleted
		item.Action = "reaped"
		item.Reason = fmt.Sprintf("Reclaimed the workspace for %q.", item.Branch)
		result.Candidates = append(result.Candidates, item)
		result.ReapedCount++
	}

	// Clear any stale worktree admin entries left by out-of-band directory removal.
	_, _ = workspaceGit(repo, "worktree", "prune")

	result.VerificationStatus = "VERIFIED"
	if result.ReapedCount == len(reapable) {
		result.Reason = fmt.Sprintf("Reclaimed %d Boatstack workspace(s).", result.ReapedCount)
	} else {
		result.Reason = fmt.Sprintf("Reclaimed %d of %d reclaimable Boatstack workspace(s); see candidates for the rest.", result.ReapedCount, len(reapable))
	}
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
	config, _, configErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	abandoned := false
	if configErr == nil {
		for _, feature := range config.Workflow.IgnoredDeliveries {
			if branchForFeature(feature) == branch {
				abandoned = true
				break
			}
		}
	}
	base := defaultPRBase(repo)
	lifecycle := assessWorkspaceLifecycle(repo, branch, base, abandoned)
	status.Merged, status.MergeSource = lifecycle.Landed, lifecycle.Source
	status.CleanupDue = configErr == nil && lifecycle.cleanupEligible(resolveWorkspace(config.Workspace).CleanupAfter)
	if status.CleanupDue {
		status.Reason = fmt.Sprintf("Workspace for %q is ready to clean up.", branch)
	} else {
		status.Reason = lifecycle.Reason
	}
	return status, nil
}
