package boatstack

import (
	"encoding/json"
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
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Source   string `json:"source,omitempty"`
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
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "empty shell input is denied by the fail-closed guard", Source: "tool-input"}}
	}
	if deliveryStatePathPattern.MatchString(command) && !isPureReadOnlyCommand(command) {
		return []SafetyFinding{{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"}}
	}
	if directPublicationPattern.MatchString(command) && !approvedPublisherPattern.MatchString(command) {
		active, activeErr := ActiveManagedDeliveries(repo)
		if activeErr != nil {
			return []SafetyFinding{{Category: "workflow-state-invalid", Reason: "publication is denied because managed delivery state cannot be verified", Source: "delivery-state"}}
		}
		if len(active) > 0 {
			return []SafetyFinding{{Category: "workflow-publication-bypass", Reason: "direct push or PR mutation is denied while a managed delivery slice is active", Source: "tool-input"}}
		}
	}
	findings := classifySafetyText(command, "command")
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
	if strings.EqualFold(name, "Bash") || strings.EqualFold(name, "Shell") || strings.EqualFold(name, "beforeShellExecution") {
		if object, ok := input.(map[string]any); ok {
			return ClassifyCommand(repo, stringValue(object["command"]))
		}
	}
	value, err := json.Marshal(input)
	if err != nil {
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "tool arguments could not be inspected", Source: "tool-input"}}
	}
	combined := name + " " + string(value)
	findings := classifySafetyText(combined, "tool-input")
	nameLower := strings.ToLower(name)
	publicationText := strings.ToLower(combined)
	if deliveryStatePathPattern.MatchString(combined) && regexp.MustCompile(`(?:write|edit|delete|remove|move|rename|create|update)`).MatchString(nameLower) {
		findings = append(findings, SafetyFinding{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"})
	}
	if (strings.Contains(publicationText, "pull_request") || strings.Contains(publicationText, "pull request")) &&
		regexp.MustCompile(`(?:create|update|edit|merge|publish)`).MatchString(publicationText) {
		active, activeErr := ActiveManagedDeliveries(repo)
		if activeErr != nil {
			findings = append(findings, SafetyFinding{Category: "workflow-state-invalid", Reason: "publication is denied because managed delivery state cannot be verified", Source: "delivery-state"})
		} else if len(active) > 0 {
			findings = append(findings, SafetyFinding{Category: "workflow-publication-bypass", Reason: "direct PR mutation is denied while a managed delivery slice is active", Source: "tool-input"})
		}
	}
	if regexp.MustCompile(`(?:delete|destroy|reset|drop|truncate|terminate)`).MatchString(nameLower) && regexp.MustCompile(`(?:database|schema|project|cluster|namespace|volume|bucket|backup|snapshot|instance)`).MatchString(strings.ToLower(combined)) {
		findings = append(findings, SafetyFinding{Category: "external-resource-destruction", Reason: "destructive external-resource tools are operator-only", Source: "tool-input"})
	}
	return dedupeFindings(findings)
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

func hookToolInput(host string, value []byte) (string, any, error) {
	var event map[string]any
	if err := DecodeJSON("parse "+host+" hook event", "stdin", value, &event); err != nil {
		return "", nil, err
	}
	if host == "cursor" {
		command := stringValue(event["command"])
		if command == "" {
			if input, ok := event["tool_input"].(map[string]any); ok {
				command = stringValue(input["command"])
			}
		}
		if command != "" {
			return "Bash", map[string]any{"command": command}, nil
		}
		name := stringValue(event["tool_name"])
		if name == "" {
			name = stringValue(event["server_name"]) + "_" + stringValue(event["method"])
		}
		input := event["tool_input"]
		if input == nil {
			input = event["arguments"]
		}
		if strings.Trim(name, "_") == "" || input == nil {
			return "", nil, fmt.Errorf("Cursor hook input has no command or MCP tool arguments")
		}
		return name, input, nil
	}
	name := stringValue(event["tool_name"])
	input := event["tool_input"]
	if name == "" || input == nil {
		return "", nil, fmt.Errorf("%s hook input is missing tool_name or tool_input", host)
	}
	return name, input, nil
}

func denialMessage(finding SafetyFinding) string {
	if finding.Category == "workflow-state-invalid" {
		return "Boatstack denied publication because managed delivery state cannot be verified. Re-run the active Boatstack operation or repair the installation before publishing."
	}
	if finding.Category == "workflow-state-tamper" {
		return "Boatstack denied direct delivery-state mutation. Use the active build, test, review, or ship transition instead of editing runtime authority."
	}
	if finding.Category == "workflow-publication-bypass" {
		return "Boatstack denied a publication bypass. Finish the active slice's test and review gates, then use ship-gate and the confirmed Boatstack publisher."
	}
	return "Boatstack denied an irreversible operation (" + finding.Category + "). Preserve the current state and use read-only diagnosis or fix-forward recovery; destructive recovery is operator-only outside the agent workflow."
}

func HookDecision(options SafetyHookOptions) ([]byte, bool) {
	host := strings.ToLower(strings.TrimSpace(options.Host))
	if host != "cursor" && host != "claude" && host != "codex" {
		finding := SafetyFinding{Category: "unsupported-host", Reason: "unknown host is denied by the fail-closed guard", Source: "hook"}
		value, _ := hookDenyJSON("codex", finding)
		return value, true
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		finding := SafetyFinding{Category: "unresolved-repository", Reason: "repository identity could not be established", Source: "hook"}
		value, _ := hookDenyJSON(host, finding)
		return value, true
	}
	name, input, err := hookToolInput(host, options.Input)
	if err != nil {
		finding := SafetyFinding{Category: "malformed-tool-input", Reason: err.Error(), Source: "hook"}
		value, _ := hookDenyJSON(host, finding)
		return value, true
	}
	findings := ClassifyTool(repo, name, input)
	if len(findings) == 0 {
		if host == "cursor" {
			value, _ := json.Marshal(map[string]any{"continue": true, "permission": "allow"})
			return append(value, '\n'), false
		}
		return nil, false
	}
	value, _ := hookDenyJSON(host, findings[0])
	return value, true
}

func hookDenyJSON(host string, finding SafetyFinding) ([]byte, error) {
	message := denialMessage(finding)
	if host == "cursor" {
		value, err := json.Marshal(map[string]any{
			"continue": true, "permission": "deny", "user_message": message, "agent_message": message,
		})
		return append(value, '\n'), err
	}
	value, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": message,
		},
	})
	return append(value, '\n'), err
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
