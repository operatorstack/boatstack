package boatstack

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type InitOptions struct {
	Repo              string
	BinaryPath        string
	IntegrationChoice string
	Yes               bool
	Update            bool
	Repair            bool
	AllowDowngrade    bool
	Input             io.Reader
	Output            io.Writer
}

var (
	initDoctor     = Doctor
	initCheckpoint = func(string) error { return nil }
)

func gitOutput(repo string, arguments ...string) string {
	command := exec.Command("git", append([]string{"-C", repo}, arguments...)...)
	value, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func ResolveRepository(path string) (string, error) {
	if path == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root := gitOutput(absolute, "rev-parse", "--show-toplevel")
	if root == "" {
		return "", fmt.Errorf("Boatstack must be initialized inside a Git repository")
	}
	return root, nil
}

func detectTestCommand(repo string) string {
	if fileExists(filepath.Join(repo, "scripts", "check.sh")) {
		return "bash scripts/check.sh"
	}
	packagePath := filepath.Join(repo, "package.json")
	if value, err := os.ReadFile(packagePath); err == nil {
		var packageJSON struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(value, &packageJSON) == nil && strings.TrimSpace(packageJSON.Scripts["test"]) != "" {
			switch {
			case fileExists(filepath.Join(repo, "pnpm-lock.yaml")):
				return "pnpm test"
			case fileExists(filepath.Join(repo, "yarn.lock")):
				return "yarn test"
			case fileExists(filepath.Join(repo, "bun.lock")), fileExists(filepath.Join(repo, "bun.lockb")):
				return "bun test"
			default:
				return "npm test"
			}
		}
	}
	for _, candidate := range []struct{ path, command string }{
		{"go.mod", "go test ./..."}, {"Cargo.toml", "cargo test"}, {"Makefile", "make test"},
	} {
		if fileExists(filepath.Join(repo, candidate.path)) {
			return candidate.command
		}
	}
	pyprojectUsesPytest := false
	if value, err := os.ReadFile(filepath.Join(repo, "pyproject.toml")); err == nil {
		pyprojectUsesPytest = strings.Contains(strings.ToLower(string(value)), "pytest")
	}
	pythonProject := fileExists(filepath.Join(repo, "pytest.ini")) ||
		fileExists(filepath.Join(repo, "conftest.py")) || pyprojectUsesPytest
	if pythonProject {
		switch {
		case fileExists(filepath.Join(repo, "uv.lock")):
			return "uv run pytest"
		case fileExists(filepath.Join(repo, "poetry.lock")):
			return "poetry run pytest"
		default:
			return "python -m pytest"
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func detectContext(repo string) []string {
	paths := []string{}
	for _, candidate := range []string{"README.md", "AGENTS.md", "CLAUDE.md", "docs/architecture/", "docs/decisions/"} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(strings.TrimSuffix(candidate, "/")))); err == nil {
			paths = append(paths, candidate)
		}
	}
	return paths
}

func DetectHosts(repo string) []string {
	hosts := []string{}
	checks := []struct {
		name     string
		paths    []string
		commands []string
	}{
		{"cursor", []string{".cursor"}, []string{"cursor", "cursor-agent"}},
		{"claude", []string{".claude", "CLAUDE.md"}, []string{"claude"}},
		{"codex", []string{".agents", "AGENTS.md"}, []string{"codex"}},
	}
	for _, check := range checks {
		detected := false
		for _, path := range check.paths {
			if _, err := os.Stat(filepath.Join(repo, path)); err == nil {
				detected = true
			}
		}
		for _, command := range check.commands {
			if _, err := lookPath(command); err == nil {
				detected = true
			}
		}
		if detected {
			hosts = append(hosts, check.name)
		}
	}
	if strings.Contains(gitOutput(repo, "remote", "get-url", "origin"), "github.com") || fileExists(filepath.Join(repo, ".github")) {
		hosts = append(hosts, "github")
	}
	return hosts
}

func defaultConfig(repo, testCommand string) ProjectConfig {
	branch := strings.TrimPrefix(gitOutput(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/")
	if branch == "" {
		branch = gitOutput(repo, "branch", "--show-current")
	}
	if branch == "" {
		branch = "main"
	}
	return ProjectConfig{
		SchemaVersion: 1,
		Project: Project{
			Name: filepath.Base(repo), DefaultBranch: branch, Context: detectContext(repo),
			Commands: map[string]string{"test": testCommand},
		},
		Workflow:  Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true},
		Workspace: Workspace{Enabled: true, Mode: "worktree", Cleanup: "confirm", CleanupAfter: "merge"},
		Adapters:  []string{"cursor", "claude", "codex", "github"},
		Integrations: map[string]IntegrationState{
			"gstack":   {Requested: false, Version: GStackRef},
			"spec-kit": {Requested: false, Version: SpecKitVersion},
		},
	}
}

func promptLine(reader *bufio.Reader, output io.Writer, prompt string) (string, error) {
	fmt.Fprint(output, prompt)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func requestInstallationRepair(options *InitOptions, result InstallationRepairResult, reader *bufio.Reader) error {
	fmt.Fprintf(options.Output, "Boatstack found recoverable drift in Boatstack-owned control state (%s -> %s, %s):\n", result.InstalledVersion, result.TargetVersion, result.Direction)
	for _, item := range result.Items {
		if item.Classification == RepairOwnedDrifted {
			fmt.Fprintf(options.Output, "  %s: %s\n", item.Path, item.Reason)
		}
	}
	fmt.Fprintln(options.Output, "Repair package: "+result.PackageFingerprint)
	fmt.Fprintln(options.Output, "The repair will remain in this fresh update branch and its update PR.")
	if options.Yes {
		return fmt.Errorf("recoverable Boatstack-owned drift requires explicit repair authority\nNEXT=%s", installationRepairRetryCommand(result.TargetVersion))
	}
	answer, err := promptLine(reader, options.Output, "Repair Boatstack-owned state and continue the update? [y/N] ")
	if err != nil {
		return err
	}
	if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
		return fmt.Errorf("update left unchanged\nNEXT=%s", installationRepairRetryCommand(result.TargetVersion))
	}
	options.Repair = true
	return nil
}

func installationRepairRetryCommand(version string) string {
	if runtime.GOOS == "windows" {
		return `$env:BOATSTACK_MODE="update"; $env:BOATSTACK_VERSION="` + version + `"; $env:BOATSTACK_REPO=(Get-Location).Path; $env:BOATSTACK_YES="1"; $env:BOATSTACK_REPAIR="1"; irm https://raw.githubusercontent.com/operatorstack/boatstack/` + version + `/install.ps1 | iex`
	}
	return `BOATSTACK_MODE=update BOATSTACK_VERSION=` + version + ` BOATSTACK_REPO="$PWD" BOATSTACK_YES=1 BOATSTACK_REPAIR=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/` + version + `/install.sh)"`
}

func copyHelper(source, repo string) (string, string, error) {
	if source == "" {
		var err error
		source, err = os.Executable()
		if err != nil {
			return "", "", err
		}
	}
	value, err := os.ReadFile(source)
	if err != nil {
		return "", "", err
	}
	destination := filepath.Join(repo, ".product-loop", "bin", helperName())
	if err := atomicWriteMode(destination, value, 0o755); err != nil {
		return "", "", err
	}
	return destination, SHA256Bytes(value), nil
}

func writeInstallLock(repo, binaryPath, binaryHash string, integrations map[string]IntegrationState) error {
	value, err := buildInstallLock(repo, binaryPath, binaryHash, integrations)
	if err != nil {
		return err
	}
	return atomicWriteMode(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"), value, 0o644)
}

func buildInstallLock(repo, binaryPath, binaryHash string, integrations map[string]IntegrationState) ([]byte, error) {
	relativeBinaryPath, err := repositoryRelativePath(repo, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("invalid Boatstack helper path: %w", err)
	}
	lock := map[string]any{
		"schema_version":           1,
		"boatstack_version":        Version,
		"source_commit":            SourceCommit,
		"platform":                 runtime.GOOS + "/" + runtime.GOARCH,
		"binary_path":              relativeBinaryPath,
		"binary_sha256":            binaryHash,
		"release_checksums_sha256": ChecksumsSHA256,
		"integrations":             integrations,
	}
	value, err := MarshalJSON(lock)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	if err := ValidateJSON("validate generated install lock", lockPath, value); err != nil {
		return nil, err
	}
	return value, nil
}

func readInstalledIntegrations(repo string, config ProjectConfig) (map[string]IntegrationState, error) {
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if err != nil {
		return nil, fmt.Errorf("missing previous local install lock: %w", err)
	}
	var lock struct {
		Integrations map[string]IntegrationState `json:"integrations"`
	}
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	if err := DecodeJSON("load previous local install lock", lockPath, value, &lock); err != nil {
		return nil, err
	}
	if len(lock.Integrations) > 0 {
		return lock.Integrations, nil
	}
	states := map[string]IntegrationState{}
	for name, configured := range config.Integrations {
		configured.Status = "preserved"
		configured.Detail = "selection preserved during Boatstack core update"
		states[name] = configured
	}
	return states, nil
}

func updateChangedPaths(repo string) []string {
	seen := map[string]bool{}
	for _, arguments := range [][]string{{"diff", "--name-only"}, {"ls-files", "--others", "--exclude-standard"}} {
		for _, path := range strings.Split(gitOutput(repo, arguments...), "\n") {
			path = strings.TrimSpace(path)
			if path != "" {
				seen[filepath.ToSlash(path)] = true
			}
		}
	}
	return sortedKeys(seen)
}

func checkUpdateDiffScope(repo string, currentFiles map[string][]byte, previous map[string]string, hookPaths []string) ([]string, error) {
	allowed := map[string]bool{".boatstack-project.json": true}
	for path := range currentFiles {
		allowed[filepath.ToSlash(path)] = true
	}
	for path := range previous {
		allowed[filepath.ToSlash(path)] = true
	}
	for _, path := range hookPaths {
		allowed[filepath.ToSlash(path)] = true
	}
	changed := updateChangedPaths(repo)
	unexpected := []string{}
	for _, path := range changed {
		if !allowed[path] {
			unexpected = append(unexpected, path)
		}
	}
	if len(unexpected) > 0 {
		return changed, fmt.Errorf("update touched non-Boatstack paths: %s", strings.Join(unexpected, ", "))
	}
	return changed, nil
}

func RunInit(options InitOptions) (returnErr error) {
	if options.Input == nil {
		options.Input = os.Stdin
	}
	if options.Output == nil {
		options.Output = os.Stdout
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(options.Input)
	configPath := filepath.Join(repo, ".boatstack-project.json")
	configExists := fileExists(configPath)
	installed := fileExists(filepath.Join(repo, ".product-loop", "generated.lock.json")) || fileExists(filepath.Join(repo, ".product-loop", "bin", helperName()))
	if installed && !options.Update {
		return fmt.Errorf("Boatstack is already installed; use update, or invoke the verified installer with --repair when owned control state prevents updating")
	}
	if options.Update && !configExists {
		return fmt.Errorf("Boatstack update requires an existing .boatstack-project.json")
	}
	var config ProjectConfig
	var rawConfig []byte
	var migrationFrom, migrationTo int
	var migrationChanged bool
	if configExists {
		var err error
		rawConfig, err = os.ReadFile(configPath)
		if err != nil {
			return err
		}
		var upgraded []byte
		upgraded, migrationFrom, migrationTo, migrationChanged, err = MigrateConfigBytes(rawConfig)
		if err != nil {
			return fmt.Errorf("failed to migrate project config: %w", err)
		}
		if migrationChanged {
			rawConfig = upgraded
		}
		if err := DecodeJSON("load project configuration", configPath, rawConfig, &config); err != nil {
			return fmt.Errorf("existing Boatstack config is invalid: %w", err)
		}
		if err := ValidateConfig(config); err != nil {
			return fmt.Errorf("existing Boatstack config is invalid: %w", err)
		}
	} else {
		testCommand := detectTestCommand(repo)
		if testCommand == "" {
			if options.Yes {
				return fmt.Errorf("no test command could be detected; rerun interactively or create .boatstack-project.json")
			}
			testCommand, err = promptLine(reader, options.Output, "No test command was detected. Enter the real project test command: ")
			if err != nil || testCommand == "" {
				return fmt.Errorf("a real project test command is required")
			}
		}
		config = defaultConfig(repo, testCommand)
	}
	var repairResult InstallationRepairResult
	if options.Update {
		repairResult, err = ClassifyInstallationRepair(repo, config.Adapters, options.AllowDowngrade)
		if err != nil {
			return err
		}
		if repairResult.Direction == "DOWNGRADE" && (!options.Repair || !options.AllowDowngrade) {
			return fmt.Errorf("Boatstack %s to %s is a downgrade; rerun with both --repair and --allow-downgrade after reviewing the target release", repairResult.InstalledVersion, repairResult.TargetVersion)
		}
		if repairResult.VerificationStatus == "BLOCKED" {
			return fmt.Errorf("Boatstack update cannot safely repair this installation: %s", strings.Join(repairResult.Blockers, "; "))
		}
		if repairResult.VerificationStatus == "REPAIR_AVAILABLE" && !options.Repair {
			if err := requestInstallationRepair(&options, repairResult, reader); err != nil {
				return err
			}
		}
		if err := ValidateUpdateWorkspaceForRepair(repo, config, repairResult, options.Repair); err != nil {
			return err
		}
	}
	var preservedStates map[string]IntegrationState
	if options.Update {
		preservedStates, err = readInstalledIntegrations(repo, config)
		if err != nil && options.Repair && len(config.Integrations) > 0 {
			preservedStates = config.Integrations
			err = nil
		}
		if err != nil {
			return err
		}
	}

	detected := DetectHosts(repo)
	if len(detected) == 0 {
		fmt.Fprintln(options.Output, "Detected host signals: none; installing all thin adapters for portability.")
	} else {
		fmt.Fprintf(options.Output, "Detected host signals: %s. Installing portable Cursor, Claude, Codex, and GitHub adapters.\n", strings.Join(detected, ", "))
	}

	choice := options.IntegrationChoice
	if options.Update && choice != "" {
		return fmt.Errorf("Boatstack update preserves existing integrations; change integrations separately")
	}
	if !options.Update && choice == "" {
		if options.Yes {
			choice = "core"
		} else {
			fmt.Fprintln(options.Output, "\nOptional integrations:")
			fmt.Fprintln(options.Output, "  core     Boatstack only; no external runtimes")
			fmt.Fprintln(options.Output, "  gstack   product/design/engineering/DX review lenses; requires Git, Bun, and a supported host")
			fmt.Fprintln(options.Output, "  spec-kit specification/plan/task/checklist generation; requires uv and a managed Python environment")
			fmt.Fprintln(options.Output, "  both     install both optional integrations")
			choice, err = promptLine(reader, options.Output, "Choose [core]: ")
			if err != nil {
				return err
			}
			if choice == "" {
				choice = "core"
			}
		}
	}
	if !options.Update {
		wantGStack, wantSpecKit, choiceErr := RequestedIntegrations(choice)
		if choiceErr != nil {
			return choiceErr
		}
		config.Integrations = map[string]IntegrationState{
			"gstack":   {Requested: wantGStack, Version: GStackRef},
			"spec-kit": {Requested: wantSpecKit, Version: SpecKitVersion},
		}
		rawConfig, err = MarshalJSON(config)
		if err != nil {
			return err
		}
	}
	previousGenerated := previousFiles(repo)
	var repairGeneratedLock []byte
	if options.Update && options.Repair && len(previousGenerated) == 0 {
		var recoverErr error
		repairGeneratedLock, previousGenerated, recoverErr = committedGeneratedProvenance(repo)
		if recoverErr != nil {
			return recoverErr
		}
	}
	bundle, err := BuildExportBundle(configPath, config, rawConfig, "boatstack")
	if err != nil {
		return err
	}
	if err := ValidateJSON("validate project configuration before initialization", configPath, rawConfig); err != nil {
		return err
	}
	if _, err := PrepareHostHooksForUpdate(repo, config.Adapters, options.Update && options.Repair); err != nil {
		return err
	}
	if problems := ExportCollisions(repo, bundle.Files); len(problems) > 0 {
		if options.Update && options.Repair {
			allowed := repairOwnedPaths(repairResult)
			remaining := []string{}
			for _, problem := range problems {
				if !allowed[problem] {
					remaining = append(remaining, problem)
				}
			}
			problems = remaining
		}
		if len(problems) > 0 {
			return fmt.Errorf("refusing to overwrite user-owned files: %s", strings.Join(problems, ", "))
		}
	}
	paths := sortedKeys(bundle.Files)
	fmt.Fprintf(options.Output, "\nBoatstack will generate %d paths:\n", len(paths))
	for _, path := range paths {
		fmt.Fprintln(options.Output, "  "+path)
	}
	for _, path := range HostHookPaths(config.Adapters) {
		fmt.Fprintln(options.Output, "  "+path+" (merge Boatstack safety hook; preserve existing settings)")
	}
	if !configExists {
		fmt.Fprintln(options.Output, "  .boatstack-project.json (editable repository facts)")
	}
	if !options.Yes {
		answer, promptErr := promptLine(reader, options.Output, "Write these files? [y/N] ")
		if promptErr != nil {
			return promptErr
		}
		if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
			return fmt.Errorf("installation cancelled before writing files")
		}
	}
	helperSource := options.BinaryPath
	if helperSource == "" {
		helperSource, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if options.Update && options.Repair {
		currentRepair, classifyErr := ClassifyInstallationRepair(repo, config.Adapters, options.AllowDowngrade)
		if classifyErr != nil {
			return classifyErr
		}
		if currentRepair.PackageFingerprint != repairResult.PackageFingerprint {
			return fmt.Errorf("Boatstack-owned repair state changed after preview; inspect the new repair-status before retrying")
		}
		backup, backupErr := writeInstallationRepairBackup(repo, repairResult)
		if backupErr != nil {
			return fmt.Errorf("create Boatstack repair backup: %w", backupErr)
		}
		repairResult.BackupPath = backup
		fmt.Fprintf(options.Output, "Repair package %s backed up at %s.\n", repairResult.PackageFingerprint, backup)
	}
	snapshot, err := beginRepositorySnapshot(repo)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			if rollbackErr := snapshot.rollback(); rollbackErr != nil {
				returnErr = fmt.Errorf("%v; initialization rollback failed: %w", returnErr, rollbackErr)
			}
		}
	}()
	if _, err := installSharedRuntime(helperSource, repo, config.Integrations); err != nil {
		return fmt.Errorf("cannot install the repository-family Boatstack runtime: %w", err)
	}
	var states map[string]IntegrationState
	if options.Update {
		states = preservedStates
	} else {
		states, err = InstallIntegrations(choice, repo, config.Adapters)
	}
	if err != nil {
		return err
	}
	helperValue, err := os.ReadFile(helperSource)
	if err != nil {
		return fmt.Errorf("read Boatstack helper before initialization commit: %w", err)
	}
	prospectiveBinaryPath := filepath.Join(repo, ".product-loop", "bin", helperName())
	if _, err := buildInstallLock(repo, prospectiveBinaryPath, SHA256Bytes(helperValue), states); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, rawConfig, 0o644); err != nil {
		return err
	}
	if err := initCheckpoint("config-written"); err != nil {
		return fmt.Errorf("initialization checkpoint config-written: %w", err)
	}
	if len(repairGeneratedLock) > 0 {
		lockPath := filepath.Join(repo, ".product-loop", "generated.lock.json")
		if err := rejectSymlinkComponents(repo, lockPath); err != nil {
			return err
		}
		if err := atomicWrite(lockPath, repairGeneratedLock); err != nil {
			return fmt.Errorf("restore generated provenance inside repair transaction: %w", err)
		}
	}
	var writeErr error
	if options.Update && options.Repair {
		writeErr = WriteExportForRepair(repo, bundle.Files, repairOwnedPaths(repairResult))
	} else {
		writeErr = WriteExport(repo, bundle.Files)
	}
	if writeErr != nil {
		return writeErr
	}
	if err := initCheckpoint("export-written"); err != nil {
		return fmt.Errorf("initialization checkpoint export-written: %w", err)
	}
	if err := InstallHostHooksForUpdate(repo, config.Adapters, options.Update && options.Repair); err != nil {
		return err
	}
	if err := initCheckpoint("hooks-written"); err != nil {
		return fmt.Errorf("initialization checkpoint hooks-written: %w", err)
	}
	if err := InstallExecutionInterceptors(repo, config.Adapters); err != nil {
		return err
	}
	if err := initCheckpoint("interceptors-written"); err != nil {
		return fmt.Errorf("initialization checkpoint interceptors-written: %w", err)
	}
	binaryPath, binaryHash, err := copyHelper(helperSource, repo)
	if err != nil {
		return err
	}
	if err := initCheckpoint("helper-written"); err != nil {
		return fmt.Errorf("initialization checkpoint helper-written: %w", err)
	}
	if _, err := installSharedRuntime(helperSource, repo, states); err != nil {
		return fmt.Errorf("cannot finalize the repository-family Boatstack runtime: %w", err)
	}
	if err := writeInstallLock(repo, binaryPath, binaryHash, states); err != nil {
		return err
	}
	if err := initCheckpoint("install-lock-written"); err != nil {
		return fmt.Errorf("initialization checkpoint install-lock-written: %w", err)
	}
	if err := CheckExport(repo, bundle.Files); err != nil {
		return err
	}
	if err := CheckHostHooks(repo, config.Adapters); err != nil {
		return err
	}
	if err := initDoctor(repo); err != nil {
		return fmt.Errorf("post-install smoke check failed: %w", err)
	}
	if options.Update {
		changed, scopeErr := checkUpdateDiffScope(repo, bundle.Files, previousGenerated, HostHookPaths(config.Adapters))
		if scopeErr != nil {
			return scopeErr
		}
		fmt.Fprintf(options.Output, "\nPASS: Boatstack updated to %s on a dedicated infrastructure branch.\n", Version)
		if migrationChanged {
			fmt.Fprintf(options.Output, "PASS: migrated .boatstack-project.json from schema version %d to %d.\n", migrationFrom, migrationTo)
		} else {
			fmt.Fprintln(options.Output, "PASS: no product files changed.")
		}
		fmt.Fprintln(options.Output, "Changed Boatstack paths:")
		for _, path := range changed {
			fmt.Fprintln(options.Output, "  "+path)
		}
	} else {
		fmt.Fprintln(options.Output, "\nPASS: Boatstack core installed without a language runtime.")
	}
	fmt.Fprintln(options.Output, "PASS: generated irreversible-operation hook contracts verified for installed hosts.")
	fmt.Fprintln(options.Output, "Host activation remains an operator-visible boundary; run doctor after reload and verify each host reports its hook as active.")
	fmt.Fprintln(options.Output, "Hooks are defense in depth; keep least-privilege credentials and service-side destructive approval.")
	keys := sortedKeys(states)
	for _, name := range keys {
		state := states[name]
		fmt.Fprintf(options.Output, "  %s: %s — %s\n", name, state.Status, state.Detail)
	}
	if options.Update {
		fmt.Fprintln(options.Output, "\nReview the generated diff before publishing the update PR:")
	} else {
		fmt.Fprintln(options.Output, "\nBefore product work, commit Boatstack infrastructure in its own PR:")
	}
	stagePaths := append([]string{".boatstack-project.json"}, paths...)
	if options.Update {
		for path := range previousGenerated {
			stagePaths = append(stagePaths, path)
		}
	}
	stagePaths = append(stagePaths, HostHookPaths(config.Adapters)...)
	stageSet := map[string]bool{}
	for _, path := range stagePaths {
		stageSet[path] = true
	}
	stagePaths = sortedKeys(stageSet)
	fmt.Fprintln(options.Output, "  git status --short")
	fmt.Fprintln(options.Output, "  git add -- "+strings.Join(stagePaths, " "))
	if options.Update {
		fmt.Fprintf(options.Output, "  git commit -m \"chore: update Boatstack to %s\"\n", Version)
		fmt.Fprintf(options.Output, "  git push -u origin chore/update-boatstack-%s\n", Version)
		fmt.Fprintln(options.Output, "Do not publish until the human replies `open update PR`; never merge automatically.")
	} else {
		fmt.Fprintln(options.Output, "  git commit -m \"chore: install Boatstack\"")
		fmt.Fprintln(options.Output, "  git push -u origin chore/install-boatstack")
	}
	fmt.Fprintln(options.Output, "The verified runtime is shared by worktrees in this Git clone; each worktree hydrates its ignored .product-loop/bin/ files automatically on first use.")
	fmt.Fprintln(options.Output, "A separate fresh clone still requires one verified installer run.")
	if options.Update {
		fmt.Fprintln(options.Output, "\nAfter the update PR is merged, reload Cursor, Codex, or Claude.")
	} else {
		fmt.Fprintln(options.Output, "\nAfter that PR is merged, reload Cursor, Codex, or Claude and start in Plan mode:")
		fmt.Fprintln(options.Output, "  1. Describe the product change and save the host plan (use .product-loop/intake/ if the host exposes no path).")
	}
	fmt.Fprintln(options.Output, "Host activation checklist:")
	fmt.Fprintln(options.Output, "  Cursor: reload the window and confirm beforeShellExecution and beforeMCPExecution are paired with their after events, plus synchronous pre/post native-tool hooks; the hooks are defense in depth.")
	fmt.Fprintln(options.Output, "  Claude Code: reload, then use /hooks to confirm Boatstack PreToolUse, PostToolUse, and PostToolUseFailure hooks are active (Bash is required).")
	fmt.Fprintln(options.Output, "  Codex: trust this exact linked-worktree path, use /hooks to review and trust the Boatstack PreToolUse and PostToolUse hooks, then start a new task.")
	if contains(config.Adapters, "gemini") {
		fmt.Fprintln(options.Output, "  Gemini CLI: reload and confirm the Boatstack BeforeTool and AfterTool hooks are active.")
	}
	fmt.Fprintln(options.Output, "Boatstack start command by host:")
	fmt.Fprintln(options.Output, "  Claude Code: /auto-plan")
	fmt.Fprintln(options.Output, "  Cursor: /auto-plan")
	fmt.Fprintln(options.Output, "  Codex: $boatstack auto-plan")
	fmt.Fprintln(options.Output, "Return later with Claude Code or Cursor: /boatstack-next")
	fmt.Fprintln(options.Output, "Return later with Codex: $boatstack next")
	fmt.Fprintln(options.Output, "If Boatstack created .claude/skills during an active Claude Code session, reload Claude Code before using its slash commands.")
	if err := snapshot.commit(); err != nil {
		return fmt.Errorf("remove initialization rollback snapshot: %w", err)
	}
	return nil
}

const ExecutionBoundaryDX = `
**Boatstack Execution Boundary:**
When the user approves a plan within your native Plan Mode, **do not immediately transition to Auto-Edit or begin mutating product files.** Because this repository is managed by Boatstack, execution must pass through verifiable gates. Instead of executing the code:
1. Save your proposed plan to ` + "`.product-loop/intake/source-plan.md`" + `.
2. Before auto-plan succeeds, the user may still choose an unmanaged workflow. Once auto-plan creates a saved feature plan, do not offer direct product editing: resolve Boatstack state and continue through plan-gate, approval when configured, and build activation.
3. Async task completion, conversation state, or an execution-mode transition never creates implementation authority. Only a current plan lock does.
`

const interceptorHeader = "<!-- BEGIN BOATSTACK EXECUTION INTERCEPTOR -->\n"
const interceptorFooter = "\n<!-- END BOATSTACK EXECUTION INTERCEPTOR -->\n"

func injectExecutionInterceptor(repo, file string) error {
	path := filepath.Join(repo, file)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			content = []byte{}
		} else {
			return err
		}
	}
	text := string(content)
	start := strings.Index(text, interceptorHeader)
	end := strings.Index(text, interceptorFooter)
	injection := interceptorHeader + strings.TrimSpace(ExecutionBoundaryDX) + interceptorFooter

	if strings.Count(text, interceptorHeader) != strings.Count(text, interceptorFooter) || strings.Count(text, interceptorHeader) > 1 || (start >= 0 && end < start) {
		return fmt.Errorf("ambiguous Boatstack execution interceptor markers in %s; preserve the file and repair the marker boundary manually", path)
	}
	if start >= 0 && end > start {
		text = text[:start] + injection + text[end+len(interceptorFooter):]
	} else {
		text = strings.TrimSpace(text) + "\n\n" + injection
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(text)+"\n"), 0o644)
}

