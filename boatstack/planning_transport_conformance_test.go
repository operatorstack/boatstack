package boatstack

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const planningTransportDelimiter = "BOATSTACK_PLAN_EOF"

func quotedLiteral(t *testing.T, value string) string {
	t.Helper()
	if strings.Contains(value, "'") {
		t.Fatalf("test path cannot be represented by the bounded literal fixture: %q", value)
	}
	return "'" + value + "'"
}

func planningHeader(t *testing.T, helper, repo, feature, artifact string) string {
	t.Helper()
	sourcePlan := "README.md"
	sourceSHA, err := SHA256File(filepath.Join(repo, sourcePlan))
	if err != nil {
		return quotedLiteral(t, helper) + " planning-write --repo " + quotedLiteral(t, repo) + " --feature " + feature + " --artifact " + artifact
	}
	return quotedLiteral(t, helper) + " planning-write --repo " + quotedLiteral(t, repo) + " --feature " + feature + " --artifact " + artifact + " --source-plan " + sourcePlan + " --source-plan-sha256 " + sourceSHA
}

func posixPlanningEnvelope(t *testing.T, helper, repo, feature, artifact, body string) string {
	t.Helper()
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return planningHeader(t, helper, repo, feature, artifact) + " <<'" + planningTransportDelimiter + "'\n" + body + planningTransportDelimiter + "\n"
}

func powerShellPlanningEnvelope(t *testing.T, helper, repo, feature, artifact, body string) string {
	t.Helper()
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return "& {\n" + powerShellPlanningEncodingLine + "\n@'\n" + body + "'@ | & " + planningHeader(t, helper, repo, feature, artifact) + "\n" + powerShellPlanningExitLine + "\n}\n"
}

func planningHookInput(t *testing.T, host, command string) []byte {
	t.Helper()
	var event map[string]any
	switch host {
	case "cursor":
		event = map[string]any{"hook_event_name": "beforeShellExecution", "command": command}
	case "claude", "codex":
		event = map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": command}}
	case "gemini":
		event = map[string]any{"hook_event_name": "BeforeTool", "tool_name": "run_shell_command", "tool_input": map[string]any{"command": command}}
	default:
		t.Fatalf("unsupported test host %q", host)
	}
	value, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func buildPlanningHelperAt(t *testing.T, binary string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve package source")
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", binary, "./cmd/boatstack-helper")
	command.Dir = filepath.Dir(source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, output)
	}
	return binary
}

func buildPlanningHelper(t *testing.T, repo string) string {
	t.Helper()
	return buildPlanningHelperAt(t, filepath.Join(repo, ".product-loop", "bin", helperName()))
}

// installPlanningTransportFixture gives execution tests the same healthy,
// generated state that production planning-write requires. Classifier-only
// tests intentionally keep using the smaller uninstalled repository fixture.
func installPlanningTransportFixture(t *testing.T, repo string) string {
	t.Helper()
	if !fileExists(filepath.Join(repo, "go.mod")) {
		if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module planning-transport\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "go.mod")
		runGit(t, repo, "commit", "-m", "add test command")
	}
	source := buildPlanningHelperAt(t, filepath.Join(t.TempDir(), helperName()))
	if err := RunInit(InitOptions{
		Repo: repo, BinaryPath: source, IntegrationChoice: "core", Yes: true, Output: io.Discard,
	}); err != nil {
		t.Fatalf("install healthy planning transport fixture: %v", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(repo, ".product-loop", "boatstack.ps1")
	}
	return filepath.Join(repo, ".product-loop", "boatstack")
}

func executePlanningEnvelope(t *testing.T, repo, command string) {
	t.Helper()
	var execution *exec.Cmd
	if runtime.GOOS == "windows" {
		powershell, err := exec.LookPath("powershell")
		if err != nil {
			t.Skip("Windows PowerShell is unavailable")
		}
		execution = exec.Command(powershell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
	} else {
		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash is unavailable")
		}
		execution = exec.Command(bash, "-c", command)
	}
	execution.Dir = repo
	if output, err := execution.CombinedOutput(); err != nil {
		t.Fatalf("execute admitted planning transport: %v: %s", err, output)
	}
}

