package boatstack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const prPreviewSchemaVersion = 2

var prStatusPattern = regexp.MustCompile(`(?i)^(PASS|PASS_WITH_GAPS|NOT_VERIFIED|BLOCKED)$`)

type PRContextOptions struct {
	Repo    string
	Feature string
	SliceID string
	Base    string
}

type PRSource struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PRContext struct {
	SchemaVersion      int               `json:"schema_version"`
	Mode               string            `json:"mode"`
	Feature            string            `json:"feature,omitempty"`
	SliceID            string            `json:"slice_id,omitempty"`
	BaseBranch         string            `json:"base_branch"`
	HeadBranch         string            `json:"head_branch"`
	BaseCommit         string            `json:"base_commit"`
	MergeBaseCommit    string            `json:"merge_base_commit"`
	HeadCommit         string            `json:"head_commit"`
	ProductDiffSHA256  string            `json:"product_diff_sha256"`
	ContextFingerprint string            `json:"context_fingerprint"`
	ChangedFiles       []string          `json:"changed_files"`
	Commits            []string          `json:"commits"`
	DiffStat           string            `json:"diff_stat"`
	ContextPaths       []string          `json:"context_paths,omitempty"`
	ProjectCommands    map[string]string `json:"project_commands,omitempty"`
	HighRiskFiles      []string          `json:"high_risk_files,omitempty"`
	GateStatus         map[string]string `json:"gate_status,omitempty"`
	SafetyStatus       string            `json:"safety_status"`
	SafetyFindings     []SafetyFinding   `json:"safety_findings,omitempty"`
	Sources            []PRSource        `json:"sources,omitempty"`
	PreviewPath        string            `json:"preview_path"`
}

type PRPreview struct {
	SchemaVersion      int
	Title              string
	Mode               string
	Feature            string
	SliceID            string
	BaseBranch         string
	HeadBranch         string
	ContextFingerprint string
	Body               string
	Path               string
	Fingerprint        string
}

type PRPublishOptions struct {
	Repo                string
	PreviewPath         string
	ExpectedFingerprint string
	Action              string
}

func commandOutput(repo string, name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = repo
	value, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(value))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(string(value)), nil
}

func gitCommand(repo string, arguments ...string) (string, error) {
	return commandOutput(repo, "git", append([]string{"-C", repo}, arguments...)...)
}

func defaultPRBase(repo string) string {
	configPath := filepath.Join(repo, ".product-loop", "project.json")
	if config, _, err := LoadConfig(configPath); err == nil && strings.TrimSpace(config.Project.DefaultBranch) != "" {
		return strings.TrimSpace(config.Project.DefaultBranch)
	}
	if branch := strings.TrimPrefix(gitOutput(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/"); branch != "" {
		return branch
	}
	return "main"
}

func resolveBaseCommit(repo, base string) (string, error) {
	for _, candidate := range []string{"refs/remotes/origin/" + base, "refs/heads/" + base, base} {
		if commit, err := gitCommand(repo, "rev-parse", "--verify", candidate+"^{commit}"); err == nil {
			return commit, nil
		}
	}
	return "", fmt.Errorf("base branch %q is not available locally; fetch it and try again", base)
}

func previewSlug(branch string) string {
	value := strings.ToLower(branch)
	var result strings.Builder
	lastDash := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
			lastDash = false
		} else if !lastDash && result.Len() > 0 {
			result.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func expectedPRPreviewPath(mode, feature, head string) (string, error) {
	switch mode {
	case "managed":
		if !featureSlugPattern.MatchString(feature) {
			return "", fmt.Errorf("managed PR context requires a lowercase kebab-case feature")
		}
		return filepath.ToSlash(filepath.Join(".product-loop", "features", feature, "pr.md")), nil
	case "ad-hoc":
		slug := previewSlug(head)
		if slug == "" {
			return "", fmt.Errorf("current branch cannot be converted into a PR brief slug")
		}
		return filepath.ToSlash(filepath.Join(".product-loop", "pr-briefs", slug, "pr.md")), nil
	default:
		return "", fmt.Errorf("unsupported PR context mode: %s", mode)
	}
}

func dirtyPaths(repo string) ([]string, error) {
	command := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	value, err := command.Output()
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, nil
	}
	paths := []string{}
	records := bytes.Split(value, []byte{0})
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		status := string(record[:2])
		paths = append(paths, string(record[3:]))
		if (strings.Contains(status, "R") || strings.Contains(status, "C")) && index+1 < len(records) && len(records[index+1]) > 0 {
			paths = append(paths, string(records[index+1]))
			index++
		}
	}
	return paths, nil
}

