package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FrontendStack are the detected facts about a repository's frontend tooling. It
// is the input to a context-aware provisioning guide: it is discovered, never
// imposed, so a repository without a frontend framework is reported honestly
// rather than forced to adopt one.
type FrontendStack struct {
	Framework      string `json:"framework"`       // next | vite | react | vue | svelte | angular | none
	PackageManager string `json:"package_manager"` // npm | pnpm | yarn | bun | none
	DevCommand     string `json:"dev_command,omitempty"`
	HasPlaywright  bool   `json:"has_playwright"`
	HasCypress     bool   `json:"has_cypress"`
	HasStorybook   bool   `json:"has_storybook"`
}

// detectFrontendStack reads package.json, lockfiles, and known config files to
// report a repository's frontend facts. It mirrors detectTestCommand's idiom:
// evidence-driven, no network, and silent (all-zero) when nothing is found.
func detectFrontendStack(repo string) FrontendStack {
	stack := FrontendStack{Framework: "none", PackageManager: "none"}
	var manifest struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	value, err := os.ReadFile(filepath.Join(repo, "package.json"))
	if err != nil || json.Unmarshal(value, &manifest) != nil {
		return stack
	}

	stack.PackageManager = detectPackageManager(repo)
	dependency := func(name string) bool {
		_, direct := manifest.Dependencies[name]
		_, dev := manifest.DevDependencies[name]
		return direct || dev
	}
	switch {
	case dependency("next"):
		stack.Framework = "next"
	case dependency("@angular/core"):
		stack.Framework = "angular"
	case dependency("svelte"):
		stack.Framework = "svelte"
	case dependency("vue"):
		stack.Framework = "vue"
	case dependency("vite"):
		stack.Framework = "vite"
	case dependency("react"):
		stack.Framework = "react"
	}
	for _, name := range []string{"dev", "start", "serve"} {
		if strings.TrimSpace(manifest.Scripts[name]) != "" {
			stack.DevCommand = packageManagerRun(stack.PackageManager, name)
			break
		}
	}
	stack.HasPlaywright = dependency("@playwright/test") || dependency("playwright") ||
		fileExists(filepath.Join(repo, "playwright.config.ts")) || fileExists(filepath.Join(repo, "playwright.config.js"))
	stack.HasCypress = dependency("cypress") ||
		fileExists(filepath.Join(repo, "cypress.config.ts")) || fileExists(filepath.Join(repo, "cypress.config.js"))
	stack.HasStorybook = dependency("storybook") || dependency("@storybook/react") ||
		dirExists(filepath.Join(repo, ".storybook"))
	return stack
}