// control-law: planning-document-body-is-literal-data
//
// Relation conformance: the real host event admits the exact command that the
// real shell executes, and the real helper receives a non-empty document. This
// closes the old split test (bare command classification plus a direct library
// write), which never exercised the transport between the hook and stdin.
func TestPlanningTransportRunsHookShellHelperAndSavedArtifact(t *testing.T) {
	repo := safetyTestRepo(t)
	helper := installPlanningTransportFixture(t, repo)
	body := "# Literal transport\n\nUnicode survives: ü 船\n`rm -rf /` and $(git reset --hard HEAD~1) are documentation.\n"

	var command string
	if runtime.GOOS == "windows" {
		command = powerShellPlanningEnvelope(t, helper, repo, "literal-transport", "source-plan.md", body)
	} else {
		command = posixPlanningEnvelope(t, helper, repo, "literal-transport", "source-plan.md", body)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, command)})
		if denied {
			t.Fatalf("%s denied the complete literal envelope: %s", host, output)
		}
	}

	executePlanningEnvelope(t, repo, command)
	written, err := os.ReadFile(filepath.Join(repo, ".product-loop", "features", "literal-transport", "source-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != body {
		t.Fatalf("saved Markdown differs from transported body:\nwant %q\n got %q", body, written)
	}
}

func TestPlanningTransportRunsThroughDetachedWorkspaceBinding(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/planning-transport.git")
	source := buildPlanningHelperAt(t, filepath.Join(t.TempDir(), helperName()))
	result, err := AttachDetached(AttachOptions{Repo: repo, BinaryPath: source})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach detached workspace: %+v %v", result, err)
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil || workspace.Mode != SupervisionDetached {
		t.Fatalf("resolve detached workspace: %+v %v", workspace, err)
	}
	helper := workspace.HelperPath()
	body := "# Detached literal transport\n\nThe controller remains outside the product repository.\n"
	command := posixPlanningEnvelope(t, helper, repo, "detached-transport", "plan.md", body)
	if runtime.GOOS == "windows" {
		command = powerShellPlanningEnvelope(t, helper, repo, "detached-transport", "plan.md", body)
	}
	output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: repo, Input: planningHookInput(t, "codex", command)})
	if denied {
		t.Fatalf("detached literal envelope was denied: %s", output)
	}
	executePlanningEnvelope(t, repo, command)
	written, err := os.ReadFile(filepath.Join(workspace.FeatureDir("detached-transport"), "plan.md"))
	if err != nil || strings.ReplaceAll(string(written), "\r\n", "\n") != body {
		t.Fatalf("detached planning artifact mismatch: %v %q", err, written)
	}
	if _, err := os.Stat(filepath.Join(repo, productLoopDirName)); !os.IsNotExist(err) {
		t.Fatal("detached planning transport leaked controller state into the product repository")
	}
}

