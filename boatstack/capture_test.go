package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubCaptureRunner drives capture deterministically without a browser or dev
// server. write decides what (if anything) lands at the requested output path.
type stubCaptureRunner struct {
	calls int
	write func(request CaptureRequest) error
}

func (runner *stubCaptureRunner) Run(request CaptureRequest) error {
	runner.calls++
	return runner.write(request)
}

// captureTestRepo builds a managed feature whose plan declares one relevant
// visual scenario and registers a visual capability command. The command itself
// is never executed in tests — the injected runner stands in for the repo-owned
// harness.
func captureTestRepo(t *testing.T, feature string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")

	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Workflow.PRVisualEvidence = "require"
	config.Project.Commands["visual"] = "exit 1" // proves the runner, not the shell command, drives capture
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
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")

	runGit(t, repo, "switch", "-c", "feat/"+feature)
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = feature
	plan["pr_visual_evidence"] = map[string]any{
		"relevance": "relevant",
		"scenarios": []any{map[string]any{
			"id": "warning", "entry": "/onboarding", "state": "picker open", "viewport": "1440x900",
			"expected": []any{"warning visible"},
		}},
	}
	writeMarkdownPlan(t, filepath.Join(directory, "plan.md"), plan, true)
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature work")
	return repo
}

func TestCaptureEvidenceProducesManifestTrustedByPRContext(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	runner := &stubCaptureRunner{write: func(request CaptureRequest) error {
		if request.Capability != "visual" || request.Scenario.ID != "warning" || request.OutputPath == "" {
			t.Fatalf("runner received an ill-formed capture request: %#v", request)
		}
		writeTestPNG(t, request.OutputPath)
		return nil
	}}

	manifest, err := CaptureEvidence(CaptureEvidenceOptions{
		Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner,
	})
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if manifest.Status != "PASS" || len(manifest.Items) != 1 || manifest.Items[0].PrivacyStatus != "clean" {
		t.Fatalf("capture did not produce a conformant PASS manifest: %#v", manifest)
	}
	if !strings.Contains(manifest.Items[0].Path, filepath.Join("boatstack", "visual-evidence")) {
		t.Fatalf("captured PNG was not ingested into Boatstack state: %s", manifest.Items[0].Path)
	}

	// Capture must leave the product tree untouched (evidence lives in Git-common state).
	if status := runGit(t, repo, "status", "--short"); status != "" {
		t.Fatalf("capture mutated the product tree: %s", status)
	}

	// The manifest must be trusted by the same resolver pr-context uses: an
	// identical product diff → status is the manifest's PASS, not NOT_VERIFIED.
	head := runGit(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	_, diffHash, err := captureProductDiff(repo, "main", "reviewer-ready", head)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, status, count, _, _, _, _, resolved, err := resolvePRVisualEvidence(repo, config, "managed", "reviewer-ready", head, diffHash)
	if err != nil {
		t.Fatal(err)
	}
	if status != "PASS" || count != 1 || resolved == nil {
		t.Fatalf("captured evidence was not trusted by pr-context: status=%s count=%d", status, count)
	}

	// A second capture on the same commit is idempotent: it reuses the supervised
	// operation's successful artifact instead of re-running the harness.
	priorCalls := runner.calls
	if _, err := CaptureEvidence(CaptureEvidenceOptions{
		Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner,
	}); err != nil {
		t.Fatalf("idempotent re-capture failed: %v", err)
	}
	if runner.calls != priorCalls {
		t.Fatalf("re-capture re-ran the harness (%d extra calls) instead of reusing the receipt", runner.calls-priorCalls)
	}
}

// control-law: scenario-verification-requires-a-current-receipt
func TestCaptureEvidenceDoesNotReuseStaleOptionalReceipt(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	withReceipt := true
	runner := &stubCaptureRunner{write: func(request CaptureRequest) error {
		writeTestPNG(t, request.OutputPath)
		if withReceipt {
			return os.WriteFile(request.ReceiptPath, []byte(`{"scenario_id":"warning","reached_state_or_url":"/onboarding","checks":[{"name":"warning visible","result":"PASS"}],"overall_result":"PASS"}`), 0o600)
		}
		return nil
	}}
	first, err := CaptureEvidence(CaptureEvidenceOptions{Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].VerificationStatus != "SCENARIO_VERIFIED" {
		t.Fatalf("valid receipt was not verified: %#v", first.Items[0])
	}

	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	config.Project.Commands["visual"] = "changed-capture-command"
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".product-loop/project.json")
	runGit(t, repo, "commit", "-m", "change capture command")
	withReceipt = false
	second, err := CaptureEvidence(CaptureEvidenceOptions{Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if second.Items[0].VerificationStatus != "CAPTURED" || second.Items[0].Receipt != nil {
		t.Fatalf("PNG-only rerun inherited a stale receipt: %#v", second.Items[0])
	}
}

func TestCaptureEvidenceFailsClosedOnNonConformantOutput(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	runner := &stubCaptureRunner{write: func(request CaptureRequest) error {
		// The harness reports success but writes bytes that are not a valid PNG.
		return os.WriteFile(request.OutputPath, []byte("not a png"), 0o600)
	}}

	_, err := CaptureEvidence(CaptureEvidenceOptions{
		Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner,
	})
	if err == nil {
		t.Fatal("capture accepted a non-conformant artifact instead of failing closed")
	}
	if runner.calls != captureMaxAttempts {
		t.Fatalf("capture did not exhaust the supervised retry budget: %d attempts", runner.calls)
	}
	// Nothing may be persisted: a failed capture must not leave trusted evidence.
	if _, err := LoadPRVisualEvidence(repo, "reviewer-ready"); err == nil {
		t.Fatal("a failed capture persisted a manifest")
	}
}

func TestCaptureEvidenceRequiresAResolvedCapabilityCommand(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	// Remove every command alias so the capability resolves to unavailable.
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	delete(config.Project.Commands, "visual")
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = CaptureEvidence(CaptureEvidenceOptions{
		Repo: repo, Capability: "visual", Feature: "reviewer-ready",
		Runner: &stubCaptureRunner{write: func(CaptureRequest) error { return nil }},
	})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("capture ran without a resolved repository command: %v", err)
	}
}

// Invariant: capture resolves the harness per scenario surface — each
// scenario runs its own registered command with the surface in the request —
// and an unresolvable surface fails before any harness runs, naming the exact
// missing registration key.
func TestCaptureEvidenceResolvesPerSurfaceCommands(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	directory := filepath.Join(repo, ".product-loop", "features", "reviewer-ready")
	plan := validPlan()
	plan["feature_id"] = "reviewer-ready"
	plan["pr_visual_evidence"] = map[string]any{
		"relevance": "relevant",
		"scenarios": []any{
			map[string]any{
				"id": "warning", "entry": "/onboarding", "state": "picker open", "viewport": "1440x900",
				"expected": []any{"warning visible"}, "surface": "web",
			},
			map[string]any{
				"id": "console", "entry": "/ops/queues", "state": "backlog shown", "viewport": "1280x800",
				"expected": []any{"queue depth visible"}, "surface": "ops",
			},
		},
	}
	writeMarkdownPlan(t, filepath.Join(directory, "plan.md"), plan, true)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "declare per-surface scenarios")

	configPath := filepath.Join(repo, ".product-loop", "project.json")
	config, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config.Project.Commands["visual:web"] = "run-web-harness"
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "register web surface harness")

	// One surface key missing and no usable... the global "visual" command is
	// still registered in the fixture, so ops falls back to it: capture runs.
	commandsSeen := map[string]string{}
	surfacesSeen := map[string]string{}
	runner := &stubCaptureRunner{write: func(request CaptureRequest) error {
		commandsSeen[request.Scenario.ID] = request.Command
		surfacesSeen[request.Scenario.ID] = request.Scenario.Surface
		writeTestPNG(t, request.OutputPath)
		return nil
	}}
	if _, err := CaptureEvidence(CaptureEvidenceOptions{Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner}); err != nil {
		t.Fatalf("per-surface capture failed: %v", err)
	}
	if commandsSeen["warning"] != "run-web-harness" || surfacesSeen["warning"] != "web" {
		t.Fatalf("web scenario did not run its surface harness: %q (%q)", commandsSeen["warning"], surfacesSeen["warning"])
	}
	if commandsSeen["console"] != "exit 1" || surfacesSeen["console"] != "ops" {
		t.Fatalf("ops scenario did not fall back to the global command: %q (%q)", commandsSeen["console"], surfacesSeen["console"])
	}

	// Remove the global fallback: the ops surface now has no registration and
	// capture refuses up front, naming the exact missing key.
	delete(config.Project.Commands, "visual")
	delete(config.Project.Commands, "screenshot")
	delete(config.Project.Commands, "e2e")
	value, err = MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "remove global harness")
	priorCalls := runner.calls
	if _, err := CaptureEvidence(CaptureEvidenceOptions{Repo: repo, Capability: "visual", Feature: "reviewer-ready", Runner: runner}); err == nil || !strings.Contains(err.Error(), "visual:ops") {
		t.Fatalf("unresolvable surface was not named: %v", err)
	}
	if runner.calls != priorCalls {
		t.Fatal("capture ran a harness despite an unresolvable surface")
	}
}

// Invariant: capability-register --surface writes the surface-scoped command
// key, and the registered command is what surface resolution selects.
func TestRegisterCapabilityCommandWithSurface(t *testing.T) {
	repo := captureTestRepo(t, "reviewer-ready")
	registered, err := RegisterCapabilityCommand(repo, "visual", "ops", "npm run capture:ops")
	if err != nil {
		t.Fatal(err)
	}
	if registered.Alias != "visual:ops" {
		t.Fatalf("surface registration wrote the wrong key: %#v", registered)
	}
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveCapabilityForSurface("visual", "ops", config)
	if err != nil || resolution.Command != "npm run capture:ops" {
		t.Fatalf("registered surface command did not resolve: %#v %v", resolution, err)
	}
	if _, err := RegisterCapabilityCommand(repo, "visual", "Web Ops", "x"); err == nil || !strings.Contains(err.Error(), "kebab") {
		t.Fatalf("invalid surface slug was not rejected: %v", err)
	}
}
