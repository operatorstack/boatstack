package runtime

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyFlowProjectionAtRevisionBindsActiveBytesToWorkspaceBase(t *testing.T) {
	// control-law: workspace-base-contains-the-active-flow-projection
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"init", "-q"}, {"config", "user.name", "Boatstack Tests"}, {"config", "user.email", "boatstack@example.invalid"}} {
		command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
		if output, commandErr := command.CombinedOutput(); commandErr != nil {
			t.Fatalf("git %v: %v\n%s", arguments, commandErr, output)
		}
	}
	path := filepath.Join(repository, "generated.txt")
	if err := os.WriteFile(path, []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"add", "generated.txt"}, {"commit", "-q", "-m", "projection"}} {
		command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
		if output, commandErr := command.CombinedOutput(); commandErr != nil {
			t.Fatalf("git %v: %v\n%s", arguments, commandErr, output)
		}
	}
	if err := VerifyFlowProjectionAtRevision(context.Background(), repository, "HEAD", []string{"generated.txt"}); err != nil {
		t.Fatalf("exact projection rejected: %v", err)
	}
	if err := os.WriteFile(path, []byte("regenerated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFlowProjectionAtRevision(context.Background(), repository, "HEAD", []string{"generated.txt"}); err == nil || !strings.Contains(err.Error(), "generated.txt differs") {
		t.Fatalf("uncommitted projection result = %v", err)
	}
}

