package boatstack

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := InstallHostHooks(repo, []string{"cursor", "claude", "codex", "gemini"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostHooks(repo, []string{"cursor", "claude", "codex", "gemini"}); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(path)
	if !strings.Contains(string(value), `"theme": "kept"`) || !strings.Contains(string(value), "existing-check.sh") {
		t.Fatalf("hook merge discarded unrelated configuration: %s", value)
	}
	if err := InstallHostHooks(repo, []string{"cursor", "claude", "codex", "gemini"}); err != nil {
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
	handler := config["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	handler["timeout"] = float64(99)
	value, _ = MarshalJSON(config)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostHooks(repo, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("expected drifted fragment failure, got %v", err)
	}
}

func TestGeneratedHostHooksSatisfyHarnessShapes(t *testing.T) {
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		t.Run(host, func(t *testing.T) {
			for _, event := range hookEvents(host) {
				entry := desiredHostHookForEvent(host, event)
				if err := validateBoatstackHookEntry(host, event, entry); err != nil {
					t.Fatal(err)
				}
				if host == "claude" {
					handler := entry["hooks"].([]any)[0].(map[string]any)
					if handler["shell"] != "bash" || !strings.Contains(handler["command"].(string), "${CLAUDE_PROJECT_DIR}") {
						t.Fatalf("Claude hook does not use its documented project Bash harness: %#v", handler)
					}
				}
				if host == "codex" {
					handler := entry["hooks"].([]any)[0].(map[string]any)
					if stringValue(handler["commandWindows"]) == "" {
						t.Fatalf("Codex hook lacks commandWindows: %#v", handler)
					}
				}
				if host == "cursor" && event == "preToolUse" && !strings.Contains(stringValue(entry["matcher"]), "Write") {
					t.Fatalf("Cursor preToolUse does not supervise native writes: %#v", entry)
				}
				if host == "gemini" {
					handler := entry["hooks"].([]any)[0].(map[string]any)
					if entry["sequential"] != true || handler["timeout"] != 10000 {
						t.Fatalf("Gemini BeforeTool hook has an invalid fail-closed shape: %#v", entry)
					}
				}
			}
		})
	}
}

func TestHostHookValidationRejectsUnsupportedBoatstackFields(t *testing.T) {
	entry := desiredHostHook("claude")
	handler := entry["hooks"].([]any)[0].(map[string]any)
	handler["commandWindows"] = "unsupported"
	if err := validateBoatstackHookEntry("claude", "PreToolUse", entry); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported Claude field failure, got %v", err)
	}
}

func TestHostHookValidationRejectsWrongEventAndCursorVersion(t *testing.T) {
	codex := map[string]any{"hooks": map[string]any{"PostToolUse": []any{desiredHostHook("codex")}}}
	if err := validateHostHookConfig("codex", codex); err == nil || !strings.Contains(err.Error(), "unsupported event") {
		t.Fatalf("expected wrong Codex event failure, got %v", err)
	}
	cursor := map[string]any{"version": float64(2), "hooks": map[string]any{"beforeShellExecution": []any{desiredHostHook("cursor")}}}
	if err := validateHostHookConfig("cursor", cursor); err == nil || !strings.Contains(err.Error(), "version must be 1") {
		t.Fatalf("expected Cursor version failure, got %v", err)
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

func TestDiagnoseHookAcceptsCanonicalEventsForEveryHost(t *testing.T) {
	repo := safetyTestRepo(t)
	previous := hookDiagnosticRunner
	defer func() { hookDiagnosticRunner = previous }()
	hookDiagnosticRunner = func(_ context.Context, _ string, host string, input []byte) ([]byte, error) {
		if len(input) == 0 {
			t.Fatal("diagnostic omitted canonical input")
		}
		if host == "cursor" {
			return []byte(`{"continue":true,"permission":"allow"}`), nil
		}
		if host == "gemini" {
			return []byte(`{"decision":"allow"}`), nil
		}
		return nil, nil
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		t.Run(host, func(t *testing.T) {
			diagnostic, err := DiagnoseHook(repo, host)
			if err != nil {
				t.Fatal(err)
			}
			if diagnostic.Host != host || diagnostic.ContractStatus != "PASS" || diagnostic.LiveEventObserved {
				t.Fatalf("unexpected diagnostic: %+v", diagnostic)
			}
		})
	}
}

func TestDiagnoseHookRejectsUnsupportedHost(t *testing.T) {
	repo := safetyTestRepo(t)
	if _, err := DiagnoseHook(repo, "other"); err == nil || !strings.Contains(err.Error(), "unsupported hook host") {
		t.Fatalf("unsupported host was not rejected: %v", err)
	}
}

func TestDiagnoseHookSupportsRepositoryPathsWithSpaces(t *testing.T) {
	repo := safetyTestRepo(t)
	renamed := repo + " with spaces"
	if err := os.Rename(repo, renamed); err != nil {
		t.Fatal(err)
	}
	previous := hookDiagnosticRunner
	defer func() { hookDiagnosticRunner = previous }()
	hookDiagnosticRunner = func(_ context.Context, observedRepo, _ string, _ []byte) ([]byte, error) {
		observedInfo, observedErr := os.Stat(observedRepo)
		wantedInfo, wantedErr := os.Stat(renamed)
		if observedErr != nil || wantedErr != nil || !os.SameFile(observedInfo, wantedInfo) {
			t.Fatalf("diagnostic repo = %q, want %q", observedRepo, renamed)
		}
		return []byte(`{"continue":true,"permission":"allow"}`), nil
	}
	if _, err := DiagnoseHook(renamed, "cursor"); err != nil {
		t.Fatal(err)
	}
}

func TestHookDiagnosticRejectsMalformedAllowOutput(t *testing.T) {
	for _, test := range []struct {
		host, output string
	}{
		{"cursor", `{}`},
		{"cursor", `not-json`},
		{"claude", `{}`},
		{"codex", `unexpected`},
		{"gemini", `{}`},
	} {
		if err := validateCanonicalHookOutput(test.host, []byte(test.output)); err == nil {
			t.Fatalf("%s malformed output was accepted: %q", test.host, test.output)
		}
	}
}

func TestDiagnoseHookReportsGuardRuntimeFailures(t *testing.T) {
	repo := safetyTestRepo(t)
	previous := hookDiagnosticRunner
	defer func() { hookDiagnosticRunner = previous }()
	for _, message := range []string{"Boatstack shared runtime is missing", "Boatstack shared runtime checksum is invalid"} {
		hookDiagnosticRunner = func(_ context.Context, _ string, _ string, _ []byte) ([]byte, error) {
			return []byte(message), errors.New("exit status 2")
		}
		if _, err := DiagnoseHook(repo, "cursor"); err == nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("runtime failure %q was not diagnosed: %v", message, err)
		}
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
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
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
