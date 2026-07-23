package boatstack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SafetyFinding is intentionally small and secret-free. The guard reports the
// class and a stable explanation, never the full command or tool arguments.
type SafetyFinding struct {
	Category               string `json:"category"`
	Reason                 string `json:"reason"`
	Source                 string `json:"source,omitempty"`
	BlockingFeature        string `json:"blocking_feature,omitempty"`
	BlockingSlice          string `json:"blocking_slice,omitempty"`
	BranchRelation         string `json:"branch_relation,omitempty"`
	NextOperation          string `json:"next_operation,omitempty"`
	ParentDelivery         string `json:"parent_delivery,omitempty"`
	WorkflowStage          string `json:"workflow_stage,omitempty"`
	AttemptedPath          string `json:"attempted_path,omitempty"`
	OperationID            string `json:"operation_id,omitempty"`
	OperationState         string `json:"operation_state,omitempty"`
	AttemptNumber          int    `json:"attempt_number,omitempty"`
	ReconciliationRequired bool   `json:"reconciliation_required,omitempty"`
}

type SafetyReport struct {
	Status   string          `json:"status"`
	Findings []SafetyFinding `json:"findings"`
}

type SafetyHookOptions struct {
	Host  string
	Repo  string
	Input []byte
}

type hookDecodeError struct {
	code string
}

func (err hookDecodeError) Error() string { return err.code }

func malformedHookInput(code string) error {
	return hookDecodeError{code: code}
}

var readOnlyStage = regexp.MustCompile(`(?i)^\s*(?:env\s+[^ ]+\s+)*(?:rg|grep|git\s+(?:grep|diff|status|show|log)|cat|sed|head|tail|less|find\s+[^\n]*-(?:print|ls)|psql\s+[^\n]*\s-c\s+["']?\s*select\b)`)

var irreversiblePatterns = []struct {
	category string
	reason   string
	pattern  *regexp.Regexp
}{
	{"database-destruction", "database or schema destruction is operator-only", regexp.MustCompile(`(?is)\bdrop\s+(?:database|schema|table)\b|\balter\s+table\b[^;\n]*\bdrop\s+(?:column|constraint)\b|\btruncate(?:\s+table)?\b|\bdrop\s+schema\b[^;\n]*\bcascade\b`)},
	{"database-reset", "database reset, flush, or destructive downgrade is operator-only", regexp.MustCompile(`(?i)(?:--reset-public\b|\b(?:supabase\s+db\s+reset|prisma\s+migrate\s+reset|rails\s+db:(?:drop|reset)|django-admin\s+flush|manage\.py\s+flush|alembic\s+downgrade\s+base|pg_restore\b[^\n]*\s--clean\b))`)},
	{"filesystem-destruction", "recursive deletion of a broad or protected path is denied", regexp.MustCompile(`(?i)\b(?:rm\s+-[^\n;]*(?:r[^\n;]*f|f[^\n;]*r)|remove-item\s+[^\n;]*-recurse[^\n;]*-force)\s+(?:["']?(?:/|~|\$home|\$HOME|\.|\.\.)["']?\s*(?:;|&&|\|\||$)|[^\s;]*\*[^\s;]*)`)},
	{"git-history-destruction", "destructive Git cleanup or history replacement is denied", regexp.MustCompile(`(?i)\bgit\s+(?:reset\s+--hard\b|clean\s+-[^\s]*(?:f[^\s]*d|d[^\s]*f|x)[^\s]*|push\b[^\n]*(?:--force(?:-with-lease)?|-f\b))`)},
	{"infrastructure-destruction", "cloud or infrastructure destruction is operator-only", regexp.MustCompile(`(?i)\b(?:terraform|tofu|pulumi)\s+destroy\b|\bkubectl\s+delete\s+(?:namespace|cluster|persistentvolume|persistentvolumeclaim|pvc)\b|\bdocker\s+volume\s+(?:rm|prune)\b|\bgcloud\s+(?:projects|sql\s+instances|compute\s+(?:instances|disks))\s+delete\b|\baws\s+[^\n]*(?:delete-cluster|delete-db-instance|terminate-instances|delete-volume|delete-bucket)\b`)},
	{"recovery-destruction", "backup deletion or recovery disablement is operator-only", regexp.MustCompile(`(?i)\b(?:delete|remove|disable)\b[^\n;]*(?:backup|snapshot|point-in-time|pitr|recovery)\b`)},
}

var operationalPathPattern = regexp.MustCompile(`(?i)(?:^|/)(?:scripts?|migrations?|schema|database|db|deploy|infra|ops|terraform|k8s)(?:/|$)|\.(?:sql|ps1|sh|bash|py)$`)

// Match SQL mutation grammar rather than isolated English or command tokens.
// Requiring DELETE FROM or UPDATE <target> SET keeps executable SQL visible
// without treating names such as check-update or API method labels as queries.
var mutationStatementPattern = regexp.MustCompile(`(?is)\b(?:delete\s+from\s+(?:[a-z_][a-z0-9_$.-]*|"[^"]+")|update\s+(?:[a-z_][a-z0-9_$.-]*|"[^"]+")\s+set\b)[^;]*`)
var directPublicationPattern = regexp.MustCompile(`(?i)(?:\bgit\b[^\n;&|]*\bpush\b|\bgh\s+pr\s+(?:create|edit|ready|merge)\b|\bgh\s+api\b[^\n;&|]*(?:/pulls\b|/pull-requests\b)|\bhub\s+pull-request\b|\bcurl\b[^\n;&|]*(?:api\.github\.com|/pulls\b)[^\n;&|]*(?:\s-X\s*(?:POST|PATCH)|--request\s+(?:POST|PATCH)))`)
var approvedPublisherPattern = regexp.MustCompile(`(?i)^\s*(?:[^\s]*/)?boatstack-helper\s+publish-pr\b[^\n;&|]*$`)
var deliveryStatePathPattern = regexp.MustCompile(`(?i)(?:boatstack[/\\]deliveries|\.git[/\\](?:worktrees[/\\][^/\\]+[/\\])?boatstack(?:[/\\]|$))`)
var mutationToolPattern = regexp.MustCompile(`(?i)(?:write|edit|apply[_-]?patch|create|delete|remove|move|rename|update|insert|upload|install)`)
var planningMutationToolPattern = regexp.MustCompile(`(?i)(?:write|edit|apply[_-]?patch|create)`)
var externalReadOnlyToolPattern = regexp.MustCompile(`(?i)(?:^|[_-])(?:get|list|read|search|find|status|inspect|query|fetch|open)(?:[_-]|$)`)

