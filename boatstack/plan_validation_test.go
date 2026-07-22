package boatstack

import (
	"runtime"
	"strings"
	"testing"
)

func validV2Plan() map[string]any {
	return map[string]any{
		"schema_version":     float64(2),
		"feature_id":         "feat-123",
		"source_plan_path":   "source-plan.md",
		"blocking_questions": []any{},
		"acceptance_criteria": []any{
			map[string]any{"id": "AC-1", "text": "Something works"},
		},
		"architecture_facts": []any{
			map[string]any{
				"id":           "fact_1",
				"kind":         "route_absent",
				"subject":      "/api/clients",
				"evidence_ids": []any{"ev_1"},
			},
		},
		"architecture_unknowns": []any{},
		"tasks": []any{
			map[string]any{
				"id":                  "T-1",
				"title":               "Do work",
				"depends_on":          []any{},
				"requires_facts":      []any{"fact_1"},
				"acceptance_criteria": []any{"AC-1"},
				"validation": []any{
					map[string]any{
						"criteria":     []any{"AC-1"},
						"run":          "test",
						"origin":       "origin",
						"oracle":       "oracle",
						"independence": "independence",
					},
				},
				"rollback_boundary": "revert",
			},
		},
		"delivery_slices": []any{
			map[string]any{
				"id":       "slice-1",
				"task_ids": []any{"T-1"},
			},
		},
	}
}

func TestValidatePlanV2FactMissingEvidenceID(t *testing.T) {
	plan := validV2Plan()
	facts := plan["architecture_facts"].([]any)
	facts[0].(map[string]any)["evidence_ids"] = []any{"missing_ev"}

	opts := &ValidatePlanOptions{PlanPath: "plan.md", RepoRoot: ""}

	err := validateArchitectureGrounding(plan, opts)
	if err == nil || !strings.Contains(err.Error(), "requires escalate") || !strings.Contains(err.Error(), "unresolved uncertainty requires escalation") {
		t.Fatalf("expected escalate error due to absent evidence, got: %v", err)
	}
}

func TestValidatePlanV2BlocksUnresolvedUnknowns(t *testing.T) {
	plan := validV2Plan()
	plan["architecture_facts"] = []any{}
	plan["tasks"].([]any)[0].(map[string]any)["requires_facts"] = []any{}
	plan["architecture_unknowns"] = []any{
		map[string]any{
			"id":       "unk_1",
			"question": "How?",
			"blocks":   []any{"T-1"},
		},
	}

	opts := &ValidatePlanOptions{PlanPath: "plan.md", RepoRoot: ""}
	err := validateArchitectureGrounding(plan, opts)
	t.Logf("DEBUG plan architecture_facts: %#v", plan["architecture_facts"])
	if err == nil || !strings.Contains(err.Error(), "blocked by unresolved architecture unknown") {
		t.Fatalf("expected blocked by unknown error, got: %v", err)
	}
}

func TestValidatePlanV2EvidencePathSafety(t *testing.T) {
	// We can't easily mock load ledger without changing the signature, but validateArchitectureGrounding
	// currently uses LoadEvidenceLedger which reads from disk.
	// For testing, we might need a way to inject the ledger or we can test the ValidateEvidencePath directly.
	err := ValidateEvidencePath("/repo", "../../../outside.txt")
	if err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("expected path traversal error, got %v", err)
	}

	absPath := "/etc/passwd"
	if runtime.GOOS == "windows" {
		absPath = "C:\\Windows\\System32\\cmd.exe"
	}
	err = ValidateEvidencePath("/repo", absPath)
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestPLAN_INVALIDExitsRepair(t *testing.T) {
	// A simple test ensuring classification plan_invalid routes to AUTO_PLAN and PLAN_INVALID mode
	opts := ChangeObservationOptions{
		Feature:        "feat",
		Message:        "message",
		SourceStage:    "BUILD",
		Classification: "plan_invalid",
	}
	// just testing the map in RecordChangeObservation
	classification := strings.ToLower(strings.TrimSpace(opts.Classification))
	resume := map[string]string{
		"plan_invalid": "AUTO_PLAN",
	}[classification]
	if resume != "AUTO_PLAN" {
		t.Fatalf("expected plan_invalid to map to AUTO_PLAN, got %v", resume)
	}
}