func InstallExecutionInterceptors(repo string, adapters []string) error {
	for _, adapter := range adapters {
		if adapter == "gemini" {
			if err := injectExecutionInterceptor(repo, "GEMINI.md"); err != nil {
				return err
			}
		} else if adapter == "claude" {
			if err := injectExecutionInterceptor(repo, "CLAUDE.md"); err != nil {
				return err
			}
		} else if adapter == "cursor" {
			if err := injectExecutionInterceptor(repo, ".cursorrules"); err != nil {
				return err
			}
		}
	}
	return nil
}

func RunUpdate(options InitOptions) error {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return err
	}
	if options.Input == nil {
		options.Input = os.Stdin
	}
	if options.Output == nil {
		options.Output = os.Stdout
	}
	config, _, configErr := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if configErr != nil {
		return configErr
	}
	preflight, classifyErr := ClassifyInstallationRepair(repo, config.Adapters, options.AllowDowngrade)
	if classifyErr != nil {
		return classifyErr
	}
	if preflight.VerificationStatus == "REPAIR_AVAILABLE" && !options.Repair {
		reader := bufio.NewReader(options.Input)
		options.Input = reader
		if err := requestInstallationRepair(&options, preflight, reader); err != nil {
			return err
		}
	}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	repairAuthority := fmt.Sprintf("repair=%t\x00allow-downgrade=%t", options.Repair, options.AllowDowngrade)
	packageFingerprint := SHA256Bytes([]byte(Version + "\x00" + SourceCommit + "\x00" + ChecksumsSHA256 + "\x00" + repairAuthority))
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: "install-update", Scope: OperationScope{Worktree: filepath.Base(repo), HeadBranch: branch},
		Target: "boatstack-install:" + Version, PackageFingerprint: packageFingerprint,
		AuthorizationFingerprint: SHA256Bytes([]byte("update-request\x00" + branch + "\x00" + packageFingerprint + "\x00" + repairAuthority)),
		RetryClass:               "ATOMIC_LOCAL", MaxAttempts: 2,
		ExpectedPostcondition: "the generated runtime, adapters, hooks, and preserved integration state match the pinned release",
	})
	if err != nil {
		return err
	}
	begin, err := BeginOperation(repo, receipt.OperationID, SHA256Bytes([]byte("install-update\x00"+packageFingerprint)), "boatstack-helper update")
	if err != nil {
		if begin.Receipt.State == OperationSucceeded {
			return nil
		}
		return err
	}
	if begin.Receipt.State == OperationSucceeded {
		return nil
	}
	options.Update = true
	if err := RunInit(options); err != nil {
		_, _ = CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "RETRYABLE", "the atomic update transaction rolled back", "")
		return err
	}
	_, err = CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "SUCCEEDED", "post-install doctor and generated projections passed", Version)
	return err
}