func controlledPhaseTransition(command, stage string) bool {
	if strings.ContainsAny(command, "\n`><;&|") || strings.Contains(command, "$(") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 {
		return false
	}
	executable := strings.TrimSuffix(strings.ToLower(filepath.Base(fields[0])), ".exe")
	if executable != "boatstack-helper" {
		return false
	}
	readOnlyHelpers := map[string]bool{
		"check-plan": true, "check-source-plan": true, "next-status": true, "delivery-status": true,
		"recovery-status": true, "repair-status": true, "operation-status": true, "check-safety": true, "workspace-status": true, "diagnose-hook": true,
		"doctor": true, "version": true,
	}
	if readOnlyHelpers[fields[1]] {
		return true
	}
	switch stage {
	case "DRAFT_PLAN":
		return fields[1] == "planning-write" || fields[1] == "record-approval"
	case "INVALID_STATE":
		return fields[1] == "planning-write" || fields[1] == "record-approval"
	case "APPROVED", "POLICY_READY":
		return fields[1] == "activate-plan" || fields[1] == "workspace-cut"
	default:
		return false
	}
}

func controlledWorkspaceSync(repo, command string) bool {
	if strings.ContainsAny(command, "\n`><;&|") || strings.Contains(command, "$(") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 4 || fields[1] != "workspace-sync" {
		return false
	}
	executable := fields[0]
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(repo, filepath.FromSlash(executable))
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return false
	}
	expected := filepath.Join(repo, ".product-loop", "bin", helperName())
	expected, err = filepath.Abs(expected)
	if err != nil || filepath.Clean(executable) != filepath.Clean(expected) {
		return false
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	seenSource := false
	for index := 2; index < len(fields); index += 2 {
		if index+1 >= len(fields) {
			return false
		}
		switch fields[index] {
		case "--source":
			seenSource = strings.TrimSpace(fields[index+1]) != ""
		case "--branch":
			if strings.TrimSpace(fields[index+1]) == "" {
				return false
			}
		case "--repo":
			candidate := fields[index+1]
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(repo, filepath.FromSlash(candidate))
			}
			absolute, absErr := filepath.Abs(candidate)
			if absErr != nil || filepath.Clean(absolute) != filepath.Clean(repo) {
				return false
			}
		default:
			return false
		}
	}
	return seenSource
}

