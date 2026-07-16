package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validPlan() map[string]any {
	return map[string]any{
		"schema_version":   float64(1),
		"feature_id":       "feature-one",
		"source_plan_path": "source-plan.md",
		"acceptance_criteria": []any{
			map[string]any{"id": "AC-1", "text": "observable result"},
		},
		"tasks": []any{
			map[string]any{
				"id": "T-1", "title": "implement result", "depends_on": []any{},
				"acceptance_criteria": []any{"AC-1"},
				"validation": []any{map[string]any{
					"criteria": []any{"AC-1"},
					"run":      "go test ./...", "origin": "AC-1",
					"oracle": "approved contract assertions", "independence": "contract-derived",
				}},
			},
		},
	}
}

func TestPlanCompilationApprovalAndStaleness(t *testing.T) {
	root := t.TempDir()
	sourcePlan := filepath.Join(root, "source-plan.md")
	spec := filepath.Join(root, "spec.md")
	planPath := filepath.Join(root, "plan.json")
	compiled := filepath.Join(root, "compiled")
	lock := filepath.Join(root, "plan.lock.json")
	if err := os.WriteFile(sourcePlan, []byte("# Host Plan-mode proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("# Accepted spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planJSON, _ := MarshalJSON(validPlan())
	if err := os.WriteFile(planPath, planJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CompilePlanFiles(planPath, compiled); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(compiled, "tasks.json")
	options := ApprovalOptions{
		SourcePlanPath: sourcePlan,
		SpecPath:       spec, PlanPath: planPath, TasksPath: tasks,
		ApprovedBy: "Test Human", ApprovedAt: "2026-07-16T12:00:00Z",
		SourceCommit: "test", OutputPath: lock,
	}
	if err := CreateApprovalLock(options); err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalLock(options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePlan, []byte("# Changed host plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalLock(options); err == nil || !strings.Contains(err.Error(), "source_plan") {
		t.Fatalf("expected stale source plan lock, got %v", err)
	}
	if err := os.WriteFile(sourcePlan, []byte("# Host Plan-mode proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(planPath)
	if err := os.WriteFile(planPath, append(value, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalLock(options); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale plan lock, got %v", err)
	}
}

func TestSourcePlanPreflightBlocksMissingAndEmptyFiles(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.md")
	if err := CheckSourcePlan(missing); err == nil {
		t.Fatal("expected missing source plan to block")
	}
	empty := filepath.Join(root, "empty.md")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckSourcePlan(empty); err == nil {
		t.Fatal("expected empty source plan to block")
	}
	valid := filepath.Join(root, "valid.md")
	if err := os.WriteFile(valid, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckSourcePlan(valid); err != nil {
		t.Fatal(err)
	}
}

func TestSourcePlanDiscoveryUsesOneBoundedCandidateAndBlocksAmbiguity(t *testing.T) {
	repo := t.TempDir()
	intake := filepath.Join(repo, ".product-loop", "intake")
	if err := os.MkdirAll(intake, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(intake, "feature-a.md")
	if err := os.WriteFile(first, []byte("# Feature A plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	discovered, err := DiscoverSourcePlan(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if discovered != ".product-loop/intake/feature-a.md" {
		t.Fatalf("unexpected discovered path: %s", discovered)
	}
	second := filepath.Join(intake, "feature-b.md")
	if err := os.WriteFile(second, []byte("# Feature B plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverSourcePlan(repo, ""); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected ambiguous source plans to block, got %v", err)
	}
	explicit, err := DiscoverSourcePlan(repo, ".product-loop/intake/feature-b.md")
	if err != nil {
		t.Fatal(err)
	}
	if explicit != ".product-loop/intake/feature-b.md" {
		t.Fatalf("unexpected explicit path: %s", explicit)
	}
}

func TestCompilerRequiresSourcePlanPath(t *testing.T) {
	plan := validPlan()
	delete(plan, "source_plan_path")
	_, _, _, err := CompilePlan(plan)
	if err == nil || !strings.Contains(err.Error(), "source_plan_path") {
		t.Fatalf("expected missing source plan path failure, got %v", err)
	}
}

func TestCompilerRejectsValidationWithoutOracleProvenance(t *testing.T) {
	plan := validPlan()
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["validation"] = []any{map[string]any{"criteria": []any{"AC-1"}, "run": "go test ./..."}}
	_, _, _, err := CompilePlan(plan)
	if err == nil || !strings.Contains(err.Error(), "origin, oracle, and independence") {
		t.Fatalf("expected validation provenance failure, got %v", err)
	}
}

func TestValidationsOnlySupportTheirMappedCriteria(t *testing.T) {
	plan := validPlan()
	plan["acceptance_criteria"] = []any{
		map[string]any{"id": "AC-1", "text": "first result"},
		map[string]any{"id": "AC-2", "text": "second result"},
	}
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["acceptance_criteria"] = []any{"AC-1", "AC-2"}
	task["validation"] = []any{
		map[string]any{
			"criteria": []any{"AC-1"}, "run": "check first",
			"origin": "AC-1", "oracle": "first oracle", "independence": "pre-existing",
		},
		map[string]any{
			"criteria": []any{"AC-2"}, "run": "check second",
			"origin": "AC-2", "oracle": "second oracle", "independence": "external",
		},
	}
	_, matrix, _, err := CompilePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	rows := matrix["requirements"].([]any)
	for _, item := range rows {
		row := item.(map[string]any)
		validations := row["validations"].([]any)
		if len(validations) != 1 {
			t.Fatalf("criterion %s received unrelated validations: %v", row["criterion_id"], validations)
		}
		validation := validations[0].(map[string]any)
		expected := "check first"
		if row["criterion_id"] == "AC-2" {
			expected = "check second"
		}
		if validation["check"] != expected {
			t.Fatalf("criterion %s received %v, expected %s", row["criterion_id"], validation["check"], expected)
		}
	}
}

func TestCompilerBlocksUncoveredCriterion(t *testing.T) {
	plan := validPlan()
	criteria := plan["acceptance_criteria"].([]any)
	plan["acceptance_criteria"] = append(criteria, map[string]any{"id": "AC-2", "text": "uncovered"})
	_, _, _, err := CompilePlan(plan)
	if err == nil || !strings.Contains(err.Error(), "uncovered acceptance criteria") {
		t.Fatalf("expected uncovered criterion failure, got %v", err)
	}
}

func TestCompiledTaskGraphPreservesTaskFields(t *testing.T) {
	tasks, _, _, err := CompilePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(tasks)
	if !strings.Contains(string(value), "implement result") {
		t.Fatal("compiler dropped an approved task field")
	}
}