func productDiff(repo, baseCommit, previewPath string) ([]byte, []string, error) {
	pathspec := []string{"--", ".", ":(exclude).product-loop/features/*/pr.md", ":(exclude).product-loop/pr-briefs/*/pr.md"}
	arguments := append([]string{"diff", "--binary", "--no-ext-diff", baseCommit, "HEAD"}, pathspec...)
	diff, err := exec.Command("git", append([]string{"-C", repo}, arguments...)...).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read product diff: %w", err)
	}
	nameArguments := append([]string{"diff", "--name-only", baseCommit, "HEAD"}, pathspec...)
	names, err := gitCommand(repo, nameArguments...)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list changed files: %w", err)
	}
	changed := []string{}
	if names != "" {
		changed = strings.Split(names, "\n")
	}
	dirty, err := dirtyPaths(repo)
	if err != nil {
		return nil, nil, err
	}
	unexpected := []string{}
	for _, path := range dirty {
		if filepath.ToSlash(path) != filepath.ToSlash(previewPath) {
			unexpected = append(unexpected, path)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return nil, nil, fmt.Errorf("commit or remove non-preview working-tree changes before preparing the PR: %s", strings.Join(unexpected, ", "))
	}
	return diff, changed, nil
}

func productDiffStat(repo, baseCommit string) (string, error) {
	pathspec := []string{"--", ".", ":(exclude).product-loop/features/*/pr.md", ":(exclude).product-loop/pr-briefs/*/pr.md"}
	arguments := append([]string{"diff", "--stat", baseCommit, "HEAD"}, pathspec...)
	return gitCommand(repo, arguments...)
}

func highRiskChangedFiles(changed, patterns []string) []string {
	result := []string{}
	for _, path := range changed {
		matched := false
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(filepath.ToSlash(pattern))
			if pattern == "" {
				continue
			}
			prefix := strings.TrimSuffix(pattern, "/")
			globMatch, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path))
			if globMatch || path == prefix || strings.HasPrefix(path, prefix+"/") {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result
}

