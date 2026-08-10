package boatstack

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

var featureSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var planningArtifacts = map[string]bool{
	"source-plan.md":  true,
	"feature-spec.md": true,
	"questions.md":    true,
	"gaps.md":         true,
	"test-plan.md":    true,
	"plan.md":         true,
}

// planningArtifactNames returns the accepted artifact tokens, sorted, for error
// messages. Discoverability: a rejection that gates on a closed domain must name
// the accepted set at the point of failure. The tokens are filenames carrying a
// .md suffix, so the natural guess ("plan") is always wrong — listing them (and the
// suffix) is what turns a dead-end rejection into an actionable one.
func planningArtifactNames() []string {
	names := make([]string, 0, len(planningArtifacts))
	for name := range planningArtifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type PlanningWriteOptions struct {
	Repo                    string
	Feature                 string
	Artifact                string
	Content                 []byte
	SourcePlan              string
	SourcePlanSHA256        string
	ExpectedLifecycleSHA256 string
	ExpectedPlanLockSHA256  string
	ExpectedObservation     string
}

type ApprovalRecordOptions struct {
	Repo                    string
	PlanPath                string
	OutputPath              string
	ApprovedBy              string
	ApprovedAt              string
	Fingerprint             string
	BaselineDiffSHA256      string
	ExpectedLifecycleSHA256 string
	ExpectedPlanLockSHA256  string
	ExpectedObservation     string
}

type PlanningBaseline struct {
	DiffSHA256   string
	ChangedPaths []string
}

// Tests may replace this seam. Nil selects the production pure installation
// health check immediately before the first feature artifact can be written.
var planningInstallationHealth func(string) error

func relativeBaselineExclusions(repo string, paths ...string) map[string]bool {
	excluded := map[string]bool{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(repo, absolute)
		}
		if canonicalParent, err := filepath.EvalSymlinks(filepath.Dir(absolute)); err == nil {
			absolute = filepath.Join(canonicalParent, filepath.Base(absolute))
		}
		if relative, err := filepath.Rel(repo, filepath.Clean(absolute)); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			excluded[filepath.ToSlash(relative)] = true
		}
	}
	return excluded
}

func productBaseline(repo string, artifactPaths ...string) (PlanningBaseline, error) {
	excluded := relativeBaselineExclusions(repo, artifactPaths...)
	baselineRef := "HEAD"
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		emptyTree := exec.Command("git", "-C", repo, "hash-object", "-t", "tree", "--stdin")
		emptyTree.Stdin = strings.NewReader("")
		value, hashErr := emptyTree.Output()
		if hashErr != nil {
			return PlanningBaseline{}, fmt.Errorf("resolve empty repository baseline: %w", hashErr)
		}
		baselineRef = strings.TrimSpace(string(value))
	}
	ignored := func(path string) bool {
		path = filepath.ToSlash(path)
		if path == ".product-loop" || strings.HasPrefix(path, ".product-loop/") || excluded[path] {
			return true
		}
		for prefix := range excluded {
			if strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
				return true
			}
		}
		return false
	}
	changedCommand := exec.Command("git", "-C", repo, "diff", "--name-only", "-z", baselineRef, "--")
	changedValue, err := changedCommand.Output()
	if err != nil {
		return PlanningBaseline{}, fmt.Errorf("inspect tracked planning baseline: %w", err)
	}
	paths := []string{}
	for _, raw := range bytes.Split(changedValue, []byte{0}) {
		path := filepath.ToSlash(string(raw))
		if path != "" && !ignored(path) {
			paths = append(paths, path)
		}
	}
	untrackedCommand := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard", "-z", "--")
	untrackedValue, err := untrackedCommand.Output()
	if err != nil {
		return PlanningBaseline{}, fmt.Errorf("inspect untracked planning baseline: %w", err)
	}
	untracked := []string{}
	for _, raw := range bytes.Split(untrackedValue, []byte{0}) {
		path := filepath.ToSlash(string(raw))
		if path != "" && !ignored(path) {
			paths = append(paths, path)
			untracked = append(untracked, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return PlanningBaseline{ChangedPaths: []string{}}, nil
	}
	args := []string{"-C", repo, "diff", "--binary", "--no-ext-diff", baselineRef, "--", ".", ":(exclude).product-loop/**"}
	for path := range excluded {
		args = append(args, ":(exclude)"+path)
	}
	diffValue, err := exec.Command("git", args...).Output()
	if err != nil {
		return PlanningBaseline{}, fmt.Errorf("render tracked planning baseline: %w", err)
	}
	var canonical bytes.Buffer
	canonical.Write(diffValue)
	sort.Strings(untracked)
	for _, path := range untracked {
		absolute := filepath.Join(repo, filepath.FromSlash(path))
		info, statErr := os.Lstat(absolute)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return PlanningBaseline{}, fmt.Errorf("untracked planning baseline path is not a regular non-symlink file: %s", path)
		}
		value, readErr := os.ReadFile(absolute)
		if readErr != nil {
			return PlanningBaseline{}, fmt.Errorf("read untracked planning baseline path %s: %w", path, readErr)
		}
		fmt.Fprintf(&canonical, "\nuntracked %s %s\n", path, SHA256Bytes(value))
	}
	return PlanningBaseline{DiffSHA256: SHA256Bytes(canonical.Bytes()), ChangedPaths: paths}, nil
}

