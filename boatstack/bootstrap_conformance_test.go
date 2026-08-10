package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func bootstrapTestShell() BootstrapShell {
	if runtime.GOOS == "windows" {
		return BootstrapShellPowerShell
	}
	return BootstrapShellPOSIX
}

func writeBootstrapSourcePlan(t *testing.T, repo string) string {
	t.Helper()
	path := filepath.Join(repo, "docs", "source plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Source plan\n\nBuild the bounded feature.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join("docs", "source plan.md"))
}

func executePlanningEnvelopeOutput(repo, command string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		powershell, err := exec.LookPath("powershell")
		if err != nil {
			return nil, err
		}
		process := exec.Command(powershell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
		process.Dir = repo
		return process.CombinedOutput()
	}
	process := exec.Command("bash", "-c", command)
	process.Dir = repo
	return process.CombinedOutput()
}

func executePlanningEnvelopeWith(repo, executable string, shell BootstrapShell, command string) ([]byte, error) {
	var process *exec.Cmd
	if shell == BootstrapShellPowerShell {
		process = exec.Command(executable, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
	} else {
		process = exec.Command(executable, "-c", command)
	}
	process.Dir = repo
	return process.CombinedOutput()
}

func bootstrapInputEnvelope(t *testing.T, workspace WorkspaceContext, repo, feature, sourcePlan, artifact string, shell BootstrapShell, body []byte) string {
	t.Helper()
	program := bootstrapProgram(workspace, shell)
	argv := []string{
		program, "flow", "bootstrap", "--repo", repo, "--feature", feature,
		"--source-plan", sourcePlan, "--artifact", artifact, "--shell", string(shell),
	}
	if shell == BootstrapShellPowerShell {
		envelope, err := powerShellPlanningEnvelopeFor(argv, body)
		if err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	return posixPlanningEnvelopeFor(argv, body)
}

// Positive, relation, and bypass conformance for control-law:
// bootstrap-command-authority-is-workspace-bound. The exact stdin envelope
// admitted by every host runs the real bootstrap CLI; its exact stdout envelope
// is admitted again, runs in a real shell, and creates the intended artifact.
func TestBootstrapOracleRunsInputHookRendererOutputHookShellHelper(t *testing.T) {
	repo := safetyTestRepo(t)
	installPlanningTransportFixture(t, repo)
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlan := writeBootstrapSourcePlan(t, repo)
	body := []byte("# Synthetic plan\n\nCommands such as `rm -rf /` and `git reset --hard` are inert text.\n")
	shell := bootstrapTestShell()
	bootstrapCommand := bootstrapInputEnvelope(t, workspace, repo, "bootstrap-oracle", sourcePlan, "source-plan.md", shell, body)

	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, bootstrapCommand)}); denied {
			t.Fatalf("%s denied the bootstrap input envelope: %s", host, output)
		}
	}
	rendered, err := executePlanningEnvelopeOutput(repo, bootstrapCommand)
	if err != nil {
		t.Fatalf("execute bootstrap renderer: %v: %s", err, rendered)
	}
	planningEnvelope := string(rendered)
	if !strings.Contains(planningEnvelope, "--source-plan-sha256") || !strings.Contains(planningEnvelope, workspace.LauncherPath(shell == BootstrapShellPowerShell)) {
		t.Fatalf("bootstrap output lost source evidence or workspace launcher: %s", planningEnvelope)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, planningEnvelope)}); denied {
			t.Fatalf("%s denied the rendered planning envelope: %s", host, output)
		}
	}
	if output, err := executePlanningEnvelopeOutput(repo, planningEnvelope); err != nil {
		t.Fatalf("execute rendered planning envelope: %v: %s", err, output)
	}
	written, err := os.ReadFile(filepath.Join(workspace.FeatureDir("bootstrap-oracle"), "source-plan.md"))
	if err != nil || string(written) != string(body) {
		t.Fatalf("bootstrap artifact mismatch: %v %q", err, written)
	}
}

// Positive and relation conformance for control-law:
// bootstrap-command-authority-is-workspace-bound. Detached mode must render the
// external bound helper and keep every controller byte outside the product repo.
func TestDetachedBootstrapOracleUsesExternalHelperAcrossHosts(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/bootstrap-oracle.git")
	t.Setenv(stateRootEnv, filepath.Join(t.TempDir(), "Application Support"))
	invalidateWorkspaceCache()
	sourcePlan := writeBootstrapSourcePlan(t, repo)
	source := buildPlanningHelperAt(t, filepath.Join(t.TempDir(), helperName()))
	result, err := AttachDetached(AttachOptions{Repo: repo, BinaryPath: source})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach detached fixture: %+v %v", result, err)
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("# Detached bootstrap\n\nThe controller stays external.\n")
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: "detached-bootstrap", SourcePlan: sourcePlan,
		Artifact: "source-plan.md", Shell: bootstrapTestShell(), Document: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prescription.Disposition != "CREATE_CANDIDATE" || prescription.HelperPath != workspace.HelperPath() || !strings.Contains(prescription.HelperPath, "Application Support") {
		t.Fatalf("detached prescription is not bound to the external workspace: %+v", prescription)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, prescription.PlanningEnvelope)}); denied {
			t.Fatalf("%s denied detached bootstrap output: %s", host, output)
		}
	}
	if output, err := executePlanningEnvelopeOutput(repo, prescription.PlanningEnvelope); err != nil {
		t.Fatalf("execute detached bootstrap output: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(workspace.FeatureDir("detached-bootstrap"), "source-plan.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, productLoopDirName)); !os.IsNotExist(err) {
		t.Fatal("detached bootstrap leaked controller state into the product repository")
	}
}

