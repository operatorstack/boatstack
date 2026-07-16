package boatstack

import (
	"bytes"
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
		".product-loop/bin/install.lock.json", ".cursor/commands/auto-plan.md",
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
	configValue, _ := os.ReadFile(filepath.Join(repo, ".boatstack-project.json"))
	if strings.Contains(string(configValue), `"status"`) {
		t.Fatal("machine-local integration status leaked into repository configuration")
	}
	installValue, _ := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if !strings.Contains(string(installValue), `"binary_sha256"`) || !strings.Contains(string(installValue), `"integrations"`) {
		t.Fatal("local install lock did not record binary and integration state")
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
