package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const bootstrapPrescriptionSchemaVersion = 1

// Tests may replace this seam. Nil selects the production installation health
// check immediately before a bootstrap prescription is rendered.
var bootstrapInstallationHealth func(string) error

type BootstrapShell string

const (
	BootstrapShellPOSIX      BootstrapShell = "posix"
	BootstrapShellPowerShell BootstrapShell = "powershell"
)

type BootstrapOptions struct {
	Repo       string
	Feature    string
	SourcePlan string
	Artifact   string
	Shell      BootstrapShell
	Document   []byte
}

// BootstrapPrescription is the read-only, mode-aware answer for the first
// managed planning write. It binds creation intent, source-plan freshness, the
// selected worktree, and the exact helper into one literal shell envelope.
// control-law: bootstrap-command-authority-is-workspace-bound
type BootstrapPrescription struct {
	SchemaVersion      int             `json:"schema_version"`
	VerificationStatus string          `json:"verification_status"`
	Disposition        string          `json:"disposition"`
	SupervisionMode    SupervisionMode `json:"supervision_mode"`
	Repository         string          `json:"repository"`
	RepositoryID       string          `json:"repository_id,omitempty"`
	WorktreeID         string          `json:"worktree_id,omitempty"`
	ControllerRoot     string          `json:"controller_root"`
	HelperPath         string          `json:"helper_path"`
	Feature            string          `json:"feature"`
	SourcePlan         string          `json:"source_plan"`
	SourcePlanSHA256   string          `json:"source_plan_sha256"`
	Artifact           string          `json:"artifact"`
	ArtifactPath       string          `json:"artifact_path"`
	DocumentSHA256     string          `json:"document_sha256"`
	LifecycleState     string          `json:"lifecycle_state,omitempty"`
	LifecycleSHA256    string          `json:"lifecycle_sha256,omitempty"`
	ObservationID      string          `json:"observation_id,omitempty"`
	PreviousPlanLock   string          `json:"previous_plan_lock_sha256,omitempty"`
	Shell              BootstrapShell  `json:"shell"`
	Argv               []string        `json:"argv"`
	PlanningEnvelope   string          `json:"planning_envelope"`
}

func normalizedPlanningDocument(document []byte) ([]byte, error) {
	document = normalizePlanningTransportBytes(document)
	if reason := validPlanningBody(string(document)); reason != "" {
		return nil, fmt.Errorf("planning document is invalid: %s", reason)
	}
	value := append([]byte(nil), document...)
	if len(value) == 0 || value[len(value)-1] != '\n' {
		value = append(value, '\n')
	}
	return value, nil
}

func bootstrapFeatureDisposition(repo string, workspace WorkspaceContext, feature string) (string, *LifecycleSnapshot, error) {
	directory := workspace.FeatureDir(feature)
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return "CREATE_CANDIDATE", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("feature %s has conflicting planning state; run recovery-status before bootstrapping", feature)
	}
	statePath, stateErr := deliveryStatePath(repo, feature)
	if stateErr != nil {
		return "", nil, stateErr
	}
	managed := false
	for _, path := range []string{
		statePath,
		filepath.Join(directory, "plan.lock.json"),
		filepath.Join(directory, "pr.md"),
		filepath.Join(directory, "approval.md"),
		filepath.Join(directory, "autonomy.md"),
	} {
		if fileExists(path) {
			managed = true
			break
		}
	}
	if managed {
		snapshot, snapshotErr := ResolveLifecycleSnapshot(repo, feature)
		if snapshotErr != nil {
			return "", nil, fmt.Errorf("feature %s carries managed authority that cannot be verified: %w", feature, snapshotErr)
		}
		if !amendmentLifecycleState(snapshot.State) {
			return "", nil, fmt.Errorf("feature %s already carries managed authority; use flow next --feature %s", feature, feature)
		}
		return "AMEND_ACTIVE", &snapshot, nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !planningArtifacts[entry.Name()] {
			return "", nil, fmt.Errorf("feature %s has conflicting planning state; run recovery-status before bootstrapping", feature)
		}
	}
	if fileExists(filepath.Join(directory, "plan.md")) {
		if _, err := CheckPlan(filepath.Join(directory, "plan.md")); err != nil {
			return "", nil, fmt.Errorf("feature %s has an invalid saved plan; run recovery-status before bootstrapping: %w", feature, err)
		}
	}
	return "RESUME_CANDIDATE", nil, nil
}

func bootstrapProgram(workspace WorkspaceContext, shell BootstrapShell) string {
	if workspace.Mode == SupervisionDetached {
		return workspace.HelperPath()
	}
	return workspace.LauncherPath(shell == BootstrapShellPowerShell)
}

func planningArgv(program, repo, feature, artifact, sourcePlan, sourceSHA string, lifecycle *LifecycleSnapshot) []string {
	argv := []string{
		program, "planning-write",
		"--repo", repo,
		"--feature", feature,
		"--artifact", artifact,
		"--source-plan", sourcePlan,
		"--source-plan-sha256", sourceSHA,
	}
	if lifecycle != nil {
		argv = append(argv,
			"--expected-lifecycle-sha256", lifecycle.Fingerprint,
			"--expected-plan-lock-sha256", lifecycle.PlanLockSHA256,
			"--expected-observation", lifecycle.ObservationID,
		)
	}
	return argv
}