func TestPlanningTransportRejectsUnverifiedDetachedBinding(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/planning-binding.git")
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach detached workspace: %+v %v", result, err)
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	command := posixPlanningEnvelope(t, workspace.HelperPath(), repo, "detached-binding", "plan.md", "# Plan\n")
	identity, err := repoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(os.Getenv(stateRootEnv), "boatstack")
	if err := os.WriteFile(bindingPath(root, identity.RepoID), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	findings := ClassifyCommand(repo, command)
	if len(findings) != 1 || findings[0].Category != "planning-transport-invalid" || findings[0].Reason != "workspace-binding-unverified" {
		t.Fatalf("unverified detached binding did not fail closed: %#v", findings)
	}
}

func TestPlanningTransportTreatsDocumentTextAsInertAcrossHosts(t *testing.T) {
	repo := safetyTestRepo(t)
	body := strings.Join([]string{
		"# Threat-model examples",
		"`rm -rf /`",
		"$(git reset --hard HEAD~1)",
		"DROP TABLE accounts;",
		".git/boatstack/deliveries/demo/state.json > /tmp/example",
		"terraform destroy | Remove-Item -Recurse -Force $HOME",
		"secret-marker-that-must-never-be-rendered",
	}, "\n") + "\n"
	posix := posixPlanningEnvelope(t, ".product-loop/boatstack", repo, "threat-model", "questions.md", body)
	commands := []struct {
		command, expected  string
		hostConformanceRun bool
	}{
		{posix, body, true},
		{powerShellPlanningEnvelope(t, `.product-loop\boatstack.ps1`, repo, "threat-model", "questions.md", body), body, runtime.GOOS == "windows"},
		{strings.ReplaceAll(posix, "\n", "\r\n"), body, true},
	}
	for _, test := range commands {
		inspection := inspectPlanningWriteTransport(test.command)
		if !inspection.Matched || inspection.InvalidReason != "" || string(inspection.Content) != test.expected {
			t.Fatalf("literal envelope was not recovered exactly: %#v", inspection)
		}
		if !test.hostConformanceRun {
			continue
		}
		for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
			output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, test.command)})
			if denied {
				t.Fatalf("%s treated literal Markdown as an effect: %s", host, output)
			}
		}
	}
}

func TestPlanningTransportFailureClassesFailClosedWithoutExecuting(t *testing.T) {
	repo := safetyTestRepo(t)
	otherRepo := t.TempDir()
	header := planningHeader(t, ".product-loop/boatstack", repo, "transport-failures", "plan.md")
	valid := posixPlanningEnvelope(t, ".product-loop/boatstack", repo, "transport-failures", "plan.md", "# Plan\n")
	powerShellValid := powerShellPlanningEnvelope(t, `.product-loop\boatstack.ps1`, repo, "transport-failures", "plan.md", "# Plan\n")
	cases := map[string]string{
		"bare command":                   header,
		"leading command":                "touch sentinel\n" + valid,
		"compound prefix":                "cd . && " + valid,
		"environment wrapper":            "env BOATSTACK_TEST=1 " + valid,
		"unquoted delimiter":             header + " <<BOATSTACK_PLAN_EOF\n# $(touch sentinel)\nBOATSTACK_PLAN_EOF\n",
		"double-quoted delimiter":        header + " <<\"BOATSTACK_PLAN_EOF\"\n# Plan\nBOATSTACK_PLAN_EOF\n",
		"indented heredoc":               header + " <<-'BOATSTACK_PLAN_EOF'\n\t# Plan\n\tBOATSTACK_PLAN_EOF\n",
		"missing terminator":             header + " <<'BOATSTACK_PLAN_EOF'\n# Plan\nrm -rf /\n",
		"delimiter collision":            header + " <<'BOATSTACK_PLAN_EOF'\n# Plan\nBOATSTACK_PLAN_EOF\ntouch sentinel\nBOATSTACK_PLAN_EOF\n",
		"trailing command":               strings.TrimSuffix(valid, "\n") + "; touch sentinel\n",
		"empty content":                  header + " <<'BOATSTACK_PLAN_EOF'\n\nBOATSTACK_PLAN_EOF\n",
		"unknown artifact":               strings.Replace(valid, "--artifact plan.md", "--artifact notes.md", 1),
		"missing feature value":          strings.Replace(valid, "--feature transport-failures", "--feature --artifact", 1),
		"command substitution header":    strings.Replace(valid, "--repo "+quotedLiteral(t, repo), "--repo $(touch sentinel)", 1),
		"repository mismatch":            strings.Replace(valid, quotedLiteral(t, repo), quotedLiteral(t, otherRepo), 1),
		"helper path mismatch":           strings.Replace(valid, ".product-loop/boatstack", "/tmp/boatstack-helper", 1),
		"helper alias":                   strings.Replace(valid, ".product-loop/boatstack", ".product-loop/bin/helper-alias", 1),
		"invalid UTF-8":                  header + " <<'BOATSTACK_PLAN_EOF'\n" + string([]byte{0xff}) + "\nBOATSTACK_PLAN_EOF\n",
		"NUL content":                    header + " <<'BOATSTACK_PLAN_EOF'\nplan\x00body\nBOATSTACK_PLAN_EOF\n",
		"PowerShell no UTF-8 scope":      "@'\n# Plan\n'@ | & " + header,
		"PowerShell truncated":           "& {\n" + powerShellPlanningEncodingLine + "\n@'\n# Plan\n",
		"PowerShell no exit propagation": strings.Replace(powerShellValid, powerShellPlanningExitLine+"\n", "", 1),
		"PowerShell delimiter collision": powerShellPlanningEnvelope(t, `.product-loop\boatstack.ps1`, repo, "transport-failures", "plan.md", "# Plan\n'@\ntouch sentinel\n"),
		"PowerShell trailing command":    strings.TrimSuffix(powerShellValid, "\n") + "; touch sentinel\n",
	}
	// Dormant host probes are intentionally inert. The explicit bootstrap/helper
	// boundary validates these envelopes before mutation; host-level denial is an
	// additional active-delivery control and is tested under a real lease here.
	engageHookFixture(t, repo)
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			findings := ClassifyCommand(repo, command)
			if len(findings) == 0 || findings[0].Category != "planning-transport-invalid" {
				t.Fatalf("malformed transport did not fail with its bounded category: %#v", findings)
			}
			// A JSON string cannot carry invalid UTF-8: encoding/json replaces it
			// before any host hook sees the command. The direct classifier above
			// still holds the lower-level boundary for non-JSON callers.
			if name == "invalid UTF-8" {
				return
			}
			output, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: planningHookInput(t, "claude", command)})
			if !denied || !strings.Contains(string(output), "PLANNING_TRANSPORT_INVALID:") {
				t.Fatalf("host did not deny malformed transport: %s", output)
			}
			if strings.Contains(string(output), "secret-marker") || strings.Contains(string(output), "touch sentinel") {
				t.Fatalf("denial echoed document or command content: %s", output)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(repo, "sentinel")); !os.IsNotExist(err) {
		t.Fatal("classification or hook evaluation executed malformed transport")
	}
	if _, err := os.Stat(filepath.Join(repo, ".product-loop", "features", "transport-failures")); !os.IsNotExist(err) {
		t.Fatal("rejected transport partially created a planning artifact")
	}
}

