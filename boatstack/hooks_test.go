package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostHookMergePreservesUnrelatedConfiguration(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"version":1,"theme":"kept","hooks":{"beforeShellExecution":[{"command":"./existing-check.sh"}]}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallHostHooks(repo, []string{"cursor", "claude", "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostHooks(repo, []string{"cursor", "claude", "codex"}); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(path)
	if !strings.Contains(string(value), `"theme": "kept"`) || !strings.Contains(string(value), "existing-check.sh") {
		t.Fatalf("hook merge discarded unrelated configuration: %s", value)
	}
	if err := InstallHostHooks(repo, []string{"cursor", "claude", "codex"}); err != nil {
		t.Fatalf("idempotent reinstall failed: %v", err)
	}
}

func TestHostHookMergeRejectsAmbiguousCollisionAndDrift(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := desiredHostHook("codex")
	config := map[string]any{"hooks": map[string]any{"PreToolUse": []any{entry, entry}}}
	value, _ := MarshalJSON(config)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallHostHooks(repo, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous collision, got %v", err)
	}
	config = map[string]any{"hooks": map[string]any{"PreToolUse": []any{desiredHostHook("codex")}}}
	config["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["timeout"] = float64(99)
	value, _ = MarshalJSON(config)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostHooks(repo, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("expected drifted fragment failure, got %v", err)
	}
}

func TestInstalledHookValidationAllowsTemplateMigrationButRejectsUserDrift(t *testing.T) {
	repo := t.TempDir()
	adapters := []string{"claude"}
	if err := InstallHostHooks(repo, adapters); err != nil {
		t.Fatal(err)
	}

	fragment, err := hookFragmentJSON("claude")
	if err != nil {
		t.Fatal(err)
	}
	fragment = []byte(strings.ReplaceAll(string(fragment), "Checking Boatstack execution policy", "Checking irreversible-operation policy"))
	fragmentPath := filepath.Join(repo, ".product-loop", "hooks", "claude.fragment.json")
	if err := os.MkdirAll(filepath.Dir(fragmentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragmentPath, fragment, 0o644); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(repo, ".claude", "settings.json")
	hookValue, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	hookValue = []byte(strings.ReplaceAll(string(hookValue), "Checking Boatstack execution policy", "Checking irreversible-operation policy"))
	if err := os.WriteFile(hookPath, hookValue, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CheckHostHooks(repo, adapters); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("incoming template unexpectedly accepted the installed hook: %v", err)
	}
	if err := CheckInstalledHostHooks(repo, adapters); err != nil {
		t.Fatalf("healthy installed hook blocked template migration: %v", err)
	}

	hookValue = []byte(strings.ReplaceAll(string(hookValue), `"timeout": 10`, `"timeout": 99`))
	if err := os.WriteFile(hookPath, hookValue, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckInstalledHostHooks(repo, adapters); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("user drift was not rejected against the installed fragment: %v", err)
	}
}

func TestMissingHelperLauncherFailsClosed(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	path := filepath.Join(repo, ".product-loop", "hooks", "guard.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, guardShellScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", path, "cursor")
	command.Dir = repo
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "shared runtime is missing") {
		t.Fatalf("missing helper did not fail closed: err=%v output=%s", err, output)
	}
}

func TestGuardRejectsTamperedSharedRuntimeBeforeExecution(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	repo := runtimeTestRepo(t)
	binaryPath, _, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".product-loop", "hooks", "guard.sh")
	command := exec.Command("bash", path, "claude")
	command.Dir = repo
	command.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git status --short"}}`)
	output, runErr := command.CombinedOutput()
	if runErr == nil || !strings.Contains(string(output), "checksum is invalid") {
		t.Fatalf("tampered shared helper was not denied before execution: err=%v output=%s", runErr, output)
	}
}

func TestHookFragmentsAreValidJSON(t *testing.T) {
	for _, host := range []string{"cursor", "claude", "codex"} {
		value, err := hookFragmentJSON(host)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(value, &decoded); err != nil {
			t.Fatalf("%s fragment is invalid JSON: %v", host, err)
		}
		if !strings.Contains(string(value), hookCommandMarker) {
			t.Fatalf("%s fragment lacks Boatstack marker", host)
		}
	}
}
