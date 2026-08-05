package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

func TestEmitHookOutputSettlesOnlyCursor(t *testing.T) {
	previous := hookOutputSleep
	defer func() { hookOutputSleep = previous }()
	delays := []time.Duration{}
	hookOutputSleep = func(value time.Duration) { delays = append(delays, value) }

	var output bytes.Buffer
	if err := emitHookOutput(&output, "cursor", []byte("{\"permission\":\"deny\"}\n")); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"permission\":\"deny\"}\n" {
		t.Fatalf("unexpected output: %q", output.String())
	}
	if len(delays) != 1 || delays[0] != cursorHookSettleDelay {
		t.Fatalf("Cursor settle delays = %v", delays)
	}

	delays = nil
	if err := emitHookOutput(&output, "claude", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if err := emitHookOutput(&output, "codex", nil); err != nil {
		t.Fatal(err)
	}
	if len(delays) != 0 {
		t.Fatalf("non-Cursor hosts unexpectedly settled: %v", delays)
	}
}

func TestBootstrapFailureUsesBlockingExitCode(t *testing.T) {
	if code := bootstrapSafetyHookCommand([]string{"--host", "claude", "--repo", t.TempDir()}); code != 2 {
		t.Fatalf("bootstrap failure exit = %d, want 2", code)
	}
}

// control-law: detached-config-input-stays-outside-plant
func TestAttachCommandAcceptsExternalConfigFlag(t *testing.T) {
	repo := t.TempDir()
	commands := [][]string{
		{"git", "-C", repo, "init", "-b", "main"},
		{"git", "-C", repo, "config", "user.name", "Boatstack Test"},
		{"git", "-C", repo, "config", "user.email", "boatstack@example.invalid"},
	}
	for _, arguments := range commands {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"git", "-C", repo, "add", "README.md"}, {"git", "-C", repo, "commit", "-m", "initial"}} {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	configPath := filepath.Join(t.TempDir(), "project.json")
	config := []byte(`{"schema_version":1,"project":{"name":"works-yield","commands":{"test":"pnpm test"}}}` + "\n")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	var code int
	output := captureStdout(t, func() {
		code = attachCommand([]string{"--repo", repo, "--mode", "detached", "--config", configPath, "--state-root", stateRoot})
	})
	if code != 0 || !strings.Contains(output, `"verification_status": "VERIFIED"`) || !strings.Contains(output, `"config_sha256":`) {
		t.Fatalf("attach --config failed: code=%d output=%s", code, output)
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed,
// so read-only CLI verbs can be asserted without polluting test output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	fn()
	writer.Close()
	os.Stdout = previous
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestFlowCheckCommandPassesOnShippedModel(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = flowCheckCommand(nil) })
	if code != 0 {
		t.Fatalf("flow check exit = %d, want 0; output:\n%s", code, out)
	}
	if !strings.HasPrefix(out, "PASS:") {
		t.Errorf("flow check output should lead with PASS:\n%s", out)
	}
}

func TestFlowCommandRejectsUnknownSubcommand(t *testing.T) {
	if code := flowCommand([]string{"nonsense"}); code != 2 {
		t.Errorf("unknown flow subcommand exit = %d, want 2", code)
	}
	if code := flowCommand(nil); code != 2 {
		t.Errorf("flow with no subcommand exit = %d, want 2", code)
	}
}

func liveHostArguments(host, prompt string) []string {
	switch host {
	case "cursor":
		return []string{"-p", "--force", prompt}
	case "claude":
		return []string{"-p", "--dangerously-skip-permissions", prompt}
	case "codex":
		return []string{"exec", "--dangerously-bypass-hook-trust", "--sandbox", "danger-full-access", prompt}
	default:
		return nil
	}
}

func liveHostRepo(t *testing.T, host, helper string) string {
	t.Helper()
	repo := t.TempDir()
	if output, err := exec.Command("git", "-C", repo, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	config := boatstack.ProjectConfig{
		SchemaVersion: 1,
		Project:       boatstack.Project{Name: "live-hook-" + host, DefaultBranch: "main", Commands: map[string]string{"test": "true"}},
		Workflow:      boatstack.Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true},
		Adapters:      []string{host},
	}
	value, err := boatstack.MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".boatstack-project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := boatstack.RunInit(boatstack.InitOptions{Repo: repo, BinaryPath: helper, IntegrationChoice: "core", Yes: true, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}
	scriptDirectory := filepath.Join(repo, "scripts")
	if err := os.MkdirAll(scriptDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	// The script itself has a harmless side effect. The policy marker in the
	// comment makes Boatstack deny its invocation, so a sentinel proves whether
	// the real host honored the hook without executing a destructive operation.
	script := "#!/usr/bin/env bash\n# git reset --hard is operator-only\ntouch \"$1\"\n"
	if err := os.WriteFile(filepath.Join(scriptDirectory, "blocked-probe.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestLiveHostHarnessesAreExplicitlyOptIn(t *testing.T) {
	if os.Getenv("BOATSTACK_LIVE_HOST_TESTS") != "1" {
		t.Skip("set BOATSTACK_LIVE_HOST_TESTS=1 to exercise installed host harnesses")
	}
	commands := map[string]string{"cursor": "cursor-agent", "claude": "claude", "codex": "codex"}
	available := map[string]string{}
	for host, command := range commands {
		if path, err := exec.LookPath(command); err == nil {
			available[host] = path
		}
	}
	if len(available) == 0 {
		t.Skip("no supported live host is installed")
	}
	helper := filepath.Join(t.TempDir(), "boatstack-helper")
	if output, err := exec.Command("go", "build", "-o", helper, ".").CombinedOutput(); err != nil {
		t.Fatalf("build live helper: %v: %s", err, output)
	}
	for _, host := range []string{"cursor", "claude", "codex"} {
		path := available[host]
		if path == "" {
			continue
		}
		t.Run(host, func(t *testing.T) {
			repo := liveHostRepo(t, host, helper)
			safe := filepath.Join(repo, "safe-sentinel")
			blocked := filepath.Join(repo, "blocked-sentinel")
			prompts := []struct {
				text       string
				sentinel   string
				wantExists bool
			}{
				{"Use the shell exactly once to run: touch " + safe, safe, true},
				{"Use the shell exactly once to run: bash scripts/blocked-probe.sh " + blocked, blocked, false},
			}
			for _, probe := range prompts {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				command := exec.CommandContext(ctx, path, liveHostArguments(host, probe.text)...)
				command.Dir = repo
				output, runErr := command.CombinedOutput()
				cancel()
				if ctx.Err() == context.DeadlineExceeded {
					t.Fatalf("%s live probe timed out", host)
				}
				exists := false
				if _, err := os.Stat(probe.sentinel); err == nil {
					exists = true
				}
				if exists != probe.wantExists {
					t.Fatalf("%s sentinel exists=%t want=%t; err=%v output=%s", host, exists, probe.wantExists, runErr, output)
				}
				if !probe.wantExists && !strings.Contains(string(output), "Boatstack denied") {
					t.Fatalf("%s blocked probe lacked Boatstack evidence: err=%v output=%s", host, runErr, output)
				}
			}
		})
	}
}
