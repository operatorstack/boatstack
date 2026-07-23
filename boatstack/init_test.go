package boatstack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeFreeInit(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	packageJSON := `{"scripts":{"test":"node --test"}}`
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(packageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &output}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		".boatstack-project.json", ".product-loop/project.json", ".product-loop/generated.lock.json",
		".product-loop/bin/install.lock.json", ".cursor/commands/auto-plan.md", ".claude/skills/auto-plan/SKILL.md", ".product-loop/hooks/guard.sh",
		".cursor/hooks.json", ".claude/settings.json", ".codex/hooks.json",
	} {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(path))) {
			t.Fatalf("init did not create %s", path)
		}
	}
	binaryName := "boatstack-helper"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	if !fileExists(filepath.Join(repo, ".product-loop", "bin", binaryName)) {
		t.Fatal("init did not install the project-local helper")
	}
	if !strings.Contains(output.String(), "PASS: Boatstack core installed without a language runtime") {
		t.Fatalf("unexpected init output: %s", output.String())
	}
	for _, expected := range []string{"generated irreversible-operation hook contracts verified", "Host activation remains an operator-visible boundary", "least-privilege credentials"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output is missing safety guidance %q: %s", expected, output.String())
		}
	}
	for _, expected := range []string{"commit Boatstack infrastructure in its own PR", ".boatstack-project.json", "git push -u origin chore/install-boatstack", "reload Cursor, Codex, or Claude"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output is missing %q: %s", expected, output.String())
		}
	}
	for _, expected := range []string{
		"Boatstack start command by host:",
		"Codex: $boatstack next",
		"Claude Code: /auto-plan",
		"Cursor: /auto-plan",
		"Codex: $boatstack auto-plan",
		"reload Claude Code before using its slash commands",
		"trust this exact linked-worktree path",
		"beforeShellExecution and beforeMCPExecution",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output is missing host start guidance %q: %s", expected, output.String())
		}
	}
	configValue, _ := os.ReadFile(filepath.Join(repo, ".boatstack-project.json"))
	if strings.Contains(string(configValue), `"status"`) {
		t.Fatal("machine-local integration status leaked into repository configuration")
	}
	if !strings.Contains(string(configValue), `"maintain_changelog": false`) {
		t.Fatal("fresh initialization did not default changelog maintenance off")
	}
	installValue, _ := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if !strings.Contains(string(installValue), `"binary_sha256"`) || !strings.Contains(string(installValue), `"integrations"`) {
		t.Fatal("local install lock did not record binary and integration state")
	}
	var lock installLock
	if err := json.Unmarshal(installValue, &lock); err != nil {
		t.Fatal(err)
	}
	wantBinaryPath := filepath.ToSlash(filepath.Join(".product-loop", "bin", binaryName))
	if lock.BinaryPath != wantBinaryPath {
		t.Fatalf("install lock binary_path = %q, want repository-relative %q", lock.BinaryPath, wantBinaryPath)
	}
}

func TestInitFreshThirdPartyPythonRepositoryWithValidConfig(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(repo, "pyproject.toml"), []byte("[project]\nname = \"hatch-fixture\"\n[tool.pytest.ini_options]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.Project.Name = "hatch-fixture"
	config.Project.Commands["test"] = "python -m pytest"
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".boatstack-project.json")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		".product-loop/project.json", ".product-loop/generated.lock.json",
		".product-loop/bin/install.lock.json", ".cursor/commands/auto-plan.md",
	} {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(relative))) {
			t.Fatalf("fresh third-party init did not create %s", relative)
		}
	}
	if err := Doctor(repo); err != nil {
		t.Fatalf("fresh third-party installation is not controller-ready: %v", err)
	}
}

func TestInitDoesNotResetAnExistingInstallation(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"scripts":{"test":"node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	configBefore, _ := os.ReadFile(filepath.Join(repo, ".boatstack-project.json"))
	err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("existing installation was reinitialized: %v", err)
	}
	configAfter, _ := os.ReadFile(filepath.Join(repo, ".boatstack-project.json"))
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("failed reinstall changed project configuration")
	}
}