// Negative and failure-state conformance for control-law:
// bootstrap-command-authority-is-workspace-bound. A raw first write and stale
// source evidence both fail without creating managed feature state.
func TestBootstrapFirstWriteRequiresFreshSourceEvidence(t *testing.T) {
	repo := safetyTestRepo(t)
	installPlanningTransportFixture(t, repo)
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "raw-first-write", Artifact: "source-plan.md", Content: []byte("# Raw\n"),
	}); err == nil || !strings.Contains(err.Error(), "flow bootstrap") {
		t.Fatalf("raw first write did not require bootstrap evidence: %v", err)
	}
	if _, err := os.Stat(workspace.FeatureDir("raw-first-write")); !os.IsNotExist(err) {
		t.Fatal("rejected raw first write created feature state")
	}

	sourcePlan := writeBootstrapSourcePlan(t, repo)
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: "stale-source", SourcePlan: sourcePlan,
		Artifact: "source-plan.md", Shell: bootstrapTestShell(), Document: []byte("# Candidate\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(sourcePlan)), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, runErr := executePlanningEnvelopeOutput(repo, prescription.PlanningEnvelope)
	if runErr == nil || !strings.Contains(string(output), "source plan changed after bootstrap") {
		t.Fatalf("stale source evidence was not rejected: %v %s", runErr, output)
	}
	if _, err := os.Stat(workspace.FeatureDir("stale-source")); !os.IsNotExist(err) {
		t.Fatal("stale prescription created feature state")
	}
}

// Negative, bypass, and failure-state conformance for control-law:
// bootstrap-command-authority-is-workspace-bound.
func TestBootstrapOracleRejectsInvalidIdentityStateAndShell(t *testing.T) {
	repo := safetyTestRepo(t)
	installPlanningTransportFixture(t, repo)
	sourcePlan := writeBootstrapSourcePlan(t, repo)
	base := BootstrapOptions{
		Repo: repo, Feature: "safe-bootstrap", SourcePlan: sourcePlan,
		Artifact: "source-plan.md", Shell: bootstrapTestShell(), Document: []byte("# Candidate\n"),
	}
	tests := []struct {
		name   string
		mutate func(*BootstrapOptions)
	}{
		{"invalid slug", func(o *BootstrapOptions) { o.Feature = "Wrong Slug" }},
		{"unknown artifact", func(o *BootstrapOptions) { o.Artifact = "state.json" }},
		{"unknown shell", func(o *BootstrapOptions) { o.Shell = "cmd" }},
		{"missing source", func(o *BootstrapOptions) { o.SourcePlan = "docs/missing.md" }},
		{"powershell collision", func(o *BootstrapOptions) {
			o.Shell = BootstrapShellPowerShell
			o.Document = []byte("# Body\n'@ collision\n")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := base
			test.mutate(&options)
			if _, err := ResolvePlanningBootstrap(options); err == nil {
				t.Fatal("invalid bootstrap input was accepted")
			}
		})
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace.FeatureDir(base.Feature), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.FeatureDir(base.Feature), "plan.lock.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePlanningBootstrap(base); err == nil || !strings.Contains(err.Error(), "managed authority") {
		t.Fatalf("managed feature was allowed back into bootstrap: %v", err)
	}
}

func TestFlowBootstrapJSONMatchesCanonicalPrescription(t *testing.T) {
	repo := safetyTestRepo(t)
	installPlanningTransportFixture(t, repo)
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlan := writeBootstrapSourcePlan(t, repo)
	body := []byte("# JSON prescription\n")
	command := exec.Command(workspace.HelperPath(), "flow", "bootstrap", "--repo", repo, "--feature", "json-bootstrap", "--source-plan", sourcePlan, "--artifact", "source-plan.md", "--shell", string(bootstrapTestShell()), "--json")
	command.Dir = repo
	command.Stdin = strings.NewReader(string(body))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("flow bootstrap --json: %v: %s", err, output)
	}
	var prescription BootstrapPrescription
	if err := json.Unmarshal(output, &prescription); err != nil {
		t.Fatal(err)
	}
	if prescription.VerificationStatus != "VERIFIED" || prescription.Feature != "json-bootstrap" || prescription.SourcePlanSHA256 == "" || len(prescription.Argv) == 0 || prescription.PlanningEnvelope == "" {
		t.Fatalf("incomplete bootstrap JSON: %+v", prescription)
	}
}