func detectPackageManager(repo string) string {
	switch {
	case fileExists(filepath.Join(repo, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(repo, "yarn.lock")):
		return "yarn"
	case fileExists(filepath.Join(repo, "bun.lock")), fileExists(filepath.Join(repo, "bun.lockb")):
		return "bun"
	default:
		return "npm"
	}
}

func packageManagerRun(manager, script string) string {
	if manager == "" || manager == "none" {
		manager = "npm"
	}
	return manager + " run " + script
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ProvisionGuide is the context-aware answer to "this repository cannot yet
// produce <capability> evidence — how do we help?". Boatstack ships the contract
// the in-repository harness must satisfy plus stack-tailored steps; the harness
// itself is authored in the user's repository, never shipped by Boatstack.
type ProvisionGuide struct {
	Capability       string        `json:"capability"`
	Tier             string        `json:"tier"` // available | provision | unsupported
	Available        bool          `json:"available"`
	ResolvedCommand  string        `json:"resolved_command,omitempty"`
	Stack            FrontendStack `json:"stack"`
	SuggestedAlias   string        `json:"suggested_alias,omitempty"`
	SuggestedCommand string        `json:"suggested_command,omitempty"`
	Contract         []string      `json:"contract"`
	Steps            []string      `json:"steps"`
}

// captureContract is the framework-agnostic contract every capture harness must
// satisfy, expressed as the environment the harness is invoked with. It mirrors
// execCaptureRunner so the guide and the runtime never drift.
var captureContract = []string{
	"Boatstack invokes the registered command once per scenario.",
	"BOATSTACK_CAPTURE_CAPABILITY — the capability being captured (e.g. visual).",
	"BOATSTACK_CAPTURE_SCENARIO_ID — the plan scenario id.",
	"BOATSTACK_CAPTURE_ENTRY — the scenario entry point (route or component).",
	"BOATSTACK_CAPTURE_STATE — the scenario state to render.",
	"BOATSTACK_CAPTURE_VIEWPORT — the required viewport, e.g. 1440x900.",
	"BOATSTACK_CAPTURE_OUTPUT — the absolute path the harness must write exactly one PNG to.",
	"Render fixture or mock data only; never production secrets or PII.",
}

// CapabilityProvisionGuide composes capability detection with frontend-stack
// facts to produce a context-aware provisioning guide for a repository.
func CapabilityProvisionGuide(repo, name string) (ProvisionGuide, error) {
	resolved, err := ResolveRepository(repo)
	if err != nil {
		return ProvisionGuide{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = "visual"
	}
	capability, ok := LookupCapability(name)
	if !ok {
		return ProvisionGuide{}, fmt.Errorf("unknown evidence capability %q", name)
	}
	config, _, err := LoadConfig(filepath.Join(resolved, ".product-loop", "project.json"))
	if err != nil {
		return ProvisionGuide{}, fmt.Errorf("provisioning requires a valid Boatstack project configuration: %w", err)
	}
	resolution, err := ResolveCapability(name, config)
	if err != nil {
		return ProvisionGuide{}, err
	}

	guide := ProvisionGuide{Capability: capability.Name, Contract: captureContract, Stack: detectFrontendStack(resolved)}
	if resolution.Kind == "repository-command" {
		guide.Tier = "available"
		guide.Available = true
		guide.ResolvedCommand = resolution.Command
		guide.Steps = []string{
			fmt.Sprintf("%s evidence is already wired: project.commands resolves %q.", capability.Name, resolution.Command),
			"Run capture-evidence to generate evidence for the active feature.",
		}
		return guide, nil
	}

	guide.SuggestedAlias = capability.Name
	if guide.Stack.Framework == "none" {
		guide.Tier = "unsupported"
		guide.Steps = []string{
			"No frontend framework was detected, so visual evidence cannot be captured yet.",
			"Boatstack never adds a framework for you: if this change is user-visible, set one up (or opt out by recording the scenario as not_relevant).",
			"Once a framework and dev command exist, re-run provisioning to get a stack-tailored harness guide.",
		}
		return guide, nil
	}

	guide.Tier = "provision"
	guide.SuggestedCommand = packageManagerRun(guide.Stack.PackageManager, "capture:"+capability.Name)
	guide.Steps = provisionSteps(guide.Stack, guide.SuggestedAlias, guide.SuggestedCommand)
	return guide, nil
}

func provisionSteps(stack FrontendStack, alias, suggestedCommand string) []string {
	steps := []string{
		fmt.Sprintf("Detected a %s app using %s.", stack.Framework, stack.PackageManager),
	}
	if stack.HasPlaywright {
		steps = append(steps, "Reuse the existing Playwright setup: add a script that reads the BOATSTACK_CAPTURE_* env and screenshots the scenario to BOATSTACK_CAPTURE_OUTPUT.")
	} else {
		steps = append(steps, "Add a headless rasterizer (Playwright is the least-effort fit) that renders one scenario and writes it to BOATSTACK_CAPTURE_OUTPUT.")
	}
	if stack.HasStorybook {
		steps = append(steps, "Storybook is present: map each scenario id to a story so the harness renders isolated component state with fixture data.")
	}
	steps = append(steps,
		fmt.Sprintf("Expose the harness as a project command, then register it: capability-register --capability %s --command \"%s\".", alias, suggestedCommand),
		"Confirm registration with provision-capability; its tier should flip to available. Then run capture-evidence.",
	)
	return steps
}

// RegisteredCapability reports the outcome of registering a capability command.
type RegisteredCapability struct {
	Capability string `json:"capability"`
	Alias      string `json:"alias"`
	Command    string `json:"command"`
	Source     string `json:"source"` // source-and-export | generated-only
}

// RegisterCapabilityCommand records a repository-owned command for a capability.
// When a canonical .boatstack-project.json source exists it mutates the source
// and regenerates the full export so the source and every generated file stay in
// sync; otherwise it round-trips the generated project.json alone, matching the
// IgnoreDelivery idiom.
func RegisterCapabilityCommand(repo, name, command string) (RegisteredCapability, error) {
	resolved, err := ResolveRepository(repo)
	if err != nil {
		return RegisteredCapability{}, err
	}
	capability, ok := LookupCapability(strings.TrimSpace(name))
	if !ok {
		return RegisteredCapability{}, fmt.Errorf("unknown evidence capability %q", name)
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return RegisteredCapability{}, fmt.Errorf("capability-register requires a non-empty --command")
	}

	sourcePath := filepath.Join(resolved, ".boatstack-project.json")
	if fileExists(sourcePath) {
		config, _, err := LoadConfig(sourcePath)
		if err != nil {
			return RegisteredCapability{}, err
		}
		setCapabilityCommand(&config, capability.Name, command)
		if err := ValidateConfig(config); err != nil {
			return RegisteredCapability{}, err
		}
		rawConfig, err := MarshalJSON(config)
		if err != nil {
			return RegisteredCapability{}, err
		}
		bundle, err := BuildExportBundle(sourcePath, config, rawConfig, "boatstack")
		if err != nil {
			return RegisteredCapability{}, err
		}
		if err := WriteExport(resolved, bundle.Files); err != nil {
			return RegisteredCapability{}, err
		}
		if err := atomicWriteMode(sourcePath, rawConfig, 0o644); err != nil {
			return RegisteredCapability{}, err
		}
		return RegisteredCapability{Capability: capability.Name, Alias: capability.Name, Command: command, Source: "source-and-export"}, nil
	}

	configPath := filepath.Join(resolved, ".product-loop", "project.json")
	config, _, err := LoadConfig(configPath)
	if err != nil {
		return RegisteredCapability{}, err
	}
	setCapabilityCommand(&config, capability.Name, command)
	if err := ValidateConfig(config); err != nil {
		return RegisteredCapability{}, err
	}
	value, err := GeneratedJSON(config)
	if err != nil {
		return RegisteredCapability{}, err
	}
	if err := atomicWriteMode(configPath, value, 0o644); err != nil {
		return RegisteredCapability{}, err
	}
	return RegisteredCapability{Capability: capability.Name, Alias: capability.Name, Command: command, Source: "generated-only"}, nil
}

func setCapabilityCommand(config *ProjectConfig, alias, command string) {
	if config.Project.Commands == nil {
		config.Project.Commands = map[string]string{}
	}
	config.Project.Commands[alias] = command
}