func evidenceGateStatus(value, gate string) string {
	quoted := regexp.QuoteMeta(gate)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?mi)^\s*-\s*` + quoted + `\s+gate\s*:\s*` + "`?" + `([A-Z_]+)` + "`?" + `\s*$`),
		regexp.MustCompile(`(?mi)^\s*\|\s*` + quoted + `\s+gate\s*\|\s*` + "`?" + `([A-Z_]+)` + "`?"),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(value); len(match) == 2 {
			return strings.ToUpper(match[1])
		}
	}
	return ""
}

func relativeSource(repo, path, kind string) (PRSource, error) {
	hash, err := SHA256File(path)
	if err != nil {
		return PRSource{}, err
	}
	relative, err := repositoryRelativePath(repo, path)
	if err != nil {
		return PRSource{}, err
	}
	return PRSource{Kind: kind, Path: relative, SHA256: hash}, nil
}

func managedPRSources(repo, feature string) ([]PRSource, map[string]string, error) {
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	planPath := filepath.Join(directory, "plan.md")
	approvalPath := filepath.Join(directory, "approval.md")
	lockPath := filepath.Join(directory, "plan.lock.json")
	check, err := CheckPlan(planPath)
	if err != nil {
		return nil, nil, fmt.Errorf("managed PR requires a current plan: %w", err)
	}
	if _, err := CheckApprovalReceipt(approvalPath, check); err != nil {
		return nil, nil, fmt.Errorf("managed PR requires current approval: %w", err)
	}
	tasksPath := filepath.Join(directory, "compiled", "tasks.json")
	if err := CheckApprovalLock(ApprovalOptions{
		SourcePlanPath: check.SourcePlanPath,
		SpecPath:       check.SpecPath,
		PlanPath:       planPath,
		TasksPath:      tasksPath,
		OutputPath:     lockPath,
	}); err != nil {
		return nil, nil, fmt.Errorf("managed PR requires a current build lock: %w", err)
	}
	evidencePath := filepath.Join(directory, "evidence.md")
	if !fileExists(evidencePath) {
		evidencePath = filepath.Join(directory, "compiled", "evidence.md")
	}
	if err := checkNonEmptyFile(evidencePath, "feature evidence"); err != nil {
		return nil, nil, err
	}
	evidence, err := os.ReadFile(evidencePath)
	if err != nil {
		return nil, nil, err
	}
	deliveryState, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return nil, nil, err
	}
	activeSlice, err := activeDeliverySlice(deliveryState)
	if err != nil {
		return nil, nil, err
	}
	explicitSlices := len(deliveryState.Slices) > 1 || deliveryState.Slices[0].ID != "delivery"
	gateStatus := map[string]string{
		"test":   deliveryEvidenceGateStatus(string(evidence), "Test", activeSlice.ID, explicitSlices),
		"review": deliveryEvidenceGateStatus(string(evidence), "Review", activeSlice.ID, explicitSlices),
	}
	for _, gate := range []string{"test", "review"} {
		status := gateStatus[gate]
		if status != "PASS" && status != "PASS_WITH_GAPS" {
			return nil, nil, fmt.Errorf("managed PR requires %s-gate evidence marked PASS or PASS_WITH_GAPS; found %q", gate, status)
		}
	}
	paths := []struct{ kind, path string }{
		{"source_plan", check.SourcePlanPath},
		{"feature_spec", check.SpecPath},
		{"plan", planPath},
		{"approval", approvalPath},
		{"plan_lock", lockPath},
		{"evidence", evidencePath},
	}
	for _, optional := range []struct{ kind, name string }{
		{"questions", "questions.md"}, {"gaps", "gaps.md"}, {"test_plan", "test-plan.md"},
	} {
		path := filepath.Join(directory, optional.name)
		if fileExists(path) {
			paths = append(paths, struct{ kind, path string }{optional.kind, path})
		}
	}
	sources := make([]PRSource, 0, len(paths))
	for _, item := range paths {
		source, err := relativeSource(repo, item.path, item.kind)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, gateStatus, nil
}

func PreparePRContext(options PRContextOptions) (PRContext, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return PRContext{}, err
	}
	if strings.TrimSpace(options.Feature) == "" {
		active, activeErr := ActiveManagedDeliveries(repo)
		if activeErr != nil {
			return PRContext{}, activeErr
		}
		if len(active) > 0 {
			return PRContext{}, fmt.Errorf("ad-hoc PR preparation is disabled while managed delivery is active: %s", strings.Join(active, ", "))
		}
	}
	head, err := gitCommand(repo, "branch", "--show-current")
	if err != nil || head == "" {
		return PRContext{}, fmt.Errorf("PR preparation requires a named branch")
	}
	configPath := filepath.Join(repo, ".product-loop", "project.json")
	config, _, err := LoadConfig(configPath)
	if err != nil {
		return PRContext{}, fmt.Errorf("PR preparation requires a valid Boatstack project configuration: %w", err)
	}
	base := strings.TrimSpace(options.Base)
	if base == "" {
		base = strings.TrimSpace(config.Project.DefaultBranch)
		if base == "" {
			base = defaultPRBase(repo)
		}
	}
	if head == base {
		return PRContext{}, fmt.Errorf("current branch %q is the configured base branch", head)
	}
	baseCommit, err := resolveBaseCommit(repo, base)
	if err != nil {
		return PRContext{}, err
	}
	mergeBaseCommit, err := gitCommand(repo, "merge-base", baseCommit, "HEAD")
	if err != nil || mergeBaseCommit == "" {
		return PRContext{}, fmt.Errorf("cannot determine the merge base between %s and %s", base, head)
	}
	headCommit, err := gitCommand(repo, "rev-parse", "HEAD")
	if err != nil {
		return PRContext{}, err
	}
	mode := "ad-hoc"
	if strings.TrimSpace(options.Feature) != "" {
		mode = "managed"
	}
	previewPath, err := expectedPRPreviewPath(mode, options.Feature, head)
	if err != nil {
		return PRContext{}, err
	}
	diff, changed, err := productDiff(repo, mergeBaseCommit, previewPath)
	if err != nil {
		return PRContext{}, err
	}
	if len(changed) == 0 {
		return PRContext{}, fmt.Errorf("branch has no committed product changes relative to %s", base)
	}
	diffStat, err := productDiffStat(repo, mergeBaseCommit)
	if err != nil {
		return PRContext{}, err
	}
	log, err := gitCommand(repo, "log", "--format=%h %s", mergeBaseCommit+"..HEAD")
	if err != nil {
		return PRContext{}, err
	}
	commits := []string{}
	if log != "" {
		commits = strings.Split(log, "\n")
	}
	configSource, err := relativeSource(repo, configPath, "project_config")
	if err != nil {
		return PRContext{}, err
	}
	sources := []PRSource{configSource}
	gateStatus := map[string]string{}
	sliceID := ""
	safety, err := CheckRepositorySafety(repo)
	if err != nil {
		return PRContext{}, fmt.Errorf("cannot establish operational safety evidence: %w", err)
	}
	if mode == "managed" && safety.Status != "PASS" {
		return PRContext{}, fmt.Errorf("managed PR is blocked by executable irreversible capability: %s", safety.Findings[0].Category)
	}
	if mode == "managed" {
		managedSources, statuses, sourceErr := managedPRSources(repo, options.Feature)
		if sourceErr != nil {
			return PRContext{}, sourceErr
		}
		sources = append(sources, managedSources...)
		gateStatus = statuses
		_, slice, gateSources, deliveryErr := CheckDeliveryReadyForShip(repo, options.Feature, base, head, SHA256Bytes(diff), changed)
		if deliveryErr != nil {
			return PRContext{}, deliveryErr
		}
		if options.SliceID != "" && options.SliceID != slice.ID {
			return PRContext{}, fmt.Errorf("delivery slice %s is not active; current slice is %s", options.SliceID, slice.ID)
		}
		sliceID = slice.ID
		sources = append(sources, gateSources...)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	fingerprintPayload, err := MarshalJSON(map[string]any{
		"schema_version":      prPreviewSchemaVersion,
		"mode":                mode,
		"feature":             options.Feature,
		"slice_id":            sliceID,
		"base_branch":         base,
		"head_branch":         head,
		"base_commit":         baseCommit,
		"merge_base_commit":   mergeBaseCommit,
		"product_diff_sha256": SHA256Bytes(diff),
		"gate_status":         gateStatus,
		"safety_status":       safety.Status,
		"safety_findings":     safety.Findings,
		"sources":             sources,
	})
	if err != nil {
		return PRContext{}, err
	}
	return PRContext{
		SchemaVersion: prPreviewSchemaVersion, Mode: mode, Feature: options.Feature, SliceID: sliceID,
		BaseBranch: base, HeadBranch: head, BaseCommit: baseCommit, MergeBaseCommit: mergeBaseCommit, HeadCommit: headCommit,
		ProductDiffSHA256: SHA256Bytes(diff), ContextFingerprint: SHA256Bytes(fingerprintPayload),
		ChangedFiles: changed, Commits: commits, DiffStat: diffStat,
		ContextPaths: config.Project.Context, ProjectCommands: config.Project.Commands,
		HighRiskFiles: highRiskChangedFiles(changed, config.Project.HighRiskPaths),
		GateStatus:    gateStatus, Sources: sources,
		SafetyStatus: safety.Status, SafetyFindings: safety.Findings,
		PreviewPath: previewPath,
	}, nil
}

func parsePRFrontmatter(value string) (map[string]string, string, error) {
	if !strings.HasPrefix(value, "---\n") {
		return nil, "", fmt.Errorf("PR preview must start with YAML frontmatter")
	}
	end := strings.Index(value[4:], "\n---\n")
	if end < 0 {
		return nil, "", fmt.Errorf("PR preview frontmatter is missing its closing delimiter")
	}
	frontmatter := value[4 : 4+end]
	body := strings.TrimSpace(value[4+end+len("\n---\n"):])
	fields := map[string]string{}
	allowed := map[string]bool{
		"boatstack_pr_version": true, "title": true, "mode": true, "feature": true,
		"slice": true, "base": true, "head": true, "context_fingerprint": true,
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		key, raw, found := strings.Cut(line, ":")
		if !found {
			return nil, "", fmt.Errorf("invalid PR frontmatter line: %s", line)
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if !allowed[key] {
			return nil, "", fmt.Errorf("unsupported PR frontmatter field: %s", key)
		}
		if _, exists := fields[key]; exists {
			return nil, "", fmt.Errorf("duplicate PR frontmatter field: %s", key)
		}
		if key == "boatstack_pr_version" {
			fields[key] = raw
			continue
		}
		var decoded string
		if err := DecodeJSON("parse PR frontmatter", "field "+key, []byte(raw), &decoded); err != nil {
			return nil, "", fmt.Errorf("%w; value must be a JSON-quoted string", err)
		}
		fields[key] = decoded
	}
	for _, key := range []string{"boatstack_pr_version", "title", "mode", "feature", "base", "head", "context_fingerprint"} {
		if _, exists := fields[key]; !exists {
			return nil, "", fmt.Errorf("PR frontmatter is missing %s", key)
		}
	}
	return fields, body, nil
}

func section(value, heading string) string {
	start := strings.Index(value, heading)
	if start < 0 {
		return ""
	}
	remainder := value[start+len(heading):]
	if next := strings.Index(remainder, "\n## "); next >= 0 {
		remainder = remainder[:next]
	}
	return strings.TrimSpace(remainder)
}

func validateEvidenceTable(body string, mode string) error {
	evidence := section(body, "## Evidence")
	if evidence == "" {
		return fmt.Errorf("PR body requires a non-empty Evidence section")
	}
	lines := strings.Split(evidence, "\n")
	rows := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.Contains(strings.ToLower(trimmed), "| claim ") || strings.Contains(trimmed, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) != 4 {
			return fmt.Errorf("Evidence rows require Claim, Evidence, Result, and Source columns")
		}
		status := strings.ToUpper(strings.Trim(strings.TrimSpace(cells[2]), "`"))
		if !prStatusPattern.MatchString(status) {
			return fmt.Errorf("unsupported evidence result %q", status)
		}
		if strings.TrimSpace(cells[0]) == "" || strings.TrimSpace(cells[1]) == "" || strings.TrimSpace(cells[3]) == "" {
			return fmt.Errorf("Evidence rows must include claim, evidence, and source")
		}
		if mode == "managed" && (status == "NOT_VERIFIED" || status == "BLOCKED") {
			return fmt.Errorf("managed PR evidence cannot contain %s results", status)
		}
		rows++
	}
	if rows == 0 {
		return fmt.Errorf("PR body requires at least one structured evidence row")
	}
	return nil
}

