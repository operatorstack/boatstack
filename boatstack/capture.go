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
	Repo       string
	Capability string
	Command    string
	Scenario   PRVisualScenario
	OutputPath string
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
		"BOATSTACK_CAPTURE_OUTPUT="+request.OutputPath,
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
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		return PRVisualEvidenceManifest{}, fmt.Errorf("capture requires a valid Boatstack project configuration: %w", err)
	}
	resolution, err := ResolveCapability(name, config)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if resolution.Kind != "repository-command" {
		return PRVisualEvidenceManifest{}, fmt.Errorf("evidence capability %q is unavailable: register a repository command (project.commands) or provision it first", name)
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
		if err := captureScenario(repo, capability, resolution.Command, scenario, outputPath, feature, head, headCommit, diffHash, runner); err != nil {
			return PRVisualEvidenceManifest{}, err
		}
		items = append(items, PRVisualEvidenceItem{
			ScenarioID:    scenario.ID,
			Path:          outputPath,
			MIMEType:      "image/png",
			Viewport:      scenario.Viewport,
			CapturedAt:    time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
			Status:        "captured",
			PrivacyStatus: "clean",
		})
	}

	manifest := PRVisualEvidenceManifest{
		Key:               key,
		Policy:            config.Workflow.PRVisualEvidence,
		Relevance:         relevance,
		RelevanceSource:   source,
		Status:            "PASS",
		SourceCommit:      headCommit,
		ProductDiffSHA256: diffHash,
		Scenarios:         scenarios,
		Items:             items,
	}
	saved, err := SavePRVisualEvidence(repo, manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, fmt.Errorf("captured %s evidence is non-conformant (BLOCKED): %w", name, err)
	}
	return saved, nil
}

// captureProductDiff reproduces the pr-context product-diff fingerprint so a
// captured manifest is trusted (PASS) by resolvePRVisualEvidence: same head
// commit and same product diff.
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
func captureScenario(repo string, capability Capability, command string, scenario PRVisualScenario, outputPath, feature, head, headCommit, diffHash string, runner CaptureRunner) error {
	fingerprint := SHA256Bytes([]byte(strings.Join([]string{
		command, scenario.ID, scenario.Viewport, scenario.Entry, scenario.State, headCommit, diffHash,
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
		runErr := runner.Run(CaptureRequest{
			Repo: repo, Capability: capability.Name, Command: command, Scenario: scenario, OutputPath: outputPath,
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
