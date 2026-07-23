package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workspaceSyncRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := workspaceRepo(t, defaultWorkspace())
	workspaceGitDo(t, repo, "add", ".product-loop/project.json")
	workspaceGitDo(t, repo, "commit", "-m", "configure workspace")
	remote := filepath.Join(t.TempDir(), "remote.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init remote: %v: %s", err, output)
	}
	workspaceGitDo(t, repo, "remote", "add", "origin", remote)
	workspaceGitDo(t, repo, "push", "-u", "origin", "main")
	return repo, remote
}

func advanceWorkspaceRemote(t *testing.T, repo string) (string, string) {
	t.Helper()
	oldCommit := runGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "remote.txt"), []byte("remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", "remote.txt")
	workspaceGitDo(t, repo, "commit", "-m", "advance remote")
	newCommit := runGit(t, repo, "rev-parse", "HEAD")
	workspaceGitDo(t, repo, "push", "origin", "main")
	workspaceGitDo(t, repo, "reset", "--hard", oldCommit)
	return oldCommit, newCommit
}

func TestSyncWorkspaceNoChangeCreatesNoRecoveryState(t *testing.T) {
	repo, _ := workspaceSyncRepo(t)
	result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "NO_CHANGE" || result.RecoveryRef != "" || result.CheckpointRef != "" {
		t.Fatalf("unexpected no-change result: %+v", result)
	}
	if refs := runGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/boatstack/recovery/workspace-sync"); refs != "" {
		t.Fatalf("no-change sync created recovery refs: %s", refs)
	}
}

func TestSyncWorkspaceCheckpointsDirtyWorktreeAndAlignsBranch(t *testing.T) {
	repo, _ := workspaceSyncRepo(t)
	_, newCommit := advanceWorkspaceRemote(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("dirty untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", "README.md")

	result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SYNCED" || result.RecoveryRef == "" || result.CheckpointRef == "" {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if got := runGit(t, repo, "rev-parse", "main"); got != newCommit {
		t.Fatalf("main=%s, want %s", got, newCommit)
	}
	if status := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("target worktree is dirty after sync: %s", status)
	}
	workspaceGitDo(t, repo, "stash", "apply", "--index", result.CheckpointRef)
	if value, err := os.ReadFile(filepath.Join(repo, "README.md")); err != nil || strings.ReplaceAll(string(value), "\r\n", "\n") != "dirty tracked\n" {
		t.Fatalf("tracked checkpoint was not restorable: %q %v", value, err)
	}
	if value, err := os.ReadFile(filepath.Join(repo, "untracked.txt")); err != nil || strings.ReplaceAll(string(value), "\r\n", "\n") != "dirty untracked\n" {
		t.Fatalf("untracked checkpoint was not restorable: %q %v", value, err)
	}
}

func TestSyncWorkspaceReplacesDivergedBranchAndRetainsOldHead(t *testing.T) {
	repo, _ := workspaceSyncRepo(t)
	_, newCommit := advanceWorkspaceRemote(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", "local.txt")
	workspaceGitDo(t, repo, "commit", "-m", "local divergence")
	localCommit := runGit(t, repo, "rev-parse", "HEAD")

	result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SYNCED" || runGit(t, repo, "rev-parse", "main") != newCommit {
		t.Fatalf("diverged branch was not synchronized: %+v", result)
	}
	if got := runGit(t, repo, "rev-parse", result.RecoveryRef); got != localCommit {
		t.Fatalf("recovery ref=%s, want old head %s", got, localCommit)
	}
}

func TestSyncWorkspaceUsesNamedBranchOwningWorktree(t *testing.T) {
	repo, _ := workspaceSyncRepo(t)
	caller := filepath.Join(t.TempDir(), "caller")
	workspaceGitDo(t, repo, "worktree", "add", "-b", "caller", caller, "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caller, "caller.txt"), []byte("caller remains dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: caller, Branch: "main", Source: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	ownerInfo, ownerErr := os.Stat(result.WorktreePath)
	repoInfo, repoErr := os.Stat(repo)
	if result.Status != "SYNCED" || ownerErr != nil || repoErr != nil || !os.SameFile(ownerInfo, repoInfo) {
		t.Fatalf("sync did not target main's owning worktree: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(caller, "caller.txt")); err != nil {
		t.Fatalf("caller worktree was modified: %v", err)
	}
	if status := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("main worktree is dirty after sync: %s", status)
	}
}

func TestSyncWorkspaceBlocksActiveManagedDelivery(t *testing.T) {
	repo, _ := workspaceSyncRepo(t)
	state := DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion,
		Feature:       "active-main",
		PlanLockHash:  strings.Repeat("a", 64),
		ActiveIndex:   0,
		Mode:          "NORMAL",
		Slices: []DeliverySlice{{
			ID: "delivery", Title: "Delivery", Status: "BUILD", HeadBranch: "main",
		}},
	}
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	before := runGit(t, repo, "rev-parse", "main")
	result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "BLOCKED" || !strings.Contains(result.Reason, "use repair instead") {
		t.Fatalf("active delivery was not blocked: %+v", result)
	}
	if after := runGit(t, repo, "rev-parse", "main"); after != before {
		t.Fatalf("blocked sync changed main from %s to %s", before, after)
	}
}

