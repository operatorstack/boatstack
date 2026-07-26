package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
)

// Detached activation. A detached repository has no in-repo host hook, so the
// developer installs one user-level (developer-scoped) hook per coding agent. That
// hook runs Boatstack's ambient guard, which enforces policy only on attached
// repositories and no-ops everywhere else (RepositoryIsManaged / AmbientHookDecision).
//
// activation deliberately does NOT silently rewrite a developer's global host
// configuration. It emits the exact per-host config location and the precise
// Boatstack-owned snippet to add, so activation is transparent and never clobbers
// existing global hooks. The snippet is host-neutral in intent: every supported
// agent gets the same ambient guard, shaped for that agent's hook schema.

// HostActivation is the activation instruction for one coding agent.
type HostActivation struct {
	Host        string `json:"host"`
	ConfigPath  string `json:"config_path"`
	Snippet     string `json:"snippet"`
	Instruction string `json:"instruction"`
}

// ActivationPlan is the set of per-host activation instructions for a repository.
type ActivationPlan struct {
	SchemaVersion int              `json:"schema_version"`
	Mode          string           `json:"mode"`
	Attached      bool             `json:"attached"`
	RepoRoot      string           `json:"repo_root"`
	HelperPath    string           `json:"helper_path"`
	Hosts         []HostActivation `json:"hosts"`
	Reason        string           `json:"reason"`
}

// userHostConfigPath returns the developer-level (not repo-level) config path a
// host reads across all projects. BOATSTACK_USER_CONFIG_ROOT overrides the base for
// tests and for a launcher that keeps host state external.
func userHostConfigPath(host string) (string, error) {
	base := os.Getenv("BOATSTACK_USER_CONFIG_ROOT")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = home
	}
	switch host {
	case "cursor":
		return filepath.Join(base, ".cursor", "hooks.json"), nil
	case "claude":
		return filepath.Join(base, ".claude", "settings.json"), nil
	case "codex":
		return filepath.Join(base, ".codex", "hooks.json"), nil
	case "gemini":
		return filepath.Join(base, ".gemini", "settings.json"), nil
	default:
		return "", fmt.Errorf("unsupported host %q", host)
	}
}

// ambientHookCommand builds the shell command a user-level hook runs: the absolute
// helper binary invoking the ambient guard for the current repository. claude
// exposes the project directory as ${CLAUDE_PROJECT_DIR}; the others resolve it
// from Git at hook time.
func ambientHookCommand(host, helper string) string {
	if host == "claude" {
		return fmt.Sprintf(`%q ambient-safety-hook --host claude --repo "${CLAUDE_PROJECT_DIR}"`, helper)
	}
	return fmt.Sprintf(`%q ambient-safety-hook --host %s --repo "$(git rev-parse --show-toplevel)"`, helper, host)
}

// ambientHostFragment shapes the ambient guard into a host's hook schema, reusing
// the embedded entry shape and overriding only the command so the guard runs from
// the external helper rather than an in-repo guard script.
func ambientHostFragment(host, helper string) ([]byte, error) {
	command := ambientHookCommand(host, helper)
	events := map[string]any{}
	for _, event := range hookEvents(host) {
		entry := desiredHostHookForEvent(host, event)
		overrideHookCommand(entry, command)
		events[event] = entry
	}
	return GeneratedJSON(map[string]any{"schema_version": 1, "host": host, "scope": "user", "events": events})
}

// overrideHookCommand replaces the command in a desired-hook entry (both the flat
// cursor form and the nested hooks[] form) with the ambient command.
func overrideHookCommand(entry map[string]any, command string) {
	if _, ok := entry["command"]; ok {
		entry["command"] = command
		delete(entry, "commandWindows")
	}
	if nested, ok := entry["hooks"].([]any); ok {
		for _, item := range nested {
			if hook, ok := item.(map[string]any); ok {
				hook["command"] = command
				delete(hook, "commandWindows")
			}
		}
	}
}

// DetachedActivationPlan returns the per-host activation instructions for a
// repository. It is read-only. It requires the repository to be attached in
// detached mode (an unattached repository has nothing to activate).
func DetachedActivationPlan(repoPath string, hosts []string) (ActivationPlan, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return ActivationPlan{}, err
	}
	plan := ActivationPlan{SchemaVersion: detachedSchemaVersion, Mode: string(SupervisionEmbedded), RepoRoot: root}
	ctx, ok, verifyErr := detachedContextFor(root)
	if verifyErr != nil {
		plan.Mode = string(SupervisionDetached)
		plan.Attached = true
		plan.Reason = verifyErr.Error()
		return plan, nil
	}
	if !ok {
		plan.Reason = "This repository is not attached in detached mode. Run `boatstack-helper attach --repo . --mode detached` first."
		return plan, nil
	}
	plan.Mode = string(SupervisionDetached)
	plan.Attached = true
	_ = ctx

	helper, err := os.Executable()
	if err != nil || helper == "" {
		helper = "boatstack-helper"
	}
	plan.HelperPath = helper

	if len(hosts) == 0 {
		hosts = []string{"cursor", "claude", "codex", "gemini"}
	}
	for _, host := range hosts {
		configPath, pathErr := userHostConfigPath(host)
		if pathErr != nil {
			continue
		}
		snippet, fragErr := ambientHostFragment(host, helper)
		if fragErr != nil {
			return ActivationPlan{}, fragErr
		}
		plan.Hosts = append(plan.Hosts, HostActivation{
			Host:        host,
			ConfigPath:  configPath,
			Snippet:     string(snippet),
			Instruction: fmt.Sprintf("Merge the Boatstack ambient guard for %s into %s (developer-level, applies to every repository; it enforces Boatstack only on attached repositories).", host, configPath),
		})
	}
	plan.Reason = "Add the developer-level ambient guard for each coding agent you use. It no-ops on repositories you have not attached."
	return plan, nil
}
