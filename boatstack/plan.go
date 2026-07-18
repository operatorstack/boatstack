package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	planMarkerStart     = "<!-- boatstack-plan:v1 -->"
	planMarkerEnd       = "<!-- /boatstack-plan -->"
	approvalMarkerStart = "<!-- boatstack-approval:v1 -->"
	approvalMarkerEnd   = "<!-- /boatstack-approval -->"
)

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func stringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		result = append(result, text)
	}
	return result, true
}

func objectSlice(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		result = append(result, object)
	}
	return result, true
}

func validationSlice(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, false
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		validation, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		for _, field := range []string{"run", "origin", "oracle", "independence"} {
			if strings.TrimSpace(stringValue(validation[field])) == "" {
				return nil, false
			}
		}
		criteria, criteriaOK := stringSlice(validation["criteria"])
		if !criteriaOK || len(criteria) == 0 {
			return nil, false
		}
		result = append(result, validation)
	}
	return result, true
}

func fencedJSONBlocks(value string) ([]string, error) {
	lines := strings.Split(value, "\n")
	blocks := []string{}
	inJSON := false
	current := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inJSON {
			if trimmed == "```json" {
				inJSON = true
				current = nil
			}
			continue
		}
		if trimmed == "```" {
			blocks = append(blocks, strings.Join(current, "\n"))
			inJSON = false
			current = nil
			continue
		}
		current = append(current, line)
	}
	if inJSON {
		return nil, fmt.Errorf("unterminated json fence")
	}
	return blocks, nil
}

func markedJSON(value, label, startMarker, endMarker string, allowLegacy bool) ([]byte, error) {
	startCount := strings.Count(value, startMarker)
	endCount := strings.Count(value, endMarker)
	if startCount == 0 && endCount == 0 {
		if !allowLegacy {
			return nil, fmt.Errorf("%s is missing %s markers", label, label)
		}
		blocks, err := fencedJSONBlocks(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", label, err)
		}
		if len(blocks) != 1 {
			return nil, fmt.Errorf("%s requires exactly one json fence; found %d", label, len(blocks))
		}
		return []byte(blocks[0]), nil
	}
	if startCount != 1 || endCount != 1 {
		return nil, fmt.Errorf("%s requires exactly one marker pair", label)
	}
	start := strings.Index(value, startMarker) + len(startMarker)
	end := strings.Index(value, endMarker)
	if end <= start {
		return nil, fmt.Errorf("%s markers are out of order", label)
	}
	blocks, err := fencedJSONBlocks(value[start:end])
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if len(blocks) != 1 {
		return nil, fmt.Errorf("marked %s requires exactly one json fence; found %d", label, len(blocks))
	}
	return []byte(blocks[0]), nil
}

func loadJSONObject(path, label, startMarker, endMarker string, allowLegacyMarkdown bool) (map[string]any, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload := value
	if strings.EqualFold(filepath.Ext(path), ".md") {
		payload, err = markedJSON(string(value), label, startMarker, endMarker, allowLegacyMarkdown)
		if err != nil {
			return nil, err
		}
	}
	var plan map[string]any
	if err := DecodeJSON("load "+label, path, payload, &plan); err != nil {
		return nil, fmt.Errorf("invalid %s json: %w", label, err)
	}
	return plan, nil
}

func LoadPlan(path string) (map[string]any, error) {
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil, fmt.Errorf("structured plan must be a Markdown file: %s", path)
	}
	return loadJSONObject(path, "structured plan", planMarkerStart, planMarkerEnd, true)
}

func CheckSourcePlan(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("source plan path is required; start in the host Plan mode and save its plan before running auto-plan")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("source plan does not exist as a regular file: %s", path)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("source plan is unreadable: %w", err)
	}
	if strings.TrimSpace(string(value)) == "" {
		return fmt.Errorf("source plan is empty: %s", path)
	}
	return nil
}