func posixPlanningEnvelopeFor(argv []string, document []byte) string {
	words := make([]string, len(argv))
	for index, word := range argv {
		words[index] = posixPlanningWord(word)
	}
	delimiter := "BOATSTACK_PLAN_" + strings.ToUpper(SHA256Bytes(document)[:16])
	return strings.Join(words, " ") + " <<'" + delimiter + "'\n" + string(document) + delimiter + "\n"
}

func powerShellPlanningWord(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powerShellPlanningEnvelopeFor(argv []string, document []byte) (string, error) {
	for _, line := range strings.Split(strings.ReplaceAll(string(document), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "'@") {
			return "", fmt.Errorf("PowerShell cannot represent a document line beginning with '@; use --shell posix with Git Bash")
		}
	}
	words := make([]string, len(argv))
	for index, word := range argv {
		if strings.Contains(word, "'") {
			return "", fmt.Errorf("PowerShell cannot safely bind an argument containing a single quote; use --shell posix with Git Bash")
		}
		words[index] = powerShellPlanningWord(word)
	}
	return "& {\n" + powerShellPlanningEncodingLine + "\n@'\n" + string(document) + "'@ | & " + strings.Join(words, " ") + "\n" + powerShellPlanningExitLine + "\n}\n", nil
}

// ResolvePlanningBootstrap is pure with respect to repository and controller
// state: it validates current evidence and returns bytes to execute, but writes
// nothing. The later planning-write rechecks the source-plan digest before its
// atomic first write.
// control-law: bootstrap-command-authority-is-workspace-bound
func ResolvePlanningBootstrap(options BootstrapOptions) (BootstrapPrescription, error) {
	if !featureSlugPattern.MatchString(options.Feature) {
		return BootstrapPrescription{}, fmt.Errorf("feature must be a lowercase kebab-case slug")
	}
	if !planningArtifacts[options.Artifact] {
		return BootstrapPrescription{}, fmt.Errorf("unsupported planning artifact %q; use one of: %s", options.Artifact, strings.Join(planningArtifactNames(), ", "))
	}
	if options.Shell != BootstrapShellPOSIX && options.Shell != BootstrapShellPowerShell {
		return BootstrapPrescription{}, fmt.Errorf("shell must be posix or powershell")
	}
	document, err := normalizedPlanningDocument(options.Document)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	healthCheck := CheckInstallationHealth
	if bootstrapInstallationHealth != nil {
		healthCheck = bootstrapInstallationHealth
	}
	if err := healthCheck(repo); err != nil {
		return BootstrapPrescription{}, fmt.Errorf("bootstrap requires a healthy Boatstack installation: %w", DoctorRepairHint(err))
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	sourcePlan, err := DiscoverSourcePlan(repo, options.SourcePlan)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	sourceAbsolute := filepath.Join(repo, filepath.FromSlash(sourcePlan))
	if err := rejectSymlinkComponents(repo, sourceAbsolute); err != nil {
		return BootstrapPrescription{}, fmt.Errorf("source plan must be a regular in-repository file without symlink indirection: %w", err)
	}
	if pathWithin(workspace.FeatureDir(options.Feature), sourceAbsolute) {
		return BootstrapPrescription{}, fmt.Errorf("source plan must remain outside the generated feature package")
	}
	sourceSHA, err := SHA256File(sourceAbsolute)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	disposition, lifecycle, err := bootstrapFeatureDisposition(repo, workspace, options.Feature)
	if err != nil {
		return BootstrapPrescription{}, err
	}
	program := bootstrapProgram(workspace, options.Shell)
	argv := planningArgv(program, repo, options.Feature, options.Artifact, sourcePlan, sourceSHA, lifecycle)
	if options.Shell == BootstrapShellPOSIX {
		// Git Bash accepts Windows drive paths in slash form. Keep the typed argv
		// identical to the bytes rendered for that shell.
		argv[0] = filepath.ToSlash(argv[0])
		argv[3] = filepath.ToSlash(argv[3])
	}
	var envelope string
	if options.Shell == BootstrapShellPowerShell {
		envelope, err = powerShellPlanningEnvelopeFor(argv, document)
	} else {
		envelope = posixPlanningEnvelopeFor(argv, document)
	}
	if err != nil {
		return BootstrapPrescription{}, err
	}
	prescription := BootstrapPrescription{
		SchemaVersion: bootstrapPrescriptionSchemaVersion, VerificationStatus: "VERIFIED",
		Disposition: disposition, SupervisionMode: workspace.Mode,
		Repository: repo, RepositoryID: workspace.RepoID, WorktreeID: workspace.WorktreeID,
		ControllerRoot: workspace.ExportRoot(), HelperPath: program,
		Feature: options.Feature, SourcePlan: sourcePlan, SourcePlanSHA256: sourceSHA,
		Artifact: options.Artifact, ArtifactPath: filepath.Join(workspace.FeatureDir(options.Feature), options.Artifact),
		DocumentSHA256: SHA256Bytes(document), Shell: options.Shell, Argv: argv,
		PlanningEnvelope: envelope,
	}
	if lifecycle != nil {
		prescription.LifecycleState = string(lifecycle.State)
		prescription.LifecycleSHA256 = lifecycle.Fingerprint
		prescription.ObservationID = lifecycle.ObservationID
		prescription.PreviousPlanLock = lifecycle.PlanLockSHA256
	}
	return prescription, nil
}
