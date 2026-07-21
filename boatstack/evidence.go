package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EvidenceRecord represents a host-generated observation of the repository state.
type EvidenceRecord struct {
	ID                 string   `json:"id"`
	Operation          string   `json:"operation"`
	Path               string   `json:"path,omitempty"`
	Query              string   `json:"query,omitempty"`
	Matches            []string `json:"matches,omitempty"`
	Anchors            []string `json:"anchors,omitempty"`
	RepositoryRevision string   `json:"repository_revision"`
	CreatedBy          string   `json:"created_by"`
}

// LoadEvidenceLedger loads the evidence records from the specified path.
func LoadEvidenceLedger(path string) (map[string]EvidenceRecord, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]EvidenceRecord), nil
		}
		return nil, fmt.Errorf("failed to read evidence ledger: %w", err)
	}

	var records []EvidenceRecord
	if err := json.Unmarshal(value, &records); err != nil {
		return nil, fmt.Errorf("failed to parse evidence ledger: %w", err)
	}

	ledger := make(map[string]EvidenceRecord)
	for _, record := range records {
		ledger[record.ID] = record
	}
	return ledger, nil
}

// ValidateEvidencePath ensures an evidence path is repository-relative and safe.
func ValidateEvidencePath(repoRoot string, evPath string) error {
	repoAbsolute, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve repository root: %w", err)
	}

	if filepath.IsAbs(evPath) {
		return fmt.Errorf("evidence path must be relative, got absolute: %s", evPath)
	}

	cleanPath := filepath.Clean(evPath)
	if strings.HasPrefix(cleanPath, "..") || cleanPath == "." {
		return fmt.Errorf("evidence path cannot traverse outside repository: %s", evPath)
	}

	targetPath := filepath.Join(repoAbsolute, cleanPath)
	
	// Check if path resolves outside repo (e.g. through symlinks)
	evalPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("evidence path does not exist: %s", evPath)
		}
		return fmt.Errorf("failed to evaluate evidence path: %w", err)
	}

	if !strings.HasPrefix(evalPath, repoAbsolute) {
		return fmt.Errorf("evidence path resolves outside repository: %s", evPath)
	}

	info, err := os.Stat(evalPath)
	if err != nil {
		return fmt.Errorf("failed to stat evidence path: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("evidence path is not a regular file: %s", evPath)
	}

	return nil
}

// CheckFileAnchors verifies that all expected anchors are present in the file content.
func CheckFileAnchors(filePath string, anchors []string) error {
	if len(anchors) == 0 {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file for anchor check: %w", err)
	}
	contentStr := string(content)

	var missing []string
	for _, anchor := range anchors {
		if !strings.Contains(contentStr, anchor) {
			missing = append(missing, anchor)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("file missing expected anchors: %v", missing)
	}

	return nil
}
