package boatstack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const prPreviewSchemaVersion = 3

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
	SchemaVersion               int                       `json:"schema_version"`
	Mode                        string                    `json:"mode"`
	Feature                     string                    `json:"feature,omitempty"`
	SliceID                     string                    `json:"slice_id,omitempty"`
	SliceIndex                  int                       `json:"slice_index,omitempty"`
	TotalSlices                 int                       `json:"total_slices,omitempty"`
	BaseBranch                  string                    `json:"base_branch"`
	HeadBranch                  string                    `json:"head_branch"`
	BaseCommit                  string                    `json:"base_commit"`
	MergeBaseCommit             string                    `json:"merge_base_commit"`
	HeadCommit                  string                    `json:"head_commit"`
	ProductDiffSHA256           string                    `json:"product_diff_sha256"`
	ContextFingerprint          string                    `json:"context_fingerprint"`
	ChangedFiles                []string                  `json:"changed_files"`
	Commits                     []string                  `json:"commits"`
	DiffStat                    string                    `json:"diff_stat"`
	ContextPaths                []string                  `json:"context_paths,omitempty"`
	ProjectCommands             map[string]string         `json:"project_commands,omitempty"`
	HighRiskFiles               []string                  `json:"high_risk_files,omitempty"`
	GateStatus                  map[string]string         `json:"gate_status,omitempty"`
	SafetyStatus                string                    `json:"safety_status"`
	SafetyFindings              []SafetyFinding           `json:"safety_findings,omitempty"`
	PRVisualEvidencePolicy      string                    `json:"pr_visual_evidence_policy"`
	PRVisualEvidenceStatus      string                    `json:"pr_visual_evidence_status"`
	PRVisualEvidenceCount       int                       `json:"pr_visual_evidence_count"`
	PRVisualEvidenceFingerprint string                    `json:"pr_visual_evidence_fingerprint"`
	PRVisualEvidenceRelevance   string                    `json:"pr_visual_evidence_relevance"`
	PRVisualEvidenceSource      string                    `json:"pr_visual_evidence_source"`
	PRVisualEvidence            *PRVisualEvidenceManifest `json:"pr_visual_evidence,omitempty"`
	Sources                     []PRSource                `json:"sources,omitempty"`
	PreviewPath                 string                    `json:"preview_path"`
}

type PRPreview struct {
	SchemaVersion               int
	Title                       string
	Mode                        string
	Feature                     string
	SliceID                     string
	BaseBranch                  string
	HeadBranch                  string
	ContextFingerprint          string
	PRVisualEvidencePolicy      string
	PRVisualEvidenceStatus      string
	PRVisualEvidenceCount       int
	PRVisualEvidenceFingerprint string
	Body                        string
	Path                        string
	Fingerprint                 string
}

func planVisualDecision(repo, feature string) (string, string, []PRVisualScenario, error) {
	plan, err := LoadPlan(filepath.Join(repo, ".product-loop", "features", feature, "plan.md"))
	if err != nil {
		return "unresolved", "managed-plan", nil, err
	}
	value, ok := plan["pr_visual_evidence"].(map[string]any)
	if !ok {
		return "unresolved", "managed-plan", nil, nil
	}
	relevance := stringValue(value["relevance"])
	if relevance == "not_relevant" {
		return relevance, "managed-plan", nil, nil
	}
	rows, _ := objectSlice(value["scenarios"])
	scenarios := make([]PRVisualScenario, 0, len(rows))
	for _, row := range rows {
		expected, _ := stringSlice(row["expected"])
		scenarios = append(scenarios, PRVisualScenario{
			ID: stringValue(row["id"]), Entry: stringValue(row["entry"]), State: stringValue(row["state"]),
			Viewport: stringValue(row["viewport"]), Expected: expected,
		})
	}
	return relevance, "managed-plan", scenarios, nil
}