func TestSyncWorkspaceFailuresDoNotChangeTargetState(t *testing.T) {
	t.Run("fetch", func(t *testing.T) {
		repo, remote := workspaceSyncRepo(t)
		before := runGit(t, repo, "rev-parse", "main")
		if err := os.Rename(remote, remote+".missing"); err != nil {
			t.Fatal(err)
		}
		result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "BLOCKED" || runGit(t, repo, "rev-parse", "main") != before {
			t.Fatalf("fetch failure changed target state: %+v", result)
		}
	})

	t.Run("checkpoint", func(t *testing.T) {
		repo, _ := workspaceSyncRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		beforeHead := runGit(t, repo, "rev-parse", "main")
		oldGit := workspaceGit
		workspaceGit = func(path string, arguments ...string) (string, error) {
			if len(arguments) >= 2 && arguments[0] == "stash" && arguments[1] == "push" {
				return "", fmt.Errorf("injected checkpoint failure")
			}
			return gitCommand(path, arguments...)
		}
		t.Cleanup(func() { workspaceGit = oldGit })

		result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "BLOCKED" || runGit(t, repo, "rev-parse", "main") != beforeHead {
			t.Fatalf("checkpoint failure changed target ref: %+v", result)
		}
		if value, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(value) != "dirty\n" {
			t.Fatalf("checkpoint failure changed worktree: %q", value)
		}
		if refs := runGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/boatstack/recovery/workspace-sync"); refs != "" {
			t.Fatalf("checkpoint failure retained recovery refs: %s", refs)
		}
	})

	t.Run("checkpoint verification preserves prior stash", func(t *testing.T) {
		repo, _ := workspaceSyncRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "prior.txt"), []byte("prior\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		workspaceGitDo(t, repo, "stash", "push", "--include-untracked", "--message", "prior user stash")
		priorStash := runGit(t, repo, "rev-parse", "refs/stash")
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("current dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		oldGit := workspaceGit
		workspaceGit = func(path string, arguments ...string) (string, error) {
			if len(arguments) >= 2 && arguments[0] == "stash" && arguments[1] == "push" {
				return "injected success without checkpoint", nil
			}
			return gitCommand(path, arguments...)
		}
		t.Cleanup(func() { workspaceGit = oldGit })

		result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Branch: "main", Source: "origin/main"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "BLOCKED" || runGit(t, repo, "rev-parse", "refs/stash") != priorStash {
			t.Fatalf("checkpoint verification disturbed the prior stash: %+v", result)
		}
		if value, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(value) != "current dirty\n" {
			t.Fatalf("checkpoint verification changed current worktree: %q", value)
		}
	})

	t.Run("detached", func(t *testing.T) {
		repo, _ := workspaceSyncRepo(t)
		workspaceGitDo(t, repo, "checkout", "--detach")
		before := runGit(t, repo, "rev-parse", "HEAD")
		result, err := SyncWorkspace(WorkspaceSyncOptions{Repo: repo, Source: "origin/main"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "BLOCKED" || runGit(t, repo, "rev-parse", "HEAD") != before {
			t.Fatalf("detached target was not safely blocked: %+v", result)
		}
	})
}

func TestSyncRecoveryRefsAreUnique(t *testing.T) {
	oldNow := workspaceSyncNow
	workspaceSyncNow = func() time.Time { return time.Date(2026, 7, 23, 12, 0, 0, 1, time.UTC) }
	t.Cleanup(func() { workspaceSyncNow = oldNow })
	head, worktree := syncRecoveryRefs("main", strings.Repeat("a", 40))
	if head == worktree || !strings.HasSuffix(head, "/head") || !strings.HasSuffix(worktree, "/worktree") {
		t.Fatalf("unexpected recovery refs: %s %s", head, worktree)
	}
}
