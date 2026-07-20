package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func runtimeTestRepo(t *testing.T) string {
	t.Helper()
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true}); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestHydrateWorktreeRestoresIgnoredRuntime(t *testing.T) {
	repo := runtimeTestRepo(t)
	localDirectory := filepath.Join(repo, ".product-loop", "bin")
	if err := os.RemoveAll(localDirectory); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err != nil {
		t.Fatal(err)
	}
}

func TestHydrationPrecedesEveryHostContractDecision(t *testing.T) {
	repo := runtimeTestRepo(t)
	inputs := map[string][]byte{
		"cursor": []byte(`{"hook_event_name":"beforeShellExecution","command":"git status --short"}`),
		"claude": []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`),
		"codex":  []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`),
	}
	for _, host := range []string{"cursor", "claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			if err := os.RemoveAll(filepath.Join(repo, ".product-loop", "bin")); err != nil {
				t.Fatal(err)
			}
			if err := HydrateWorktree(repo); err != nil {
				t.Fatal(err)
			}
			if _, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: inputs[host]}); denied {
				t.Fatalf("%s denied its safe first hydrated event", host)
			}
			if err := verifyLocalRuntime(repo); err != nil {
				t.Fatalf("%s did not leave a verified local runtime: %v", host, err)
			}
		})
	}
}

func TestHydrateWorktreeIsSafeUnderConcurrentFirstUse(t *testing.T) {
	repo := runtimeTestRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, ".product-loop", "bin")); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errors <- HydrateWorktree(repo)
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := Doctor(repo); err != nil {
		t.Fatal(err)
	}
}

func TestHydrateWorktreeRecoversInterruptedActivationLock(t *testing.T) {
	repo := runtimeTestRepo(t)
	localDirectory := filepath.Join(repo, ".product-loop", "bin")
	if err := os.RemoveAll(localDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(localDirectory, ".hydrate.lock")
	if err := os.WriteFile(lockPath, []byte("interrupted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale activation lock was not removed: %v", err)
	}
}

func TestSharedRuntimeTamperingAndWorktreeVersionDriftFailClosed(t *testing.T) {
	repo := runtimeTestRepo(t)
	binaryPath, _, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected shared checksum failure, got %v", err)
	}
	if _, err := installSharedRuntime(os.Args[0], repo, nil); err != nil {
		t.Fatalf("verified installer could not repair the corrupt cache: %v", err)
	}
	if err := HydrateWorktree(repo); err != nil {
		t.Fatalf("repaired shared cache did not hydrate: %v", err)
	}

	repo = runtimeTestRepo(t)
	lockPath := filepath.Join(repo, ".product-loop", "generated.lock.json")
	value, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(value, &lock); err != nil {
		t.Fatal(err)
	}
	lock["boatstack_version"] = "v99.0.0"
	value, err = MarshalJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err == nil || !strings.Contains(err.Error(), "expects Boatstack") {
		t.Fatalf("expected generated-runtime drift failure, got %v", err)
	}
}

func TestLocalRuntimePlatformDriftFailsClosed(t *testing.T) {
	repo := runtimeTestRepo(t)
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	value, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(value, &lock); err != nil {
		t.Fatal(err)
	}
	lock["platform"] = "different/architecture"
	value, err = MarshalJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyLocalRuntime(repo); err == nil || !strings.Contains(err.Error(), "platform drift") {
		t.Fatalf("expected local platform drift failure, got %v", err)
	}
	if err := HydrateWorktree(repo); err != nil {
		t.Fatalf("shared cache did not repair platform-drifted local state: %v", err)
	}
}

func TestSharedRuntimeRejectsSymlinkedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated Windows permissions")
	}
	repo := runtimeTestRepo(t)
	binaryPath, _, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(target, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(binaryPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, binaryPath); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err == nil || !strings.Contains(err.Error(), "symlinked path") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestHydrateWorktreeRejectsSymlinkedRuntimeDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated Windows permissions")
	}
	repo := runtimeTestRepo(t)
	localDirectory := filepath.Join(repo, ".product-loop", "bin")
	if err := os.RemoveAll(localDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), localDirectory); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err == nil || !strings.Contains(err.Error(), "symlinked path") {
		t.Fatalf("expected local runtime-directory symlink rejection, got %v", err)
	}

	repo = runtimeTestRepo(t)
	common, err := gitCommonDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(common, "boatstack")
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), cacheRoot); err != nil {
		t.Fatal(err)
	}
	if err := HydrateWorktree(repo); err == nil || !strings.Contains(err.Error(), "symlinked path") {
		t.Fatalf("expected shared runtime-directory symlink rejection, got %v", err)
	}
}

func TestSharedRuntimePathsKeepVersionsAndSourcesSeparate(t *testing.T) {
	repo := planningRepo(t)
	current, _, err := sharedRuntimePaths(repo, "v0.6.0", "current-source")
	if err != nil {
		t.Fatal(err)
	}
	olderVersion, _, err := sharedRuntimePaths(repo, "v0.5.0", "older-source")
	if err != nil {
		t.Fatal(err)
	}
	if current == olderVersion {
		t.Fatal("versioned worktrees must select separate shared runtimes")
	}
	if !strings.Contains(current, filepath.Join("v0.6.0", "current-source", platformKey())) {
		t.Fatalf("current runtime path lacks provenance: %s", current)
	}
	if !strings.Contains(olderVersion, filepath.Join("v0.5.0", "older-source", platformKey())) {
		t.Fatalf("older runtime path lacks provenance: %s", olderVersion)
	}
}
