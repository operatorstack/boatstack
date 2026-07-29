package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const journeyManifestSchemaVersion = 1

type JourneyResult struct {
	OracleID string   `json:"oracle_id"`
	Status   string   `json:"status"`
	Evidence []string `json:"evidence"`
}

type JourneyResults struct {
	SchemaVersion  int             `json:"schema_version"`
	Feature        string          `json:"feature"`
	ManifestSHA256 string          `json:"manifest_sha256"`
	HeadCommit     string          `json:"head_commit"`
	DiffSHA256     string          `json:"diff_sha256"`
	Results        []JourneyResult `json:"results"`
	RecordedAt     string          `json:"recorded_at"`
}

type JourneyResultsOptions struct {
	Repo       string
	Feature    string
	BaseBranch string
	InputPath  string
}

func CompileJourneyManifest(plan map[string]any) ([]byte, error) {
	version, _ := plan["schema_version"].(float64)
	if err := validateJourneyEvidence(plan, version); err != nil {
		return nil, err
	}
	decision, _ := plan["journey_evidence"].(map[string]any)
	relevance := "not_relevant"
	reason := "legacy plan does not require journey evidence"
	oracles := []any{}
	if decision != nil {
		relevance = strings.ToLower(stringValue(decision["relevance"]))
		reason = stringValue(decision["reason"])
		if values, ok := decision["oracles"].([]any); ok {
			oracles = values
		}
	}
	body := map[string]any{
		"schema_version": journeyManifestSchemaVersion,
		"feature_id":     stringValue(plan["feature_id"]),
		"relevance":      relevance,
		"reason":         reason,
		"oracles":        oracles,
	}
	canonical, err := MarshalJSON(body)
	if err != nil {
		return nil, err
	}
	body["manifest_sha256"] = SHA256Bytes(canonical)
	return MarshalJSON(body)
}

func journeyManifestPath(repo, feature string) string {
	directory := filepath.Join(WorkspaceFor(repo).GeneratedRoot(), "features", feature)
	return featureArtifactPath(directory, filepath.Join("compiled", "journey-oracles.json"), "journey-oracles.json")
}

func journeyResultsPath(repo, feature string) string {
	statePath, err := deliveryStatePath(repo, feature)
	if err != nil {
		return filepath.Join(repo, ".boatstack-invalid-state", feature, "journey-results.json")
	}
	return filepath.Join(filepath.Dir(statePath), "journey-results.json")
}

func loadJourneyManifest(path string) (map[string]any, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest map[string]any
	if err := DecodeJSON("journey oracle manifest", path, value, &manifest); err != nil {
		return nil, err
	}
	fingerprint := stringValue(manifest["manifest_sha256"])
	if fingerprint == "" {
		return nil, fmt.Errorf("journey oracle manifest fingerprint is missing")
	}
	delete(manifest, "manifest_sha256")
	canonical, err := MarshalJSON(manifest)
	if err != nil {
		return nil, err
	}
	if SHA256Bytes(canonical) != fingerprint {
		return nil, fmt.Errorf("journey oracle manifest fingerprint does not match its contents")
	}
	manifest["manifest_sha256"] = fingerprint
	return manifest, nil
}

func RecordJourneyResults(options JourneyResultsOptions) (JourneyResults, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return JourneyResults{}, err
	}
	manifestPath := journeyManifestPath(repo, options.Feature)
	manifest, err := loadJourneyManifest(manifestPath)
	if err != nil {
		return JourneyResults{}, fmt.Errorf("journey oracle manifest is missing: %w", err)
	}
	if stringValue(manifest["relevance"]) != "relevant" {
		return JourneyResults{}, fmt.Errorf("journey results may only be recorded for a relevant journey manifest")
	}
	input, err := os.ReadFile(options.InputPath)
	if err != nil {
		return JourneyResults{}, err
	}
	var submitted struct {
		Results []JourneyResult `json:"results"`
	}
	if err := DecodeJSON("journey results", options.InputPath, input, &submitted); err != nil {
		return JourneyResults{}, err
	}
	required := map[string]bool{}
	oracles, _ := objectSlice(manifest["oracles"])
	for _, oracle := range oracles {
		required[stringValue(oracle["id"])] = true
	}
	seen := map[string]bool{}
	for _, result := range submitted.Results {
		if !required[result.OracleID] || seen[result.OracleID] {
			return JourneyResults{}, fmt.Errorf("journey result has unknown or duplicate oracle %q", result.OracleID)
		}
		if result.Status != "PASS" && result.Status != "FAIL" {
			return JourneyResults{}, fmt.Errorf("journey oracle %s status must be PASS or FAIL", result.OracleID)
		}
		if len(result.Evidence) == 0 {
			return JourneyResults{}, fmt.Errorf("journey oracle %s requires evidence", result.OracleID)
		}
		for _, evidence := range result.Evidence {
			if strings.TrimSpace(evidence) == "" {
				return JourneyResults{}, fmt.Errorf("journey oracle %s evidence must be non-empty", result.OracleID)
			}
		}
		seen[result.OracleID] = true
	}
	for id := range required {
		if !seen[id] {
			return JourneyResults{}, fmt.Errorf("journey result is missing required oracle %s", id)
		}
	}
	base := strings.TrimSpace(options.BaseBranch)
	if base == "" {
		base = defaultPRBase(repo)
	}
	_, headCommit, diffSHA, _, err := currentDiffIdentity(repo, base, "")
	if err != nil {
		return JourneyResults{}, err
	}
	results := JourneyResults{
		SchemaVersion: 1, Feature: options.Feature,
		ManifestSHA256: stringValue(manifest["manifest_sha256"]),
		HeadCommit:     headCommit, DiffSHA256: diffSHA, Results: submitted.Results,
		RecordedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	sort.Slice(results.Results, func(i, j int) bool { return results.Results[i].OracleID < results.Results[j].OracleID })
	value, _ := MarshalJSON(results)
	if err := atomicWriteMode(journeyResultsPath(repo, options.Feature), value, 0o644); err != nil {
		return JourneyResults{}, err
	}
	return results, nil
}

func checkCurrentJourneyResults(repo, feature, base, headCommit, diffSHA string) error {
	manifestPath := journeyManifestPath(repo, feature)
	manifest, err := loadJourneyManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("required journey manifest is missing: %w", err)
	}
	if stringValue(manifest["relevance"]) != "relevant" {
		return nil
	}
	value, err := os.ReadFile(journeyResultsPath(repo, feature))
	if err != nil {
		return fmt.Errorf("required journey results are missing")
	}
	var results JourneyResults
	if err := DecodeJSON("journey results", journeyResultsPath(repo, feature), value, &results); err != nil {
		return err
	}
	if results.ManifestSHA256 != stringValue(manifest["manifest_sha256"]) || results.HeadCommit != headCommit || results.DiffSHA256 != diffSHA {
		return fmt.Errorf("required journey results are stale for the current manifest, head, or diff")
	}
	for _, result := range results.Results {
		if result.Status != "PASS" {
			return fmt.Errorf("required journey oracle %s did not pass", result.OracleID)
		}
	}
	return nil
}
