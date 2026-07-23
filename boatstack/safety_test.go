package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func safetyTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Safety fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func writeValidSavedFeaturePlan(t *testing.T, repo, feature string) string {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "source-plan.md"), []byte("# Source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "spec.md"), []byte("# Feature spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = feature
	planPath := filepath.Join(directory, "plan.md")
	writeMarkdownPlan(t, planPath, plan, true)
	return planPath
}

func TestIrreversibleCommandCorpusIsDenied(t *testing.T) {
	repo := safetyTestRepo(t)
	cases := map[string]string{
		"schema drop":        `psql -c "DROP SCHEMA public CASCADE"`,
		"truncate":           `psql -c "TRUNCATE TABLE accounts"`,
		"unbounded delete":   `psql -c "DELETE FROM accounts"`,
		"multiline update":   "psql <<'SQL'\nUPDATE accounts\nSET active = false;\nSQL",
		"database reset":     `supabase db reset`,
		"recursive root":     `rm -rf /`,
		"wildcard deletion":  `rm -rf build/*`,
		"compound pipeline":  `rg reset scripts | rm -rf .`,
		"subshell":           `echo $(git reset --hard HEAD~1)`,
		"environment prefix": `TARGET=dev sh -c 'DROP SCHEMA public CASCADE'`,
		"hard reset":         `git reset --hard HEAD~1`,
		"force push":         `git push --force origin main`,
		"cloud deletion":     `gcloud sql instances delete primary`,
		"namespace deletion": `kubectl delete namespace production`,
		"volume deletion":    `docker volume rm data-volume`,
		"backup deletion":    `aws rds delete-db-snapshot --db-snapshot-identifier backup-1`,
		"powershell":         `Remove-Item -Recurse -Force $HOME`,
	}
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			if findings := ClassifyCommand(repo, command); len(findings) == 0 {
				t.Fatalf("dangerous command was allowed: %s", command)
			}
		})
	}
}

func TestWorkspaceSyncIsTheOnlyAllowedRepositoryAlignmentCommand(t *testing.T) {
	repo := safetyTestRepo(t)
	writeValidSavedFeaturePlan(t, repo, "pending-feature")
	helper := filepath.Join(repo, ".product-loop", "bin", helperName())
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := ".product-loop/bin/" + helperName() + " workspace-sync --repo . --branch main --source origin/main"
	if findings := ClassifyCommand(repo, command); len(findings) != 0 {
		t.Fatalf("exact project-local workspace sync was denied: %#v", findings)
	}
	for _, command := range []string{
		"boatstack-helper workspace-sync --repo . --branch main --source origin/main",
		"/tmp/boatstack-helper workspace-sync --repo . --branch main --source origin/main",
		".product-loop/bin/" + helperName() + " workspace-sync --repo /tmp --branch main --source origin/main",
	} {
		findings := ClassifyCommand(repo, command)
		if len(findings) == 0 || findings[0].Category != "workspace-sync-bypass" {
			t.Fatalf("unverified workspace sync was allowed: %s %#v", command, findings)
		}
	}
	raw := ClassifyCommand(repo, "git reset --hard origin/main")
	if len(raw) == 0 || raw[0].Category != "git-history-destruction" {
		t.Fatalf("raw hard reset was not denied: %#v", raw)
	}
	message := denialMessage("cursor", raw[0])
	for _, expected := range []string{"project-local workspace-sync", "do not scan delivery artifacts", "do not", "retry"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("hard-reset denial omitted %q: %s", expected, message)
		}
	}
}

