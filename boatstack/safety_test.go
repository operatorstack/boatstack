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
		{"codex allow", "codex", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, false, ""},
		{"codex deny", "codex", `{"hook_event_name":"PreToolUse","tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary"}}`, true, `"permissionDecision":"deny"`},
		{"wrong event deny", "codex", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, true, `"permissionDecision":"deny"`},
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
		{"claude", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"secret-command"}}`, "unsupported-event"},
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