func resolvePRVisualEvidence(repo string, config ProjectConfig, mode, feature, head, headCommit, diffHash string) (string, string, int, string, string, string, *PRVisualEvidenceManifest, error) {
	policy := normalizedPRVisualEvidencePolicy(config.Workflow.PRVisualEvidence)
	relevance, source := "unresolved", "agent-proposed"
	var scenarios []PRVisualScenario
	if mode == "managed" {
		var err error
		relevance, source, scenarios, err = planVisualDecision(repo, feature)
		if err != nil {
			return "", "", 0, "", "", "", nil, err
		}
	}
	key, err := visualEvidenceKey(mode, feature, head)
	if err != nil {
		return "", "", 0, "", "", "", nil, err
	}
	status := "NOT_APPLICABLE"
	var manifest *PRVisualEvidenceManifest
	if policy != "off" && relevance != "not_relevant" {
		loaded, loadErr := LoadPRVisualEvidence(repo, key)
		if loadErr == nil {
			manifest = &loaded
			relevance, source, scenarios = loaded.Relevance, loaded.RelevanceSource, loaded.Scenarios
			if loaded.SourceCommit == headCommit && loaded.ProductDiffSHA256 == diffHash {
				status = loaded.Status
			} else {
				status = "NOT_VERIFIED"
				manifest = nil
			}
		} else {
			status = "NOT_VERIFIED"
		}
		if policy == "require" && status != "PASS" {
			status = "BLOCKED"
		}
	}
	payload := map[string]any{
		"schema_version": visualEvidenceSchemaVersion, "policy": policy, "status": status,
		"relevance": relevance, "relevance_source": source, "scenarios": scenarios,
	}
	count := 0
	if manifest != nil {
		payload["manifest_fingerprint"] = manifest.Fingerprint
		payload["items"] = manifest.Items
		count = len(manifest.Items)
	}
	raw, err := MarshalJSON(payload)
	if err != nil {
		return "", "", 0, "", "", "", nil, err
	}
	return policy, status, count, SHA256Bytes(raw), relevance, source, manifest, nil
}

type PRPublishOptions struct {
	Repo                string
	PreviewPath         string
	ExpectedFingerprint string
	Action              string
	VisualPublisher     PRVisualEvidencePublisher
}

// PRVisualEvidencePublisher is implemented by a host that can upload exact
// machine-local PNG bytes to one Boatstack-owned pull-request comment. ExistingCommentURL
// is empty on first publication and lets later updates reuse the same comment.
type PRVisualEvidencePublisher interface {
	PublishVisualEvidence(repo, prURL, existingCommentURL string, manifest PRVisualEvidenceManifest) (commentURL string, err error)
}