func validateManagedEvidenceSources(body string, sources []PRSource) error {
	evidencePaths := []string{}
	for _, source := range sources {
		if source.Kind == "evidence" {
			evidencePaths = append(evidencePaths, source.Path)
		}
	}
	if len(evidencePaths) == 0 {
		return fmt.Errorf("managed PR context has no current evidence source")
	}
	evidence := section(body, "## Evidence")
	for _, line := range strings.Split(evidence, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.Contains(strings.ToLower(trimmed), "| claim ") || strings.Contains(trimmed, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) != 4 {
			continue
		}
		sourceCell := strings.TrimSpace(cells[3])
		matched := false
		for _, path := range evidencePaths {
			if strings.Contains(sourceCell, path) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("managed PR evidence rows must link the current evidence ledger: %s", strings.Join(evidencePaths, " or "))
		}
	}
	return nil
}

func ParsePRPreview(path string) (PRPreview, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return PRPreview{}, err
	}
	fields, body, err := parsePRFrontmatter(string(value))
	if err != nil {
		return PRPreview{}, err
	}
	version, err := strconv.Atoi(fields["boatstack_pr_version"])
	if err != nil || version != prPreviewSchemaVersion {
		return PRPreview{}, fmt.Errorf("boatstack_pr_version must be %d", prPreviewSchemaVersion)
	}
	preview := PRPreview{
		SchemaVersion: version, Title: strings.TrimSpace(fields["title"]), Mode: fields["mode"],
		Feature: fields["feature"], SliceID: fields["slice"], BaseBranch: fields["base"], HeadBranch: fields["head"],
		ContextFingerprint: fields["context_fingerprint"], Body: body, Path: path,
		Fingerprint: SHA256Bytes(value),
	}
	if preview.Title == "" || strings.Contains(preview.Title, "\n") || len([]rune(preview.Title)) > 120 {
		return PRPreview{}, fmt.Errorf("PR title must be one non-empty line of at most 120 characters")
	}
	if preview.Mode != "managed" && preview.Mode != "ad-hoc" {
		return PRPreview{}, fmt.Errorf("PR mode must be managed or ad-hoc")
	}
	if preview.Mode == "managed" && !featureSlugPattern.MatchString(preview.Feature) {
		return PRPreview{}, fmt.Errorf("managed PR preview requires a lowercase kebab-case feature")
	}
	if preview.Mode == "managed" && !featureSlugPattern.MatchString(preview.SliceID) {
		return PRPreview{}, fmt.Errorf("managed PR preview requires a lowercase kebab-case delivery slice")
	}
	if preview.Mode == "ad-hoc" && preview.Feature != "" {
		return PRPreview{}, fmt.Errorf("ad-hoc PR preview must not claim a managed feature")
	}
	if preview.Mode == "ad-hoc" && preview.SliceID != "" {
		return PRPreview{}, fmt.Errorf("ad-hoc PR preview must not claim a managed delivery slice")
	}
	if strings.TrimSpace(preview.BaseBranch) == "" || strings.TrimSpace(preview.HeadBranch) == "" {
		return PRPreview{}, fmt.Errorf("PR preview requires base and head branches")
	}
	if len(preview.ContextFingerprint) != 64 {
		return PRPreview{}, fmt.Errorf("PR preview requires a valid context fingerprint")
	}
	for _, heading := range []string{
		"## Why this change", "## What changed", "## Review order", "## Evidence",
		"## Operational safety", "## Known gaps and risks", "## Rollout and rollback",
	} {
		if section(body, heading) == "" {
			return PRPreview{}, fmt.Errorf("PR body requires a non-empty %s section", strings.TrimPrefix(heading, "## "))
		}
	}
	if !strings.Contains(body, "<details>") || !strings.Contains(body, "<summary>Boatstack provenance</summary>") || !strings.Contains(body, "</details>") {
		return PRPreview{}, fmt.Errorf("PR body requires collapsed Boatstack provenance")
	}
	if err := validateEvidenceTable(body, preview.Mode); err != nil {
		return PRPreview{}, err
	}
	return preview, nil
}

