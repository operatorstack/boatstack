package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// engagementHookMarker identifies the user-level engagement probe. The legacy
// marker is recognized only so updates can remove the superseded hook entry.
const engagementHookMarker = "engagement-probe"
const legacyAmbientHookMarker = "ambient-safety-hook"

func containsEngagementHook(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, engagementHookMarker) || strings.Contains(typed, legacyAmbientHookMarker)
	case []any:
		for _, item := range typed {
			if containsEngagementHook(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsEngagementHook(item) {
				return true
			}
		}
	}
	return false
}

// detachedHelperPath resolves the helper the engagement probe should invoke: the
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

func engagementDesiredEntry(host, event, helper string) map[string]any {
	entry := desiredHostHookForEvent(host, event)
	overrideHookCommands(entry, engagementProbeCommand(host, helper), engagementProbePowerShellCommand(host, helper))
	return entry
}

// Detached activation. A detached repository has no in-repo host hook, so the
// developer installs one user-level engagement probe per coding agent. The probe
// is inert unless a worktree-local active-delivery lease is valid.
//
// activation deliberately does NOT silently rewrite a developer's global host
// configuration. It emits the exact per-host config location and the precise
// Boatstack-owned snippet to add, so activation is transparent and never clobbers
// existing global hooks. The snippet is host-neutral in intent: every supported
// agent gets the same engagement probe, shaped for that agent's hook schema.

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

// engagementProbeCommand builds the shell command a user-level hook runs. Claude
// exposes the project directory as ${CLAUDE_PROJECT_DIR}; the others resolve it
// from Git at hook time.
func engagementProbeCommand(host, helper string) string {
	root := `ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"`
	if host == "claude" {
		root = `ROOT="${CLAUDE_PROJECT_DIR:-}"`
	}
	return fmt.Sprintf(`%s; [ -n "$ROOT" ] || exit 0; GIT_DIR="$(git -C "$ROOT" rev-parse --path-format=absolute --git-dir 2>/dev/null)" || exit 0; LEASE="$GIT_DIR/boatstack/engagement.json"; [ -f "$LEASE" ] && [ ! -L "$LEASE" ] || exit 0; BRANCH="$(git -C "$ROOT" branch --show-current 2>/dev/null)"; LEASE_BRANCH="$(sed -n 's/.*"branch"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$LEASE" | head -n 1)"; [ -n "$BRANCH" ] && [ "$BRANCH" = "$LEASE_BRANCH" ] || exit 0; exec %q engagement-probe --host %s --repo "$ROOT"`, root, helper, host)
}

func engagementProbePowerShellCommand(host, helper string) string {
	root := `(& git rev-parse --show-toplevel 2>$null)`
	if host == "claude" {
		root = `$env:CLAUDE_PROJECT_DIR`
	}
	return fmt.Sprintf(`$root = %s; if (-not $root) { exit 0 }; $gitDir = (& git -C $root rev-parse --path-format=absolute --git-dir 2>$null); if (-not $gitDir) { exit 0 }; $leasePath = Join-Path $gitDir 'boatstack/engagement.json'; if (-not (Test-Path -LiteralPath $leasePath -PathType Leaf)) { exit 0 }; $leaseInfo = Get-Item -LiteralPath $leasePath; if ($leaseInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) { exit 0 }; try { $lease = Get-Content -LiteralPath $leasePath -Raw | ConvertFrom-Json } catch { exit 0 }; $branch = (& git -C $root branch --show-current 2>$null); if (-not $branch -or $lease.branch -ne $branch) { exit 0 }; & %s engagement-probe --host %s --repo $root; exit $LASTEXITCODE`, root, powerShellPlanningWord(helper), host)
}

// engagementHostFragment shapes the engagement probe into a host's hook schema, reusing
// the embedded entry shape and overriding only the command so the guard runs from
// the external helper rather than an in-repo guard script.
func engagementHostFragment(host, helper string) ([]byte, error) {
	command := engagementProbeCommand(host, helper)
	commandWindows := engagementProbePowerShellCommand(host, helper)
	events := map[string]any{}
	for _, event := range hookEvents(host) {
		entry := desiredHostHookForEvent(host, event)
		overrideHookCommands(entry, command, commandWindows)
		events[event] = entry
	}
	return GeneratedJSON(map[string]any{"schema_version": 1, "host": host, "scope": "user", "events": events})
}