func TestInvokedSymlinkFailsClosed(t *testing.T) {
	repo := safetyTestRepo(t)
	target := filepath.Join(repo, "target.py")
	link := filepath.Join(repo, "run.py")
	if err := os.WriteFile(target, []byte("print('safe')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	findings := ClassifyCommand(repo, "python run.py")
	if len(findings) == 0 || findings[0].Category != "symlink-entrypoint" {
		t.Fatalf("invoked symlink did not fail closed: %#v", findings)
	}
}

func TestSafeDiagnosticsAndFixForwardCommandsRemainAllowed(t *testing.T) {
	repo := safetyTestRepo(t)
	safeScript := filepath.Join(repo, "scripts", "apply_schema.py")
	if err := os.MkdirAll(filepath.Dir(safeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/safety/safe_apply.py.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(safeScript, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	commands := []string{
		`rg -n "reset-public|DROP SCHEMA public CASCADE" scripts/apply_schema.py`,
		`git diff -- scripts/apply_schema.py | head -20`,
		`python scripts/apply_schema.py --dry-run`,
		`psql -c "SELECT current_database()"`,
		`.product-loop/bin/boatstack-helper check-update --repo . --force`,
		`psql -c "UPDATE accounts SET active = false WHERE id = 7"`,
	}
	for _, command := range commands {
		if findings := ClassifyCommand(repo, command); len(findings) != 0 {
			t.Fatalf("safe command %q was denied: %#v", command, findings)
		}
	}
}

func TestAPIMethodNamesDoNotMasqueradeAsSQLMutations(t *testing.T) {
	repo := safetyTestRepo(t)
	path := filepath.Join(repo, "api", "main.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	value := []byte("" +
		"from fastapi import FastAPI\n" +
		"from fastapi.middleware.cors import CORSMiddleware\n\n" +
		"app = FastAPI()\n" +
		"app.add_middleware(\n" +
		"    CORSMiddleware,\n" +
		"    allow_methods=[\"GET\", \"POST\", \"PUT\", \"PATCH\", \"DELETE\"],\n" +
		")\n" +
		"metadata.update({\"supported_method\": \"DELETE\"})\n")
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := ClassifyCommand(repo, "python api/main.py"); len(findings) != 0 {
		t.Fatalf("ordinary API method configuration was denied: %#v", findings)
	}

	runGit(t, repo, "add", "api/main.py")
	runGit(t, repo, "commit", "-m", "add API")
	runGit(t, repo, "switch", "-c", "feat/cors")
	if err := os.WriteFile(path, append(value, []byte("# localhost ports 3000-3010\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := CheckRepositorySafety(repo)
	if err != nil || report.Status != "PASS" {
		t.Fatalf("CORS operational diff was denied: %#v %v", report, err)
	}
}

func TestInvokedRepositoryScriptIsInspected(t *testing.T) {
	repo := safetyTestRepo(t)
	path := filepath.Join(repo, "scripts", "apply_schema.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	unsafe, err := os.ReadFile("testdata/safety/unsafe_apply.py.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, unsafe, 0o644); err != nil {
		t.Fatal(err)
	}
	findings := ClassifyCommand(repo, "python scripts/apply_schema.py")
	if len(findings) == 0 || findings[0].Source != "scripts/apply_schema.py" {
		t.Fatalf("indirect destructive script was not denied with a repository-relative source: %#v", findings)
	}
	if strings.Contains(findings[0].Source, repo) {
		t.Fatal("guard leaked an absolute repository path")
	}
}

func TestMCPAndMalformedEventsFailClosedWithoutEchoingSecrets(t *testing.T) {
	repo := safetyTestRepo(t)
	event := []byte(`{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary","token":"secret-value"}}`)
	cursorOutput, cursorDenied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: event})
	if !cursorDenied || !strings.Contains(string(cursorOutput), `"permission":"deny"`) {
		t.Fatalf("Cursor MCP deletion was not denied: %s", cursorOutput)
	}
	for _, host := range []string{"claude", "codex"} {
		preToolEvent := []byte(`{"hook_event_name":"PreToolUse","tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary","token":"secret-value"}}`)
		output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: preToolEvent})
		if !denied || !strings.Contains(string(output), `"permissionDecision":"deny"`) {
			t.Fatalf("%s MCP deletion was not denied: %s", host, output)
		}
		if strings.Contains(string(output), "secret-value") || strings.Contains(string(output), "primary") {
			t.Fatalf("%s denial leaked tool arguments: %s", host, output)
		}
	}
	output, denied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: []byte(`{"bad":true}`)})
	if !denied || !strings.Contains(string(output), `"permission":"deny"`) {
		t.Fatalf("malformed Cursor event did not fail closed: %s", output)
	}
}

func TestHostContractsNormalizeCanonicalInputs(t *testing.T) {
	repo := safetyTestRepo(t)
	cases := []struct {
		name, host string
		input      string
		denied     bool
		output     string
	}{
		{"cursor shell allow", "cursor", `{"hook_event_name":"beforeShellExecution","command":"git status --short"}`, false, `"permission":"allow"`},
		{"cursor shell deny", "cursor", `{"hook_event_name":"beforeShellExecution","command":"git reset --hard HEAD~1"}`, true, `"permission":"deny"`},
		{"cursor MCP object deny ignores transport command", "cursor", `{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary"},"command":"docker"}`, true, `"permission":"deny"`},
		{"cursor MCP string deny ignores transport URL", "cursor", `{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__cloud__delete_database","tool_input":"{\"database\":\"primary\"}","url":"https://example.invalid/mcp"}`, true, `"permission":"deny"`},
		{"cursor legacy shell allow", "cursor", `{"command":"git status --short"}`, false, `"permission":"allow"`},
		{"cursor legacy ambiguous deny", "cursor", `{"command":"docker","tool_name":"mcp__status__read","tool_input":{}}`, true, `"permission":"deny"`},
		{"claude allow", "claude", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, false, ""},
		{"claude deny", "claude", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git reset --hard HEAD~1"}}`, true, `"permissionDecision":"deny"`},
		{"cursor Claude compatibility allow", "claude", `{"hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"git status --short"}}`, false, ""},
		{"cursor Claude compatibility deny", "claude", `{"hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"git reset --hard HEAD~1"}}`, true, `"permissionDecision":"deny"`},
		{"codex allow", "codex", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, false, ""},
		{"codex deny", "codex", `{"hook_event_name":"PreToolUse","tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary"}}`, true, `"permissionDecision":"deny"`},
		{"codex lowercase PreToolUse deny", "codex", `{"hook_event_name":"preToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, true, `"permissionDecision":"deny"`},
		{"malformed post deny", "codex", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, true, `"permissionDecision":"deny"`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, denied := HookDecision(SafetyHookOptions{Host: test.host, Repo: repo, Input: []byte(test.input)})
			if denied != test.denied {
				t.Fatalf("denied = %t, want %t; output=%s", denied, test.denied, output)
			}
			if test.output != "" && !strings.Contains(string(output), test.output) {
				t.Fatalf("output %s does not contain %s", output, test.output)
			}
			if test.output == "" && len(output) != 0 {
				t.Fatalf("expected empty allow output, got %s", output)
			}
		})
	}
}

func TestMalformedHostPayloadsDenyWithoutLeakingInput(t *testing.T) {
	repo := safetyTestRepo(t)
	for _, test := range []struct{ host, input, reason string }{
		{"cursor", ``, "empty-input"},
		{"cursor", `{`, "invalid-json"},
		{"cursor", `{"hook_event_name":"beforeShellExecution"}`, "missing-command"},
		{"cursor", `{"hook_event_name":"beforeShellExecution","command":""}`, "empty-command"},
		{"cursor", `{"hook_event_name":"beforeMCPExecution","tool_input":{}}`, "missing-tool-name"},
		{"cursor", `{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__cloud__delete_database","tool_input":"secret-not-json"}`, "invalid-tool-input-json"},
		{"cursor", `{"hook_event_name":"unknown","command":"secret-command"}`, "unsupported-event"},
		{"claude", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"secret-command"}}`, "invalid-post-event"},
		{"codex", `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, "missing-tool-input"},
	} {
		output, denied := HookDecision(SafetyHookOptions{Host: test.host, Repo: repo, Input: []byte(test.input)})
		if !denied {
			t.Fatalf("%s malformed payload was allowed", test.host)
		}
		body := string(output)
		if !strings.Contains(body, "HOST_PAYLOAD_MALFORMED:"+test.reason) {
			t.Fatalf("%s malformed payload did not expose safe reason %s: %s", test.host, test.reason, body)
		}
		if !strings.Contains(body, "No unsafe operation was detected") || strings.Contains(body, "denied an irreversible operation") {
			t.Fatalf("%s malformed payload was misattributed: %s", test.host, body)
		}
		if strings.Contains(body, "run the verified installer") || strings.Contains(body, "hydrate") {
			t.Fatalf("%s malformed payload recommended runtime repair: %s", test.host, body)
		}
		if strings.Contains(string(output), "secret") {
			t.Fatalf("%s denial leaked input: %s", test.host, output)
		}
	}
}

func TestCursorMalformedPayloadGuidesOneRetryThenExternalDiagnosis(t *testing.T) {
	repo := safetyTestRepo(t)
	output, denied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: []byte(`{"hook_event_name":"beforeShellExecution"}`)})
	if !denied {
		t.Fatal("missing Cursor command was allowed")
	}
	body := string(output)
	for _, expected := range []string{"Retry once", "stop shell and tool retries", "preserve current edits", "Start a new Cursor task", "diagnose-hook --host cursor", "Do not reinstall Boatstack"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Cursor recovery omitted %q: %s", expected, body)
		}
	}
}

func TestEveryHostUsesStableEmptyCommandReason(t *testing.T) {
	repo := safetyTestRepo(t)
	inputs := map[string]string{
		"cursor": `{"hook_event_name":"beforeShellExecution","command":""}`,
		"claude": `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":""}}`,
		"codex":  `{"hook_event_name":"PreToolUse","tool_name":"Shell","tool_input":{"command":""}}`,
	}
	for _, host := range []string{"cursor", "claude", "codex"} {
		output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(inputs[host])})
		if !denied || !strings.Contains(string(output), "HOST_PAYLOAD_MALFORMED:empty-command") {
			t.Fatalf("%s did not return stable empty-command reason: %s", host, output)
		}
	}
}