func attemptedRepositoryPath(repo string, input any) string {
	keys := map[string]bool{"path": true, "file_path": true, "filepath": true, "target_path": true, "destination": true}
	var visit func(any) string
	visit = func(value any) string {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if keys[strings.ToLower(key)] {
					if candidate, ok := child.(string); ok && strings.TrimSpace(candidate) != "" {
						path := candidate
						if !filepath.IsAbs(path) {
							path = filepath.Join(repo, filepath.FromSlash(path))
						}
						absolute, err := filepath.Abs(path)
						if err != nil {
							return "<invalid-path>"
						}
						relative, err := filepath.Rel(repo, absolute)
						if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
							return "<outside-repository>"
						}
						if err := rejectSymlinkComponents(repo, absolute); err != nil {
							return "<invalid-path>"
						}
						return filepath.ToSlash(relative)
					}
				}
			}
			for _, child := range typed {
				if found := visit(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := visit(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return visit(input)
}

func planningMarkdownPath(path string) bool {
	if !strings.HasPrefix(path, ".product-loop/features/") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	return len(parts) == 4 && featureSlugPattern.MatchString(parts[2]) && planningArtifacts[parts[3]]
}

func preActivationFinding(repo, attemptedPath string) (SafetyFinding, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return SafetyFinding{Category: "workflow-state-invalid", Reason: "managed delivery state cannot be verified", Source: "delivery-state", NextOperation: "repair-state"}, true
	}
	if len(active) > 0 {
		return SafetyFinding{}, false
	}
	candidates, err := featurePlanCandidates(repo)
	if err != nil {
		return SafetyFinding{Category: "workflow-state-invalid", Reason: "saved feature plans cannot be verified", Source: "planning-state", NextOperation: "repair-state"}, true
	}
	if len(candidates) == 0 {
		return SafetyFinding{}, false
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		return SafetyFinding{Category: "workflow-state-invalid", Reason: "workflow state cannot be resolved", Source: "planning-state", NextOperation: "repair-state"}, true
	}
	if status.ObservedStage != "DRAFT_PLAN" && status.ObservedStage != "APPROVED" && status.ObservedStage != "POLICY_READY" && status.ObservedStage != "AMBIGUOUS" && status.ObservedStage != "INVALID_STATE" {
		return SafetyFinding{}, false
	}
	if len(candidates) == 1 && status.ObservedStage != "AMBIGUOUS" {
		planPath := filepath.Join(repo, ".product-loop", "features", candidates[0], "plan.md")
		check, checkErr := CheckPlan(planPath)
		if checkErr != nil {
			return SafetyFinding{
				Category: "workflow-phase-bypass", Reason: "saved plan state is invalid", Source: "planning-state",
				BlockingFeature: candidates[0], WorkflowStage: "INVALID_STATE", AttemptedPath: attemptedPath, NextOperation: "repair-state",
			}, true
		}
		if status.ObservedStage == "APPROVED" {
			approvalPath := filepath.Join(filepath.Dir(planPath), "approval.md")
			if _, approvalErr := CheckApprovalReceipt(approvalPath, check); approvalErr != nil {
				return SafetyFinding{
					Category: "workflow-phase-bypass", Reason: "approval or product baseline is stale", Source: "planning-state",
					BlockingFeature: candidates[0], WorkflowStage: "INVALID_STATE", AttemptedPath: attemptedPath, NextOperation: "plan-gate",
				}, true
			}
		}
	}
	return SafetyFinding{
		Category: "workflow-phase-bypass", Reason: "product mutation is denied until the saved plan reaches its controlled activation boundary", Source: "planning-state",
		BlockingFeature: status.Feature, WorkflowStage: status.ObservedStage, AttemptedPath: attemptedPath, NextOperation: status.NextOperation,
	}, true
}

func publicationBypassFinding(repo, reason, source string) (SafetyFinding, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return SafetyFinding{Category: "workflow-state-invalid", Reason: "publication is denied because managed delivery state cannot be verified", Source: "delivery-state"}, true
	}
	if len(active) == 0 {
		return SafetyFinding{}, false
	}
	branch, _ := gitCommand(repo, "branch", "--show-current")
	selected := ""
	relation := "unrelated"
	for _, feature := range active {
		state, loadErr := LoadDeliveryState(repo, feature)
		if loadErr == nil && stateMatchesBranch(state, strings.TrimSpace(branch)) {
			if selected != "" {
				selected = strings.Join(active, ",")
				relation = "ambiguous"
				break
			}
			selected = feature
			relation = "current_branch"
		}
	}
	if selected == "" {
		if len(active) == 1 {
			selected = active[0]
		} else {
			selected = strings.Join(active, ",")
			relation = "ambiguous"
		}
	}
	finding := SafetyFinding{
		Category: "workflow-publication-bypass", Reason: reason, Source: source,
		BlockingFeature: selected, BranchRelation: relation, NextOperation: "recovery-status",
	}
	if !strings.Contains(selected, ",") {
		if state, loadErr := LoadDeliveryState(repo, selected); loadErr == nil {
			_, finding.BlockingSlice, _ = deliveryBranchAndSlice(state)
			finding.ParentDelivery = state.ParentDelivery
		}
	}
	return finding, true
}

func classifySafetyText(value, source string) []SafetyFinding {
	if isPureReadOnlyCommand(value) {
		return nil
	}
	findings := []SafetyFinding{}
	seen := map[string]bool{}
	for _, rule := range irreversiblePatterns {
		if rule.pattern.MatchString(value) && !seen[rule.category] {
			seen[rule.category] = true
			findings = append(findings, SafetyFinding{Category: rule.category, Reason: rule.reason, Source: source})
		}
	}
	for _, statement := range mutationStatementPattern.FindAllString(strings.ToLower(value), -1) {
		normalized := " " + strings.Join(strings.Fields(statement), " ") + " "
		if !strings.Contains(normalized, " where ") {
			findings = append(findings, SafetyFinding{Category: "unbounded-data-mutation", Reason: "unbounded data deletion or update is denied", Source: source})
			break
		}
	}
	return findings
}

// isPureReadOnlyCommand recognizes a deliberately narrow diagnostic surface.
// Every pipeline stage must itself be read-only, and compound shell syntax is
// rejected. Quoted search patterns may name dangerous operations without
// turning the diagnostic search into an executable capability.
func isPureReadOnlyCommand(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, "\n`><") || strings.Contains(trimmed, "$(") ||
		strings.Contains(trimmed, ";") || strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") || strings.Contains(trimmed, "<<") {
		return false
	}
	stages, ok := shellPipelineStages(trimmed)
	if !ok {
		return false
	}
	for _, stage := range stages {
		if !readOnlyStage.MatchString(strings.TrimSpace(stage)) {
			return false
		}
	}
	return true
}