func TestInitRollsBackRepositoryWhenPostInstallVerificationFails(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	packagePath := filepath.Join(repo, "package.json")
	original := []byte(`{"scripts":{"test":"node --test"}}`)
	if err := os.WriteFile(packagePath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	oldDoctor := initDoctor
	initDoctor = func(string) error { return fmt.Errorf("injected verification failure") }
	defer func() { initDoctor = oldDoctor }()
	err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "injected verification failure") {
		t.Fatalf("expected injected initialization failure, got %v", err)
	}
	value, readErr := os.ReadFile(packagePath)
	if readErr != nil || !bytes.Equal(value, original) {
		t.Fatalf("rollback did not restore original repository file: %v", readErr)
	}
	info, statErr := os.Stat(packagePath)
	if statErr != nil {
		t.Fatalf("rollback did not restore original file metadata: %v", statErr)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("rollback did not restore original file mode: %v %#o", statErr, info.Mode().Perm())
	}
	for _, relative := range []string{".boatstack-project.json", ".product-loop", ".cursor", ".claude", ".codex"} {
		if _, statErr := os.Lstat(filepath.Join(repo, relative)); !os.IsNotExist(statErr) {
			t.Fatalf("partial installation state remains at %s: %v", relative, statErr)
		}
	}
}

func TestInitRollsBackAtEveryCommitStage(t *testing.T) {
	stages := []string{"config-written", "export-written", "hooks-written", "helper-written", "install-lock-written"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			repo := t.TempDir()
			if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			original := []byte(`{"scripts":{"test":"node --test"}}`)
			packagePath := filepath.Join(repo, "package.json")
			if err := os.WriteFile(packagePath, original, 0o640); err != nil {
				t.Fatal(err)
			}
			oldCheckpoint := initCheckpoint
			initCheckpoint = func(current string) error {
				if current == stage {
					return fmt.Errorf("injected %s failure", stage)
				}
				return nil
			}
			defer func() { initCheckpoint = oldCheckpoint }()
			err := RunInit(InitOptions{Repo: repo, IntegrationChoice: "core", Yes: true, Output: &bytes.Buffer{}})
			if err == nil || !strings.Contains(err.Error(), "injected "+stage+" failure") {
				t.Fatalf("expected injected %s failure, got %v", stage, err)
			}
			value, readErr := os.ReadFile(packagePath)
			if readErr != nil || !bytes.Equal(value, original) {
				t.Fatalf("rollback at %s did not restore original file: %v", stage, readErr)
			}
			info, statErr := os.Stat(packagePath)
			if statErr != nil {
				t.Fatalf("rollback at %s did not restore file metadata: %v", stage, statErr)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
				t.Fatalf("rollback at %s did not restore file mode: %v", stage, statErr)
			}
			for _, relative := range []string{".boatstack-project.json", ".product-loop", ".cursor", ".claude", ".codex"} {
				if _, statErr := os.Lstat(filepath.Join(repo, relative)); !os.IsNotExist(statErr) {
					t.Fatalf("partial state remains after %s at %s: %v", stage, relative, statErr)
				}
			}
		})
	}
}

func TestDetectTestCommandCoversCheckScriptAndPythonProjects(t *testing.T) {
	for name, setup := range map[string]struct {
		files map[string]string
		want  string
	}{
		"check script":  {files: map[string]string{"scripts/check.sh": "#!/bin/sh\n"}, want: "bash scripts/check.sh"},
		"uv pytest":     {files: map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n", "uv.lock": ""}, want: "uv run pytest"},
		"poetry pytest": {files: map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n", "poetry.lock": ""}, want: "poetry run pytest"},
		"plain pytest":  {files: map[string]string{"pytest.ini": "[pytest]\n"}, want: "python -m pytest"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			for relative, value := range setup.files {
				path := filepath.Join(repo, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := detectTestCommand(repo); got != setup.want {
				t.Fatalf("detectTestCommand() = %q, want %q", got, setup.want)
			}
		})
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "pyproject.toml"), []byte("[project]\nname = \"fixture\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectTestCommand(repo); got != "" {
		t.Fatalf("plain pyproject.toml invented a test command: %q", got)
	}
}

func TestGStackMissingPrerequisiteIsPartialNotCoreFailure(t *testing.T) {
	oldLookPath := lookPath
	defer func() { lookPath = oldLookPath }()
	lookPath = func(name string) (string, error) {
		if name == "bun" {
			return "", fmt.Errorf("missing")
		}
		return oldLookPath(name)
	}
	state := installGStack([]string{"codex"})
	if state.Status != "partial" || !strings.Contains(state.Detail, "bun") {
		t.Fatalf("expected honest partial integration result, got %#v", state)
	}
}

func TestRequestedIntegrationChoices(t *testing.T) {
	for choice, expected := range map[string][2]bool{
		"core": {false, false}, "gstack": {true, false}, "spec-kit": {false, true}, "both": {true, true},
	} {
		gstack, specKit, err := RequestedIntegrations(choice)
		if err != nil || [2]bool{gstack, specKit} != expected {
			t.Fatalf("choice %s: %v %v %v", choice, gstack, specKit, err)
		}
	}
}