func PlanningBaselineForPlan(planPath string) (PlanningBaseline, error) {
	repo, err := ResolveControllerRepository(filepath.Dir(planPath))
	if err != nil {
		return PlanningBaseline{}, err
	}
	return PlanningBaselineForRepository(repo, planPath)
}

func PlanningBaselineForRepository(repoPath, planPath string) (PlanningBaseline, error) {
	repo, err := ResolveControllerRepositoryFor(repoPath, filepath.Dir(planPath))
	if err != nil {
		return PlanningBaseline{}, err
	}
	check, err := CheckPlanForRepository(repo, planPath)
	if err != nil {
		return PlanningBaseline{}, err
	}
	return productBaseline(repo, planPath, check.SourcePlanPath, check.SpecPath, filepath.Join(filepath.Dir(planPath), "approval.md"))
}

func rejectSymlinkComponents(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes repository boundary: %s", target)
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlinked path: %s", current)
		}
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".boatstack-planning-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}

func WritePlanningArtifact(options PlanningWriteOptions) (string, error) {
	if !featureSlugPattern.MatchString(options.Feature) {
		return "", fmt.Errorf("feature must be a lowercase kebab-case slug")
	}
	if !planningArtifacts[options.Artifact] {
		return "", fmt.Errorf("unsupported planning artifact %q; use one of: %s (note the .md suffix)", options.Artifact, strings.Join(planningArtifactNames(), ", "))
	}
	// Windows PowerShell 5.1 may prepend the UTF-8 byte-order mark and serialize
	// line endings as CRLF when a here-string is piped to a native command even
	// when $OutputEncoding uses a no-BOM encoder. Treat those transport signatures
	// as encoding metadata, not Markdown content, so every supported shell
	// produces the same artifact.
	content := normalizePlanningTransportBytes(options.Content)
	if !utf8.Valid(content) {
		return "", fmt.Errorf("planning artifact must be valid UTF-8 Markdown")
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", fmt.Errorf("planning artifact must not be empty")
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return "", err
	}
	healthCheck := CheckInstallationHealth
	if planningInstallationHealth != nil {
		healthCheck = planningInstallationHealth
	}
	if err := healthCheck(repo); err != nil {
		return "", fmt.Errorf("planning write requires a healthy Boatstack installation: %w", DoctorRepairHint(err))
	}
	ctx, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return "", err
	}
	statePath, err := deliveryStatePath(repo, options.Feature)
	if err != nil {
		return "", err
	}
	hasManagedDelivery := fileExists(statePath)
	hasLifecycleEvidence := strings.TrimSpace(options.ExpectedLifecycleSHA256) != "" ||
		strings.TrimSpace(options.ExpectedPlanLockSHA256) != "" ||
		strings.TrimSpace(options.ExpectedObservation) != ""
	if hasManagedDelivery {
		if strings.TrimSpace(options.ExpectedLifecycleSHA256) == "" || strings.TrimSpace(options.ExpectedPlanLockSHA256) == "" {
			return "", fmt.Errorf("an active delivery planning write requires a lifecycle-bound flow bootstrap prescription")
		}
		snapshot, snapshotErr := ResolveLifecycleSnapshot(repo, options.Feature)
		if snapshotErr != nil {
			return "", snapshotErr
		}
		if !amendmentLifecycleState(snapshot.State) {
			return "", fmt.Errorf("active delivery %s is not in an amendment planning state", options.Feature)
		}
		if snapshot.Fingerprint != strings.TrimSpace(options.ExpectedLifecycleSHA256) ||
			snapshot.PlanLockSHA256 != strings.TrimSpace(options.ExpectedPlanLockSHA256) ||
			snapshot.ObservationID != strings.TrimSpace(options.ExpectedObservation) {
			return "", fmt.Errorf("active delivery lifecycle changed after bootstrap; resolve a fresh flow bootstrap prescription")
		}
	} else if hasLifecycleEvidence {
		return "", fmt.Errorf("lifecycle-bound planning evidence does not match an active managed delivery")
	}
	featureDirectory := ctx.FeatureDir(options.Feature)
	_, featureErr := os.Lstat(featureDirectory)
	firstWrite := os.IsNotExist(featureErr)
	if featureErr != nil && !firstWrite {
		return "", featureErr
	}
	hasSourceEvidence := strings.TrimSpace(options.SourcePlan) != "" || strings.TrimSpace(options.SourcePlanSHA256) != ""
	if firstWrite && !hasSourceEvidence {
		return "", fmt.Errorf("a new feature requires a flow bootstrap prescription with current source-plan evidence")
	}
	if hasSourceEvidence {
		if strings.TrimSpace(options.SourcePlan) == "" || strings.TrimSpace(options.SourcePlanSHA256) == "" {
			return "", fmt.Errorf("source-plan path and SHA-256 must be supplied together")
		}
		sourcePlan, discoverErr := DiscoverSourcePlan(repo, options.SourcePlan)
		if discoverErr != nil {
			return "", discoverErr
		}
		sourceAbsolute := filepath.Join(repo, filepath.FromSlash(sourcePlan))
		if rejectErr := rejectSymlinkComponents(repo, sourceAbsolute); rejectErr != nil {
			return "", fmt.Errorf("source plan must be a regular in-repository file without symlink indirection: %w", rejectErr)
		}
		currentSHA, hashErr := SHA256File(sourceAbsolute)
		if hashErr != nil {
			return "", hashErr
		}
		if currentSHA != strings.TrimSpace(options.SourcePlanSHA256) {
			return "", fmt.Errorf("source plan changed after bootstrap; resolve a fresh flow bootstrap prescription")
		}
	}
	destination := filepath.Join(ctx.FeatureDir(options.Feature), options.Artifact)
	if err := rejectSymlinkComponents(ctx.ExportRoot(), destination); err != nil {
		return "", err
	}
	if err := atomicWrite(destination, content); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(ctx.ExportRoot(), destination)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func normalizePlanningTransportBytes(content []byte) []byte {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

func RecordApproval(options ApprovalRecordOptions) error {
	if strings.TrimSpace(options.ApprovedBy) == "" {
		return fmt.Errorf("approval requires a named human")
	}
	approvedAt, err := time.Parse(time.RFC3339, options.ApprovedAt)
	if err != nil {
		return fmt.Errorf("approval timestamp must be RFC3339")
	}
	if strings.TrimSpace(options.Repo) == "" {
		return fmt.Errorf("approval requires the invoking repository context")
	}
	repo, err := ResolveControllerRepositoryFor(options.Repo, filepath.Dir(options.PlanPath))
	if err != nil {
		return err
	}
	check, err := CheckPlanForRepository(repo, options.PlanPath)
	if err != nil {
		return err
	}
	if options.Fingerprint != check.Fingerprint {
		return fmt.Errorf("approval fingerprint does not match the current plan; the plan now fingerprints as %s — re-approve against that value (run check-plan to confirm)", check.Fingerprint)
	}
	feature := strings.TrimSpace(stringValue(check.Plan["feature_id"]))
	statePath, statePathErr := deliveryStatePath(repo, feature)
	if statePathErr != nil {
		return statePathErr
	}
	hasLifecycleEvidence := strings.TrimSpace(options.ExpectedLifecycleSHA256) != "" ||
		strings.TrimSpace(options.ExpectedPlanLockSHA256) != "" ||
		strings.TrimSpace(options.ExpectedObservation) != ""
	if fileExists(statePath) {
		snapshot, snapshotErr := ResolveLifecycleSnapshot(repo, feature)
		if snapshotErr != nil {
			return snapshotErr
		}
		if snapshot.State != deliverycontrol.StateAmendmentDrafted {
			return fmt.Errorf("active delivery approval requires a drafted amendment, got %s", snapshot.State)
		}
		if strings.TrimSpace(options.ExpectedLifecycleSHA256) == "" || strings.TrimSpace(options.ExpectedPlanLockSHA256) == "" {
			return fmt.Errorf("active delivery approval requires current lifecycle and plan-lock fingerprints")
		}
		if snapshot.Fingerprint != strings.TrimSpace(options.ExpectedLifecycleSHA256) ||
			snapshot.PlanLockSHA256 != strings.TrimSpace(options.ExpectedPlanLockSHA256) ||
			snapshot.ObservationID != strings.TrimSpace(options.ExpectedObservation) {
			return fmt.Errorf("active delivery lifecycle changed before approval; resolve flow next again")
		}
	} else if hasLifecycleEvidence {
		return fmt.Errorf("lifecycle-bound approval evidence does not match an active managed delivery")
	}
	expectedOutput := filepath.Join(filepath.Dir(options.PlanPath), "approval.md")
	output := options.OutputPath
	if output == "" {
		output = expectedOutput
	}
	expectedAbsolute, err := filepath.Abs(expectedOutput)
	if err != nil {
		return err
	}
	outputAbsolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if filepath.Clean(outputAbsolute) != filepath.Clean(expectedAbsolute) {
		return fmt.Errorf("approval receipt must be written beside plan.md as approval.md")
	}
	planDirectory, err := filepath.Abs(filepath.Dir(options.PlanPath))
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(planDirectory, outputAbsolute); err != nil {
		return err
	}
	baseline, err := productBaseline(repo, options.PlanPath, check.SourcePlanPath, check.SpecPath, outputAbsolute)
	if err != nil {
		return err
	}
	if baseline.DiffSHA256 != strings.TrimSpace(options.BaselineDiffSHA256) {
		if baseline.DiffSHA256 != "" && strings.TrimSpace(options.BaselineDiffSHA256) == "" {
			return fmt.Errorf("approval requires the displayed baseline-diff fingerprint because product edits existed when planning began")
		}
		return fmt.Errorf("baseline product diff drifted after it was displayed")
	}
	schemaVersion := 2
	payloadValue := map[string]any{
		"schema_version":         schemaVersion,
		"status":                 "APPROVED",
		"approved_by":            strings.TrimSpace(options.ApprovedBy),
		"approved_at":            approvedAt.Format(time.RFC3339),
		"approval_fingerprint":   check.Fingerprint,
		"baseline_diff_sha256":   baseline.DiffSHA256,
		"baseline_changed_paths": baseline.ChangedPaths,
	}
	if hasLifecycleEvidence {
		payloadValue["lifecycle_sha256"] = strings.TrimSpace(options.ExpectedLifecycleSHA256)
		payloadValue["plan_lock_sha256"] = strings.TrimSpace(options.ExpectedPlanLockSHA256)
		payloadValue["observation_id"] = strings.TrimSpace(options.ExpectedObservation)
	}
	if version, _ := check.Plan["schema_version"].(float64); version >= 3 {
		readiness, readinessErr := checkPlanReadiness(repo, options.PlanPath)
		if readinessErr != nil {
			return readinessErr
		}
		payloadValue["schema_version"] = 3
		payloadValue["readiness_fingerprint"] = readiness.Fingerprint
		payloadValue["base_branch"] = readiness.BaseBranch
		payloadValue["head_branch"] = readiness.HeadBranch
		payloadValue["base_commit"] = readiness.BaseCommit
		payloadValue["head_commit"] = readiness.HeadCommit
		payloadValue["upstream"] = readiness.Upstream
		payloadValue["upstream_relation"] = readiness.Relation
		payloadValue["journey_manifest_sha256"] = readiness.JourneyManifestSHA256
	}
	payload, err := MarshalJSON(payloadValue)
	if err != nil {
		return err
	}
	body := "# Plan approval\n\n" + approvalMarkerStart + "\n```json\n" + strings.TrimSpace(string(payload)) + "\n```\n" + approvalMarkerEnd + "\n"
	return atomicWrite(outputAbsolute, []byte(body))
}

type installLock struct {
	BoatstackVersion string                      `json:"boatstack_version"`
	SourceCommit     string                      `json:"source_commit"`
	Platform         string                      `json:"platform"`
	BinaryPath       string                      `json:"binary_path"`
	BinarySHA256     string                      `json:"binary_sha256"`
	Integrations     map[string]IntegrationState `json:"integrations,omitempty"`
}

// CheckInstallationHealth validates installed and generated state without
// changing repository, runtime, or bookkeeping state.
func CheckInstallationHealth(repoPath string) error {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return err
	}
	topology, err := RequireManagedConfiguration(repo)
	if err != nil {
		return err
	}
	if topology.Shape == ConfigShapeHybrid {
		repositoryConfig, repositoryRaw, loadErr := LoadConfig(topology.RepositorySourcePath)
		if loadErr != nil {
			return fmt.Errorf("invalid repository Boatstack configuration: %w", loadErr)
		}
		repositoryBundle, buildErr := BuildExportBundle(topology.RepositorySourcePath, repositoryConfig, embeddedConfigBytes(repositoryRaw), "boatstack")
		if buildErr != nil {
			return buildErr
		}
		if checkErr := CheckExport(topology.RepositoryBundleRoot, repositoryBundle.Files); checkErr != nil {
			return fmt.Errorf("repository Boatstack package is stale: %w", checkErr)
		}
	}
	ctx, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return err
	}
	configPath := ctx.SourceConfigPath()
	config, raw, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("invalid or missing .boatstack-project.json: %w", err)
	}
	configBytes := raw
	if ctx.Mode == SupervisionEmbedded {
		configBytes = embeddedConfigBytes(raw)
	}
	bundle, err := BuildExportBundle(configPath, config, configBytes, "boatstack")
	if err != nil {
		return err
	}
	if err := CheckExport(ctx.ExportRoot(), bundle.Files); err != nil {
		return err
	}
	// Embedded installations own merged host settings in the repository and can
	// verify them here. Detached controller state owns generated engagement probes;
	// developer-level host activation is a separate, operator-visible boundary.
	// CheckExport above verifies those fragments without misreading them as merged
	// .cursor/.claude/.codex/.gemini configurations.
	if ctx.Mode == SupervisionEmbedded {
		if err := CheckHostHooks(ctx.HostActivationRoot(), config.Adapters); err != nil {
			return err
		}
	}
	hostAdapters := normalizedAdapters(config.Adapters)
	if contains(hostAdapters, "claude") {
		if _, err := lookPath("bash"); err != nil {
			return fmt.Errorf("Claude Code engagement probes require Bash; install Git Bash or Bash, then rerun doctor")
		}
	}
	if err := verifyGeneratedRuntime(ctx.ExportRoot()); err != nil {
		return err
	}
	if _, _, err := loadSharedRuntime(repo); err != nil {
		return err
	}
	for _, host := range []string{"cursor", "claude", "codex"} {
		if !contains(hostAdapters, host) {
			continue
		}
		inputs := [][]byte{}
		if host == "cursor" {
			inputs = append(inputs,
				[]byte(`{"hook_event_name":"beforeShellExecution","command":"git status --short"}`),
				[]byte(`{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__status__read","tool_input":"{\"scope\":\"local\"}","command":"status-server"}`),
			)
		} else {
			inputs = append(inputs, []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`))
		}
		for _, input := range inputs {
			if _, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: input}); denied {
				return fmt.Errorf("%s engagement probe denied its read-only smoke event", host)
			}
		}
		_, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(`{"malformed":true}`)})
		engaged := ResolveEngagement(repo, EngagementRequest{}).Mode == EngagementActive
		if engaged != denied {
			return fmt.Errorf("%s engagement probe contract drifted: active=%t denied=%t", host, engaged, denied)
		}
	}
	return verifyLocalRuntime(ctx.ExportRoot())
}

func Doctor(repoPath string) error {
	if err := CheckInstallationHealth(repoPath); err != nil {
		return err
	}
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return err
	}
	// Doctor keeps its legacy best-effort hygiene, but the preflight health
	// boundary above remains pure.
	pruneLegacyOperationLedger(repo)
	return nil
}

func DoctorHookHosts(repoPath string) ([]string, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return nil, err
	}
	config, _, err := LoadConfig(WorkspaceFor(repo).SourceConfigPath())
	if err != nil {
		return nil, err
	}
	hosts := []string{}
	for _, host := range []string{"cursor", "claude", "codex"} {
		if contains(normalizedAdapters(config.Adapters), host) {
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
}

func DoctorRepairHint(err error) error {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if strings.Contains(errStr, "config schema is behind") {
		return fmt.Errorf("%s; remediation: run /boatstack-update to migrate project configuration", errStr)
	}
	if strings.Contains(errStr, "config was written by a newer Boatstack") {
		return fmt.Errorf("%s; remediation: update your Boatstack installation to load this configuration", errStr)
	}
	return fmt.Errorf("%w; repair: rerun the verified Boatstack installer once from any checkout in this Git clone, then reload the coding host", err)
}
