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
		"schema_version":     float64(1),
		"feature_id":         "feature-one",
		"source_plan_path":   "source-plan.md",
		"spec_path":          "spec.md",
		"blocking_questions": []any{},
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

func writeMarkdownPlan(t *testing.T, path string, plan map[string]any, marked bool) {
	t.Helper()
	value, err := MarshalJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	body := "# Structured plan\n\nHuman-readable summary covered by approval.\n\n"
	if marked {
		body += planMarkerStart + "\n"
	}
	body += "```json\n" + strings.TrimSpace(string(value)) + "\n```\n"
	if marked {
		body += planMarkerEnd + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeApprovalReceipt(t *testing.T, path, fingerprint string) {
	t.Helper()
	body := `# Plan approval

<!-- boatstack-approval:v1 -->
` + "```json\n" + `{
  "schema_version": 1,
  "status": "APPROVED",
  "approved_by": "Test Human",
  "approved_at": "2026-07-16T12:00:00Z",
  "approval_fingerprint": "` + fingerprint + `"
}
` + "```\n" + `<!-- /boatstack-approval -->
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePlanInputs(t *testing.T, root string, marked bool) (string, string, string) {
	t.Helper()
	sourcePlan := filepath.Join(root, "source-plan.md")
	spec := filepath.Join(root, "spec.md")
	planPath := filepath.Join(root, "plan.md")
	if err := os.WriteFile(sourcePlan, []byte("# Host Plan-mode proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("# Accepted spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkdownPlan(t, planPath, validPlan(), marked)
	return sourcePlan, spec, planPath
}

func TestMarkdownPlanActivationAndStaleness(t *testing.T) {
	root := t.TempDir()
	sourcePlan, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record approved planning inputs")
	approval := filepath.Join(root, "approval.md")
	compiled := filepath.Join(root, "compiled")
	lock := filepath.Join(root, "plan.lock.json")
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	writeApprovalReceipt(t, approval, check.Fingerprint)
	options := ActivationOptions{PlanPath: planPath, ApprovalPath: approval, OutDir: compiled, OutputPath: lock, SourceCommit: "test"}
	if err := ActivatePlan(options); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(compiled, "tasks.json"), filepath.Join(compiled, "test-matrix.json"), filepath.Join(compiled, "evidence.md"), lock} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected activated artifact %s", path)
		}
	}
	if err := os.WriteFile(sourcePlan, []byte("# Changed host plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActivatePlan(options); err == nil || !strings.Contains(err.Error(), "stale approval") {
		t.Fatalf("expected stale approval after source-plan change, got %v", err)
	}
	if err := os.WriteFile(sourcePlan, []byte("# Host Plan-mode proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(planPath)
	if err := os.WriteFile(planPath, append([]byte("Changed human summary.\n"), value...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActivatePlan(options); err == nil || !strings.Contains(err.Error(), "stale approval") {
		t.Fatalf("expected stale approval after plan prose change, got %v", err)
	}
}

func TestCurrentCursorSingleJSONFencePlanIsAccepted(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, false)
	if _, err := CheckPlan(planPath); err != nil {
		t.Fatalf("current Cursor plan.md shape should be accepted: %v", err)
	}
}

func TestPlanJSONIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	value, _ := MarshalJSON(validPlan())
	if err := os.WriteFile(path, value, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlan(path); err == nil || !strings.Contains(err.Error(), "Markdown") {
		t.Fatalf("expected clean-cut Markdown-only plan contract, got %v", err)
	}
}

func TestMarkdownPlanRejectsMissingMultipleMalformedAndOpenQuestions(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)

	if err := os.WriteFile(planPath, []byte("# no structured block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPlan(planPath); err == nil {
		t.Fatal("expected missing block to fail")
	}

	value, _ := MarshalJSON(validPlan())
	multiple := "# ambiguous\n\n```json\n" + string(value) + "```\n\n```json\n" + string(value) + "```\n"
	if err := os.WriteFile(planPath, []byte(multiple), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected multiple blocks to fail, got %v", err)
	}

	malformed := planMarkerStart + "\n```json\n{bad}\n```\n" + planMarkerEnd + "\n"
	if err := os.WriteFile(planPath, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "invalid structured plan json") {
		t.Fatalf("expected malformed json to fail, got %v", err)
	}

	plan := validPlan()
	plan["blocking_questions"] = []any{"Q-4"}
	writeMarkdownPlan(t, planPath, plan, true)
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "Q-4") {
		t.Fatalf("expected open material question to block, got %v", err)
	}
}

func TestExternalWritePlanRequiresSafeExplicitSideEffects(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	plan := validPlan()
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["title"] = "apply database schema migration"
	task["affected_paths"] = []any{"scripts/apply_schema.py"}
	task["rollback_boundary"] = "reset local DB"
	writeMarkdownPlan(t, planPath, plan, true)
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "destructive rollback") {
		t.Fatalf("ambiguous destructive rollback did not block planning: %v", err)
	}
	task["rollback_boundary"] = "stop and fix forward"
	writeMarkdownPlan(t, planPath, plan, true)
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "structured side_effects") {
		t.Fatalf("missing external side-effect declaration did not block: %v", err)
	}
	task["side_effects"] = []any{map[string]any{
		"kind": "database-schema-write", "target": "project-ref-7f31",
		"reversibility": "transactional", "failure_policy": "rollback-transaction", "destructive": false,
	}}
	writeMarkdownPlan(t, planPath, plan, true)
	if _, err := CheckPlan(planPath); err != nil {
		t.Fatalf("safe explicit external-write plan should pass: %v", err)
	}
	task["side_effects"].([]any)[0].(map[string]any)["target"] = "local database"
	writeMarkdownPlan(t, planPath, plan, true)
	if _, err := CheckPlan(planPath); err == nil || !strings.Contains(err.Error(), "immutable target identity") {
		t.Fatalf("ambiguous external target did not block: %v", err)
	}
}

func TestReadOnlyCheckAndFailedActivationWriteNothing(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	before, _ := os.ReadDir(root)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(root)
	if len(after) != len(before) {
		t.Fatalf("check-plan wrote files: before=%d after=%d", len(before), len(after))
	}
	approval := filepath.Join(root, "approval.md")
	compiled := filepath.Join(root, "compiled")
	lock := filepath.Join(root, "plan.lock.json")
	activation := ActivationOptions{PlanPath: planPath, ApprovalPath: approval, OutDir: compiled, OutputPath: lock}
	err = ActivatePlan(activation)
	if err == nil {
		t.Fatal("expected missing approval receipt to block")
	}
	writeApprovalReceipt(t, approval, "wrong-"+check.Fingerprint)
	err = ActivatePlan(activation)
	if err == nil || !strings.Contains(err.Error(), "stale approval") {
		t.Fatalf("expected invalid receipt to block, got %v", err)
	}
	if _, err := os.Stat(compiled); !os.IsNotExist(err) {
		t.Fatal("failed activation created compiled output")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("failed activation created a plan lock")
	}
}

func TestApprovalReceiptRequiresMarkersHumanTimestampAndFingerprint(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	approval := filepath.Join(root, "approval.md")
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "unmarked", body: "```json\n{}\n```\n", want: "markers"},
		{name: "missing human", body: `{"schema_version":1,"status":"APPROVED","approved_by":"","approved_at":"2026-07-16T12:00:00Z","approval_fingerprint":"` + check.Fingerprint + `"}`, want: "human approver"},
		{name: "bad timestamp", body: `{"schema_version":1,"status":"APPROVED","approved_by":"Test Human","approved_at":"today","approval_fingerprint":"` + check.Fingerprint + `"}`, want: "RFC3339"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := test.body
			if test.name != "unmarked" {
				body = approvalMarkerStart + "\n```json\n" + body + "\n```\n" + approvalMarkerEnd + "\n"
			}
			if err := os.WriteFile(approval, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := CheckApprovalReceipt(approval, check); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
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
