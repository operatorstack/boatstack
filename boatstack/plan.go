package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

func LoadPlan(path string) (map[string]any, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan map[string]any
	if err := json.Unmarshal(value, &plan); err != nil {
		return nil, err
	}
	return plan, nil
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
	return nil
}

func CompilePlan(plan map[string]any) (map[string]any, map[string]any, string, error) {
	if err := ValidatePlan(plan); err != nil {
		return nil, nil, "", err
	}
	criteria, _ := objectSlice(plan["acceptance_criteria"])
	tasks, _ := objectSlice(plan["tasks"])
	rows := make([]any, 0, len(criteria))
	evidence := []string{
		"# Evidence ledger: " + stringValue(plan["feature_id"]), "",
		"- Approved plan lock: pending", "- Test gate: `BLOCKED`", "- Review gate: `BLOCKED`", "- Ship gate: `BLOCKED`", "",
		"## Acceptance evidence", "", "| Criterion | Tasks | Result | Evidence |", "|---|---|---|---|",
	}
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
	evidence = append(evidence, "", "## Commands and checks", "", "## Review findings", "", "## Known gaps", "", "## Rollout and rollback", "")
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
	if err := json.Unmarshal(value, &lock); err != nil {
		return fmt.Errorf("plan lock is unreadable: %w", err)
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