func DiscoverSourcePlan(repo, explicit string) (string, error) {
	repoAbsolute, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(explicit) != "" {
		candidate := explicit
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(repoAbsolute, candidate)
		}
		candidate = filepath.Clean(candidate)
		if err := CheckSourcePlan(candidate); err != nil {
			return "", err
		}
		relative, err := filepath.Rel(repoAbsolute, candidate)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative), nil
		}
		return candidate, nil
	}

	roots := []string{
		".product-loop/intake",
		".cursor/plans",
		".claude/plans",
		".codex/plans",
	}
	allowed := map[string]bool{".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true}
	candidates := []string{}
	for _, root := range roots {
		absoluteRoot := filepath.Join(repoAbsolute, filepath.FromSlash(root))
		if _, err := os.Stat(absoluteRoot); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		err := filepath.WalkDir(absoluteRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 || !allowed[strings.ToLower(filepath.Ext(entry.Name()))] {
				return nil
			}
			if strings.EqualFold(entry.Name(), "README.md") || CheckSourcePlan(path) != nil {
				return nil
			}
			relative, relErr := filepath.Rel(repoAbsolute, path)
			if relErr != nil {
				return relErr
			}
			candidates = append(candidates, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no saved Plan-mode file found; save the current host plan under .product-loop/intake/ and run auto-plan again")
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("multiple saved Plan-mode files found: %s; keep one active intake file or pass the intended path", strings.Join(candidates, ", "))
	}
	return candidates[0], nil
}

func SourcePlanForStructuredPlan(planPath string) (string, error) {
	plan, err := LoadPlan(planPath)
	if err != nil {
		return "", err
	}
	sourcePlan := stringValue(plan["source_plan_path"])
	if strings.TrimSpace(sourcePlan) == "" {
		return "", fmt.Errorf("source_plan_path is required")
	}
	if !filepath.IsAbs(sourcePlan) {
		sourcePlan = filepath.Join(filepath.Dir(planPath), sourcePlan)
	}
	return filepath.Clean(sourcePlan), nil
}

func SpecForStructuredPlan(planPath string) (string, error) {
	plan, err := LoadPlan(planPath)
	if err != nil {
		return "", err
	}
	spec := stringValue(plan["spec_path"])
	if strings.TrimSpace(spec) == "" {
		return "", fmt.Errorf("spec_path is required")
	}
	if !filepath.IsAbs(spec) {
		spec = filepath.Join(filepath.Dir(planPath), spec)
	}
	return filepath.Clean(spec), nil
}

func checkNonEmptyFile(path, label string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s does not exist as a regular file: %s", label, path)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s is unreadable: %w", label, err)
	}
	if strings.TrimSpace(string(value)) == "" {
		return fmt.Errorf("%s is empty: %s", label, path)
	}
	return nil
}

type PlanCheck struct {
	Plan           map[string]any
	PlanPath       string
	SourcePlanPath string
	SpecPath       string
	PlanHash       string
	SourcePlanHash string
	SpecHash       string
	Fingerprint    string
}

func CheckPlan(planPath string) (PlanCheck, error) {
	plan, err := LoadPlan(planPath)
	if err != nil {
		return PlanCheck{}, err
	}
	if err := ValidatePlan(plan); err != nil {
		return PlanCheck{}, err
	}
	sourcePlan, err := SourcePlanForStructuredPlan(planPath)
	if err != nil {
		return PlanCheck{}, err
	}
	if err := CheckSourcePlan(sourcePlan); err != nil {
		return PlanCheck{}, err
	}
	spec, err := SpecForStructuredPlan(planPath)
	if err != nil {
		return PlanCheck{}, err
	}
	if err := checkNonEmptyFile(spec, "feature spec"); err != nil {
		return PlanCheck{}, err
	}
	planHash, err := SHA256File(planPath)
	if err != nil {
		return PlanCheck{}, err
	}
	sourcePlanHash, err := SHA256File(sourcePlan)
	if err != nil {
		return PlanCheck{}, err
	}
	specHash, err := SHA256File(spec)
	if err != nil {
		return PlanCheck{}, err
	}
	fingerprintInput, err := MarshalJSON(map[string]any{
		"schema_version":     1,
		"plan_path":          filepath.Base(planPath),
		"plan_sha256":        planHash,
		"source_plan_path":   filepath.ToSlash(filepath.Clean(stringValue(plan["source_plan_path"]))),
		"source_plan_sha256": sourcePlanHash,
		"spec_path":          filepath.ToSlash(filepath.Clean(stringValue(plan["spec_path"]))),
		"spec_sha256":        specHash,
	})
	if err != nil {
		return PlanCheck{}, err
	}
	return PlanCheck{
		Plan: plan, PlanPath: filepath.Clean(planPath), SourcePlanPath: sourcePlan, SpecPath: spec,
		PlanHash: planHash, SourcePlanHash: sourcePlanHash, SpecHash: specHash,
		Fingerprint: SHA256Bytes(fingerprintInput),
	}, nil
}

