package boatstack

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const captureMaxAttempts = 3

// CaptureRequest is the framework-agnostic contract Boatstack passes to the
// repository-owned capture harness for one scenario. Boatstack ships this
// contract; the harness (authored in the user's repository) satisfies it by
// writing exactly one PNG to OutputPath. The contract is surfaced to the harness
// as environment variables (see execCaptureRunner).
type CaptureRequest struct {
	Repo        string
	Capability  string
	Command     string
	Scenario    PRVisualScenario
	OutputPath  string
	ReceiptPath string
}

// CaptureRunner runs one scenario's repository capture command. It must produce
// a valid PNG at request.OutputPath. It is an interface so tests can drive
// capture deterministically without a real browser or dev server.
type CaptureRunner interface {
	Run(request CaptureRequest) error
}

// execCaptureRunner invokes the repository-owned command through the shell,
// exposing the capture contract as environment variables. The command is taken
// from trusted project configuration.
type execCaptureRunner struct{}

func (execCaptureRunner) Run(request CaptureRequest) error {
	command := exec.Command("sh", "-c", request.Command)
	command.Dir = request.Repo
	command.Env = append(os.Environ(),
		"BOATSTACK_CAPTURE_CAPABILITY="+request.Capability,
		"BOATSTACK_CAPTURE_SCENARIO_ID="+request.Scenario.ID,
		"BOATSTACK_CAPTURE_ENTRY="+request.Scenario.Entry,
		"BOATSTACK_CAPTURE_STATE="+request.Scenario.State,
		"BOATSTACK_CAPTURE_VIEWPORT="+request.Scenario.Viewport,
		"BOATSTACK_CAPTURE_SURFACE="+request.Scenario.Surface,
		"BOATSTACK_CAPTURE_OUTPUT="+request.OutputPath,
		"BOATSTACK_CAPTURE_RECEIPT="+request.ReceiptPath,
	)
	// The harness's authoritative output is the PNG on disk, not stdout; only
	// stderr is retained, as bounded diagnostics for a failed capture.
	var diagnostics bytes.Buffer
	command.Stdout = nil
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return fmt.Errorf("capture command failed: %w: %s", err, boundedObservation(diagnostics.String()))
	}
	return nil
}

// CaptureEvidenceOptions configures a managed capture run.
type CaptureEvidenceOptions struct {
	Repo       string
	Capability string
	Feature    string
	Base       string
	Runner     CaptureRunner
}

// CaptureEvidence orchestrates capture for a managed feature. It resolves the
// repository-owned capability command, reads the plan-declared scenarios, runs
// each scenario as a supervised operation, stamps the manifest to the current
// head commit and product diff, and ingests it through SavePRVisualEvidence.
// The manifest is trusted only if it conforms; a non-conformant manifest is a
// blocking error, never a silent PASS.
func CaptureEvidence(options CaptureEvidenceOptions) (PRVisualEvidenceManifest, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	name := strings.TrimSpace(options.Capability)
	if name == "" {
		name = "visual"
	}
	capability, ok := LookupCapability(name)
	if !ok {
		return PRVisualEvidenceManifest{}, fmt.Errorf("unknown evidence capability %q", name)
	}
	feature := strings.TrimSpace(options.Feature)
	if feature == "" {
		return PRVisualEvidenceManifest{}, fmt.Errorf("capture requires a managed --feature")
	}
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		return PRVisualEvidenceManifest{}, fmt.Errorf("capture requires a valid Boatstack project configuration: %w", err)
	}
	relevance, source, scenarios, err := planVisualDecision(repo, feature)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if relevance == "not_relevant" {
		return PRVisualEvidenceManifest{}, fmt.Errorf("%s evidence is marked not_relevant for %q; nothing to capture", name, feature)
	}
	if len(scenarios) == 0 {
		return PRVisualEvidenceManifest{}, fmt.Errorf("no %s scenarios declared in the plan (pr_visual_evidence.scenarios)", name)
	}
	// Every scenario's command must resolve before any capture runs — a
	// surface-scoped key outranks the global alias; a missing surface key
	// with no global fallback is named exactly, never captured around.
	commands, err := resolveScenarioCaptureCommands(name, scenarios, config)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}

	head, err := gitCommand(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	base := strings.TrimSpace(options.Base)
	if base == "" {
		base = strings.TrimSpace(config.Project.DefaultBranch)
	}
	if base == "" {
		base = defaultPRBase(repo)
	}
	headCommit, diffHash, err := captureProductDiff(repo, base, feature, head)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}

	key, err := visualEvidenceKey("managed", feature, head)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	stagingDir, err := captureStagingDirectory(repo, key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	runner := options.Runner
	if runner == nil {
		runner = execCaptureRunner{}
	}

	items := make([]PRVisualEvidenceItem, 0, len(scenarios))
	for _, scenario := range scenarios {
		outputPath := filepath.Join(stagingDir, scenario.ID+".png")
		receiptPath := filepath.Join(stagingDir, scenario.ID+".receipt.json")
		if err := captureScenario(repo, capability, commands[scenario.ID], scenario, outputPath, receiptPath, feature, head, headCommit, diffHash, runner); err != nil {
			return PRVisualEvidenceManifest{}, err
		}
		verificationStatus := "CAPTURED"
		var receipt *PRVisualScenarioReceipt
		if parsed, receiptErr := loadVisualScenarioReceipt(receiptPath, scenario.ID); receiptErr == nil {
			receipt, verificationStatus = parsed, "SCENARIO_VERIFIED"
		} else if !os.IsNotExist(receiptErr) {
			return PRVisualEvidenceManifest{}, receiptErr
		}
		items = append(items, PRVisualEvidenceItem{
			ScenarioID:         scenario.ID,
			Path:               outputPath,
			MIMEType:           "image/png",
			Viewport:           scenario.Viewport,
			CapturedAt:         time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
			Status:             "CAPTURED",
			PrivacyStatus:      "clean",
			VerificationStatus: verificationStatus,
			Receipt:            receipt,
		})
	}
	scenarioRaw, err := MarshalJSON(scenarios)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	commandRaw, err := MarshalJSON(commands)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}

	manifest := PRVisualEvidenceManifest{
		Key: key,
		// The manifest records the configured policy verbatim (informational);
		// the effective policy — including plan-escalated require semantics —
		// is re-derived by resolvePRVisualEvidence at every decode.
		Policy:                   config.Workflow.PRVisualEvidence,
		Relevance:                relevance,
		RelevanceSource:          source,
		Status:                   "PASS",
		SourceCommit:             headCommit,
		ProductDiffSHA256:        diffHash,
		ScenarioDefinitionSHA256: SHA256Bytes(scenarioRaw),
		CaptureCommandSHA256:     SHA256Bytes(commandRaw),
		Scenarios:                scenarios,
		Items:                    items,
	}
	saved, err := SavePRVisualEvidence(repo, manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, fmt.Errorf("captured %s evidence is non-conformant (BLOCKED): %w", name, err)
	}
	return saved, nil
}

