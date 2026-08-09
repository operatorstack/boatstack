package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func buildLauncherTestHelper(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), helperName())
	command := exec.Command("go", "build", "-o", binary, "./cmd/boatstack-helper")
	command.Dir = "."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}
	return binary
}

func launcherTestRepository(t *testing.T) (string, string) {
	t.Helper()
	repo := planningRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module launcher-fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, BinaryPath: buildLauncherTestHelper(t), IntegrationChoice: "core", Yes: true}); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initialize Boatstack")
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "--detach", linked, "HEAD")
	return repo, linked
}

func runLauncher(t *testing.T, repo string, arguments ...string) (string, error) {
	t.Helper()
	command := launcherCommand(repo, arguments...)
	command.Dir = repo
	value, err := command.CombinedOutput()
	return string(value), err
}

func launcherCommand(repo string, arguments ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		values := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(repo, ".product-loop", "boatstack.ps1")}
		return exec.Command("powershell", append(values, arguments...)...)
	}
	return exec.Command(filepath.Join(repo, ".product-loop", "boatstack"), arguments...)
}

// control-law: tracked-launcher-selects-only-the-pinned-runtime
func TestTrackedLauncherActivatesFreshLinkedWorktreeWithoutHookTrust(t *testing.T) {
	primary, linked := launcherTestRepository(t)
	local := filepath.Join(linked, ".product-loop", "bin")
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("fresh linked worktree unexpectedly inherited local runtime: %v", err)
	}

	// A stale sibling-local helper is a counterexample only if the launcher scans
	// sibling worktrees. The exact shared-runtime path must make it irrelevant.
	primaryHelper := filepath.Join(primary, ".product-loop", "bin", helperName())
	if err := os.WriteFile(primaryHelper, []byte("#!/bin/sh\necho STALE-SIBLING\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := runLauncher(t, linked, "version")
	if err != nil {
		t.Fatalf("fresh-worktree version failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "STALE-SIBLING") || !strings.Contains(output, Version) {
		t.Fatalf("launcher selected the wrong runtime: %q", output)
	}
	for _, arguments := range [][]string{
		{"doctor", "--repo", "."},
		{"run-preflight", "--repo", ".", "--health-only", "--json"},
	} {
		if output, err := runLauncher(t, linked, arguments...); err != nil {
			t.Fatalf("launcher %v failed: %v\n%s", arguments, err, output)
		}
	}
	if err := verifyLocalRuntime(linked); err != nil {
		t.Fatalf("launcher did not leave an exact local runtime: %v", err)
	}

	// Remove the exact shared slot and prove concurrent first use serializes one
	// pinned hydration, then independently activates the worktree-local runtime.
	binary, manifest, err := sharedRuntimePaths(linked, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	backup := t.TempDir()
	backupBinary := filepath.Join(backup, helperName())
	backupManifest := filepath.Join(backup, "runtime.lock.json")
	for source, target := range map[string]string{binary: backupBinary, manifest: backupManifest} {
		value, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatal(readErr)
		}
		mode := os.FileMode(0o644)
		if source == binary {
			mode = 0o755
		}
		if writeErr := os.WriteFile(target, value, mode); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := os.RemoveAll(filepath.Dir(binary)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}
	hydrate := "mkdir -p " + quotedLiteral(t, filepath.Dir(binary)) +
		" && cp " + quotedLiteral(t, backupBinary) + " " + quotedLiteral(t, binary) +
		" && cp " + quotedLiteral(t, backupManifest) + " " + quotedLiteral(t, manifest)
	if runtime.GOOS == "windows" {
		hydrate = "$null = New-Item -ItemType Directory -Force -Path " + quotedLiteral(t, filepath.Dir(binary)) +
			"; Copy-Item -Force " + quotedLiteral(t, backupBinary) + " " + quotedLiteral(t, binary) +
			"; Copy-Item -Force " + quotedLiteral(t, backupManifest) + " " + quotedLiteral(t, manifest)
	}
	const workers = 4
	errors := make(chan string, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			command := launcherCommand(linked, "version")
			command.Dir = linked
			command.Env = append(os.Environ(), "BOATSTACK_HYDRATE_COMMAND="+hydrate)
			value, runErr := command.CombinedOutput()
			if runErr != nil || !strings.Contains(string(value), Version) {
				errors <- runErrString(runErr, value)
			}
		}()
	}
	group.Wait()
	close(errors)
	for failure := range errors {
		t.Fatalf("concurrent pinned hydration failed: %s", failure)
	}
}

func runErrString(err error, output []byte) string {
	if err == nil {
		return string(output)
	}
	return err.Error() + ": " + string(output)
}

// control-law: tracked-launcher-selects-only-the-pinned-runtime
func TestTrackedLauncherRejectsTamperedSharedRuntimeBeforeDispatch(t *testing.T) {
	_, linked := launcherTestRepository(t)
	binary, manifest, err := sharedRuntimePaths(linked, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	originalBinary, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	originalManifest, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	malformedError := "shared runtime version does not match the launcher pin"
	if runtime.GOOS == "windows" {
		malformedError = "shared runtime manifest is malformed"
	}
	tests := []struct {
		name   string
		mutate func()
		error  string
	}{
		{
			name: "binary checksum",
			mutate: func() {
				if err := os.WriteFile(binary, []byte("tampered"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			error: "shared runtime checksum is invalid",
		},
		{
			name: "malformed manifest",
			mutate: func() {
				if err := os.WriteFile(manifest, []byte("{not-json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			error: malformedError,
		},
		{
			name: "version pin mismatch",
			mutate: func() {
				value := strings.Replace(string(originalManifest), `"boatstack_version": "`+Version+`"`, `"boatstack_version": "foreign"`, 1)
				if value == string(originalManifest) {
					value = strings.Replace(string(originalManifest), `"boatstack_version":"`+Version+`"`, `"boatstack_version":"foreign"`, 1)
				}
				if value == string(originalManifest) {
					value = strings.Replace(string(originalManifest), Version, "foreign", 1)
				}
				if err := os.WriteFile(manifest, []byte(value), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			error: "shared runtime version does not match the launcher pin",
		},
		{
			name: "source pin mismatch",
			mutate: func() {
				value := strings.Replace(string(originalManifest), SourceCommit, "foreign-source", 1)
				if err := os.WriteFile(manifest, []byte(value), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			error: "shared runtime source does not match the launcher pin",
		},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name   string
			mutate func()
			error  string
		}{
			name: "symlinked binary",
			mutate: func() {
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "foreign"), binary); err != nil {
					t.Fatal(err)
				}
			},
			error: "the exact pinned shared runtime is missing or unsafe",
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_ = os.Remove(binary)
			if err := os.WriteFile(binary, originalBinary, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, originalManifest, 0o644); err != nil {
				t.Fatal(err)
			}
			test.mutate()
			command := launcherCommand(linked, "version")
			command.Dir = linked
			command.Env = append(os.Environ(), "BOATSTACK_AUTO_HYDRATE=0")
			value, runErr := command.CombinedOutput()
			output := string(value)
			if runErr == nil || !strings.Contains(output, "Boatstack runtime activation failed: "+test.error) {
				t.Fatalf("invalid runtime did not fail closed: %v\n%s", runErr, output)
			}
			if !strings.Contains(output, "Recovery:") || !strings.Contains(output, Version) {
				t.Fatalf("activation failure omitted exact pinned recovery: %s", output)
			}
			if _, statErr := os.Stat(filepath.Join(linked, ".product-loop", "bin", helperName())); !os.IsNotExist(statErr) {
				t.Fatalf("failed activation partially installed a local runtime: %v", statErr)
			}
		})
	}
}

// control-law: tracked-launcher-selects-only-the-pinned-runtime
func TestLaunchersShareExactIdentityActivationAndRecoveryContract(t *testing.T) {
	for name, value := range map[string]string{
		"POSIX":      string(launcherShellScript()),
		"PowerShell": string(launcherPowerShellScript()),
	} {
		for _, required := range []string{Version, SourceCommit, "activate-worktree-runtime", "binary_sha256", "Boatstack runtime activation failed", "BOATSTACK_MODE"} {
			if !strings.Contains(value, required) {
				t.Fatalf("%s launcher omits %q", name, required)
			}
		}
		if strings.Contains(strings.ToLower(value), "latest") || strings.Contains(value, ".product-loop/worktrees") {
			t.Fatalf("%s launcher contains a non-deterministic selection path", name)
		}
	}
}