func shellPipelineStages(value string) ([]string, bool) {
	stages := []string{}
	start := 0
	var quote rune
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '|' {
			stages = append(stages, value[start:index])
			start = index + 1
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	stages = append(stages, value[start:])
	return stages, true
}

func safeRepositoryPath(repo, candidate string) (string, bool) {
	candidate = strings.Trim(candidate, "\"'`;,()[]{}")
	if candidate == "" || strings.HasPrefix(candidate, "-") {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(candidate))
	if ext != ".py" && ext != ".sh" && ext != ".bash" && ext != ".ps1" && ext != ".sql" {
		return "", false
	}
	path := candidate
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, filepath.FromSlash(candidate))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(repo, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return abs, true
}

func invokedRepositoryFiles(repo, command string) []string {
	paths := []string{}
	seen := map[string]bool{}
	for _, token := range strings.Fields(command) {
		if path, ok := safeRepositoryPath(repo, token); ok && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func ClassifyCommand(repo, command string) []SafetyFinding {
	if strings.TrimSpace(command) == "" {
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "empty-command", Source: "tool-input"}}
	}
	if deliveryStatePathPattern.MatchString(command) && !isPureReadOnlyCommand(command) {
		return []SafetyFinding{{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"}}
	}
	if directPublicationPattern.MatchString(command) && !approvedPublisherPattern.MatchString(command) {
		if finding, blocked := publicationBypassFinding(repo, "direct push or PR mutation is denied while a managed delivery slice is active", "tool-input"); blocked {
			return []SafetyFinding{finding}
		}
	}
	if strings.Contains(command, "workspace-sync") && !isPureReadOnlyCommand(command) && !controlledWorkspaceSync(repo, command) {
		return []SafetyFinding{{Category: "workspace-sync-bypass", Reason: "recoverable branch alignment must use the exact project-local Boatstack helper", Source: "command"}}
	}
	findings := classifySafetyText(command, "command")
	if len(findings) > 0 {
		return dedupeFindings(findings)
	}
	if !isPureReadOnlyCommand(command) {
		if finding, blocked := preActivationFinding(repo, ""); blocked && !controlledPhaseTransition(command, finding.WorkflowStage) && !controlledWorkspaceSync(repo, command) {
			return []SafetyFinding{finding}
		}
	}
	if regexp.MustCompile(`(?i)\b(?:rm\s+-[^\n;]*(?:r[^\n;]*f|f[^\n;]*r)|remove-item\s+[^\n;]*-recurse[^\n;]*-force)\b`).MatchString(command) && strings.Contains(command, repo) {
		findings = append(findings, SafetyFinding{Category: "filesystem-destruction", Reason: "recursive deletion of the repository is denied", Source: "command"})
	}
	if len(findings) > 0 || isPureReadOnlyCommand(command) {
		return dedupeFindings(findings)
	}
	for _, token := range strings.Fields(command) {
		candidate := strings.Trim(token, "\"'`;,()[]{}")
		if ext := strings.ToLower(filepath.Ext(candidate)); ext != ".py" && ext != ".sh" && ext != ".bash" && ext != ".ps1" && ext != ".sql" {
			continue
		}
		path := candidate
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo, filepath.FromSlash(path))
		}
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return []SafetyFinding{{Category: "symlink-entrypoint", Reason: "an invoked repository entrypoint is a symlink and cannot be inspected safely", Source: filepath.Base(path)}}
		}
	}
	for _, path := range invokedRepositoryFiles(repo, command) {
		value, err := os.ReadFile(path)
		if err != nil {
			return []SafetyFinding{{Category: "unreadable-entrypoint", Reason: "an invoked repository entrypoint could not be inspected", Source: filepath.Base(path)}}
		}
		relative, relErr := filepath.Rel(repo, path)
		if relErr != nil {
			relative = filepath.Base(path)
		}
		findings = append(findings, classifySafetyText(string(value), filepath.ToSlash(relative))...)
	}
	return dedupeFindings(findings)
}

func ClassifyTool(repo, name string, input any) []SafetyFinding {
	if strings.EqualFold(name, "Bash") || strings.EqualFold(name, "Shell") || strings.EqualFold(name, "beforeShellExecution") || strings.EqualFold(name, "run_shell_command") {
		if object, ok := input.(map[string]any); ok {
			return ClassifyCommand(repo, stringValue(object["command"]))
		}
	}
	value, err := json.Marshal(input)
	if err != nil {
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "invalid-tool-input", Source: "tool-input"}}
	}
	combined := name + " " + string(value)
	findings := classifySafetyText(combined, "tool-input")
	nameLower := strings.ToLower(name)
	attemptedPath := attemptedRepositoryPath(repo, input)
	mutationCapable := mutationToolPattern.MatchString(nameLower) || (strings.HasPrefix(nameLower, "mcp__") && !externalReadOnlyToolPattern.MatchString(nameLower))
	if mutationCapable {
		if finding, blocked := preActivationFinding(repo, attemptedPath); blocked {
			if finding.WorkflowStage != "DRAFT_PLAN" || attemptedPath == "" || !planningMarkdownPath(attemptedPath) || !planningMutationToolPattern.MatchString(nameLower) {
				findings = append(findings, finding)
			}
		}
	}
	publicationText := strings.ToLower(combined)
	if deliveryStatePathPattern.MatchString(combined) && regexp.MustCompile(`(?:write|edit|delete|remove|move|rename|create|update)`).MatchString(nameLower) {
		findings = append(findings, SafetyFinding{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"})
	}
	if (strings.Contains(publicationText, "pull_request") || strings.Contains(publicationText, "pull request")) &&
		regexp.MustCompile(`(?:create|update|edit|merge|publish)`).MatchString(publicationText) {
		if finding, blocked := publicationBypassFinding(repo, "direct PR mutation is denied while a managed delivery slice is active", "tool-input"); blocked {
			findings = append(findings, finding)
		}
	}
	if regexp.MustCompile(`(?:delete|destroy|reset|drop|truncate|terminate)`).MatchString(nameLower) && regexp.MustCompile(`(?:database|schema|project|cluster|namespace|volume|bucket|backup|snapshot|instance)`).MatchString(strings.ToLower(combined)) {
		findings = append(findings, SafetyFinding{Category: "external-resource-destruction", Reason: "destructive external-resource tools are operator-only", Source: "tool-input"})
	}
	return dedupeFindings(findings)
}

func mutationCapableTool(name string, input any) bool {
	if strings.EqualFold(name, "Bash") || strings.EqualFold(name, "Shell") || strings.EqualFold(name, "beforeShellExecution") || strings.EqualFold(name, "run_shell_command") {
		object, ok := input.(map[string]any)
		return !ok || !isPureReadOnlyCommand(stringValue(object["command"]))
	}
	lower := strings.ToLower(name)
	return mutationToolPattern.MatchString(lower) || (strings.HasPrefix(lower, "mcp__") && !externalReadOnlyToolPattern.MatchString(lower))
}

func supervisedToolIdentity(name string, input any) (string, string) {
	value, _ := json.Marshal(input)
	fingerprint := SHA256Bytes(append([]byte(strings.ToLower(strings.TrimSpace(name))+"\x00"), value...))
	return "tool:" + strings.ToLower(strings.TrimSpace(name)), fingerprint
}

func activeManagedOperationScope(repo string) (OperationScope, string, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil || len(active) == 0 {
		return OperationScope{}, "", false
	}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	for _, feature := range active {
		state, loadErr := LoadDeliveryState(repo, feature)
		if loadErr != nil || !stateMatchesBranch(state, branch) || state.ActiveIndex >= len(state.Slices) {
			continue
		}
		slice := state.Slices[state.ActiveIndex]
		return OperationScope{Feature: feature, Slice: slice.ID, Worktree: filepath.Base(repo), HeadBranch: branch}, state.PlanLockHash, true
	}
	return OperationScope{}, "", false
}

func operationRetryClassForTool(name string) string {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mcp__") || strings.Contains(lower, "upload") || strings.Contains(lower, "browser") {
		return "RECONCILE_FIRST"
	}
	if strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "patch") || strings.Contains(lower, "create") {
		return "ATOMIC_LOCAL"
	}
	return "IDEMPOTENT_EXTERNAL"
}