func resolvedTemporaryRepository(t *testing.T) string {
	t.Helper()
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestSameProjectionStateUsesHostPermissionSemantics(t *testing.T) {
	// control-law: generated-output-drift-compares-only-host-enforceable-state
	committed := projectionSnapshot{exists: true, content: []byte("skill"), mode: 0o644}
	groupWritable := projectionSnapshot{exists: true, content: []byte("skill"), mode: 0o666}
	if got := sameProjectionState(committed, groupWritable); got != (runtime.GOOS == "windows") {
		t.Fatalf("host permission equivalence = %v on %s", got, runtime.GOOS)
	}
	readOnly := projectionSnapshot{exists: true, content: []byte("skill"), mode: 0o444}
	if sameProjectionState(committed, readOnly) {
		t.Fatal("writable and read-only projection states compare equal")
	}
	changed := projectionSnapshot{exists: true, content: []byte("changed"), mode: committed.mode}
	if sameProjectionState(committed, changed) {
		t.Fatal("changed projection bytes compare equal")
	}
}

func TestFlowProjectionRejectsRepositoryParentSymlink(t *testing.T) {
	// control-law: generated-projections-never-traverse-repository-symlinks
	repository := resolvedTemporaryRepository(t)
	external := resolvedTemporaryRepository(t)
	if err := os.Symlink(external, filepath.Join(repository, ".agents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	err := ApplyFlowProjection(repository, []ProjectionWrite{{Path: target, Content: []byte("generated"), Mode: 0o644}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "repository symlink") {
		t.Fatalf("symlink projection result = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(external, "skills", "product-delivery-run", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatalf("projection escaped repository: %v", statErr)
	}
}

func TestFlowProjectionRejectsParentSymlinkSwapBeforeStaging(t *testing.T) {
	// control-law: repository-root-capability-remains-bound-through-filesystem-mutation
	repository := resolvedTemporaryRepository(t)
	external := resolvedTemporaryRepository(t)
	parent := filepath.Join(repository, ".agents")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "skills", "product-delivery-run", "SKILL.md")
	swapped := false
	err := applyFlowProjection(repository,
		[]ProjectionWrite{{Path: target, Content: []byte("generated"), Mode: 0o644}}, nil, nil,
		projectionHooks{beforeStage: func(path string) {
			if swapped || path != target {
				return
			}
			swapped = true
			if err := os.Rename(parent, parent+"-original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, parent); err != nil {
				t.Fatal(err)
			}
		}})
	if err == nil {
		t.Fatal("parent symlink swap was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(external, "skills", "product-delivery-run", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatalf("projection escaped through swapped parent: %v", statErr)
	}
}

func TestFlowProjectionStagesAllOutputsBeforeReplacement(t *testing.T) {
	// control-law: artifact-and-skills-replace-as-one-recoverable-projection
	if runtime.GOOS == "windows" {
		t.Skip("directory permission failure is not portable to Windows")
	}
	repository := resolvedTemporaryRepository(t)
	artifactDirectory := filepath.Join(repository, ".boatstack", "flows")
	artifactPath := filepath.Join(artifactDirectory, "product-delivery.flow.ir.json")
	retiredPath := filepath.Join(repository, ".agents", "skills", "old-run", "SKILL.md")
	newSkillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	for path, content := range map[string][]byte{artifactPath: []byte("old artifact"), retiredPath: []byte("old skill")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(artifactDirectory, 0o555); err != nil {
		t.Fatal(err)
	}
	writes := []ProjectionWrite{
		{Path: newSkillPath, Content: []byte("new skill"), Mode: 0o644},
		{Path: artifactPath, Content: []byte("new artifact"), Mode: 0o644, ExpectedPreviousSHA256: projectionDigest([]byte("old artifact"))},
	}
	removals := []ProjectionRemoval{{Path: retiredPath, ExpectedSHA256: projectionDigest([]byte("old skill")), AllowMissing: true}}
	err := ApplyFlowProjection(repository, writes, removals, nil)
	if restoreErr := os.Chmod(artifactDirectory, 0o755); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Skip("filesystem did not enforce the staging permission failure")
	}
	for path, expected := range map[string]string{artifactPath: "old artifact", retiredPath: "old skill"} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != expected {
			t.Fatalf("prior projection %s changed: %q, %v", path, actual, readErr)
		}
	}
	if _, statErr := os.Stat(newSkillPath); !os.IsNotExist(statErr) {
		t.Fatalf("new skill became visible before artifact staging: %v", statErr)
	}
	if err := ApplyFlowProjection(repository, writes, removals, nil); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{artifactPath: "new artifact", newSkillPath: "new skill"} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != expected {
			t.Fatalf("committed projection %s = %q, %v", path, actual, readErr)
		}
	}
	if _, statErr := os.Stat(retiredPath); !os.IsNotExist(statErr) {
		t.Fatalf("retired skill remains after successful retry: %v", statErr)
	}
}

func TestFlowProjectionRefusesConcurrentCompiler(t *testing.T) {
	// control-law: concurrent-compilers-cannot-interleave-one-projection
	repository := resolvedTemporaryRepository(t)
	lock, err := acquireProjectionLock(repository)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	err = ApplyFlowProjection(repository, []ProjectionWrite{{Path: target, Content: []byte("skill"), Mode: 0o644}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_BUSY") {
		t.Fatalf("concurrent projection result = %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("refused compiler changed projection: %v", statErr)
	}
	lock.release()
	if err := ApplyFlowProjection(repository, []ProjectionWrite{{Path: target, Content: []byte("skill"), Mode: 0o644}}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFlowProjectionLockNamespaceIgnoresUserCache(t *testing.T) {
	// control-law: every-compiler-for-one-worktree-uses-one-lock-namespace
	if os.Getenv("BOATSTACK_FLOW_LOCK_CHILD") == "1" {
		lock, err := acquireProjectionLock(os.Getenv("BOATSTACK_FLOW_LOCK_REPOSITORY"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.release()
		if _, err := os.Stdout.WriteString("ready\n"); err != nil {
			t.Fatal(err)
		}
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	repository := resolvedTemporaryRepository(t)
	command := exec.Command(os.Args[0], "-test.run=^TestFlowProjectionLockNamespaceIgnoresUserCache$")
	command.Env = append(os.Environ(),
		"BOATSTACK_FLOW_LOCK_CHILD=1",
		"BOATSTACK_FLOW_LOCK_REPOSITORY="+repository,
		"XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "child-cache"),
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("lock child readiness = %q, %v", line, err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "parent-cache"))
	target := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	err = ApplyFlowProjection(repository, []ProjectionWrite{{Path: target, Content: []byte("skill"), Mode: 0o644}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_BUSY") {
		t.Fatalf("cross-cache concurrent projection result = %v", err)
	}
	_, _ = stdin.Write([]byte("release\n"))
	_ = stdin.Close()
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestFlowProjectionRevalidatesAuthorizationUnderLock(t *testing.T) {
	// control-law: projection-authorization-remains-byte-exact-through-commit
	repository := resolvedTemporaryRepository(t)
	retiredPath := filepath.Join(repository, ".agents", "skills", "old-run", "SKILL.md")
	artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	if err := os.MkdirAll(filepath.Dir(retiredPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(retiredPath, []byte("generated old skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	removal := ProjectionRemoval{Path: retiredPath, ExpectedSHA256: projectionDigest([]byte("generated old skill")), AllowMissing: true}
	err := applyFlowProjection(repository,
		[]ProjectionWrite{{Path: artifactPath, Content: []byte("new artifact"), Mode: 0o644, PublishLast: true}},
		[]ProjectionRemoval{removal}, nil, projectionHooks{afterValidation: func() {
			if err := os.WriteFile(retiredPath, []byte("concurrent user edit"), 0o644); err != nil {
				t.Fatal(err)
			}
		}})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_INPUT_CHANGED") {
		t.Fatalf("authorization drift result = %v", err)
	}
	actual, readErr := os.ReadFile(retiredPath)
	if readErr != nil || string(actual) != "concurrent user edit" {
		t.Fatalf("concurrent edit changed: %q, %v", actual, readErr)
	}
	if _, statErr := os.Stat(artifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("authorization drift published artifact: %v", statErr)
	}
}

func TestFlowProjectionRefusesUnmanagedOrChangedWriteTargets(t *testing.T) {
	// control-law: generated-output-replacement-requires-prior-artifact-ownership-or-exact-crash-residue
	for _, test := range []struct {
		name          string
		initial       []byte
		afterValidate []byte
		wantError     bool
	}{
		{name: "unmanaged", initial: []byte("user skill"), wantError: true},
		{name: "concurrent", afterValidate: []byte("concurrent skill"), wantError: true},
		{name: "exact-crash-residue", initial: []byte("generated skill")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := resolvedTemporaryRepository(t)
			skillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
			artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
			if test.initial != nil {
				if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(skillPath, test.initial, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := applyFlowProjection(repository, []ProjectionWrite{
				{Path: skillPath, Content: []byte("generated skill"), Mode: 0o644},
				{Path: artifactPath, Content: []byte("artifact"), Mode: 0o644, PublishLast: true},
			}, nil, nil, projectionHooks{afterValidation: func() {
				if test.afterValidate == nil {
					return
				}
				if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(skillPath, test.afterValidate, 0o644); err != nil {
					t.Fatal(err)
				}
			}})
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_WRITE_UNAUTHORIZED") {
					t.Fatalf("write authorization result = %v", err)
				}
				expected := test.initial
				if test.afterValidate != nil {
					expected = test.afterValidate
				}
				if actual, readErr := os.ReadFile(skillPath); readErr != nil || string(actual) != string(expected) {
					t.Fatalf("unowned skill changed: %q, %v", actual, readErr)
				}
				if _, statErr := os.Stat(artifactPath); !os.IsNotExist(statErr) {
					t.Fatalf("unauthorized write published artifact: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if actual, readErr := os.ReadFile(artifactPath); readErr != nil || string(actual) != "artifact" {
				t.Fatalf("crash residue artifact = %q, %v", actual, readErr)
			}
		})
	}
}

func TestFlowProjectionRevalidatesCompileInputsUnderLock(t *testing.T) {
	// control-law: artifact-publication-remains-bound-to-exact-source-and-lock-bytes
	repository := resolvedTemporaryRepository(t)
	sourcePath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ts")
	artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("source A"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := applyFlowProjection(repository,
		[]ProjectionWrite{{Path: artifactPath, Content: []byte("artifact from source A"), Mode: 0o644, PublishLast: true}}, nil,
		[]ProjectionExpectation{{Path: sourcePath, Exists: true, ExpectedSHA256: projectionDigest([]byte("source A"))}}, projectionHooks{afterRemovals: func() {
			if err := os.WriteFile(sourcePath, []byte("source B"), 0o644); err != nil {
				t.Fatal(err)
			}
		}})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_INPUT_CHANGED") {
		t.Fatalf("source drift result = %v", err)
	}
	if _, statErr := os.Stat(artifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("source drift published artifact: %v", statErr)
	}
}

func TestFlowProjectionRevalidatesGeneratedOutputsBeforeArtifactPublication(t *testing.T) {
	// control-law: artifact-publication-binds-the-exact-generated-output-bytes
	repository := resolvedTemporaryRepository(t)
	skillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	err := applyFlowProjection(repository, []ProjectionWrite{
		{Path: skillPath, Content: []byte("generated skill"), Mode: 0o644},
		{Path: artifactPath, Content: []byte("artifact"), Mode: 0o644, PublishLast: true},
	}, nil, nil, projectionHooks{afterRemovals: func() {
		if err := os.WriteFile(skillPath, []byte("concurrent edit"), 0o644); err != nil {
			t.Fatal(err)
		}
	}})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_OUTPUT_CHANGED") {
		t.Fatalf("generated output drift result = %v", err)
	}
	if actual, readErr := os.ReadFile(skillPath); readErr != nil || string(actual) != "concurrent edit" {
		t.Fatalf("concurrent skill edit changed: %q, %v", actual, readErr)
	}
	if _, statErr := os.Stat(artifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("generated output drift published artifact: %v", statErr)
	}
}

func TestFlowProjectionRecoversAfterCrashBeforeArtifactPublication(t *testing.T) {
	// control-law: retired-skills-precede-artifact-publication-and-retry-recovers-a-crash
	if os.Getenv("BOATSTACK_FLOW_CRASH_CHILD") == "1" {
		repository := os.Getenv("BOATSTACK_FLOW_CRASH_REPOSITORY")
		artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
		retiredPath := filepath.Join(repository, ".agents", "skills", "old-run", "SKILL.md")
		newSkillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
		_ = applyFlowProjection(repository, []ProjectionWrite{
			{Path: newSkillPath, Content: []byte("new skill"), Mode: 0o644},
			{Path: artifactPath, Content: []byte("new artifact"), Mode: 0o644, ExpectedPreviousSHA256: projectionDigest([]byte("old artifact")), PublishLast: true},
		}, []ProjectionRemoval{{Path: retiredPath, ExpectedSHA256: projectionDigest([]byte("old skill")), AllowMissing: true}}, nil, projectionHooks{afterRemovals: func() {
			os.Exit(79)
		}})
		os.Exit(78)
	}

	repository := resolvedTemporaryRepository(t)
	artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	retiredPath := filepath.Join(repository, ".agents", "skills", "old-run", "SKILL.md")
	newSkillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	for path, content := range map[string][]byte{artifactPath: []byte("old artifact"), retiredPath: []byte("old skill")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(os.Args[0], "-test.run=^TestFlowProjectionRecoversAfterCrashBeforeArtifactPublication$")
	command.Env = append(os.Environ(), "BOATSTACK_FLOW_CRASH_CHILD=1", "BOATSTACK_FLOW_CRASH_REPOSITORY="+repository)
	err := command.Run()
	exitErr, exited := err.(*exec.ExitError)
	if !exited || exitErr.ExitCode() != 79 {
		t.Fatalf("crash fixture result = %v", err)
	}
	if actual, err := os.ReadFile(artifactPath); err != nil || string(actual) != "old artifact" {
		t.Fatalf("artifact published before retirement boundary: %q, %v", actual, err)
	}
	if _, err := os.Stat(retiredPath); !os.IsNotExist(err) {
		t.Fatalf("retired skill survived crash boundary: %v", err)
	}

	writes := []ProjectionWrite{
		{Path: newSkillPath, Content: []byte("new skill"), Mode: 0o644},
		{Path: artifactPath, Content: []byte("new artifact"), Mode: 0o644, ExpectedPreviousSHA256: projectionDigest([]byte("old artifact")), PublishLast: true},
	}
	removals := []ProjectionRemoval{{Path: retiredPath, ExpectedSHA256: projectionDigest([]byte("old skill")), AllowMissing: true}}
	if err := ApplyFlowProjection(repository, writes, removals, nil); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.ReadFile(artifactPath); err != nil || string(actual) != "new artifact" {
		t.Fatalf("retry artifact = %q, %v", actual, err)
	}
	if actual, err := os.ReadFile(newSkillPath); err != nil || string(actual) != "new skill" {
		t.Fatalf("retry skill = %q, %v", actual, err)
	}
}
