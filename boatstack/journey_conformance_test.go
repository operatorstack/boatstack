package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// control-law: journey-evidence-is-explicit-complete-and-fingerprint-bound
func TestJourneyEvidenceDecisionAndManifest(t *testing.T) {
	plan := validPlan()
	plan["schema_version"] = float64(3)
	plan["architecture_facts"] = []any{}
	plan["architecture_unknowns"] = []any{}
	if err := ValidatePlan(plan, nil); err == nil || !strings.Contains(err.Error(), "journey_evidence") {
		t.Fatalf("schema-v3 plan without a journey decision must fail: %v", err)
	}

	plan["journey_evidence"] = map[string]any{"relevance": "not_relevant", "reason": ""}
	if err := ValidatePlan(plan, nil); err == nil || !strings.Contains(err.Error(), "requires a reason") {
		t.Fatalf("not_relevant without a reason must fail: %v", err)
	}

	plan["journey_evidence"] = map[string]any{
		"relevance": "relevant",
		"oracles": []any{map[string]any{
			"id": "J-1", "type": "cli", "criteria": []any{"AC-1"},
			"entry_point": "boatstack-helper", "steps": []any{"run the command"},
			"expected": []any{"exit zero"}, "run": "go test ./...",
			"oracle": "exit status", "independence": "contract-derived",
		}},
	}
	if err := ValidatePlan(plan, nil); err != nil {
		t.Fatalf("complete journey decision rejected: %v", err)
	}
	first, err := CompileJourneyManifest(plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileJourneyManifest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("journey manifest must compile deterministically")
	}
	var manifest map[string]any
	if err := json.Unmarshal(first, &manifest); err != nil {
		t.Fatal(err)
	}
	if stringValue(manifest["manifest_sha256"]) == "" {
		t.Fatal("compiled journey manifest must carry its fingerprint")
	}
	plan["journey_evidence"].(map[string]any)["oracles"].([]any)[0].(map[string]any)["run"] = "definitely-missing-boatstack-capability --check"
	if err := checkJourneyCapabilities(t.TempDir(), plan); err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("missing journey capability must block readiness: %v", err)
	}
}

// control-law: journey-results-must-match-manifest-head-diff-and-pass
func TestJourneyResultsRejectFailureAndStaleness(t *testing.T) {
	repo := prTestRepo(t)
	feature := "journey-feature"
	dir := filepath.Join(repo, ".product-loop", "features", feature, "compiled")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": 1, "feature_id": feature, "relevance": "relevant",
		"reason": "", "oracles": []any{map[string]any{"id": "J-1"}},
	}
	canonical, _ := MarshalJSON(manifest)
	manifest["manifest_sha256"] = SHA256Bytes(canonical)
	manifestValue, _ := MarshalJSON(manifest)
	if err := os.WriteFile(filepath.Join(dir, "journey-oracles.json"), manifestValue, 0o644); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "journey-input.json")
	if err := os.WriteFile(input, []byte(`{"results":[{"oracle_id":"J-1","status":"FAIL","evidence":["trace"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "record journey fixture")
	_, currentHead, currentDiff, _, err := currentDiffIdentity(repo, defaultPRBase(repo), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCurrentJourneyResults(repo, feature, defaultPRBase(repo), currentHead, currentDiff); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing journey results must block gates: %v", err)
	}
	recorded, err := RecordJourneyResults(JourneyResultsOptions{Repo: repo, Feature: feature, InputPath: input})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCurrentJourneyResults(repo, feature, defaultPRBase(repo), recorded.HeadCommit, recorded.DiffSHA256); err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("failed journey must block gates: %v", err)
	}
	if err := checkCurrentJourneyResults(repo, feature, defaultPRBase(repo), recorded.HeadCommit, "different"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale journey must block gates: %v", err)
	}
	if err := os.WriteFile(input, []byte(`{"results":[{"oracle_id":"J-1","status":"PASS","evidence":["current trace"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	passed, err := RecordJourneyResults(JourneyResultsOptions{Repo: repo, Feature: feature, InputPath: input})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCurrentJourneyResults(repo, feature, defaultPRBase(repo), passed.HeadCommit, passed.DiffSHA256); err != nil {
		t.Fatalf("current passing journey results must permit progression: %v", err)
	}
}