func publishPRVisualEvidence(repo, prURL string, context PRContext, publisher PRVisualEvidencePublisher) error {
	if context.PRVisualEvidenceStatus == "NOT_APPLICABLE" || context.PRVisualEvidence == nil {
		return nil
	}
	manifest, err := LoadPRVisualEvidence(repo, context.PRVisualEvidence.Key)
	if err != nil || manifest.Fingerprint != context.PRVisualEvidence.Fingerprint {
		return fmt.Errorf("PR opened but visual evidence became stale; preserve the PR and recapture before updating it")
	}
	if manifest.Publication.State == "published" && manifest.Publication.PRURL == prURL && strings.TrimSpace(manifest.Publication.CommentURL) != "" {
		return nil
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if publisher == nil {
		_, recordErr := recordPRVisualPublication(repo, manifest, PRVisualPublication{
			State: "manual_required", PRURL: prURL, UpdatedAt: now,
			Detail: "attach the fingerprinted local PNG files to the Boatstack visual-evidence comment",
		})
		if recordErr != nil {
			return fmt.Errorf("PR opened but manual visual-evidence fallback could not be recorded: %w", recordErr)
		}
		if context.PRVisualEvidencePolicy == "require" {
			return fmt.Errorf("PR opened at %s but required visual evidence still needs manual attachment; update the same PR after attachment", prURL)
		}
		return nil
	}
	commentURL, publishErr := publisher.PublishVisualEvidence(repo, prURL, manifest.Publication.CommentURL, manifest)
	if publishErr != nil {
		_, _ = recordPRVisualPublication(repo, manifest, PRVisualPublication{
			State: "visual_pending", PRURL: prURL, CommentURL: manifest.Publication.CommentURL,
			UpdatedAt: now, Detail: publishErr.Error(),
		})
		return fmt.Errorf("PR opened at %s but visual evidence publication failed; preserve the PR and fix forward: %w", prURL, publishErr)
	}
	if strings.TrimSpace(commentURL) == "" {
		return fmt.Errorf("visual evidence publisher returned no observable comment URL")
	}
	_, err = recordPRVisualPublication(repo, manifest, PRVisualPublication{
		State: "published", PRURL: prURL, CommentURL: strings.TrimSpace(commentURL), UpdatedAt: now,
	})
	return err
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
	config, _, configErr := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if configErr != nil {
		return nil, nil, fmt.Errorf("managed PR requires a valid Boatstack project configuration: %w", configErr)
	}
	authorizationMode := "policy"
	if config.Workflow.HumanPlanApproval {
		authorizationMode = "human"
		if _, err := CheckApprovalReceipt(approvalPath, check); err != nil {
			return nil, nil, fmt.Errorf("managed PR requires current approval: %w", err)
		}
	}
	tasksPath := filepath.Join(directory, "compiled", "tasks.json")
	if err := CheckApprovalLock(ApprovalOptions{
		SourcePlanPath:    check.SourcePlanPath,
		SpecPath:          check.SpecPath,
		PlanPath:          planPath,
		TasksPath:         tasksPath,
		AuthorizationMode: authorizationMode,
		OutputPath:        lockPath,
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
		if status == "PASS_WITH_GAPS" && !config.Workflow.AllowPassWithGaps {
			return nil, nil, fmt.Errorf("managed PR %s gate violates workflow.allow_pass_with_gaps=false", gate)
		}
	}
	paths := []struct{ kind, path string }{
		{"source_plan", check.SourcePlanPath},
		{"feature_spec", check.SpecPath},
		{"plan", planPath},
		{"plan_lock", lockPath},
		{"evidence", evidencePath},
	}
	if config.Workflow.HumanPlanApproval {
		paths = append(paths, struct{ kind, path string }{"approval", approvalPath})
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
	changelogBase, err := changelogComparisonBase(repo, options.Feature, mergeBaseCommit)
	if err != nil {
		return PRContext{}, err
	}
	if err := validateChangelogChange(repo, changelogBase, config); err != nil {
		return PRContext{}, err
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
	sliceIndex := 0
	totalSlices := 0
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
		state, slice, gateSources, deliveryErr := CheckDeliveryReadyForShip(repo, options.Feature, base, head, SHA256Bytes(diff), changed)
		if deliveryErr != nil {
			return PRContext{}, deliveryErr
		}
		if options.SliceID != "" && options.SliceID != slice.ID {
			return PRContext{}, fmt.Errorf("delivery slice %s is not active; current slice is %s", options.SliceID, slice.ID)
		}
		sliceID = slice.ID
		sliceIndex = state.ActiveIndex + 1
		totalSlices = len(state.Slices)
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
	visualPolicy, visualStatus, visualCount, visualFingerprint, visualRelevance, visualSource, visualManifest, err := resolvePRVisualEvidence(
		repo, config, mode, options.Feature, head, headCommit, SHA256Bytes(diff),
	)
	if err != nil {
		return PRContext{}, err
	}
	fingerprintPayload, err = MarshalJSON(map[string]any{
		"base":                      json.RawMessage(fingerprintPayload),
		"pr_visual_evidence_policy": visualPolicy, "pr_visual_evidence_status": visualStatus,
		"pr_visual_evidence_count": visualCount, "pr_visual_evidence_fingerprint": visualFingerprint,
	})
	if err != nil {
		return PRContext{}, err
	}
	return PRContext{
		SchemaVersion: prPreviewSchemaVersion, Mode: mode, Feature: options.Feature, SliceID: sliceID,
		SliceIndex: sliceIndex, TotalSlices: totalSlices,
		BaseBranch: base, HeadBranch: head, BaseCommit: baseCommit, MergeBaseCommit: mergeBaseCommit, HeadCommit: headCommit,
		ProductDiffSHA256: SHA256Bytes(diff), ContextFingerprint: SHA256Bytes(fingerprintPayload),
		ChangedFiles: changed, Commits: commits, DiffStat: diffStat,
		ContextPaths: config.Project.Context, ProjectCommands: config.Project.Commands,
		HighRiskFiles: highRiskChangedFiles(changed, config.Project.HighRiskPaths),
		GateStatus:    gateStatus, Sources: sources,
		SafetyStatus: safety.Status, SafetyFindings: safety.Findings,
		PRVisualEvidencePolicy: visualPolicy, PRVisualEvidenceStatus: visualStatus,
		PRVisualEvidenceCount: visualCount, PRVisualEvidenceFingerprint: visualFingerprint,
		PRVisualEvidenceRelevance: visualRelevance, PRVisualEvidenceSource: visualSource,
		PRVisualEvidence: visualManifest,
		PreviewPath:      previewPath,
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
		"pr_visual_evidence_policy": true, "pr_visual_evidence_status": true,
		"pr_visual_evidence_count": true, "pr_visual_evidence_fingerprint": true,
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
		if key == "boatstack_pr_version" || key == "pr_visual_evidence_count" {
			fields[key] = raw
			continue
		}
		var decoded string
		if err := DecodeJSON("parse PR frontmatter", "field "+key, []byte(raw), &decoded); err != nil {
			return nil, "", fmt.Errorf("%w; value must be a JSON-quoted string", err)
		}
		fields[key] = decoded
	}
	for _, key := range []string{"boatstack_pr_version", "title", "mode", "feature", "base", "head", "context_fingerprint", "pr_visual_evidence_policy", "pr_visual_evidence_status", "pr_visual_evidence_count", "pr_visual_evidence_fingerprint"} {
		if _, exists := fields[key]; !exists {
			return nil, "", fmt.Errorf("PR frontmatter is missing %s", key)
		}
	}
	return fields, body, nil
}

func validateVisualEvidenceSection(body, status string, count int) error {
	if status == "NOT_APPLICABLE" {
		return nil
	}
	visual := section(body, "## Visual evidence")
	if visual == "" {
		return fmt.Errorf("PR body requires a non-empty Visual evidence section")
	}
	rows := 0
	for _, line := range strings.Split(visual, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.Contains(strings.ToLower(trimmed), "| scenario ") || strings.Contains(trimmed, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) != 5 {
			return fmt.Errorf("Visual evidence rows require Scenario, Viewport, Commit, Result, and Publication columns")
		}
		rows++
	}
	if rows != count && status == "PASS" {
		return fmt.Errorf("Visual evidence row count %d does not match pr_visual_evidence_count %d", rows, count)
	}
	if rows == 0 {
		return fmt.Errorf("Visual evidence requires at least one structured row")
	}
	return nil
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
		PRVisualEvidencePolicy: fields["pr_visual_evidence_policy"], PRVisualEvidenceStatus: fields["pr_visual_evidence_status"],
		PRVisualEvidenceFingerprint: fields["pr_visual_evidence_fingerprint"],
		Fingerprint:                 SHA256Bytes(value),
	}
	preview.PRVisualEvidenceCount, err = strconv.Atoi(fields["pr_visual_evidence_count"])
	if err != nil || preview.PRVisualEvidenceCount < 0 || preview.PRVisualEvidenceCount > 3 {
		return PRPreview{}, fmt.Errorf("pr_visual_evidence_count must be between 0 and 3")
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
	if preview.PRVisualEvidencePolicy != "off" && preview.PRVisualEvidencePolicy != "suggest" && preview.PRVisualEvidencePolicy != "require" {
		return PRPreview{}, fmt.Errorf("unsupported pr_visual_evidence_policy")
	}
	if !map[string]bool{"PASS": true, "PASS_WITH_GAPS": true, "NOT_VERIFIED": true, "NOT_APPLICABLE": true, "BLOCKED": true}[preview.PRVisualEvidenceStatus] {
		return PRPreview{}, fmt.Errorf("unsupported pr_visual_evidence_status")
	}
	if len(preview.PRVisualEvidenceFingerprint) != 64 {
		return PRPreview{}, fmt.Errorf("PR preview requires a valid pr_visual_evidence_fingerprint")
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
	if err := validateVisualEvidenceSection(body, preview.PRVisualEvidenceStatus, preview.PRVisualEvidenceCount); err != nil {
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
	if preview.Mode != context.Mode || preview.SliceID != context.SliceID || preview.BaseBranch != context.BaseBranch || preview.HeadBranch != context.HeadBranch || preview.ContextFingerprint != context.ContextFingerprint ||
		preview.PRVisualEvidencePolicy != context.PRVisualEvidencePolicy || preview.PRVisualEvidenceStatus != context.PRVisualEvidenceStatus ||
		preview.PRVisualEvidenceCount != context.PRVisualEvidenceCount || preview.PRVisualEvidenceFingerprint != context.PRVisualEvidenceFingerprint {
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
	if context.PRVisualEvidencePolicy == "require" && context.PRVisualEvidenceStatus != "PASS" {
		return "", fmt.Errorf("PR publication is blocked until required visual evidence is current")
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
	operationTarget := "github-pr:" + options.Action + ":" + context.HeadBranch
	operationFingerprint := preview.Fingerprint
	operationID := operationID("publish-pr", operationTarget, operationFingerprint)
	prior, priorErr := loadOperation(repo, operationID)
	partialResume := priorErr == nil && (prior.State == OperationReconcileRequired || prior.State == OperationRetryable)
	if options.Action == "open" && exists && !partialResume {
		return "", fmt.Errorf("a PR already exists for %s; regenerate the preview for update", context.HeadBranch)
	}
	if options.Action == "update" && !exists {
		return "", fmt.Errorf("no PR exists for %s; regenerate the preview for opening", context.HeadBranch)
	}
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: "publish-pr",
		Scope:  OperationScope{Feature: context.Feature, Slice: context.SliceID, Worktree: filepath.Base(repo), HeadBranch: context.HeadBranch},
		Target: operationTarget, PackageFingerprint: operationFingerprint, AuthorizationFingerprint: options.ExpectedFingerprint,
		RetryClass: "RECONCILE_FIRST", MaxAttempts: 3,
		ExpectedPostcondition: "origin contains the exact head commit and one pull request reflects the fingerprinted title, body, and visual-evidence package",
	})
	if err != nil {
		return "", err
	}
	if receipt.State == OperationSucceeded {
		if receipt.Observation.Evidence != "" {
			return receipt.Observation.Evidence, nil
		}
		if exists {
			return existingURL, nil
		}
		return "", fmt.Errorf("published operation is terminal but its PR URL is unavailable")
	}
	if receipt.State == OperationReconcileRequired {
		result := "OBSERVED_ABSENT"
		detail := "no pull request exists for the exact head branch"
		evidence := context.HeadBranch
		if exists {
			result = "OBSERVED_PARTIAL"
			detail = "the pull request exists; resume remaining idempotent publication steps"
			evidence = existingURL
		}
		receipt, err = RecordOperationReconciliation(repo, receipt.OperationID, result, detail, evidence)
		if err != nil {
			return "", err
		}
	}
	attemptKey := SHA256Bytes([]byte("publish-pr\x00" + options.Action + "\x00" + preview.Fingerprint))
	begin, err := BeginOperation(repo, receipt.OperationID, attemptKey, "boatstack-helper publish-pr")
	if err != nil {
		if errors.Is(err, ErrOperationInFlight) {
			return "", fmt.Errorf("the identical PR publication is already executing; inspect operation-status instead of repeating it")
		}
		return "", err
	}
	if begin.Receipt.State == OperationSucceeded {
		return begin.Receipt.Observation.Evidence, nil
	}
	completeUnknown := func(cause error, observedURL string) (string, error) {
		_, _ = CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "UNKNOWN", "publication ended without a verifiable complete postcondition", observedURL)
		return "", cause
	}
	if _, err := gitCommand(repo, "push", "--set-upstream", "origin", context.HeadBranch); err != nil {
		return completeUnknown(fmt.Errorf("cannot push %s without rewriting history: %w", context.HeadBranch, err), existingURL)
	}
	temporary, err := os.CreateTemp("", "boatstack-pr-body-*.md")
	if err != nil {
		return completeUnknown(err, existingURL)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(preview.Body + "\n"); err != nil {
		temporary.Close()
		return completeUnknown(err, existingURL)
	}
	if err := temporary.Close(); err != nil {
		return completeUnknown(err, existingURL)
	}
	if options.Action == "open" {
		url := existingURL
		if !exists {
			url, err = commandOutput(repo, "gh", "pr", "create", "--base", context.BaseBranch, "--head", context.HeadBranch, "--title", preview.Title, "--body-file", temporaryPath)
			if err != nil {
				return completeUnknown(err, "")
			}
		}
		url = strings.TrimSpace(url)
		if err := publishPRVisualEvidence(repo, url, context, options.VisualPublisher); err != nil {
			return completeUnknown(err, url)
		}
		if context.Mode == "managed" {
			if err := MarkDeliveryPublished(repo, context.Feature, context.SliceID, url); err != nil {
				return completeUnknown(fmt.Errorf("PR opened but delivery state could not advance: %w", err), url)
			}
			if err := extractSystemicBoundaries(repo, context.Feature); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: could not extract systemic boundaries: %v\n", err)
			}
		}
		if _, err := CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "SUCCEEDED", "pull request publication postcondition observed", url); err != nil {
			return "", err
		}
		return url, nil
	}
	if _, err := commandOutput(repo, "gh", "pr", "edit", existingURL, "--title", preview.Title, "--body-file", temporaryPath); err != nil {
		return completeUnknown(err, existingURL)
	}
	if err := publishPRVisualEvidence(repo, existingURL, context, options.VisualPublisher); err != nil {
		return completeUnknown(err, existingURL)
	}
	if context.Mode == "managed" {
		if err := MarkDeliveryPublished(repo, context.Feature, context.SliceID, existingURL); err != nil {
			return completeUnknown(fmt.Errorf("PR updated but delivery state could not advance: %w", err), existingURL)
		}
		if err := extractSystemicBoundaries(repo, context.Feature); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: could not extract systemic boundaries: %v\n", err)
		}
	}
	if _, err := CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "SUCCEEDED", "pull request update postcondition observed", existingURL); err != nil {
		return "", err
	}
	return existingURL, nil
}

func extractSystemicBoundaries(repo, feature string) error {
	lockPath := filepath.Join(repo, ".product-loop", "features", feature, "plan.lock.json")
	value, err := os.ReadFile(lockPath)
	if err != nil {
		return nil // if it doesn't exist, ignore
	}
	var lock map[string]any
	if err := json.Unmarshal(value, &lock); err != nil {
		return err
	}
	boundaries, ok := lock["systemic_boundaries"].([]any)
	if !ok || len(boundaries) == 0 {
		return nil
	}
	outPath := filepath.Join(repo, ".product-loop", "verified-boundaries.md")
	f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, b := range boundaries {
		boundary, _ := b.(map[string]any)
		id := stringValue(boundary["id"])
		failureMode := stringValue(boundary["failure_mode"])
		enforcement := stringValue(boundary["enforcement_mechanism"])
		if id != "" && failureMode != "" && enforcement != "" {
			fmt.Fprintf(f, "- **%s**: Prevents `%s` using `%s`\n", id, failureMode, enforcement)
		}
	}
	return nil
}

func PRPreviewTemplate(context PRContext) string {
	quote := func(value string) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	safetySummary := "Repository safety scan: `" + context.SafetyStatus + "`. Destructive recovery remains operator-only outside Boatstack."
	lines := []string{
		"---",
		"boatstack_pr_version: 3",
		"title: " + quote("Describe the product or user value of this change (e.g., 'Enable historical data migration')"),
		"mode: " + quote(context.Mode),
		"feature: " + quote(context.Feature),
		"slice: " + quote(context.SliceID),
		"base: " + quote(context.BaseBranch),
		"head: " + quote(context.HeadBranch),
		"context_fingerprint: " + quote(context.ContextFingerprint),
		"pr_visual_evidence_policy: " + quote(context.PRVisualEvidencePolicy),
		"pr_visual_evidence_status: " + quote(context.PRVisualEvidenceStatus),
		fmt.Sprintf("pr_visual_evidence_count: %d", context.PRVisualEvidenceCount),
		"pr_visual_evidence_fingerprint: " + quote(context.PRVisualEvidenceFingerprint),
		"---",
		"## Why this change", "", "Explain the user or engineering outcome.", "",
		"## What changed", "", "| Area | Before | After | Reviewer focus |", "|---|---|---|---|", "| | | | |", "",
		"## Review order", "", "1. Start with the contract or boundary that defines the behavior.", "",
		"## Evidence", "", "| Claim | Evidence | Result | Source |", "|---|---|---|---|", "| | | `NOT_VERIFIED` | |", "",
		"## Operational safety", "", safetySummary, "",
		"## Known gaps and risks", "", "List explicit gaps or say that no material gaps are known.", "",
		"## Rollout and rollback", "", "Describe deployment impact and the smallest safe rollback.", "",
	}
	if context.PRVisualEvidenceStatus != "NOT_APPLICABLE" {
		lines = append(lines,
			"## Visual evidence", "",
			"Screenshots are human-review evidence, not mechanical proof. Public-repository attachments are publicly accessible.", "",
			"| Scenario | Viewport | Commit | Result | Publication |", "|---|---|---|---|---|",
			"| Describe the approved state | viewport | "+context.HeadCommit+" | `"+context.PRVisualEvidenceStatus+"` | Boatstack evidence comment or manual fallback |", "",
		)
	}
	if context.TotalSlices > 1 {
		lines = append(lines, fmt.Sprintf("> *(This is PR %d of %d in the `%s` feature)*", context.SliceIndex, context.TotalSlices, context.Feature), "")
	}
	lines = append(lines, "<details>", "<summary>Boatstack provenance</summary>", "", "Summarize mode, approval/evidence availability, and coding-host attribution here.", "", "</details>", "")
	return strings.Join(lines, "\n")
}

func PRContextJSON(context PRContext) ([]byte, error) {
	return MarshalJSON(context)
}

func PRBody(preview PRPreview) []byte {
	return bytes.TrimSpace([]byte(preview.Body))
}