// overrideHookCommand replaces the command in a desired-hook entry (both the flat
// cursor form and the nested hooks[] form) with the engagement-probe command.
func overrideHookCommands(entry map[string]any, command, commandWindows string) {
	if _, ok := entry["command"]; ok {
		entry["command"] = command
		entry["commandWindows"] = commandWindows
	}
	if nested, ok := entry["hooks"].([]any); ok {
		for _, item := range nested {
			if hook, ok := item.(map[string]any); ok {
				hook["command"] = command
				hook["commandWindows"] = commandWindows
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
		snippet, fragErr := engagementHostFragment(host, helper)
		if fragErr != nil {
			return ActivationPlan{}, fragErr
		}
		plan.Hosts = append(plan.Hosts, HostActivation{
			Host:        host,
			ConfigPath:  configPath,
			Snippet:     string(snippet),
			Instruction: fmt.Sprintf("Merge the Boatstack engagement probe for %s into %s. It emits no output and applies no policy unless this worktree has a verified active delivery.", host, configPath),
		})
	}
	plan.Reason = "Add the developer-level engagement probe for each coding agent you use. Repository presence and attachment alone remain inert."
	return plan, nil
}

// EngagementHostResult is the per-host outcome of an install/uninstall.
type EngagementHostResult struct {
	Host       string `json:"host"`
	ConfigPath string `json:"config_path"`
	Action     string `json:"action"` // installed | removed | unchanged
}

// EngagementActivationResult is the deterministic outcome of installing or
// removing the developer-level engagement probe.
type EngagementActivationResult struct {
	SchemaVersion      int                    `json:"schema_version"`
	VerificationStatus string                 `json:"verification_status"` // VERIFIED | BLOCKED
	Mode               string                 `json:"mode,omitempty"`
	RepoRoot           string                 `json:"repo_root,omitempty"`
	Hosts              []EngagementHostResult `json:"hosts,omitempty"`
	Reason             string                 `json:"reason"`
}

func blockedEngagementActivation(reason string) EngagementActivationResult {
	return EngagementActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

func defaultActivationHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return []string{"cursor", "claude", "codex", "gemini"}
	}
	return hosts
}

// mergeEngagementHooks installs exactly one engagement probe per host event,
// preserving every unrelated entry (a user's own hooks, and any embedded guard)
// verbatim. Stripping then re-adding the single owned entry makes reinstall
// idempotent — the same input config yields the same output.
func mergeEngagementHooks(config map[string]any, host, helper string) error {
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
			if containsEngagementHook(entry) {
				continue
			}
			kept = append(kept, entry)
		}
		kept = append(kept, engagementDesiredEntry(host, event, helper))
		hooks[event] = kept
	}
	if host == "cursor" && config["version"] == nil {
		config["version"] = float64(1)
	}
	return nil
}

// removeEngagementHooks strips only Boatstack engagement probes, preserving all
// other entries. It reports whether anything changed.
func removeEngagementHooks(config map[string]any, host string) bool {
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
			if containsEngagementHook(entry) {
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

// InstallEngagementProbes merges the engagement probe into each agent's developer-level
// config. It requires the repository to be attached in detached mode. It preserves
// existing user hooks and is idempotent.
func InstallEngagementProbes(repoPath string, hosts []string) (EngagementActivationResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return blockedEngagementActivation(err.Error()), nil
	}
	_, ok, verifyErr := detachedContextFor(root)
	if verifyErr != nil {
		return blockedEngagementActivation(verifyErr.Error() + " Reattach before activating."), nil
	}
	if !ok {
		return blockedEngagementActivation("This repository is not attached in detached mode. Run `boatstack-helper attach --repo . --mode detached` first."), nil
	}
	helper := detachedHelperPath(root)
	result := EngagementActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED", Mode: string(SupervisionDetached), RepoRoot: root}
	for _, host := range defaultActivationHosts(hosts) {
		configPath, pathErr := userHostConfigPath(host)
		if pathErr != nil {
			continue
		}
		config, loadErr := loadHookConfig(configPath)
		if loadErr != nil {
			return blockedEngagementActivation(fmt.Sprintf("Boatstack could not read %s: %v", configPath, loadErr)), nil
		}
		before, _ := MarshalJSON(config)
		if err := mergeEngagementHooks(config, host, helper); err != nil {
			return blockedEngagementActivation(err.Error()), nil
		}
		after, marshalErr := MarshalJSON(config)
		if marshalErr != nil {
			return blockedEngagementActivation(marshalErr.Error()), nil
		}
		action := "unchanged"
		if string(before) != string(after) {
			if err := atomicWriteMode(configPath, after, 0o644); err != nil {
				return blockedEngagementActivation(err.Error()), nil
			}
			action = "installed"
		}
		result.Hosts = append(result.Hosts, EngagementHostResult{Host: host, ConfigPath: configPath, Action: action})
	}
	result.Reason = "Installed the Boatstack engagement probe. It applies policy only for a verified active delivery in the current worktree and branch."
	return result, nil
}

// RemoveEngagementProbes removes the engagement probe from each agent's developer-level
// config, preserving every other entry.
func RemoveEngagementProbes(repoPath string, hosts []string) (EngagementActivationResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return blockedEngagementActivation(err.Error()), nil
	}
	result := EngagementActivationResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED", RepoRoot: root}
	for _, host := range defaultActivationHosts(hosts) {
		configPath, pathErr := userHostConfigPath(host)
		if pathErr != nil {
			continue
		}
		if !fileExists(configPath) {
			result.Hosts = append(result.Hosts, EngagementHostResult{Host: host, ConfigPath: configPath, Action: "unchanged"})
			continue
		}
		config, loadErr := loadHookConfig(configPath)
		if loadErr != nil {
			return blockedEngagementActivation(fmt.Sprintf("Boatstack could not read %s: %v", configPath, loadErr)), nil
		}
		action := "unchanged"
		if removeEngagementHooks(config, host) {
			after, marshalErr := MarshalJSON(config)
			if marshalErr != nil {
				return blockedEngagementActivation(marshalErr.Error()), nil
			}
			if err := atomicWriteMode(configPath, after, 0o644); err != nil {
				return blockedEngagementActivation(err.Error()), nil
			}
			action = "removed"
		}
		result.Hosts = append(result.Hosts, EngagementHostResult{Host: host, ConfigPath: configPath, Action: action})
	}
	result.Reason = "Removed the Boatstack engagement probe from your developer-level host configuration."
	return result, nil
}