// Relation and bypass conformance for control-law:
// bootstrap-command-authority-is-workspace-bound. Generated host instructions
// may name the oracle, but may never reconstruct a mutation helper path.
func TestGeneratedPlanningInstructionsUseBootstrapOracle(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		".product-loop/workflow.md",
		".cursor/commands/auto-plan.md",
		".claude/skills/auto-plan/SKILL.md",
		".agents/skills/auto-plan/SKILL.md",
		".gemini/skills/auto-plan/SKILL.md",
	}
	for _, path := range paths {
		content := string(bundle.Files[path])
		if !strings.Contains(content, "flow bootstrap") || !strings.Contains(content, "planning_envelope") {
			t.Fatalf("%s does not consume the canonical bootstrap oracle", path)
		}
		for _, forbidden := range []string{
			".product-loop/boatstack planning-write",
			`.product-loop\boatstack.ps1' planning-write`,
			".product-loop/bin/boatstack-helper planning-write",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s reconstructs a planning mutation command: %q", path, forbidden)
			}
		}
	}
}

func TestBootstrapRendererQuotesLiteralPathsForBothShells(t *testing.T) {
	body := []byte("# Quoted paths\n")
	argv := []string{
		filepath.Join(string(filepath.Separator), "Application Support", "Boat's helper", helperName()),
		"planning-write", "--repo", filepath.Join(string(filepath.Separator), "work trees", "consumer's repo"),
		"--feature", "quoted-bootstrap", "--artifact", "plan.md",
		"--source-plan", "docs/operator's plan.md", "--source-plan-sha256", strings.Repeat("a", 64),
	}
	posix := inspectPlanningWriteTransport(posixPlanningEnvelopeFor(argv, body))
	if !posix.Matched || posix.InvalidReason != "" || posix.Executable != argv[0] || posix.Repository != argv[3] {
		t.Fatalf("POSIX literal quoting drifted: %+v", posix)
	}
	powerShellArgv := append([]string(nil), argv...)
	powerShellArgv[0] = filepath.Join(string(filepath.Separator), "Application Support", "Boat helper", helperName())
	powerShellArgv[3] = filepath.Join(string(filepath.Separator), "work trees", "consumer repo")
	powerShellArgv[9] = "docs/operator plan.md"
	powerShellEnvelope, err := powerShellPlanningEnvelopeFor(powerShellArgv, body)
	if err != nil {
		t.Fatal(err)
	}
	powerShell := inspectPlanningWriteTransport(powerShellEnvelope)
	if !powerShell.Matched || powerShell.InvalidReason != "" || powerShell.Executable != powerShellArgv[0] || powerShell.Repository != powerShellArgv[3] {
		t.Fatalf("PowerShell literal quoting drifted: %+v", powerShell)
	}
	if _, err := powerShellPlanningEnvelopeFor(argv, body); err == nil {
		t.Fatal("PowerShell renderer accepted an ambiguously quoted path")
	}
}

// Positive platform conformance for control-law:
// bootstrap-command-authority-is-workspace-bound. Required CI executes the
// canonical output in Bash on Unix, zsh on macOS, and both PowerShell and Git
// Bash on Windows without a provider API or installed coding host.
func TestBootstrapEnvelopeExecutesInRequiredRealShells(t *testing.T) {
	repo := safetyTestRepo(t)
	installPlanningTransportFixture(t, repo)
	sourcePlan := writeBootstrapSourcePlan(t, repo)
	type shellCase struct {
		name       string
		executable string
		shell      BootstrapShell
	}
	cases := []shellCase{{name: "bash", executable: "bash", shell: BootstrapShellPOSIX}}
	if runtime.GOOS == "windows" {
		cases = append(cases, shellCase{name: "powershell", executable: "powershell", shell: BootstrapShellPowerShell})
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, shellCase{name: "zsh", executable: "zsh", shell: BootstrapShellPOSIX})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := exec.LookPath(test.executable); err != nil {
				t.Fatalf("required shell %s is unavailable: %v", test.executable, err)
			}
			feature := "real-shell-" + test.name
			prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
				Repo: repo, Feature: feature, SourcePlan: sourcePlan,
				Artifact: "source-plan.md", Shell: test.shell, Document: []byte("# Real shell\n"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if output, err := executePlanningEnvelopeWith(repo, test.executable, test.shell, prescription.PlanningEnvelope); err != nil {
				t.Fatalf("%s execution failed: %v: %s", test.name, err, output)
			}
			if _, err := os.Stat(filepath.Join(WorkspaceFor(repo).FeatureDir(feature), "source-plan.md")); err != nil {
				t.Fatalf("%s did not create the artifact: %v", test.name, err)
			}
		})
	}
}