func TestPlanningTransportDoesNotClaimUnrelatedPowerShellHereStrings(t *testing.T) {
	repo := safetyTestRepo(t)
	command := "@'\nThis document mentions planning-write as prose.\n'@ | Set-Content notes.md"
	if inspection := inspectPlanningWriteTransport(command); inspection.Matched {
		t.Fatalf("unrelated here-string was claimed as planning transport: %#v", inspection)
	}
	if findings := ClassifyCommand(repo, command); len(findings) != 0 {
		t.Fatalf("unmanaged unrelated here-string changed classification: %#v", findings)
	}
}

func TestPlanningTransportPreservesStageAdmissions(t *testing.T) {
	command := posixPlanningEnvelope(t, "/path with spaces/boatstack", "/repo with spaces", "stage-matrix", "questions.md", "# Questions\n")
	inspection := inspectPlanningWriteTransport(command)
	if !inspection.Matched || inspection.InvalidReason != "" {
		t.Fatalf("valid path variant did not parse: %#v", inspection)
	}
	for _, stage := range []string{"NOT_STARTED", "DRAFT_PLAN", "INVALID_STATE"} {
		if !controlledPhaseTransition(inspection.Header, stage) {
			t.Errorf("planning-write transport closed at %s", stage)
		}
	}
	for _, stage := range []string{"APPROVED", "POLICY_READY", "AMBIGUOUS"} {
		if controlledPhaseTransition(inspection.Header, stage) {
			t.Errorf("planning-write transport widened authority at %s", stage)
		}
	}
}