func hookAttemptKey(host, fingerprint string, eventValue []byte) string {
	var event map[string]any
	if json.Unmarshal(eventValue, &event) == nil {
		for _, key := range []string{"tool_call_id", "tool_use_id", "call_id"} {
			if identity := strings.TrimSpace(stringValue(event[key])); identity != "" {
				return SHA256Bytes([]byte(strings.ToLower(strings.TrimSpace(host)) + "\x00" + identity + "\x00" + fingerprint))
			}
		}
	}
	return SHA256Bytes([]byte(strings.ToLower(strings.TrimSpace(host)) + "\x00" + fingerprint))
}

func superviseToolAttempt(repo, host, name string, input any, eventValue []byte) *SafetyFinding {
	if !mutationCapableTool(name, input) {
		return nil
	}
	scope, authority, managed := activeManagedOperationScope(repo)
	if !managed {
		return nil
	}
	kind, fingerprint := supervisedToolIdentity(name, input)
	target := attemptedRepositoryPath(repo, input)
	if target == "" {
		target = kind
	}
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: kind, Scope: scope, Target: target, PackageFingerprint: fingerprint,
		AuthorizationFingerprint: authority, RetryClass: operationRetryClassForTool(name), MaxAttempts: 3,
		ExpectedPostcondition: "the supervised tool reports completion and its target can be reconciled",
	})
	if err != nil {
		return &SafetyFinding{Category: "operation-state-invalid", Reason: "the managed operation receipt could not be prepared", Source: "operation-controller", NextOperation: "operation-status"}
	}
	if receipt.State == OperationSucceeded {
		return &SafetyFinding{Category: "operation-already-succeeded", Reason: "the identical fingerprinted operation already succeeded", Source: "operation-controller", OperationID: receipt.OperationID, OperationState: string(receipt.State), AttemptNumber: receipt.Attempt, NextOperation: "none"}
	}
	attemptKey := hookAttemptKey(host, fingerprint, eventValue)
	begin, beginErr := BeginOperation(repo, receipt.OperationID, attemptKey, name)
	if beginErr == nil {
		return nil
	}
	finding := &SafetyFinding{
		Category: "operation-state-invalid", Reason: beginErr.Error(), Source: "operation-controller",
		OperationID: receipt.OperationID, OperationState: string(begin.Receipt.State), AttemptNumber: begin.Receipt.Attempt, NextOperation: "operation-status",
	}
	switch {
	case errors.Is(beginErr, ErrOperationInFlight):
		finding.Category = "operation-in-flight"
		finding.Reason = "the identical authorized operation is already executing"
		finding.NextOperation = "wait"
	case begin.Receipt.State == OperationReconcileRequired:
		finding.Category = "operation-reconciliation-required"
		finding.Reason = "the previous attempt ended without an observable completion"
		finding.ReconciliationRequired = true
		finding.NextOperation = "reconcile"
	case begin.Receipt.State == OperationFailedFinal:
		finding.Category = "operation-retry-exhausted"
		finding.Reason = "the persistent operation retry budget is exhausted"
		finding.NextOperation = "manual_recovery"
	}
	return finding
}

func postToolEvent(host string, value []byte) (string, any, string, bool, bool) {
	var event map[string]any
	if json.Unmarshal(value, &event) != nil {
		return "", nil, "", false, false
	}
	eventName := stringValue(event["hook_event_name"])
	postNames := map[string]bool{"postToolUse": true, "postToolUseFailure": true, "afterShellExecution": true, "afterMCPExecution": true, "PostToolUse": true, "PostToolUseFailure": true, "AfterTool": true}
	if !postNames[eventName] {
		return "", nil, "", false, false
	}
	name := stringValue(event["tool_name"])
	input := event["tool_input"]
	if eventName == "afterShellExecution" {
		name = "Bash"
		input = map[string]any{"command": stringValue(event["command"])}
	}
	if eventName == "afterMCPExecution" {
		var err error
		input, err = cursorMCPInput(input)
		if err != nil {
			return "", nil, "UNKNOWN", true, true
		}
	}
	hasResult := event["tool_response"] != nil || event["tool_result"] != nil || event["tool_output"] != nil || event["result"] != nil || event["output"] != nil || event["error"] != nil || event["tool_error"] != nil || event["exit_code"] != nil || event["exitCode"] != nil
	if strings.Contains(strings.ToLower(eventName), "failure") {
		hasResult = event["error"] != nil || event["tool_error"] != nil
	}
	if !hasResult {
		return "", nil, "UNKNOWN", true, true
	}
	outcome := "SUCCEEDED"
	failed := event["error"] != nil || event["tool_error"] != nil || event["is_error"] == true
	for _, key := range []string{"exit_code", "exitCode"} {
		if code, ok := event[key].(float64); ok && code != 0 {
			failed = true
		}
	}
	if response, ok := event["tool_response"].(map[string]any); ok {
		if response["error"] != nil || response["is_error"] == true || response["success"] == false {
			failed = true
		}
		for _, key := range []string{"exit_code", "exitCode"} {
			if code, ok := response[key].(float64); ok && code != 0 {
				failed = true
			}
		}
	}
	if failed {
		outcome = "UNKNOWN"
	}
	if strings.TrimSpace(name) == "" || input == nil {
		return "", nil, "UNKNOWN", true, true
	}
	return name, input, outcome, true, false
}

