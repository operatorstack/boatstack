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

func writeActivationConfig(t *testing.T, root string, humanApproval bool) {
	t.Helper()
	config := testConfig()
	config.Workflow.HumanPlanApproval = humanApproval
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownPlanActivationAndStaleness(t *testing.T) {
	root := t.TempDir()
	sourcePlan, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	writeActivationConfig(t, root, true)
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

func TestPolicyActivationCreatesTypedLockWithoutApproval(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	writeActivationConfig(t, root, false)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record policy-activated planning inputs")
	compiled := filepath.Join(root, "compiled")
	lockPath := filepath.Join(root, "plan.lock.json")
	options := ActivationOptions{PlanPath: planPath, OutDir: compiled, OutputPath: lockPath, SourceCommit: "test"}
	if err := ActivatePlan(options); err != nil {
		t.Fatal(err)
	}
	lockValue, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock := map[string]any{}
	if err := json.Unmarshal(lockValue, &lock); err != nil {
		t.Fatal(err)
	}
	if intValue(lock["schema_version"]) != 2 || stringValue(lock["status"]) != "LOCKED" || stringValue(lock["authorization_mode"]) != "policy" || stringValue(lock["approved_by"]) != "" {
		t.Fatalf("unexpected policy lock: %#v", lock)
	}
	tasksValue, _ := os.ReadFile(filepath.Join(compiled, "tasks.json"))
	tasks := map[string]any{}
	_ = json.Unmarshal(tasksValue, &tasks)
	if stringValue(tasks["structured_plan_status"]) != "POLICY_ACTIVATED" {
		t.Fatalf("compiled task graph hid policy activation: %#v", tasks)
	}

	lock["schema_version"] = 1
	lock["status"] = "APPROVED"
	lock["approved_by"] = "Legacy Human"
	delete(lock, "authorization_mode")
	legacy, _ := MarshalJSON(lock)
	if err := os.WriteFile(lockPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalLock(ApprovalOptions{
		SourcePlanPath: check.SourcePlanPath, SpecPath: check.SpecPath, PlanPath: planPath,
		TasksPath: filepath.Join(compiled, "tasks.json"), OutputPath: lockPath, AuthorizationMode: "human",
	}); err != nil {
		t.Fatalf("legacy v1 human lock was rejected: %v", err)
	}
}

// activatePolicyPlan sets up a committed repo and activates a policy-mode plan,
// returning the paths a caller needs to assert on the promoted artifacts. It is
// the shared fixture for the atomic-activation and managed-undo tests.
func activatePolicyPlan(t *testing.T) (root, planPath, compiled, lock, feature string) {
	t.Helper()
	root = t.TempDir()
	_, _, planPath = writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	writeActivationConfig(t, root, false)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record policy-activated planning inputs")
	// Lay the managed artifacts out under the feature directory, as the real
	// workflow does, so the undo verb's domain guard can resolve the owning
	// feature from the promoted paths.
	feature = "feature-one"
	featureDir := filepath.Join(root, ".product-loop", "features", feature)
	compiled = filepath.Join(featureDir, "compiled")
	lock = filepath.Join(featureDir, "plan.lock.json")
	options := ActivationOptions{PlanPath: planPath, OutDir: compiled, OutputPath: lock, SourceCommit: "test"}
	if err := ActivatePlan(options); err != nil {
		t.Fatal(err)
	}
	return root, planPath, compiled, lock, feature
}

// TestActivationPromotesFourArtifactsAtomically proves activation lands the
// compiled trio and the plan lock as one transactional mutation: a single
// receipt whose four recorded post-images match the bytes on disk. Because
// ApplyMutation is all-or-nothing (proven at the primitive level), one receipt
// covering all four files is the structural guarantee that no partial set can be
// left behind.
func TestActivationPromotesFourArtifactsAtomically(t *testing.T) {
	root, _, compiled, lock, _ := activatePolicyPlan(t)
	artifacts := []string{
		filepath.Join(compiled, "tasks.json"),
		filepath.Join(compiled, "test-matrix.json"),
		filepath.Join(compiled, "evidence.md"),
		lock,
	}
	for _, path := range artifacts {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected activated artifact %s", path)
		}
	}
	receipts, err := ListMutationReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	var activation *MutationReceipt
	for i := range receipts {
		if receipts[i].Kind == "plan-activation" && receipts[i].Status == "APPLIED" {
			activation = &receipts[i]
			break
		}
	}
	if activation == nil {
		t.Fatalf("no APPLIED plan-activation receipt among %d receipts", len(receipts))
	}
	if len(activation.Changes) != 4 {
		t.Fatalf("expected one atomic mutation over four artifacts, got %d changes", len(activation.Changes))
	}
	for _, change := range activation.Changes {
		hash, err := SHA256File(filepath.Join(root, filepath.FromSlash(change.Path)))
		if err != nil {
			t.Fatalf("promoted artifact %s missing: %v", change.Path, err)
		}
		if change.AfterSHA256 != hash {
			t.Fatalf("receipt post-image for %s does not match disk", change.Path)
		}
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
	runGit(t, root, "init", "-b", "main")
	writeActivationConfig(t, root, true)
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

// TestSourcePlanRequiresExplicitPlanAndNeverScansDirectories is the conformance
// guard for the "no ambient plan context" contract: Boatstack must never scan a
// directory for source plans. An empty --plan blocks even when plan-shaped files
// exist in the historically scanned locations, and only an explicit path
// resolves.
func TestSourcePlanRequiresExplicitPlanAndNeverScansDirectories(t *testing.T) {
	repo := t.TempDir()
	// Seed plan-shaped files in every location discovery used to scan. None of
	// these may be picked up.
	for _, dir := range []string{
		".product-loop/intake",
		".cursor/plans",
		".claude/plans",
		".codex/plans",
	} {
		absolute := filepath.Join(repo, filepath.FromSlash(dir))
		if err := os.MkdirAll(absolute, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(absolute, "stale.md"), []byte("# Stale unshipped plan\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := DiscoverSourcePlan(repo, ""); err == nil {
		t.Fatal("expected empty --plan to block; discovery must never scan directories")
	} else if !strings.Contains(err.Error(), "--plan") {
		t.Fatalf("expected error to require --plan, got %v", err)
	}

	// An explicit path is the only way to supply a source plan.
	explicitFile := filepath.Join(repo, "docs", "chosen-plan.md")
	if err := os.MkdirAll(filepath.Dir(explicitFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explicitFile, []byte("# Chosen plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit, err := DiscoverSourcePlan(repo, "docs/chosen-plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if explicit != "docs/chosen-plan.md" {
		t.Fatalf("unexpected explicit path: %s", explicit)
	}
}

// TestSourcePlanRejectsOutsideRepoPath is the durability guard: source_plan_path
// is recorded and re-hashed through build, so a plan outside the repository
// (whose absolute path does not travel and is never committed) must be rejected
// up front rather than drifting at build time.
func TestSourcePlanRejectsOutsideRepoPath(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real, non-empty plan file that lives outside the repository. It passes
	// CheckSourcePlan (it exists and is non-empty) but is not durable relative
	// to the repo.
	outside := filepath.Join(parent, "ephemeral-plan.md")
	if err := os.WriteFile(outside, []byte("# Ephemeral scratch plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverSourcePlan(repo, outside); err == nil {
		t.Fatal("expected an out-of-repo source plan to be rejected")
	} else if !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("expected an out-of-repo error, got %v", err)
	}
	// The same holds for a relative path that escapes the repo.
	if _, err := DiscoverSourcePlan(repo, filepath.Join("..", "ephemeral-plan.md")); err == nil {
		t.Fatal("expected a repo-escaping relative source plan to be rejected")
	}
}

// TestNoIntakeStagingReferenceInProductionSource guards against the intake
// staging concept returning: no production Go file may reference the removed
// .product-loop/intake staging directory.
func TestNoIntakeStagingReferenceInProductionSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(contents), ".product-loop/intake") {
			t.Errorf("%s still references the removed .product-loop/intake staging directory", name)
		}
	}
}

func TestCompilerRequiresSourcePlanPath(t *testing.T) {
	plan := validPlan()
	delete(plan, "source_plan_path")
	_, _, _, err := CompilePlan(plan, nil)
	if err == nil || !strings.Contains(err.Error(), "source_plan_path") {
		t.Fatalf("expected missing source plan path failure, got %v", err)
	}
}

func TestCompilerRejectsValidationWithoutOracleProvenance(t *testing.T) {
	plan := validPlan()
	task := plan["tasks"].([]any)[0].(map[string]any)
	task["validation"] = []any{map[string]any{"criteria": []any{"AC-1"}, "run": "go test ./..."}}
	_, _, _, err := CompilePlan(plan, nil)
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
	_, matrix, _, err := CompilePlan(plan, nil)
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
	_, _, _, err := CompilePlan(plan, nil)
	if err == nil || !strings.Contains(err.Error(), "uncovered acceptance criteria") {
		t.Fatalf("expected uncovered criterion failure, got %v", err)
	}
}

func TestCompiledTaskGraphPreservesTaskFields(t *testing.T) {
	tasks, _, _, err := CompilePlan(validPlan(), nil)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(tasks)
	if !strings.Contains(string(value), "implement result") {
		t.Fatal("compiler dropped an approved task field")
	}
}