func TestPlanningPrescriptionRendersACompleteGuardAdmittedEnvelope(t *testing.T) {
	repo := safetyTestRepo(t)
	command, ok := prescribePlanningVerb(repo, NextStatus{ObservedStage: "NOT_STARTED", Feature: "prescribed-transport"}, "planning-write")
	if !ok || command == nil {
		t.Fatal("planning-write prescription is missing")
	}
	line := substituteOwedFlags(command.CommandLine())
	inspection := inspectPlanningWriteTransport(line)
	if !inspection.Matched || inspection.InvalidReason != "" || string(inspection.Content) != "test-value\n" {
		t.Fatalf("prescription did not render a complete literal envelope: %q %#v", line, inspection)
	}
	if findings := ClassifyCommand(repo, line); len(findings) != 0 {
		t.Fatalf("guard denied its rendered planning prescription: %#v", findings)
	}
}

func TestPlanningPrescriptionUsesOneCompleteShellGrammar(t *testing.T) {
	repo, workspace, _ := detachedPolicyReadyFixture(t)
	command := PrescribedCommand{
		Program: workspace.HelperPath(),
		Verb:    "planning-write",
		Args:    []string{"--repo", repo, "--feature", "feature-one"},
		RequiresHumanInput: []string{
			"--artifact",
			planningMarkdownInput,
		},
	}

	for _, test := range []struct {
		name string
		goos string
	}{
		{name: "POSIX", goos: "linux"},
		{name: "PowerShell", goos: "windows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := substituteOwedFlags(command.commandLineForOS(test.goos))
			inspection := inspectPlanningWriteTransport(line)
			if !inspection.Matched || inspection.InvalidReason != "" || string(inspection.Content) != "test-value\n" {
				t.Fatalf("%s prescription is not one complete planning envelope: %q %#v", test.name, line, inspection)
			}
			if reason := planningTransportBinding(repo, inspection); reason != "" {
				t.Fatalf("%s transport lost its detached workspace binding: %s", test.name, reason)
			}
		})
	}

	hybrid := "& " + substituteOwedFlags(command.commandLineForOS("linux"))
	if inspection := inspectPlanningWriteTransport(hybrid); inspection.Matched && inspection.InvalidReason == "" {
		t.Fatalf("hybrid PowerShell/POSIX command crossed the planning transport boundary: %#v", inspection)
	}
	findings := ClassifyCommand(repo, hybrid)
	if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
		t.Fatalf("hybrid command did not fail closed at managed-state admission: %#v", findings)
	}

	ordinary := PrescribedCommand{
		Program: "gh",
		Verb:    "pr",
		Args:    []string{"merge", "https://example.invalid/pr/9", "--squash"},
	}.commandLineForOS("windows")
	if ordinary != "gh pr merge https://example.invalid/pr/9 --squash" {
		t.Fatalf("safe ordinary argv lost its stable cross-platform rendering: %q", ordinary)
	}
}

func TestPlanningPrescriptionQuotesRepositoryPath(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo with 'quoted' space")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Path fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")

	installPlanningTransportFixture(t, repo)
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: "quoted-path", SourcePlan: "README.md",
		Artifact: "plan.md", Shell: BootstrapShellPOSIX, Document: []byte("test-value\n"),
	})
	if err != nil {
		t.Fatalf("resolve quoted repository bootstrap: %v", err)
	}
	line := prescription.PlanningEnvelope
	inspection := inspectPlanningWriteTransport(line)
	if !inspection.Matched || inspection.InvalidReason != "" || inspection.Repository != prescription.Repository {
		t.Fatalf("quoted repository path did not round trip: %q %#v", line, inspection)
	}
	if findings := ClassifyCommand(repo, line); len(findings) != 0 {
		t.Fatalf("guard denied the quoted repository prescription: %#v", findings)
	}
	if runtime.GOOS != "windows" {
		executePlanningEnvelope(t, repo, line)
		written, err := os.ReadFile(filepath.Join(repo, productLoopDirName, "features", "quoted-path", "plan.md"))
		if err != nil || string(written) != "test-value\n" {
			t.Fatalf("quoted repository prescription did not execute exactly: %v %q", err, written)
		}
	}
}