func completeSupervisedToolEvent(repo, host string, value []byte) (bool, bool) {
	name, input, outcome, handled, malformed := postToolEvent(host, value)
	if !handled {
		return false, false
	}
	if malformed {
		return true, true
	}
	if name == "" || input == nil || !mutationCapableTool(name, input) {
		return true, false
	}
	kind, fingerprint := supervisedToolIdentity(name, input)
	target := attemptedRepositoryPath(repo, input)
	if target == "" {
		target = kind
	}
	id := operationID(kind, target, fingerprint)
	receipt, err := loadOperation(repo, id)
	if err != nil || receipt.State != OperationExecuting || receipt.Lease == nil {
		return true, false
	}
	attemptKey := hookAttemptKey(host, fingerprint, value)
	if _, err := CompleteOperationAttempt(repo, id, attemptKey, outcome, "host completion event observed", ""); err != nil && outcome != "UNKNOWN" {
		_, _ = CompleteOperationAttempt(repo, id, attemptKey, "UNKNOWN", "completion event could not be correlated", "")
	}
	return true, false
}

func dedupeFindings(values []SafetyFinding) []SafetyFinding {
	seen := map[string]bool{}
	result := []SafetyFinding{}
	for _, value := range values {
		key := value.Category + "\x00" + value.Source
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category == result[j].Category {
			return result[i].Source < result[j].Source
		}
		return result[i].Category < result[j].Category
	})
	return result
}

type hookHostContract struct {
	decode func([]byte) (string, any, error)
	allow  func() ([]byte, error)
	deny   func(SafetyFinding) ([]byte, error)
}

func decodeJSONObject(host string, value []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(value))) == 0 {
		return nil, malformedHookInput("empty-input")
	}
	var event map[string]any
	if err := DecodeJSON("parse "+host+" hook event", "stdin", value, &event); err != nil {
		return nil, malformedHookInput("invalid-json")
	}
	return event, nil
}

func cursorMCPInput(value any) (any, error) {
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil, malformedHookInput("empty-tool-input")
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, malformedHookInput("invalid-tool-input-json")
		}
		return decoded, nil
	}
	if value == nil {
		return nil, malformedHookInput("missing-tool-input")
	}
	return value, nil
}

func decodeCursorHook(value []byte) (string, any, error) {
	event, err := decodeJSONObject("cursor", value)
	if err != nil {
		return "", nil, err
	}
	eventName := stringValue(event["hook_event_name"])
	command := stringValue(event["command"])
	toolName := stringValue(event["tool_name"])
	toolInput := event["tool_input"]

	switch eventName {
	case "preToolUse":
		if _, present := event["tool_name"]; !present {
			return "", nil, malformedHookInput("missing-tool-name")
		}
		if strings.TrimSpace(toolName) == "" {
			return "", nil, malformedHookInput("empty-tool-name")
		}
		if toolInput == nil {
			return "", nil, malformedHookInput("missing-tool-input")
		}
		return toolName, toolInput, nil
	case "beforeShellExecution":
		if _, present := event["command"]; !present {
			return "", nil, malformedHookInput("missing-command")
		}
		if strings.TrimSpace(command) == "" {
			return "", nil, malformedHookInput("empty-command")
		}
		return "Bash", map[string]any{"command": command}, nil
	case "beforeMCPExecution":
		if _, present := event["tool_name"]; !present {
			return "", nil, malformedHookInput("missing-tool-name")
		}
		if strings.TrimSpace(toolName) == "" {
			return "", nil, malformedHookInput("empty-tool-name")
		}
		input, inputErr := cursorMCPInput(toolInput)
		if inputErr != nil {
			return "", nil, inputErr
		}
		return toolName, input, nil
	case "":
		// Older Cursor builds omitted hook_event_name. Preserve only the two
		// unambiguous shapes; an MCP transport command must never be classified
		// as the requested tool operation.
		if toolName != "" && toolInput != nil {
			if command != "" {
				return "", nil, malformedHookInput("ambiguous-event")
			}
			input, inputErr := cursorMCPInput(toolInput)
			if inputErr != nil {
				return "", nil, inputErr
			}
			return toolName, input, nil
		}
		if command != "" && toolName == "" {
			return "Bash", map[string]any{"command": command}, nil
		}
		return "", nil, malformedHookInput("missing-command-or-tool")
	default:
		return "", nil, malformedHookInput("unsupported-event")
	}
}

func decodePreToolUseHook(host string, value []byte) (string, any, error) {
	event, err := decodeJSONObject(host, value)
	if err != nil {
		return "", nil, err
	}
	eventName := stringValue(event["hook_event_name"])
	if eventName != "" && eventName != "PreToolUse" && !(host == "claude" && eventName == "preToolUse") {
		return "", nil, malformedHookInput("unsupported-event")
	}
	name := stringValue(event["tool_name"])
	input := event["tool_input"]
	if _, present := event["tool_name"]; !present {
		return "", nil, malformedHookInput("missing-tool-name")
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, malformedHookInput("empty-tool-name")
	}
	if input == nil {
		return "", nil, malformedHookInput("missing-tool-input")
	}
	return name, input, nil
}

