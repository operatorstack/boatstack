package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ambientHookMarker identifies a user-level hook entry as Boatstack's ambient
// guard. It is distinct from the embedded hookCommandMarker (".product-loop/hooks/
// guard"): the ambient command runs the external helper's ambient-safety-hook and
// never names the in-repo guard, so ownership is detected by this substring.
const ambientHookMarker = "ambient-safety-hook"

// containsAmbientHook reports whether a hook value is (or contains) a Boatstack
// ambient-guard entry, by finding the ambient marker in any command string.
func containsAmbientHook(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, ambientHookMarker)
	case []any:
		for _, item := range typed {
			if containsAmbientHook(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsAmbientHook(item) {
				return true
			}
		}
	}
	return false
}

// detachedHelperPath resolves the helper the ambient hook should invoke: the
// external shared-runtime slot's binary when present (stable across helper
// relocation), else the running executable, else the bare name.
func detachedHelperPath(repo string) string {
	if binaryPath, _, err := sharedRuntimePaths(repo, Version, SourceCommit); err == nil && fileExists(binaryPath) {
		return binaryPath
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "boatstack-helper"
}

// ambientDesiredEntry is the per-event ambient-guard entry for a host, shaped like
// the embedded entry but running the external ambient command.
func ambientDesiredEntry(host, event, helper string) map[string]any {
	entry := desiredHostHookForEvent(host, event)
	overrideHookCommand(entry, ambientHookCommand(host, helper))
	return entry
}

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

	helper := detachedHelperPath(root)
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

// AmbientHostResult is the per-host outcome of an install/uninstall.
type AmbientHostResult struct {
	Host       string `json:"host"`
	ConfigPath string `json:"config_path"`
	Action     string `json:"action"` // installed | removed | unchanged
}

// AmbientActivationResult is the deterministic outcome of installing or removing
// the developer-level ambient guard.
type AmbientActivationResult struct {
	SchemaVersion      int                 `json:"schema_version"`
	VerificationStatus string              `json:"verification_status"` // VERIFIED | BLOCKED
	Mode               string              `json:"mode,omitempty"`
	RepoRoot           string              `json:"repo_root,omitempty"`
	Hosts              []AmbientHostResult `json:"hosts,omitempty"`
	Reason             string              `json:"reason"`
}

func blockedAmbient(reason string) AmbientActivationResult {
	return AmbientActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

func defaultActivationHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return []string{"cursor", "claude", "codex", "gemini"}
	}
	return hosts
}

// mergeAmbientHooks installs exactly one ambient-guard entry per host event,
// preserving every non-ambient entry (a user's own hooks, and any embedded guard)
// verbatim. Stripping then re-adding the single owned entry makes reinstall
// idempotent — the same input config yields the same output.
func mergeAmbientHooks(config map[string]any, host, helper string) error {
	hooks, ok := config["hooks"].(map[string]any)
	if config["hooks"] == nil {
		hooks = map[string]any{}
		config["hooks"] = hooks
	} else if !ok {
		return fmt.Errorf("host hook config has non-object hooks")
	}
	for _, event := range hookEvents(host) {
		var entries []any
		if existing := hooks[event]; existing != nil {
			list, listOK := existing.([]any)
			if !listOK {
				return fmt.Errorf("host hook event %s is not a list", event)
			}
			entries = list
		}
		kept := []any{}
		for _, entry := range entries {
			if containsAmbientHook(entry) {
				continue
			}
			kept = append(kept, entry)
		}
		kept = append(kept, ambientDesiredEntry(host, event, helper))
		hooks[event] = kept
	}
	if host == "cursor" && config["version"] == nil {
		config["version"] = float64(1)
	}
	return nil
}

// removeAmbientHooks strips only Boatstack ambient-guard entries, preserving all
// other entries. It reports whether anything changed.
func removeAmbientHooks(config map[string]any, host string) bool {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for _, event := range hookEvents(host) {
		existing, listOK := hooks[event].([]any)
		if !listOK {
			continue
		}
		kept := []any{}
		for _, entry := range existing {
			if containsAmbientHook(entry) {
				changed = true
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	return changed
}

// InstallAmbientHooks merges the ambient guard into each agent's developer-level
// config. It requires the repository to be attached in detached mode. It preserves
// existing user hooks and is idempotent.
func InstallAmbientHooks(repoPath string, hosts []string) (AmbientActivationResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return blockedAmbient(err.Error()), nil
	}
	_, ok, verifyErr := detachedContextFor(root)
	if verifyErr != nil {
		return blockedAmbient(verifyErr.Error() + " Reattach before activating."), nil
	}
	if !ok {
		return blockedAmbient("This repository is not attached in detached mode. Run `boatstack-helper attach --repo . --mode detached` first."), nil
	}
	helper := detachedHelperPath(root)
	result := AmbientActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED", Mode: string(SupervisionDetached), RepoRoot: root}
	for _, host := range defaultActivationHosts(hosts) {
		configPath, pathErr := userHostConfigPath(host)
		if pathErr != nil {
			continue
		}
		config, loadErr := loadHookConfig(configPath)
		if loadErr != nil {
			return blockedAmbient(fmt.Sprintf("Boatstack could not read %s: %v", configPath, loadErr)), nil
		}
		before, _ := MarshalJSON(config)
		if err := mergeAmbientHooks(config, host, helper); err != nil {
			return blockedAmbient(err.Error()), nil
		}
		after, marshalErr := MarshalJSON(config)
		if marshalErr != nil {
			return blockedAmbient(marshalErr.Error()), nil
		}
		action := "unchanged"
		if string(before) != string(after) {
			if err := atomicWriteMode(configPath, after, 0o644); err != nil {
				return blockedAmbient(err.Error()), nil
			}
			action = "installed"
		}
		result.Hosts = append(result.Hosts, AmbientHostResult{Host: host, ConfigPath: configPath, Action: action})
	}
	result.Reason = "Installed the Boatstack ambient guard into your developer-level host configuration. It enforces Boatstack only on attached repositories and leaves all other repositories uncontrolled."
	return result, nil
}

// RemoveAmbientHooks removes the ambient guard from each agent's developer-level
// config, preserving every other entry.
func RemoveAmbientHooks(repoPath string, hosts []string) (AmbientActivationResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return blockedAmbient(err.Error()), nil
	}
	result := AmbientActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED", RepoRoot: root}
	for _, host := range defaultActivationHosts(hosts) {
		configPath, pathErr := userHostConfigPath(host)
		if pathErr != nil {
			continue
		}
		if !fileExists(configPath) {
			result.Hosts = append(result.Hosts, AmbientHostResult{Host: host, ConfigPath: configPath, Action: "unchanged"})
			continue
		}
		config, loadErr := loadHookConfig(configPath)
		if loadErr != nil {
			return blockedAmbient(fmt.Sprintf("Boatstack could not read %s: %v", configPath, loadErr)), nil
		}
		action := "unchanged"
		if removeAmbientHooks(config, host) {
			after, marshalErr := MarshalJSON(config)
			if marshalErr != nil {
				return blockedAmbient(marshalErr.Error()), nil
			}
			if err := atomicWriteMode(configPath, after, 0o644); err != nil {
				return blockedAmbient(err.Error()), nil
			}
			action = "removed"
		}
		result.Hosts = append(result.Hosts, AmbientHostResult{Host: host, ConfigPath: configPath, Action: action})
	}
	result.Reason = "Removed the Boatstack ambient guard from your developer-level host configuration."
	return result, nil
}