func checkApprovalSourcePlan(options ApprovalOptions) error {
	expected, err := SourcePlanForStructuredPlan(options.PlanPath)
	if err != nil {
		return err
	}
	expectedAbsolute, err := filepath.Abs(expected)
	if err != nil {
		return err
	}
	suppliedAbsolute, err := filepath.Abs(options.SourcePlanPath)
	if err != nil {
		return err
	}
	if filepath.Clean(expectedAbsolute) != filepath.Clean(suppliedAbsolute) {
		return fmt.Errorf("source-plan does not match structured plan source_plan_path: expected %s", expected)
	}
	return CheckSourcePlan(expected)
}

func ValidatePlan(plan map[string]any) error {
	if plan["schema_version"] != float64(1) {
		return fmt.Errorf("schema_version must be 1")
	}
	if stringValue(plan["feature_id"]) == "" {
		return fmt.Errorf("feature_id is required")
	}
	if stringValue(plan["source_plan_path"]) == "" {
		return fmt.Errorf("source_plan_path is required")
	}
	if questions, present := plan["blocking_questions"]; present {
		values, ok := stringSlice(questions)
		if !ok {
			return fmt.Errorf("blocking_questions must be a list of question ids")
		}
		if len(values) > 0 {
			return fmt.Errorf("unresolved blocking questions: %s", strings.Join(values, ", "))
		}
	}
	criteria, ok := objectSlice(plan["acceptance_criteria"])
	if !ok || len(criteria) == 0 {
		return fmt.Errorf("at least one acceptance criterion is required")
	}
	tasks, ok := objectSlice(plan["tasks"])
	if !ok || len(tasks) == 0 {
		return fmt.Errorf("at least one task is required")
	}
	criterionIDs := map[string]bool{}
	for _, criterion := range criteria {
		id := stringValue(criterion["id"])
		if id == "" || criterionIDs[id] {
			return fmt.Errorf("acceptance criterion ids must be present and unique")
		}
		criterionIDs[id] = true
	}
	taskIDs := map[string]bool{}
	for _, task := range tasks {
		id := stringValue(task["id"])
		if id == "" || taskIDs[id] {
			return fmt.Errorf("task ids must be present and unique")
		}
		taskIDs[id] = true
	}
	covered := map[string]bool{}
	validationCovered := map[string]bool{}
	graph := map[string][]string{}
	for _, task := range tasks {
		id := stringValue(task["id"])
		dependencies, dependenciesOK := stringSlice(task["depends_on"])
		if task["depends_on"] == nil {
			dependencies, dependenciesOK = []string{}, true
		}
		mapped, mappedOK := stringSlice(task["acceptance_criteria"])
		if task["acceptance_criteria"] == nil {
			mapped, mappedOK = []string{}, true
		}
		validations, validationsOK := validationSlice(task["validation"])
		if !dependenciesOK || !mappedOK || !validationsOK {
			return fmt.Errorf("task %s requires list dependencies, criteria, and at least one validation with criteria, run, origin, oracle, and independence", id)
		}
		for _, dependency := range dependencies {
			if dependency == id {
				return fmt.Errorf("task %s cannot depend on itself", id)
			}
			if !taskIDs[dependency] {
				return fmt.Errorf("task %s has unknown dependency: %s", id, dependency)
			}
		}
		for _, criterion := range mapped {
			if !criterionIDs[criterion] {
				return fmt.Errorf("task %s maps unknown criterion: %s", id, criterion)
			}
			covered[criterion] = true
		}
		for _, validation := range validations {
			validationCriteria, _ := stringSlice(validation["criteria"])
			for _, criterion := range validationCriteria {
				if !contains(mapped, criterion) {
					return fmt.Errorf("task %s validation maps criterion %s not served by the task", id, criterion)
				}
				validationCovered[criterion] = true
			}
		}
		if len(mapped) == 0 && stringValue(task["enabling_reason"]) == "" {
			return fmt.Errorf("task %s must map acceptance criteria or state an enabling_reason", id)
		}
		if err := validateTaskSafety(task); err != nil {
			return fmt.Errorf("task %s safety: %w", id, err)
		}
		graph[id] = dependencies
	}
	uncovered := []string{}
	for criterion := range criterionIDs {
		if !covered[criterion] {
			uncovered = append(uncovered, criterion)
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		return fmt.Errorf("uncovered acceptance criteria: %v", uncovered)
	}
	unvalidated := []string{}
	for criterion := range criterionIDs {
		if !validationCovered[criterion] {
			unvalidated = append(unvalidated, criterion)
		}
	}
	if len(unvalidated) > 0 {
		sort.Strings(unvalidated)
		return fmt.Errorf("acceptance criteria without validation procedures: %v", unvalidated)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("task dependency cycle includes %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range graph[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range taskIDs {
		if err := visit(id); err != nil {
			return err
		}
	}
	if _, err := deliveryDefinitions(plan); err != nil {
		return err
	}
	return nil
}

func taskSafetyText(task map[string]any) string {
	parts := []string{stringValue(task["title"]), stringValue(task["rollback_boundary"])}
	if validations, ok := objectSlice(task["validation"]); ok {
		for _, validation := range validations {
			parts = append(parts, stringValue(validation["run"]), stringValue(validation["origin"]))
		}
	}
	if paths, ok := stringSlice(task["affected_paths"]); ok {
		parts = append(parts, paths...)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func taskHasExternalWrite(task map[string]any) bool {
	text := taskSafetyText(task)
	for _, marker := range []string{
		"database", "migration", "migrate", "seed database", "deploy", "supabase", "postgres",
		"terraform", "pulumi", "kubectl", "cloud", "production", "staging", "external write",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func destructiveRollback(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return false
	}
	for _, safePrefix := range []string{"no reset", "never reset", "do not reset", "operator-only", "operator only"} {
		if strings.Contains(text, safePrefix) {
			return false
		}
	}
	for _, marker := range []string{"reset database", "reset db", "reset local db", "drop schema", "drop database", "truncate", "wipe database", "destroy infrastructure"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func validateTaskSafety(task map[string]any) error {
	if destructiveRollback(stringValue(task["rollback_boundary"])) {
		return fmt.Errorf("destructive rollback is not executable authority; use transactional rollback, fix-forward recovery, or an operator-only runbook")
	}
	if !taskHasExternalWrite(task) {
		return nil
	}
	paths, pathsOK := stringSlice(task["affected_paths"])
	if !pathsOK || len(paths) == 0 {
		return fmt.Errorf("external-write tasks require affected_paths")
	}
	effects, effectsOK := objectSlice(task["side_effects"])
	if !effectsOK || len(effects) == 0 {
		return fmt.Errorf("external-write tasks require structured side_effects")
	}
	for index, effect := range effects {
		for _, field := range []string{"kind", "target", "reversibility", "failure_policy"} {
			if strings.TrimSpace(stringValue(effect[field])) == "" {
				return fmt.Errorf("side_effects[%d].%s is required", index, field)
			}
		}
		target := strings.ToLower(strings.TrimSpace(stringValue(effect["target"])))
		for _, ambiguous := range []string{"unknown", "tbd", "database", "staging", "production", "local database"} {
			if target == ambiguous {
				return fmt.Errorf("side_effects[%d].target must use an immutable target identity", index)
			}
		}
		destructive, ok := effect["destructive"].(bool)
		if !ok {
			return fmt.Errorf("side_effects[%d].destructive must be boolean", index)
		}
		if destructive {
			return fmt.Errorf("side_effects[%d] requests a destructive operation; move it to an operator-owned surface", index)
		}
		reversibility := strings.ToLower(stringValue(effect["reversibility"]))
		if reversibility != "transactional" && reversibility != "fix-forward" && reversibility != "reversible" {
			return fmt.Errorf("side_effects[%d].reversibility must be transactional, fix-forward, or reversible", index)
		}
		failurePolicy := strings.ToLower(stringValue(effect["failure_policy"]))
		if failurePolicy != "rollback-transaction" && failurePolicy != "stop-and-fix-forward" {
			return fmt.Errorf("side_effects[%d].failure_policy must be rollback-transaction or stop-and-fix-forward", index)
		}
	}
	return nil
}

func CompilePlan(plan map[string]any) (map[string]any, map[string]any, string, error) {
	if err := ValidatePlan(plan); err != nil {
		return nil, nil, "", err
	}
	criteria, _ := objectSlice(plan["acceptance_criteria"])
	tasks, _ := objectSlice(plan["tasks"])
	deliverySlices, _ := deliveryDefinitions(plan)
	rows := make([]any, 0, len(criteria))
	evidence := []string{
		"# Evidence ledger: " + stringValue(plan["feature_id"]), "",
		"- Approved plan lock: pending", "- Test gate: `BLOCKED`", "- Review gate: `BLOCKED`", "- Ship gate: `BLOCKED`", "",
	}
	if plan["delivery_slices"] != nil {
		evidence = append(evidence, "## Delivery slices", "")
		for _, slice := range deliverySlices {
			evidence = append(evidence,
				"### "+slice.ID+": "+slice.Title, "",
				"- Test gate ("+slice.ID+"): `BLOCKED`",
				"- Review gate ("+slice.ID+"): `BLOCKED`",
				"- Ship gate ("+slice.ID+"): `BLOCKED`", "",
			)
		}
	}
	evidence = append(evidence, "## Acceptance evidence", "", "| Criterion | Tasks | Result | Evidence |", "|---|---|---|---|")
	for _, criterion := range criteria {
		criterionID := stringValue(criterion["id"])
		servingIDs := []string{}
		validations := []any{}
		for _, task := range tasks {
			mapped, _ := stringSlice(task["acceptance_criteria"])
			if !contains(mapped, criterionID) {
				continue
			}
			taskID := stringValue(task["id"])
			servingIDs = append(servingIDs, taskID)
			checks, _ := validationSlice(task["validation"])
			for _, check := range checks {
				checkCriteria, _ := stringSlice(check["criteria"])
				if !contains(checkCriteria, criterionID) {
					continue
				}
				validations = append(validations, map[string]any{
					"task_id":      taskID,
					"check":        check["run"],
					"origin":       check["origin"],
					"oracle":       check["oracle"],
					"independence": check["independence"],
				})
			}
		}
		row := map[string]any{
			"criterion_id": criterionID,
			"criterion":    stringValue(criterion["text"]),
			"tasks":        servingIDs,
			"validations":  validations,
			"result":       "BLOCKED",
			"evidence":     nil,
		}
		rows = append(rows, row)
		evidence = append(evidence, fmt.Sprintf("| %s: %s | %s | `BLOCKED` | |", criterionID, stringValue(criterion["text"]), strings.Join(servingIDs, ", ")))
	}
	evidence = append(evidence, "", "## Safety evidence", "", "- Operational diff safety: `BLOCKED`", "- External target and recovery evidence: pending", "", "## Commands and checks", "", "## Review findings", "", "## Known gaps", "", "## Rollout and rollback", "")
	taskGraph := map[string]any{
		"schema_version":         1,
		"feature_id":             plan["feature_id"],
		"source_plan_path":       plan["source_plan_path"],
		"source_plan_status":     "HASH_LOCKED_INPUT",
		"structured_plan_status": "HUMAN_APPROVED",
		"tasks":                  plan["tasks"],
	}
	testMatrix := map[string]any{
		"schema_version": 1,
		"feature_id":     plan["feature_id"],
		"requirements":   rows,
	}
	deliveryValues := make([]any, 0, len(deliverySlices))
	for _, slice := range deliverySlices {
		deliveryValues = append(deliveryValues, map[string]any{
			"id": slice.ID, "title": slice.Title, "task_ids": slice.TaskIDs,
			"acceptance_criteria": slice.AcceptanceCriteria, "affected_paths": slice.AffectedPaths,
			"base_branch": slice.BaseBranch, "head_branch": slice.HeadBranch,
		})
	}
	taskGraph["delivery_slices"] = deliveryValues
	return taskGraph, testMatrix, strings.Join(evidence, "\n"), nil
}

func CompilePlanFiles(planPath, outDir string) error {
	plan, err := LoadPlan(planPath)
	if err != nil {
		return err
	}
	sourcePlan, err := SourcePlanForStructuredPlan(planPath)
	if err != nil {
		return err
	}
	if err := CheckSourcePlan(sourcePlan); err != nil {
		return err
	}
	tasks, matrix, evidence, err := CompilePlan(plan)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	tasksJSON, err := MarshalJSON(tasks)
	if err != nil {
		return err
	}
	matrixJSON, err := MarshalJSON(matrix)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "tasks.json"), tasksJSON, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "test-matrix.json"), matrixJSON, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "evidence.md"), []byte(evidence), 0o644)
}

type ApprovalOptions struct {
	SourcePlanPath string
	SpecPath       string
	PlanPath       string
	TasksPath      string
	ApprovedBy     string
	ApprovedAt     string
	SourceCommit   string
	OutputPath     string
}

type ApprovalReceipt struct {
	SchemaVersion int
	Status        string
	ApprovedBy    string
	ApprovedAt    string
	Fingerprint   string
}

func LoadApprovalReceipt(path string) (ApprovalReceipt, error) {
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt must be a Markdown file: %s", path)
	}
	value, err := loadJSONObject(path, "approval receipt", approvalMarkerStart, approvalMarkerEnd, false)
	if err != nil {
		return ApprovalReceipt{}, err
	}
	receipt := ApprovalReceipt{
		SchemaVersion: intValue(value["schema_version"]),
		Status:        stringValue(value["status"]),
		ApprovedBy:    stringValue(value["approved_by"]),
		ApprovedAt:    stringValue(value["approved_at"]),
		Fingerprint:   stringValue(value["approval_fingerprint"]),
	}
	if receipt.SchemaVersion != 1 {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt schema_version must be 1")
	}
	if receipt.Status != "APPROVED" {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt status must be APPROVED")
	}
	if strings.TrimSpace(receipt.ApprovedBy) == "" {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt must name the human approver")
	}
	if _, err := time.Parse(time.RFC3339, receipt.ApprovedAt); err != nil {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt approved_at must be RFC3339: %w", err)
	}
	if strings.TrimSpace(receipt.Fingerprint) == "" {
		return ApprovalReceipt{}, fmt.Errorf("approval receipt fingerprint is required")
	}
	return receipt, nil
}

func intValue(value any) int {
	number, ok := value.(float64)
	if !ok {
		return 0
	}
	return int(number)
}

func CheckApprovalReceipt(path string, planCheck PlanCheck) (ApprovalReceipt, error) {
	receipt, err := LoadApprovalReceipt(path)
	if err != nil {
		return ApprovalReceipt{}, err
	}
	if receipt.Fingerprint != planCheck.Fingerprint {
		return ApprovalReceipt{}, fmt.Errorf("stale approval receipt: fingerprint does not match the current source plan, spec, and plan")
	}
	return receipt, nil
}

type ActivationOptions struct {
	PlanPath     string
	ApprovalPath string
	OutDir       string
	OutputPath   string
	SourceCommit string
}

func ActivatePlan(options ActivationOptions) error {
	check, err := CheckPlan(options.PlanPath)
	if err != nil {
		return err
	}
	receipt, err := CheckApprovalReceipt(options.ApprovalPath, check)
	if err != nil {
		return err
	}
	repo, err := ResolveRepository(filepath.Dir(options.PlanPath))
	if err != nil {
		return err
	}
	safety, err := CheckRepositorySafety(repo)
	if err != nil {
		return err
	}
	if safety.Status != "PASS" {
		return fmt.Errorf("operational diff contains an irreversible capability: %s", safety.Findings[0].Category)
	}
	tasksPath := filepath.Join(options.OutDir, "tasks.json")
	approval := ApprovalOptions{
		SourcePlanPath: check.SourcePlanPath,
		SpecPath:       check.SpecPath,
		PlanPath:       options.PlanPath,
		TasksPath:      tasksPath,
		ApprovedBy:     receipt.ApprovedBy,
		ApprovedAt:     receipt.ApprovedAt,
		SourceCommit:   options.SourceCommit,
		OutputPath:     options.OutputPath,
	}
	if fileExists(options.OutputPath) {
		if err := CheckApprovalLock(approval); err == nil {
			return initializeDeliveryState(repo, stringValue(check.Plan["feature_id"]), options.PlanPath, options.OutputPath)
		}
		value, readErr := os.ReadFile(options.OutputPath)
		if readErr != nil {
			return fmt.Errorf("existing plan lock cannot be verified: %w", readErr)
		}
		var existing map[string]any
		if err := DecodeJSON("inspect existing plan lock", options.OutputPath, value, &existing); err != nil {
			return fmt.Errorf("%w; do not overwrite activation state", err)
		}
		currentPlanHash, _ := SHA256File(options.PlanPath)
		currentSourceHash, _ := SHA256File(check.SourcePlanPath)
		currentSpecHash, _ := SHA256File(check.SpecPath)
		if stringValue(existing["plan_sha256"]) == currentPlanHash &&
			stringValue(existing["source_plan_sha256"]) == currentSourceHash &&
			stringValue(existing["spec_sha256"]) == currentSpecHash {
			return fmt.Errorf("existing activation state is invalid for the unchanged approved plan; repair it instead of resetting delivery progress")
		}
	} else if statePath, statePathErr := deliveryStatePath(repo, stringValue(check.Plan["feature_id"])); statePathErr == nil && fileExists(statePath) {
		return fmt.Errorf("managed delivery state exists without its plan lock; do not reset delivery progress")
	}
	if err := CompilePlanFiles(options.PlanPath, options.OutDir); err != nil {
		return err
	}
	if err := CreateApprovalLock(approval); err != nil {
		return err
	}
	if err := CheckApprovalLock(approval); err != nil {
		return err
	}
	return initializeDeliveryState(repo, stringValue(check.Plan["feature_id"]), options.PlanPath, options.OutputPath)
}

func gitCommit(directory string) string {
	command := exec.Command("git", "-C", directory, "rev-parse", "HEAD")
	value, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(value))
}

func CreateApprovalLock(options ApprovalOptions) error {
	if strings.TrimSpace(options.ApprovedBy) == "" {
		return fmt.Errorf("approved-by must name the human who explicitly approved the plan")
	}
	if err := checkApprovalSourcePlan(options); err != nil {
		return err
	}
	for _, path := range []string{options.SpecPath, options.PlanPath, options.TasksPath} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("required approved artifact does not exist: %s", path)
		}
	}
	approvedAt := options.ApprovedAt
	if approvedAt == "" {
		approvedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	sourceCommit := options.SourceCommit
	if sourceCommit == "" {
		sourceCommit = gitCommit(filepath.Dir(options.SpecPath))
	}
	specHash, _ := SHA256File(options.SpecPath)
	sourcePlanHash, _ := SHA256File(options.SourcePlanPath)
	planHash, _ := SHA256File(options.PlanPath)
	tasksHash, _ := SHA256File(options.TasksPath)
	lock := map[string]any{
		"schema_version":      1,
		"status":              "APPROVED",
		"approved_by":         options.ApprovedBy,
		"approved_at":         approvedAt,
		"source_commit":       sourceCommit,
		"source_plan_path":    options.SourcePlanPath,
		"source_plan_sha256":  sourcePlanHash,
		"spec_path":           options.SpecPath,
		"spec_sha256":         specHash,
		"plan_path":           options.PlanPath,
		"plan_sha256":         planHash,
		"task_graph_path":     options.TasksPath,
		"task_graph_sha256":   tasksHash,
		"invalidated_at":      nil,
		"invalidation_reason": nil,
	}
	value, err := MarshalJSON(lock)
	if err != nil {
		return err
	}
	return writeFile(options.OutputPath, value, 0o644)
}

func CheckApprovalLock(options ApprovalOptions) error {
	if err := checkApprovalSourcePlan(options); err != nil {
		return err
	}
	value, err := os.ReadFile(options.OutputPath)
	if err != nil {
		return fmt.Errorf("plan lock is missing or unreadable: %w", err)
	}
	lock := map[string]any{}
	if err := DecodeJSON("check plan approval lock", options.OutputPath, value, &lock); err != nil {
		return err
	}
	mismatches := []string{}
	paths := map[string]string{"source_plan": options.SourcePlanPath, "spec": options.SpecPath, "plan": options.PlanPath, "task_graph": options.TasksPath}
	for _, label := range []string{"source_plan", "spec", "plan", "task_graph"} {
		hash, hashErr := SHA256File(paths[label])
		if hashErr != nil || stringValue(lock[label+"_sha256"]) != hash {
			mismatches = append(mismatches, label)
		}
	}
	if stringValue(lock["status"]) != "APPROVED" || lock["invalidated_at"] != nil {
		mismatches = append(mismatches, "status")
	}
	if stringValue(lock["approved_by"]) == "" {
		mismatches = append(mismatches, "approver")
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("stale or invalid plan lock: %s", strings.Join(mismatches, ", "))
	}
	return nil
}