func CheckPRPreview(repoPath, previewPath string) (PRPreview, PRContext, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return PRPreview{}, PRContext{}, err
	}
	if !filepath.IsAbs(previewPath) {
		previewPath = filepath.Join(repo, filepath.FromSlash(previewPath))
	}
	if resolved, resolveErr := filepath.EvalSymlinks(repo); resolveErr == nil {
		repo = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(previewPath); resolveErr == nil {
		previewPath = resolved
	}
	if err := rejectSymlinkComponents(repo, previewPath); err != nil {
		return PRPreview{}, PRContext{}, err
	}
	preview, err := ParsePRPreview(previewPath)
	if err != nil {
		return PRPreview{}, PRContext{}, err
	}
	context, err := PreparePRContext(PRContextOptions{Repo: repo, Feature: preview.Feature, SliceID: preview.SliceID, Base: preview.BaseBranch})
	if err != nil {
		return PRPreview{}, PRContext{}, err
	}
	expectedPath, err := resolveRepositoryRelativePath(repo, context.PreviewPath)
	if err != nil {
		return PRPreview{}, PRContext{}, err
	}
	actualPath, err := filepath.Abs(previewPath)
	if err != nil {
		return PRPreview{}, PRContext{}, err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(expectedPath); resolveErr == nil {
		expectedPath = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(actualPath); resolveErr == nil {
		actualPath = resolved
	}
	if filepath.Clean(expectedPath) != filepath.Clean(actualPath) {
		return PRPreview{}, PRContext{}, fmt.Errorf("PR preview must be stored at %s", context.PreviewPath)
	}
	if preview.Mode != context.Mode || preview.SliceID != context.SliceID || preview.BaseBranch != context.BaseBranch || preview.HeadBranch != context.HeadBranch || preview.ContextFingerprint != context.ContextFingerprint {
		return PRPreview{}, PRContext{}, fmt.Errorf("PR preview is stale or does not match the current branch context; regenerate it")
	}
	if context.Mode == "managed" {
		if err := validateManagedEvidenceSources(preview.Body, context.Sources); err != nil {
			return PRPreview{}, PRContext{}, err
		}
	}
	return preview, context, nil
}

func ghAvailable(repo string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("GitHub CLI is unavailable; the validated preview remains available for manual publication")
	}
	if _, err := commandOutput(repo, "gh", "auth", "status", "-h", "github.com"); err != nil {
		return fmt.Errorf("GitHub CLI is not authenticated; the validated preview remains available for manual publication")
	}
	return nil
}

func existingPRURL(repo string) (string, bool, error) {
	if err := ghAvailable(repo); err != nil {
		return "", false, err
	}
	value, err := commandOutput(repo, "gh", "pr", "view", "--json", "url", "--jq", ".url")
	if err != nil {
		message := strings.ToLower(err.Error())
		for _, expected := range []string{
			"no pull requests found", "no open pull requests", "could not resolve to a pullrequest",
		} {
			if strings.Contains(message, expected) {
				return "", false, nil
			}
		}
		return "", false, fmt.Errorf("cannot determine whether this branch already has a PR: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(value), true, nil
}

func RecommendedPRAction(repo string) (string, string, error) {
	repository, err := ResolveRepository(repo)
	if err != nil {
		return "", "", err
	}
	url, exists, err := existingPRURL(repository)
	if err != nil {
		return "manual", "", err
	}
	if exists {
		return "update", url, nil
	}
	return "open", "", nil
}

func PublishPR(options PRPublishOptions) (string, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return "", err
	}
	preview, context, err := CheckPRPreview(repo, options.PreviewPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(options.ExpectedFingerprint) == "" || options.ExpectedFingerprint != preview.Fingerprint {
		return "", fmt.Errorf("publication fingerprint does not match the exact preview confirmed by the human")
	}
	if options.Action != "open" && options.Action != "update" {
		return "", fmt.Errorf("publication action must be open or update")
	}
	dirty, err := dirtyPaths(repo)
	if err != nil {
		return "", err
	}
	if len(dirty) > 0 {
		return "", fmt.Errorf("commit the exact reviewed pr.md before publication; working tree is not clean")
	}
	if err := ghAvailable(repo); err != nil {
		return "", err
	}
	if _, err := gitCommand(repo, "remote", "get-url", "origin"); err != nil {
		return "", fmt.Errorf("GitHub publication requires an origin remote")
	}
	existingURL, exists, err := existingPRURL(repo)
	if err != nil {
		return "", err
	}
	if options.Action == "open" && exists {
		return "", fmt.Errorf("a PR already exists for %s; regenerate the preview for update", context.HeadBranch)
	}
	if options.Action == "update" && !exists {
		return "", fmt.Errorf("no PR exists for %s; regenerate the preview for opening", context.HeadBranch)
	}
	if _, err := gitCommand(repo, "push", "--set-upstream", "origin", context.HeadBranch); err != nil {
		return "", fmt.Errorf("cannot push %s without rewriting history: %w", context.HeadBranch, err)
	}
	temporary, err := os.CreateTemp("", "boatstack-pr-body-*.md")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(preview.Body + "\n"); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if options.Action == "open" {
		url, err := commandOutput(repo, "gh", "pr", "create", "--base", context.BaseBranch, "--head", context.HeadBranch, "--title", preview.Title, "--body-file", temporaryPath)
		if err != nil {
			return "", err
		}
		if context.Mode == "managed" {
			if err := MarkDeliveryPublished(repo, context.Feature, context.SliceID, strings.TrimSpace(url)); err != nil {
				return "", fmt.Errorf("PR opened but delivery state could not advance: %w", err)
			}
		}
		return strings.TrimSpace(url), nil
	}
	if _, err := commandOutput(repo, "gh", "pr", "edit", existingURL, "--title", preview.Title, "--body-file", temporaryPath); err != nil {
		return "", err
	}
	if context.Mode == "managed" {
		if err := MarkDeliveryPublished(repo, context.Feature, context.SliceID, existingURL); err != nil {
			return "", fmt.Errorf("PR updated but delivery state could not advance: %w", err)
		}
	}
	return existingURL, nil
}

func PRPreviewTemplate(context PRContext) string {
	quote := func(value string) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	safetySummary := "Repository safety scan: `" + context.SafetyStatus + "`. Destructive recovery remains operator-only outside Boatstack."
	return strings.Join([]string{
		"---",
		"boatstack_pr_version: 2",
		"title: " + quote("Describe the reviewer-visible outcome"),
		"mode: " + quote(context.Mode),
		"feature: " + quote(context.Feature),
		"slice: " + quote(context.SliceID),
		"base: " + quote(context.BaseBranch),
		"head: " + quote(context.HeadBranch),
		"context_fingerprint: " + quote(context.ContextFingerprint),
		"---",
		"## Why this change", "", "Explain the user or engineering outcome.", "",
		"## What changed", "", "| Area | Before | After | Reviewer focus |", "|---|---|---|---|", "| | | | |", "",
		"## Review order", "", "1. Start with the contract or boundary that defines the behavior.", "",
		"## Evidence", "", "| Claim | Evidence | Result | Source |", "|---|---|---|---|", "| | | `NOT_VERIFIED` | |", "",
		"## Operational safety", "", safetySummary, "",
		"## Known gaps and risks", "", "List explicit gaps or say that no material gaps are known.", "",
		"## Rollout and rollback", "", "Describe deployment impact and the smallest safe rollback.", "",
		"<details>", "<summary>Boatstack provenance</summary>", "", "Summarize mode, approval/evidence availability, and coding-host attribution here.", "", "</details>", "",
	}, "\n")
}

func PRContextJSON(context PRContext) ([]byte, error) {
	return MarshalJSON(context)
}

func PRBody(preview PRPreview) []byte {
	return bytes.TrimSpace([]byte(preview.Body))
}
