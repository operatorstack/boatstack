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
		".product-loop/bin/install.lock.json", ".cursor/commands/auto-plan.md", ".product-loop/hooks/guard.sh",
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
	for _, expected := range []string{"fail-closed irreversible-operation hooks verified", "least-privilege credentials"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output is missing safety guidance %q: %s", expected, output.String())
		}
	}
	for _, expected := range []string{"commit Boatstack infrastructure in its own PR", "git add -- .boatstack-project.json", "git push -u origin chore/install-boatstack", "reload Cursor, Codex, or Claude"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output is missing %q: %s", expected, output.String())
		}
	}
	configValue, _ := os.ReadFile(filepath.Join(repo, ".boatstack-project.json"))
	if strings.Contains(string(configValue), `"status"`) {
		t.Fatal("machine-local integration status leaked into repository configuration")
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