func decodeGeminiHook(value []byte) (string, any, error) {
	event, err := decodeJSONObject("gemini", value)
	if err != nil {
		return "", nil, err
	}
	if eventName := stringValue(event["hook_event_name"]); eventName != "" && eventName != "BeforeTool" {
		return "", nil, malformedHookInput("unsupported-event")
	}
	name := stringValue(event["tool_name"])
	if strings.TrimSpace(name) == "" {
		return "", nil, malformedHookInput("missing-tool-name")
	}
	input, present := event["tool_input"]
	if !present || input == nil {
		return "", nil, malformedHookInput("missing-tool-input")
	}
	return name, input, nil
}

func structuredHookDeny(host string, finding SafetyFinding) ([]byte, error) {
	message := denialMessage(host, finding)
	value, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": message,
		},
	})
	return append(value, '\n'), err
}

var hookHostContracts = map[string]hookHostContract{
	"cursor": {
		decode: decodeCursorHook,
		allow: func() ([]byte, error) {
			value, err := json.Marshal(map[string]any{"continue": true, "permission": "allow"})
			return append(value, '\n'), err
		},
		deny: func(finding SafetyFinding) ([]byte, error) {
			message := denialMessage("cursor", finding)
			value, err := json.Marshal(map[string]any{
				"continue": true, "permission": "deny", "user_message": message, "agent_message": message,
			})
			return append(value, '\n'), err
		},
	},
	"claude": {
		decode: func(value []byte) (string, any, error) { return decodePreToolUseHook("claude", value) },
		allow:  func() ([]byte, error) { return nil, nil },
		deny:   func(finding SafetyFinding) ([]byte, error) { return structuredHookDeny("claude", finding) },
	},
	"codex": {
		decode: func(value []byte) (string, any, error) { return decodePreToolUseHook("codex", value) },
		allow:  func() ([]byte, error) { return nil, nil },
		deny:   func(finding SafetyFinding) ([]byte, error) { return structuredHookDeny("codex", finding) },
	},
	"gemini": {
		decode: decodeGeminiHook,
		allow: func() ([]byte, error) {
			value, err := json.Marshal(map[string]any{"decision": "allow"})
			return append(value, '\n'), err
		},
		deny: func(finding SafetyFinding) ([]byte, error) {
			value, err := json.Marshal(map[string]any{"decision": "deny", "reason": denialMessage("gemini", finding)})
			return append(value, '\n'), err
		},
	},
}

func denialMessage(host string, finding SafetyFinding) string {
	if finding.Category == "malformed-tool-input" {
		name := strings.ToUpper(strings.TrimSpace(host))
		if name == "" {
			name = "HOST"
		}
		message := "Boatstack could not inspect the " + name + " hook event (HOST_PAYLOAD_MALFORMED:" + finding.Reason + "). No unsafe operation was detected; execution is denied because the intended command or tool call is unavailable. Retry once with an explicit non-empty command. If this repeats, stop shell and tool retries and preserve current edits."
		if strings.EqualFold(host, "cursor") {
			message += " Start a new Cursor task and run `.product-loop/bin/boatstack-helper diagnose-hook --host cursor --repo .` from an external terminal. Do not reinstall Boatstack unless it separately reports a missing, drifted, unsafe, or checksum-invalid runtime."
		} else {
			message += " Run `.product-loop/bin/boatstack-helper diagnose-hook --host " + strings.ToLower(host) + " --repo .` from an external terminal before changing the installation."
		}
		return message
	}
	if finding.Category == "workflow-state-invalid" {
		return "Boatstack denied publication because managed delivery state cannot be verified. Re-run the active Boatstack operation or repair the installation before publishing."
	}
	if finding.Category == "workflow-state-tamper" {
		return "Boatstack denied direct delivery-state mutation. Use the active build, test, review, or ship transition instead of editing runtime authority."
	}
	if finding.Category == "workflow-phase-bypass" {
		target := "the saved Boatstack plan"
		if finding.BlockingFeature != "" {
			target = fmt.Sprintf("Boatstack feature %q", finding.BlockingFeature)
		}
		path := ""
		if finding.AttemptedPath != "" {
			path = " Attempted path: " + finding.AttemptedPath + "."
		}
		next := finding.NextOperation
		if next == "" {
			next = "repair-state"
		}
		return fmt.Sprintf("Boatstack denied product mutation because %s is at %s.%s Continue with %s; unrelated task completions do not authorize implementation.", target, finding.WorkflowStage, path, next)
	}
	if finding.Category == "workflow-publication-bypass" {
		target := "the active managed delivery"
		if finding.BlockingFeature != "" {
			target = fmt.Sprintf("managed delivery %q", finding.BlockingFeature)
		}
		relation := ""
		if finding.BranchRelation == "unrelated" {
			relation = " It is unrelated to the current branch."
		} else if finding.BranchRelation == "ambiguous" {
			relation = " More than one delivery may be blocking publication."
		}
		context := ""
		if finding.BlockingSlice != "" {
			context += " slice=" + finding.BlockingSlice
		}
		if finding.BranchRelation != "" {
			context += " relation=" + finding.BranchRelation
		}
		if finding.ParentDelivery != "" {
			context += " parent=" + finding.ParentDelivery
		}
		if finding.NextOperation != "" {
			context += " next=" + finding.NextOperation
		}
		if context != "" {
			context = " Recovery context:" + context + "."
		}
		return "Boatstack denied the publication bypass because " + target + " still owns publication authority." + relation + context + " Resolve the reported change through the managed recovery path; do not repeat this push or PR mutation manually."
	}
	if strings.HasPrefix(finding.Category, "operation-") {
		context := ""
		if finding.OperationID != "" {
			context = fmt.Sprintf(" operation=%s state=%s attempt=%d", finding.OperationID, finding.OperationState, finding.AttemptNumber)
		}
		switch finding.Category {
		case "operation-in-flight":
			return "Boatstack is already supervising this exact operation." + context + ". Wait for its completion event; do not launch it again."
		case "operation-already-succeeded":
			return "Boatstack already observed this exact operation succeed." + context + ". Continue from the resulting repository state instead of repeating it."
		case "operation-reconciliation-required":
			return "Boatstack cannot yet distinguish success from an interrupted response." + context + ". Reconcile the expected postcondition with operation-status before any retry."
		case "operation-retry-exhausted":
			return "Boatstack exhausted the persistent retry budget for this operation." + context + ". Preserve current state and use the reported manual recovery; do not repeat the tool call."
		default:
			return "Boatstack could not verify the durable operation state." + context + ". Inspect operation-status before retrying."
		}
	}
	if finding.Category == "git-history-destruction" {
		return "Boatstack denied raw destructive Git cleanup. Use the project-local workspace-sync operation to checkpoint current state and align the exact branch; do not scan delivery artifacts or retry the destructive command."
	}
	if finding.Category == "workspace-sync-bypass" {
		return "Boatstack denied an unverified workspace sync. Invoke only the exact project-local workspace-sync helper for the current repository."
	}
	return "Boatstack denied an irreversible operation (" + finding.Category + "). Preserve the current state and use read-only diagnosis or fix-forward recovery; destructive recovery is operator-only outside the agent workflow."
}