func TestBlockedHookNeverCreatesSentinelSideEffect(t *testing.T) {
	repo := safetyTestRepo(t)
	sentinel := filepath.Join(repo, "sentinel")
	command := "rm -rf . && touch " + sentinel
	event, _ := json.Marshal(map[string]any{"command": command})
	_, denied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: event})
	if !denied {
		if output, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
			t.Fatalf("sentinel simulation: %v: %s", err, output)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("blocked command created its sentinel side effect")
	}
}

func TestOperationalDiffBlocksGateProgression(t *testing.T) {
	repo := safetyTestRepo(t)
	path := filepath.Join(repo, "scripts", "recover.sql")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("DROP SCHEMA public CASCADE;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := CheckRepositorySafety(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "BLOCKED" || len(report.Findings) == 0 {
		t.Fatalf("operational destructive capability did not block gates: %#v", report)
	}
	if err := os.WriteFile(path, []byte("BEGIN;\nSELECT current_database();\nCOMMIT;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = CheckRepositorySafety(repo)
	if err != nil || report.Status != "PASS" {
		t.Fatalf("fix-forward operational diff did not pass: %#v %v", report, err)
	}
}

func TestConfiguredHighRiskPathsParticipateInSafetyScan(t *testing.T) {
	repo := safetyTestRepo(t)
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Project.HighRiskPaths = []string{"config/operations.txt"}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".product-loop/project.json")
	runGit(t, repo, "commit", "-m", "configure high-risk path")
	runGit(t, repo, "switch", "-c", "feat/operations")
	path := filepath.Join(repo, "config", "operations.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("terraform destroy -auto-approve\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "config/operations.txt")
	runGit(t, repo, "commit", "-m", "add operational instruction")
	report, err := CheckRepositorySafety(repo)
	if err != nil || report.Status != "BLOCKED" {
		t.Fatalf("configured high-risk path was not scanned: %#v %v", report, err)
	}
}

func TestSafetyGuardLatencyIsBounded(t *testing.T) {
	repo := safetyTestRepo(t)
	started := time.Now()
	for index := 0; index < 1000; index++ {
		ClassifyCommand(repo, `git status --short`)
		ClassifyCommand(repo, `psql -c "DROP SCHEMA public CASCADE"`)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("2,000 safety classifications exceeded the 2s fixture bound: %s", elapsed)
	}
}

// TestPlanningMarkdownPathRejectsIntakeStaging is the conformance guard that the
// removed intake staging directory is no longer a permitted planning-write path;
// only feature-scoped planning artifacts remain writable.
func TestPlanningMarkdownPathRejectsIntakeStaging(t *testing.T) {
	rejected := []string{
		".product-loop/intake/source-plan.md",
		".product-loop/intake/anything.md",
	}
	for _, path := range rejected {
		if planningMarkdownPath(path) {
			t.Errorf("intake staging path must no longer be a permitted planning-write path: %s", path)
		}
	}
	if !planningMarkdownPath(".product-loop/features/account-recovery/plan.md") {
		t.Fatal("feature-scoped planning artifacts must remain writable")
	}
}

func TestPreActivationMutationInterlockLatchesAfterAutoPlan(t *testing.T) {
	repo := nextTestRepo(t)
	writeValidSavedFeaturePlan(t, repo, "guarded-feature")
	statusBefore, err := gitCommand(repo, "status", "--short")
	if err != nil {
		t.Fatal(err)
	}

	assertBlocked := func(label string, findings []SafetyFinding) {
		t.Helper()
		if len(findings) == 0 || findings[0].Category != "workflow-phase-bypass" || findings[0].WorkflowStage != "DRAFT_PLAN" || findings[0].NextOperation != "plan-gate" {
			t.Fatalf("%s escaped the draft plan interlock: %#v", label, findings)
		}
	}
	assertBlocked("native edit", ClassifyTool(repo, "Write", map[string]any{"file_path": "src/app.ts", "content": "changed"}))
	assertBlocked("patch", ClassifyTool(repo, "ApplyPatch", map[string]any{"path": "src/app.ts", "patch": "diff"}))
	assertBlocked("shell redirection", ClassifyCommand(repo, "printf changed > src/app.ts"))
	assertBlocked("package installation", ClassifyCommand(repo, "npm install example"))
	assertBlocked("MCP mutation", ClassifyTool(repo, "mcp__files__update", map[string]any{"path": "src/app.ts"}))
	assertBlocked("unknown MCP capability", ClassifyTool(repo, "mcp__files__act", map[string]any{"path": "src/app.ts"}))

	if findings := ClassifyCommand(repo, "git status --short"); len(findings) != 0 {
		t.Fatalf("read-only inspection was denied: %#v", findings)
	}
	if findings := ClassifyTool(repo, "mcp__files__read", map[string]any{"path": "src/app.ts"}); len(findings) != 0 {
		t.Fatalf("explicitly read-only MCP inspection was denied: %#v", findings)
	}
	if findings := ClassifyCommand(repo, ".product-loop/bin/boatstack-helper check-plan --plan .product-loop/features/guarded-feature/plan.md"); len(findings) != 0 {
		t.Fatalf("bounded plan inspection was denied: %#v", findings)
	}
	if findings := ClassifyTool(repo, "Write", map[string]any{"file_path": ".product-loop/features/guarded-feature/plan.md", "content": "# revised plan"}); len(findings) != 0 {
		t.Fatalf("bounded planning Markdown was denied: %#v", findings)
	}
	if findings := ClassifyCommand(repo, ".product-loop/bin/boatstack-helper record-approval --plan .product-loop/features/guarded-feature/plan.md"); len(findings) != 0 {
		t.Fatalf("exact approval transition was denied: %#v", findings)
	}
	statusAfter, err := gitCommand(repo, "status", "--short")
	if err != nil {
		t.Fatal(err)
	}
	if statusAfter != statusBefore {
		t.Fatalf("denied operations changed the worktree: before=%q after=%q", statusBefore, statusAfter)
	}
}

func TestPreActivationInterlockPreservesUnmanagedAndActivatedBehavior(t *testing.T) {
	unmanaged := nextTestRepo(t)
	if findings := ClassifyTool(unmanaged, "Write", map[string]any{"file_path": "src/app.ts"}); len(findings) != 0 {
		t.Fatalf("unmanaged product editing changed: %#v", findings)
	}

	approved := nextTestRepo(t)
	planPath := writeValidSavedFeaturePlan(t, approved, "approved-feature")
	runGit(t, approved, "config", "user.name", "Boatstack Test")
	runGit(t, approved, "config", "user.email", "boatstack@example.invalid")
	runGit(t, approved, "add", ".")
	runGit(t, approved, "commit", "-m", "record approved plan")
	approvalPath := filepath.Join(approved, ".product-loop", "features", "approved-feature", "approval.md")
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	writeApprovalReceipt(t, approvalPath, check.Fingerprint)
	if _, err := CheckApprovalReceipt(approvalPath, check); err != nil {
		t.Fatal(err)
	}
	findings := ClassifyTool(approved, "Edit", map[string]any{"path": "src/app.ts"})
	if len(findings) == 0 || findings[0].WorkflowStage != "APPROVED" || findings[0].NextOperation != "build" {
		t.Fatalf("approved-but-not-activated product edit escaped: %#v", findings)
	}

	policy := nextTestRepo(t)
	config := testConfig()
	config.Workflow.HumanPlanApproval = false
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policy, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	writeValidSavedFeaturePlan(t, policy, "policy-feature")
	findings = ClassifyTool(policy, "Write", map[string]any{"path": "src/app.ts"})
	if len(findings) == 0 || findings[0].WorkflowStage != "POLICY_READY" || findings[0].NextOperation != "build" {
		t.Fatalf("policy-ready product edit escaped: %#v", findings)
	}
}

func TestCursorPreToolUseDeniesNativeEditAfterAutoPlan(t *testing.T) {
	repo := nextTestRepo(t)
	writeValidSavedFeaturePlan(t, repo, "cursor-feature")
	input := []byte(`{"hook_event_name":"preToolUse","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"changed"}}`)
	for attempt := 0; attempt < 2; attempt++ { // a conversation notification cannot change authority
		output, denied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: input})
		if !denied || !strings.Contains(string(output), `"permission":"deny"`) || !strings.Contains(string(output), "plan-gate") {
			t.Fatalf("Cursor native edit was not deterministically denied: %s", output)
		}
	}
}

func TestPreActivationNativeEditIsDeniedAcrossHostContracts(t *testing.T) {
	repo := nextTestRepo(t)
	writeValidSavedFeaturePlan(t, repo, "host-conformance")
	tests := map[string][]byte{
		"cursor": []byte(`{"hook_event_name":"preToolUse","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"changed"}}`),
		"claude": []byte(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"changed"}}`),
		"codex":  []byte(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"changed"}}`),
		"gemini": []byte(`{"hook_event_name":"BeforeTool","tool_name":"write_file","tool_input":{"file_path":"src/app.ts","content":"changed"}}`),
	}
	for host, input := range tests {
		t.Run(host, func(t *testing.T) {
			output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: input})
			if !denied || !strings.Contains(string(output), "plan-gate") {
				t.Fatalf("%s native mutation escaped: %s", host, output)
			}
		})
	}
}
