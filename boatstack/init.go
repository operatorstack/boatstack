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
	Input             io.Reader
	Output            io.Writer
}

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
		Workflow: Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true},
		Adapters: []string{"cursor", "claude", "codex", "github"},
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
	name := "boatstack-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join(repo, ".product-loop", "bin", name)
	if err := writeFile(destination, value, 0o755); err != nil {
		return "", "", err
	}
	return destination, SHA256Bytes(value), nil
}

func writeInstallLock(repo, binaryPath, binaryHash string, integrations map[string]IntegrationState) error {
	lock := map[string]any{
		"schema_version":           1,
		"boatstack_version":        Version,
		"source_commit":            SourceCommit,
		"platform":                 runtime.GOOS + "/" + runtime.GOARCH,
		"binary_path":              filepath.ToSlash(strings.TrimPrefix(binaryPath, repo+string(filepath.Separator))),
		"binary_sha256":            binaryHash,
		"release_checksums_sha256": ChecksumsSHA256,
		"integrations":             integrations,
	}
	value, err := MarshalJSON(lock)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"), value, 0o644)
}

func RunInit(options InitOptions) error {
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
	var config ProjectConfig
	var rawConfig []byte
	if fileExists(configPath) {
		config, rawConfig, err = LoadConfig(configPath)
		if err != nil {
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

	detected := DetectHosts(repo)
	if len(detected) == 0 {
		fmt.Fprintln(options.Output, "Detected host signals: none; installing all thin adapters for portability.")
	} else {
		fmt.Fprintf(options.Output, "Detected host signals: %s. Installing portable Cursor, Claude, Codex, and GitHub adapters.\n", strings.Join(detected, ", "))
	}

	choice := options.IntegrationChoice
	if choice == "" {
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
	wantGStack, wantSpecKit, err := RequestedIntegrations(choice)
	if err != nil {
		return err
	}
	config.Integrations = map[string]IntegrationState{
		"gstack":   {Requested: wantGStack, Version: GStackRef},
		"spec-kit": {Requested: wantSpecKit, Version: SpecKitVersion},
	}
	rawConfig, err = MarshalJSON(config)
	if err != nil {
		return err
	}
	bundle, err := BuildExportBundle(configPath, config, rawConfig, "boatstack")
	if err != nil {
		return err
	}
	if problems := ExportCollisions(repo, bundle.Files); len(problems) > 0 {
		return fmt.Errorf("refusing to overwrite user-owned files: %s", strings.Join(problems, ", "))
	}
	paths := sortedKeys(bundle.Files)
	fmt.Fprintf(options.Output, "\nBoatstack will generate %d paths:\n", len(paths))
	for _, path := range paths {
		fmt.Fprintln(options.Output, "  "+path)
	}
	if !fileExists(configPath) {
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
	if err := os.WriteFile(configPath, rawConfig, 0o644); err != nil {
		return err
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		return err
	}
	binaryPath, binaryHash, err := copyHelper(options.BinaryPath, repo)
	if err != nil {
		return err
	}
	states, err := InstallIntegrations(choice, repo, config.Adapters)
	if err != nil {
		return err
	}
	if err := writeInstallLock(repo, binaryPath, binaryHash, states); err != nil {
		return err
	}
	if err := CheckExport(repo, bundle.Files); err != nil {
		return err
	}
	fmt.Fprintln(options.Output, "\nPASS: Boatstack core installed without a language runtime.")
	keys := sortedKeys(states)
	for _, name := range keys {
		state := states[name]
		fmt.Fprintf(options.Output, "  %s: %s — %s\n", name, state.Status, state.Detail)
	}
	fmt.Fprintln(options.Output, "\nStart in Cursor, Codex, or Claude Plan mode:")
	fmt.Fprintln(options.Output, "  1. Describe the product change and save the host plan (use .product-loop/intake/ if the host exposes no path).")
	fmt.Fprintln(options.Output, "  2. Run /auto-plan")
	return nil
}