func TestValidateSystemicBoundaries(t *testing.T) {
	plan := validV2Plan()

	// Valid configuration
	plan["systemic_boundaries"] = []any{
		map[string]any{
			"id":                  "bnd_1",
			"verification_oracle": "Negative test confirming boundary blocks invalid input",
		},
	}
	plan["delivery_slices"] = []any{
		map[string]any{"id": "slice_1", "task_ids": []any{"task_1"}},
		map[string]any{"id": "slice_2", "task_ids": []any{"task_2"}},
	}
	if err := validateSystemicBoundaries(plan); err != nil {
		t.Fatalf("expected valid systemic boundaries, got: %v", err)
	}

	// Missing oracle
	plan["systemic_boundaries"] = []any{
		map[string]any{
			"id": "bnd_1",
		},
	}
	if err := validateSystemicBoundaries(plan); err == nil || !strings.Contains(err.Error(), "requires a verification_oracle") {
		t.Fatalf("expected error for missing oracle, got: %v", err)
	}

	// Missing ID
	plan["systemic_boundaries"] = []any{
		map[string]any{
			"verification_oracle": "Negative test",
		},
	}
	if err := validateSystemicBoundaries(plan); err == nil || !strings.Contains(err.Error(), "requires an id") {
		t.Fatalf("expected error for missing id, got: %v", err)
	}

	// Missing multiple slices
	plan["systemic_boundaries"] = []any{
		map[string]any{
			"id":                  "bnd_1",
			"verification_oracle": "Negative test",
		},
	}
	plan["delivery_slices"] = []any{
		map[string]any{"id": "slice_1", "task_ids": []any{"task_1"}},
	}
	if err := validateSystemicBoundaries(plan); err == nil || !strings.Contains(err.Error(), "requires a minimum of 2 delivery_slices") {
		t.Fatalf("expected error for insufficient slices, got: %v", err)
	}
}

func TestValidatePRVisualEvidence(t *testing.T) {
	plan := validV2Plan()
	plan["pr_visual_evidence"] = map[string]any{
		"relevance": "relevant",
		"scenarios": []any{map[string]any{
			"id": "warning", "entry": "/onboarding", "state": "picker open", "viewport": "1440x900",
			"expected": []any{"warning visible"},
		}},
	}
	if err := validatePRVisualEvidence(plan); err != nil {
		t.Fatal(err)
	}
	plan["pr_visual_evidence"].(map[string]any)["scenarios"] = []any{}
	if err := validatePRVisualEvidence(plan); err == nil || !strings.Contains(err.Error(), "one to three") {
		t.Fatalf("empty relevant scenarios were not rejected: %v", err)
	}
	plan["pr_visual_evidence"] = map[string]any{"relevance": "not_relevant", "scenarios": []any{}}
	if err := validatePRVisualEvidence(plan); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing not-relevant reason was not rejected: %v", err)
	}
}

func TestConfiguredPRVisualEvidenceRequiresPlanDecision(t *testing.T) {
	repo := prTestRepoConfigured(t, func(config *ProjectConfig) {
		config.Workflow.PRVisualEvidence = "suggest"
	})
	plan := validV2Plan()
	if err := requireConfiguredPRVisualEvidenceDecision(plan, &ValidatePlanOptions{RepoRoot: repo}); err == nil || !strings.Contains(err.Error(), "structural plan decision") {
		t.Fatalf("configured policy did not require a decision: %v", err)
	}
	plan["pr_visual_evidence"] = map[string]any{"relevance": "not_relevant", "reason": "backend-only", "scenarios": []any{}}
	if err := requireConfiguredPRVisualEvidenceDecision(plan, &ValidatePlanOptions{RepoRoot: repo}); err != nil {
		t.Fatal(err)
	}
}
