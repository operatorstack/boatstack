package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	lookPath    = exec.LookPath
	homeDir     = os.UserHomeDir
	runExternal = func(directory, name string, arguments ...string) error {
		command := exec.Command(name, arguments...)
		command.Dir = directory
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.Stdin = os.Stdin
		return command.Run()
	}
	externalOutput = func(directory, name string, arguments ...string) (string, error) {
		command := exec.Command(name, arguments...)
		command.Dir = directory
		value, err := command.Output()
		return strings.TrimSpace(string(value)), err
	}
)

func RequestedIntegrations(choice string) (bool, bool, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "core", "core-only", "none":
		return false, false, nil
	case "gstack":
		return true, false, nil
	case "spec-kit", "speckit":
		return false, true, nil
	case "both":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("integrations must be core, gstack, spec-kit, or both")
	}
}

func installGStack(adapters []string) IntegrationState {
	state := IntegrationState{Requested: true, Status: "partial", Version: GStackRef}
	for _, prerequisite := range []string{"git", "bun", "bash"} {
		if _, err := lookPath(prerequisite); err != nil {
			state.Detail = fmt.Sprintf("gstack skipped: %s is required by its official installer", prerequisite)
			return state
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := lookPath("node"); err != nil {
			state.Detail = "gstack skipped: native Windows requires Node plus Bun and Git Bash or WSL"
			return state
		}
	}
	home, err := homeDir()
	if err != nil {
		state.Detail = "gstack skipped: cannot resolve the user home directory"
		return state
	}
	installRoot := filepath.Join(home, ".claude", "skills", "gstack")
	_, claudeDetected := lookPath("claude")
	_, codexDetected := lookPath("codex")
	if claudeDetected != nil && codexDetected == nil {
		installRoot = filepath.Join(home, ".codex", "skills", "gstack")
	}
	if info, statErr := os.Stat(installRoot); statErr == nil && info.IsDir() {
		if _, gitErr := os.Stat(filepath.Join(installRoot, ".git")); gitErr != nil {
			state.Detail = "gstack skipped: its target directory exists but is not a Git checkout"
			return state
		}
		if err := runExternal(installRoot, "git", "fetch", "--depth", "1", "origin", GStackRef); err != nil {
			state.Detail = "gstack update failed while fetching the pinned revision"
			return state
		}
		if err := runExternal(installRoot, "git", "checkout", "--detach", GStackRef); err != nil {
			state.Detail = "gstack update failed while checking out the pinned revision"
			return state
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(installRoot), 0o755); err != nil {
			state.Detail = "gstack skipped: cannot create its skill directory"
			return state
		}
		if err := runExternal("", "git", "clone", "--no-checkout", "https://github.com/garrytan/gstack.git", installRoot); err != nil {
			state.Detail = "gstack clone failed"
			return state
		}
		if err := runExternal(installRoot, "git", "fetch", "--depth", "1", "origin", GStackRef); err != nil {
			state.Detail = "gstack clone could not fetch the pinned revision"
			return state
		}
		if err := runExternal(installRoot, "git", "checkout", "--detach", GStackRef); err != nil {
			state.Detail = "gstack clone could not check out the pinned revision"
			return state
		}
	}

	hosts := []string{}
	if contains(adapters, "claude") {
		if _, err := lookPath("claude"); err == nil {
			hosts = append(hosts, "claude")
		}
	}
	if contains(adapters, "codex") {
		if _, err := lookPath("codex"); err == nil {
			hosts = append(hosts, "codex")
		}
	}
	if len(hosts) == 0 {
		state.Detail = "gstack source installed, but no officially supported Claude or Codex host was detected; Cursor remains available through Boatstack core"
		return state
	}
	for _, host := range hosts {
		if err := runExternal(installRoot, "bash", "setup", "--host", host, "--prefix"); err != nil {
			state.Detail = fmt.Sprintf("gstack setup failed for %s; Boatstack core remains installed", host)
			return state
		}
	}
	state.Status = "installed"
	state.Detail = "gstack installed with namespaced /gstack-* commands"
	return state
}

func specKitExecutable() (string, error) {
	if path, err := lookPath("specify"); err == nil {
		return path, nil
	}
	uv, err := lookPath("uv")
	if err != nil {
		return "", fmt.Errorf("uv is required to install the optional Spec Kit integration")
	}
	if err := runExternal("", uv, "tool", "install", "specify-cli", "--from", "git+https://github.com/github/spec-kit.git@"+SpecKitVersion); err != nil {
		return "", fmt.Errorf("Spec Kit installation failed: %w", err)
	}
	if path, err := lookPath("specify"); err == nil {
		return path, nil
	}
	binDirectory, err := externalOutput("", uv, "tool", "dir", "--bin")
	if err != nil || binDirectory == "" {
		return "", fmt.Errorf("Spec Kit installed but specify is not available on PATH")
	}
	name := "specify"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(binDirectory, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("Spec Kit installed but specify is not available at %s", path)
	}
	return path, nil
}

func specKitHosts(adapters []string) []string {
	hosts := []string{}
	for _, pair := range []struct{ adapter, integration string }{
		{"cursor", "cursor-agent"}, {"codex", "codex"}, {"claude", "claude"},
	} {
		if contains(adapters, pair.adapter) {
			hosts = append(hosts, pair.integration)
		}
	}
	return hosts
}

func installSpecKit(repo string, adapters []string) IntegrationState {
	state := IntegrationState{Requested: true, Status: "partial", Version: SpecKitVersion}
	specify, err := specKitExecutable()
	if err != nil {
		state.Detail = err.Error()
		return state
	}
	hosts := specKitHosts(adapters)
	if len(hosts) == 0 {
		state.Detail = "Spec Kit installed, but no Cursor, Codex, or Claude adapter was selected"
		return state
	}
	if _, err := os.Stat(filepath.Join(repo, ".specify")); os.IsNotExist(err) {
		script := "sh"
		if runtime.GOOS == "windows" {
			script = "ps"
		}
		arguments := []string{"init", "--here", "--force", "--integration", hosts[0], "--ignore-agent-tools", "--script", script}
		if hosts[0] == "codex" {
			arguments = append(arguments, "--integration-options=--skills")
		}
		if err := runExternal(repo, specify, arguments...); err != nil {
			state.Detail = "Spec Kit project initialization failed; Boatstack core remains installed"
			return state
		}
		hosts = hosts[1:]
	}
	for _, host := range hosts {
		if err := runExternal(repo, specify, "integration", "install", host); err != nil {
			state.Detail = fmt.Sprintf("Spec Kit installed, but its %s integration failed", host)
			return state
		}
	}
	state.Status = "installed"
	state.Detail = "Spec Kit installed as an artifact generator; Boatstack retains approval and build authority"
	return state
}

func InstallIntegrations(choice, repo string, adapters []string) (map[string]IntegrationState, error) {
	wantGStack, wantSpecKit, err := RequestedIntegrations(choice)
	if err != nil {
		return nil, err
	}
	states := map[string]IntegrationState{
		"gstack":   {Requested: false, Status: "not_selected", Version: GStackRef},
		"spec-kit": {Requested: false, Status: "not_selected", Version: SpecKitVersion},
	}
	if wantGStack {
		states["gstack"] = installGStack(adapters)
	}
	if wantSpecKit {
		states["spec-kit"] = installSpecKit(repo, adapters)
	}
	return states, nil
}
