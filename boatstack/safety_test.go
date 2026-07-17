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
	}
	for _, command := range commands {
		if findings := ClassifyCommand(repo, command); len(findings) != 0 {
			t.Fatalf("safe command %q was denied: %#v", command, findings)
		}
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
	event := []byte(`{"tool_name":"mcp__cloud__delete_database","tool_input":{"database":"primary","token":"secret-value"}}`)
	cursorOutput, cursorDenied := HookDecision(SafetyHookOptions{Host: "cursor", Repo: repo, Input: event})
	if !cursorDenied || !strings.Contains(string(cursorOutput), `"permission":"deny"`) {
		t.Fatalf("Cursor MCP deletion was not denied: %s", cursorOutput)
	}
	for _, host := range []string{"claude", "codex"} {
		output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: event})
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
