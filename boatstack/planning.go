package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var featureSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var planningArtifacts = map[string]bool{
	"source-plan.md":  true,
	"feature-spec.md": true,
	"questions.md":    true,
	"gaps.md":         true,
	"test-plan.md":    true,
	"plan.md":         true,
}

type PlanningWriteOptions struct {
	Repo     string
	Feature  string
	Artifact string
	Content  []byte
}

type ApprovalRecordOptions struct {
	PlanPath    string
	OutputPath  string
	ApprovedBy  string
	ApprovedAt  string
	Fingerprint string
}

func rejectSymlinkComponents(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes repository boundary: %s", target)
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlinked planning path: %s", current)
		}
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".boatstack-planning-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}

func WritePlanningArtifact(options PlanningWriteOptions) (string, error) {
	if !featureSlugPattern.MatchString(options.Feature) {
		return "", fmt.Errorf("feature must be a lowercase kebab-case slug")
	}
	if !planningArtifacts[options.Artifact] {
		return "", fmt.Errorf("unsupported planning artifact: %s", options.Artifact)
	}
	if !utf8.Valid(options.Content) {
		return "", fmt.Errorf("planning artifact must be valid UTF-8 Markdown")
	}
	if strings.TrimSpace(string(options.Content)) == "" {
		return "", fmt.Errorf("planning artifact must not be empty")
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(repo, ".product-loop", "features", options.Feature, options.Artifact)
	if err := rejectSymlinkComponents(repo, destination); err != nil {
		return "", err
	}
	if err := atomicWrite(destination, options.Content); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(repo, destination)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func RecordApproval(options ApprovalRecordOptions) error {
	if strings.TrimSpace(options.ApprovedBy) == "" {
		return fmt.Errorf("approval requires a named human")
	}
	approvedAt, err := time.Parse(time.RFC3339, options.ApprovedAt)
	if err != nil {
		return fmt.Errorf("approval timestamp must be RFC3339")
	}
	check, err := CheckPlan(options.PlanPath)
	if err != nil {
		return err
	}
	if options.Fingerprint != check.Fingerprint {
		return fmt.Errorf("approval fingerprint does not match the current plan")
	}
	expectedOutput := filepath.Join(filepath.Dir(options.PlanPath), "approval.md")
	output := options.OutputPath
	if output == "" {
		output = expectedOutput
	}
	expectedAbsolute, err := filepath.Abs(expectedOutput)
	if err != nil {
		return err
	}
	outputAbsolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if filepath.Clean(outputAbsolute) != filepath.Clean(expectedAbsolute) {
		return fmt.Errorf("approval receipt must be written beside plan.md as approval.md")
	}
	planDirectory, err := filepath.Abs(filepath.Dir(options.PlanPath))
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(planDirectory, outputAbsolute); err != nil {
		return err
	}
	payload, err := MarshalJSON(map[string]any{
		"schema_version":       1,
		"status":               "APPROVED",
		"approved_by":          strings.TrimSpace(options.ApprovedBy),
		"approved_at":          approvedAt.Format(time.RFC3339),
		"approval_fingerprint": check.Fingerprint,
	})
	if err != nil {
		return err
	}
	body := "# Plan approval\n\n" + approvalMarkerStart + "\n```json\n" + strings.TrimSpace(string(payload)) + "\n```\n" + approvalMarkerEnd + "\n"
	return atomicWrite(outputAbsolute, []byte(body))
}

type installLock struct {
	BoatstackVersion string `json:"boatstack_version"`
	SourceCommit     string `json:"source_commit"`
	BinaryPath       string `json:"binary_path"`
	BinarySHA256     string `json:"binary_sha256"`
}

func Doctor(repoPath string) error {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return err
	}
	configPath := filepath.Join(repo, ".boatstack-project.json")
	config, raw, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("invalid or missing .boatstack-project.json: %w", err)
	}
	bundle, err := BuildExportBundle(configPath, config, raw, "boatstack")
	if err != nil {
		return err
	}
	if err := CheckExport(repo, bundle.Files); err != nil {
		return err
	}
	if err := CheckHostHooks(repo, config.Adapters); err != nil {
		return err
	}
	for _, host := range []string{"cursor", "claude", "codex"} {
		if !contains(config.Adapters, host) {
			continue
		}
		var input []byte
		if host == "cursor" {
			input = []byte(`{"command":"git status --short"}`)
		} else {
			input = []byte(`{"tool_name":"Bash","tool_input":{"command":"git status --short"}}`)
		}
		if _, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: input}); denied {
			return fmt.Errorf("%s safety hook denied its read-only smoke event", host)
		}
		if _, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(`{"malformed":true}`)}); !denied {
			return fmt.Errorf("%s safety hook did not fail closed on malformed input", host)
		}
	}
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	value, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("missing local install lock: %w", err)
	}
	var lock installLock
	if err := json.Unmarshal(value, &lock); err != nil {
		return fmt.Errorf("invalid local install lock: %w", err)
	}
	if lock.BoatstackVersion != Version || lock.SourceCommit != SourceCommit {
		return fmt.Errorf("helper version drift: installed %s (%s), expected %s (%s)", lock.BoatstackVersion, lock.SourceCommit, Version, SourceCommit)
	}
	binaryPath, err := resolveRepositoryRelativePath(repo, lock.BinaryPath)
	if err != nil {
		return fmt.Errorf("invalid Boatstack helper path in install lock: %w", err)
	}
	if err := checkNonEmptyFile(binaryPath, "Boatstack helper"); err != nil {
		return err
	}
	hash, err := SHA256File(binaryPath)
	if err != nil {
		return err
	}
	if hash != lock.BinarySHA256 {
		return fmt.Errorf("Boatstack helper checksum does not match the install lock")
	}
	return nil
}

func DoctorRepairHint(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w; repair: rerun the Boatstack installer from the repository root, then reload the coding host", err)
}