func HookDecision(options SafetyHookOptions) ([]byte, bool) {
	host := strings.ToLower(strings.TrimSpace(options.Host))
	contract, supported := hookHostContracts[host]
	if !supported {
		finding := SafetyFinding{Category: "unsupported-host", Reason: "unknown host is denied by the fail-closed guard", Source: "hook"}
		value, _ := structuredHookDeny("codex", finding)
		return value, true
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		finding := SafetyFinding{Category: "unresolved-repository", Reason: "repository identity could not be established", Source: "hook"}
		value, _ := contract.deny(finding)
		return value, true
	}
	if handled, malformed := completeSupervisedToolEvent(repo, host, options.Input); handled {
		if malformed {
			finding := SafetyFinding{Category: "malformed-tool-input", Reason: "invalid-post-event", Source: "hook"}
			value, _ := contract.deny(finding)
			return value, true
		}
		value, _ := contract.allow()
		return value, false
	}
	name, input, err := contract.decode(options.Input)
	if err != nil {
		reason := "invalid-event"
		var decodeErr hookDecodeError
		if errors.As(err, &decodeErr) {
			reason = decodeErr.code
		}
		finding := SafetyFinding{Category: "malformed-tool-input", Reason: reason, Source: "hook"}
		value, _ := contract.deny(finding)
		return value, true
	}
	findings := ClassifyTool(repo, name, input)
	if len(findings) == 0 {
		if finding := superviseToolAttempt(repo, host, name, input, options.Input); finding != nil {
			value, _ := contract.deny(*finding)
			return value, true
		}
		value, _ := contract.allow()
		return value, false
	}
	value, _ := contract.deny(findings[0])
	return value, true
}

func operationalChangedFiles(repo string, highRisk []string, defaultBranch string) ([]string, error) {
	diffStart := "HEAD"
	if strings.TrimSpace(defaultBranch) != "" {
		if head, headErr := gitCommand(repo, "branch", "--show-current"); headErr == nil && head != defaultBranch {
			if baseCommit, baseErr := resolveBaseCommit(repo, defaultBranch); baseErr == nil {
				if mergeBase, mergeErr := gitCommand(repo, "merge-base", baseCommit, "HEAD"); mergeErr == nil && mergeBase != "" {
					diffStart = mergeBase
				}
			}
		}
	}
	command := exec.Command("git", "-C", repo, "diff", "--name-only", "--diff-filter=ACMR", diffStart)
	value, err := command.Output()
	if err != nil {
		return nil, err
	}
	untrackedCommand := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard")
	untracked, err := untrackedCommand.Output()
	if err != nil {
		return nil, err
	}
	paths := []string{}
	seen := map[string]bool{}
	all := strings.TrimSpace(string(value)) + "\n" + strings.TrimSpace(string(untracked))
	for _, path := range strings.Split(all, "\n") {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		matched := operationalPathPattern.MatchString(path)
		for _, pattern := range highRisk {
			if ok, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path)); ok {
				matched = true
			}
			prefix := strings.TrimSuffix(filepath.ToSlash(pattern), "/**")
			if prefix != pattern && strings.HasPrefix(path, prefix+"/") {
				matched = true
			}
		}
		if matched {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func CheckRepositorySafety(repoPath string) (SafetyReport, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return SafetyReport{}, err
	}
	highRisk := []string{}
	defaultBranch := ""
	configPath := filepath.Join(repo, ".product-loop", "project.json")
	if value, readErr := os.ReadFile(configPath); readErr == nil {
		var config ProjectConfig
		if json.Unmarshal(value, &config) == nil {
			highRisk = config.Project.HighRiskPaths
			defaultBranch = config.Project.DefaultBranch
		}
	}
	paths, err := operationalChangedFiles(repo, highRisk, defaultBranch)
	if err != nil {
		return SafetyReport{}, err
	}
	findings := []SafetyFinding{}
	for _, relative := range paths {
		value, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relative)))
		if readErr != nil {
			return SafetyReport{}, readErr
		}
		findings = append(findings, classifySafetyText(string(value), relative)...)
	}
	findings = dedupeFindings(findings)
	status := "PASS"
	if len(findings) > 0 {
		status = "BLOCKED"
	}
	return SafetyReport{Status: status, Findings: findings}, nil
}