// resolveScenarioCaptureCommands resolves the harness command for every
// scenario up front (surface key first, global alias fallback), so capture
// either runs with a complete command map or fails naming the exact missing
// registration before any harness executes.
func resolveScenarioCaptureCommands(name string, scenarios []PRVisualScenario, config ProjectConfig) (map[string]string, error) {
	commands := make(map[string]string, len(scenarios))
	for _, scenario := range scenarios {
		resolution, err := ResolveCapabilityForSurface(name, scenario.Surface, config)
		if err != nil {
			return nil, err
		}
		if resolution.Kind != "repository-command" {
			if surface := strings.TrimSpace(scenario.Surface); surface != "" {
				return nil, fmt.Errorf("evidence capability %q is unavailable for surface %q: register project.commands[%q] (capability-register --capability %s --surface %s --command <command>) or a global command", name, surface, name+":"+surface, name, surface)
			}
			return nil, fmt.Errorf("evidence capability %q is unavailable: register a repository command (capability-register --capability %s --command <command>) or provision it first", name, name)
		}
		commands[scenario.ID] = resolution.Command
	}
	return commands, nil
}

// captureProductDiff reproduces the pr-context product-diff fingerprint so a
// captured manifest is trusted (PASS) by resolvePRVisualEvidence: same product
// diff. The head commit is recorded for provenance only — trust is keyed to
// product identity so committing the reviewed pr.md never stales evidence.
func captureProductDiff(repo, base, feature, head string) (headCommit, diffHash string, err error) {
	baseCommit, err := resolveBaseCommit(repo, base)
	if err != nil {
		return "", "", err
	}
	mergeBaseCommit, err := gitCommand(repo, "merge-base", baseCommit, "HEAD")
	if err != nil || mergeBaseCommit == "" {
		return "", "", fmt.Errorf("cannot determine the merge base between %s and %s", base, head)
	}
	headCommit, err = gitCommand(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	previewPath, err := expectedPRPreviewPath("managed", feature, head)
	if err != nil {
		return "", "", err
	}
	diff, _, err := productDiff(repo, mergeBaseCommit, previewPath)
	if err != nil {
		return "", "", err
	}
	return headCommit, SHA256Bytes(diff), nil
}

func captureStagingDirectory(repo, key string) (string, error) {
	directory, err := visualEvidenceDirectory(repo, key)
	if err != nil {
		return "", err
	}
	staging := filepath.Join(directory, "capture-staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return "", err
	}
	return staging, nil
}

// captureScenario runs one scenario as a supervised operation with a bounded
// retry budget. The fingerprint is stable for a given command, scenario, and
// product diff, so a successful capture on the same commit is reused rather than
// re-run.
func captureScenario(repo string, capability Capability, command string, scenario PRVisualScenario, outputPath, receiptPath, feature, head, headCommit, diffHash string, runner CaptureRunner) error {
	scenarioRaw, err := MarshalJSON(scenario)
	if err != nil {
		return err
	}
	fingerprint := SHA256Bytes([]byte(strings.Join([]string{
		command, string(scenarioRaw), diffHash,
	}, "\x00")))
	kind := "capture:" + capability.Name
	postcondition := fmt.Sprintf("valid PNG captured for scenario %s (%s)", scenario.ID, scenario.Viewport)

	prepared, err := PrepareOperation(OperationPrepareOptions{
		Repo:                     repo,
		Kind:                     kind,
		Scope:                    OperationScope{Feature: feature, HeadBranch: head},
		Target:                   outputPath,
		PackageFingerprint:       fingerprint,
		AuthorizationFingerprint: fingerprint,
		RetryClass:               capability.RetryClass,
		MaxAttempts:              captureMaxAttempts,
		ExpectedPostcondition:    postcondition,
	})
	if err != nil {
		return fmt.Errorf("prepare capture of %s: %w", scenario.ID, err)
	}
	if prepared.State == OperationSucceeded {
		if verifyCapturedPNG(outputPath) == nil {
			return nil
		}
		return fmt.Errorf("capture of %s already succeeded but its artifact is missing; run operation-status and reconcile", scenario.ID)
	}

	var lastErr error
	for attempt := 0; attempt < captureMaxAttempts; attempt++ {
		begun, beginErr := BeginOperation(repo, prepared.OperationID, fmt.Sprintf("%s@%d", scenario.ID, attempt), kind)
		if beginErr != nil {
			return fmt.Errorf("begin capture of %s: %w", scenario.ID, beginErr)
		}
		if begun.Receipt.State == OperationSucceeded {
			if verifyCapturedPNG(outputPath) == nil {
				return nil
			}
			return fmt.Errorf("capture of %s reports success but its artifact is missing", scenario.ID)
		}
		// A new package fingerprint must not inherit an optional receipt left by
		// an older harness run. PNG-only remains CAPTURED.
		if err := os.Remove(receiptPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear stale receipt for %s: %w", scenario.ID, err)
		}
		runErr := runner.Run(CaptureRequest{
			Repo: repo, Capability: capability.Name, Command: command, Scenario: scenario, OutputPath: outputPath, ReceiptPath: receiptPath,
		})
		if runErr == nil {
			runErr = verifyCapturedPNG(outputPath)
		}
		if runErr == nil {
			if _, err := CompleteOperation(repo, prepared.OperationID, begun.LeaseToken, "SUCCEEDED", "captured "+scenario.ID, outputPath); err != nil {
				return fmt.Errorf("record capture success for %s: %w", scenario.ID, err)
			}
			return nil
		}
		lastErr = runErr
		receipt, completeErr := CompleteOperation(repo, prepared.OperationID, begun.LeaseToken, "RETRYABLE", runErr.Error(), "")
		if completeErr != nil {
			return fmt.Errorf("record capture retry for %s: %w", scenario.ID, completeErr)
		}
		if receipt.State == OperationFailedFinal {
			break
		}
	}
	return fmt.Errorf("capture of scenario %s failed after %d attempts: %w", scenario.ID, captureMaxAttempts, lastErr)
}

func loadVisualScenarioReceipt(path, scenarioID string) (*PRVisualScenarioReceipt, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var receipt PRVisualScenarioReceipt
	if err := DecodeJSON("visual scenario receipt", path, raw, &receipt); err != nil {
		return nil, err
	}
	if receipt.ScenarioID != scenarioID || strings.TrimSpace(receipt.Reached) == "" || len(receipt.Checks) == 0 || !strings.EqualFold(receipt.OverallResult, "PASS") {
		return nil, fmt.Errorf("visual scenario receipt for %s is invalid or failing", scenarioID)
	}
	for _, check := range receipt.Checks {
		if strings.TrimSpace(check.Name) == "" || !strings.EqualFold(check.Result, "PASS") {
			return nil, fmt.Errorf("visual scenario receipt for %s contains an unnamed or failing check", scenarioID)
		}
	}
	return &receipt, nil
}

func verifyCapturedPNG(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("capture output is missing or unsafe: %s", path)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("capture output is not a valid PNG: %w", err)
	}
	if configuration.Width < 1 || configuration.Height < 1 {
		return fmt.Errorf("capture output has no pixels: %s", path)
	}
	return nil
}
