package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	RepairCurrent      = "CURRENT"
	RepairOwnedStale   = "OWNED_STALE"
	RepairOwnedDrifted = "OWNED_DRIFTED"
	RepairUserOwned    = "USER_OWNED"
	RepairAmbiguous    = "AMBIGUOUS"
	RepairUnsafe       = "UNSAFE"
)

type InstallationRepairItem struct {
	Path           string `json:"path"`
	Host           string `json:"host,omitempty"`
	Event          string `json:"event,omitempty"`
	Classification string `json:"classification"`
	Reason         string `json:"reason"`
	CurrentSHA256  string `json:"current_sha256,omitempty"`
}

func currentFileHash(path string) string {
	hash, err := SHA256File(path)
	if err != nil {
		return ""
	}
	return hash
}

func unsafeRepairPath(repo, path string) bool {
	if err := rejectSymlinkComponents(repo, path); err != nil {
		return true
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

type InstallationRepairResult struct {
	SchemaVersion         int                         `json:"schema_version"`
	VerificationStatus    string                      `json:"verification_status"`
	InstalledVersion      string                      `json:"installed_version,omitempty"`
	TargetVersion         string                      `json:"target_version"`
	Direction             string                      `json:"direction"`
	HeadBranch            string                      `json:"head_branch"`
	StartingHeadCommit    string                      `json:"starting_head_commit"`
	Items                 []InstallationRepairItem    `json:"items"`
	PreservedIntegrations map[string]IntegrationState `json:"preserved_integrations,omitempty"`
	PackageFingerprint    string                      `json:"package_fingerprint"`
	BackupPath            string                      `json:"backup_path,omitempty"`
	Blockers              []string                    `json:"blockers,omitempty"`
	NextOperation         string                      `json:"next_operation"`
}

func installedVersion(repo string) (string, error) {
	for _, candidate := range []string{
		filepath.Join(repo, ".product-loop", "bin", "install.lock.json"),
		filepath.Join(repo, ".product-loop", "generated.lock.json"),
	} {
		value, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var identity struct {
			BoatstackVersion string `json:"boatstack_version"`
		}
		if json.Unmarshal(value, &identity) == nil && strings.TrimSpace(identity.BoatstackVersion) != "" {
			return normalizedVersion(identity.BoatstackVersion)
		}
	}
	return "", fmt.Errorf("installed Boatstack version cannot be established from owned provenance")
}

func updateDirection(installed, target string) (string, error) {
	comparison, err := compareVersions(installed, target)
	if err != nil {
		return "", err
	}
	switch {
	case comparison < 0:
		return "UPGRADE", nil
	case comparison > 0:
		return "DOWNGRADE", nil
	default:
		return "SAME_VERSION", nil
	}
}

func sameJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}

func decodeInstalledHookEvents(path string, value []byte, host string) (map[string]any, error) {
	var fragment struct {
		SchemaVersion int            `json:"schema_version"`
		Host          string         `json:"host"`
		Events        map[string]any `json:"events"`
	}
	if err := DecodeJSON("load installed host hook fragment", path, value, &fragment); err != nil {
		return nil, err
	}
	if fragment.SchemaVersion != 1 || fragment.Host != host || len(fragment.Events) == 0 {
		return nil, fmt.Errorf("invalid installed %s hook fragment identity", host)
	}
	return fragment.Events, nil
}

func loadInstalledHookEvents(repo, host string) (map[string]any, error) {
	path := filepath.Join(repo, ".product-loop", "hooks", host+".fragment.json")
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeInstalledHookEvents(path, value, host)
}

func loadCommittedInstalledHookEvents(repo, host string) (map[string]any, error) {
	relative := filepath.ToSlash(filepath.Join(".product-loop", "hooks", host+".fragment.json"))
	expected, ok := previousFiles(repo)[relative]
	if !ok {
		return nil, fmt.Errorf("installed %s hook fragment has no generated provenance", host)
	}
	value, err := exec.Command("git", "-C", repo, "show", "HEAD:"+relative).Output()
	if err != nil || SHA256Bytes(value) != expected {
		return nil, fmt.Errorf("committed %s hook fragment does not match generated provenance", host)
	}
	return decodeInstalledHookEvents("HEAD:"+relative, value, host)
}

func classifyHookState(repo, host string) []InstallationRepairItem {
	path := hostHookConfigPath(repo, host)
	currentHash := currentFileHash(path)
	relative, _ := filepath.Rel(repo, path)
	relative = filepath.ToSlash(relative)
	if unsafeRepairPath(repo, path) {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "host configuration uses a symlinked path"}}
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		_, fragmentErr := loadInstalledHookEvents(repo, host)
		if fragmentErr != nil {
			_, fragmentErr = loadCommittedInstalledHookEvents(repo, host)
		}
		if fragmentErr == nil {
			return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairOwnedStale, Reason: "missing installed host configuration can be reconstructed from its ownership fragment"}}
		}
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairAmbiguous, Reason: "host configuration and its ownership fragment are both missing"}}
	} else if err != nil {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "host configuration path cannot be inspected"}}
	}
	config, configErr := loadHookConfig(path)
	if configErr != nil {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "host configuration is not valid JSON", CurrentSHA256: currentHash}}
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "host hooks field is not an object", CurrentSHA256: currentHash}}
	}
	installed, installedErr := loadInstalledHookEvents(repo, host)
	if installedErr != nil {
		installed, installedErr = loadCommittedInstalledHookEvents(repo, host)
	}
	if installedErr != nil {
		exactTarget := true
		for _, event := range hookEvents(host) {
			entries, entriesOK := hooks[event].([]any)
			matches := 0
			if entriesOK {
				for _, entry := range entries {
					if containsBoatstackHook(entry) {
						matches++
						exactTarget = exactTarget && sameJSON(entry, desiredHostHookForEvent(host, event))
					}
				}
			}
			exactTarget = exactTarget && entriesOK && matches == 1
		}
		for event, raw := range hooks {
			if !contains(hookEvents(host), event) && containsBoatstackHook(raw) {
				exactTarget = false
			}
		}
		if exactTarget {
			fragmentPath := filepath.ToSlash(filepath.Join(".product-loop", "hooks", host+".fragment.json"))
			return []InstallationRepairItem{{Path: fragmentPath, Host: host, Classification: RepairOwnedDrifted, Reason: "missing ownership fragment can be reconstructed from exact target hooks"}}
		}
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairAmbiguous, Reason: "installed hook ownership fragment is missing and current hooks do not exactly match the target", CurrentSHA256: currentHash}}
	}
	desiredEvents := map[string]bool{}
	for _, event := range hookEvents(host) {
		desiredEvents[event] = true
	}
	items := []InstallationRepairItem{}
	for event, raw := range hooks {
		entries, ok := raw.([]any)
		if !ok {
			if containsBoatstackHook(raw) {
				items = append(items, InstallationRepairItem{Path: relative, Host: host, Event: event, Classification: RepairUnsafe, Reason: "Boatstack hook event is not a list", CurrentSHA256: currentHash})
			}
			continue
		}
		ownedEntries := []any{}
		for _, entry := range entries {
			if containsBoatstackHook(entry) {
				ownedEntries = append(ownedEntries, entry)
			}
		}
		if len(ownedEntries) == 0 {
			continue
		}
		classification := RepairOwnedDrifted
		reason := "Boatstack-marked hook differs from its installed ownership fragment"
		allInstalled := installed[event] != nil
		allDesired := desiredEvents[event]
		for _, entry := range ownedEntries {
			allInstalled = allInstalled && sameJSON(entry, installed[event])
			allDesired = allDesired && sameJSON(entry, desiredHostHookForEvent(host, event))
		}
		switch {
		case allDesired && len(ownedEntries) == 1:
			classification, reason = RepairCurrent, "hook matches the target release"
		case allInstalled:
			classification, reason = RepairOwnedStale, "hook exactly matches installed provenance and can be migrated"
		case len(ownedEntries) > 1:
			classification, reason = RepairAmbiguous, "multiple non-identical Boatstack-marked hooks require review"
		}
		items = append(items, InstallationRepairItem{Path: relative, Host: host, Event: event, Classification: classification, Reason: reason, CurrentSHA256: currentHash})
	}
	return items
}

func classifyExecutionInterceptor(repo, host string) []InstallationRepairItem {
	relative := executionInterceptorPath(host)
	if relative == "" {
		return nil
	}
	path := filepath.Join(repo, relative)
	if unsafeRepairPath(repo, path) {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "execution interceptor uses a symlinked path"}}
	}
	value, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairUnsafe, Reason: "execution interceptor file cannot be read"}}
	}
	text := string(value)
	starts := strings.Count(text, interceptorHeader)
	ends := strings.Count(text, interceptorFooter)
	if starts == 0 && ends == 0 {
		return nil
	}
	if starts != 1 || ends != 1 || strings.Index(text, interceptorFooter) < strings.Index(text, interceptorHeader) {
		return []InstallationRepairItem{{Path: relative, Host: host, Classification: RepairAmbiguous, Reason: "execution interceptor markers are incomplete or duplicated", CurrentSHA256: SHA256Bytes(value)}}
	}
	expected := interceptorHeader + strings.TrimSpace(ExecutionBoundaryDX) + interceptorFooter
	start := strings.Index(text, interceptorHeader)
	end := strings.Index(text, interceptorFooter) + len(interceptorFooter)
	classification, reason := RepairOwnedStale, "marker-bounded Boatstack interceptor can be migrated"
	if text[start:end] == expected {
		classification, reason = RepairCurrent, "marker-bounded Boatstack interceptor matches the target release"
	}
	return []InstallationRepairItem{{Path: relative, Host: host, Classification: classification, Reason: reason, CurrentSHA256: SHA256Bytes(value)}}
}

func ClassifyInstallationRepair(repoPath string, adapters []string, allowDowngrade bool) (InstallationRepairResult, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return InstallationRepairResult{}, err
	}
	target, err := normalizedVersion(Version)
	if err != nil {
		return InstallationRepairResult{}, err
	}
	result := InstallationRepairResult{
		SchemaVersion: 1, VerificationStatus: "VERIFIED", TargetVersion: target, NextOperation: "update",
		HeadBranch: strings.TrimSpace(gitOutput(repo, "branch", "--show-current")), StartingHeadCommit: strings.TrimSpace(gitOutput(repo, "rev-parse", "HEAD")),
	}
	installed, versionErr := installedVersion(repo)
	if versionErr != nil {
		result.Direction = "UNKNOWN"
		result.Items = append(result.Items, InstallationRepairItem{Path: ".product-loop/bin/install.lock.json", Classification: RepairAmbiguous, Reason: versionErr.Error()})
	} else {
		result.InstalledVersion = installed
		result.Direction, err = updateDirection(installed, target)
		if err != nil {
			return InstallationRepairResult{}, err
		}
		if result.Direction == "DOWNGRADE" && !allowDowngrade {
			result.Blockers = append(result.Blockers, "downgrades require both --repair and --allow-downgrade")
		}
	}
	installLockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	if unsafeRepairPath(repo, installLockPath) {
		result.Items = append(result.Items, InstallationRepairItem{Path: ".product-loop/bin/install.lock.json", Classification: RepairUnsafe, Reason: "install provenance uses a symlinked path"})
	} else if provenanceErr := CheckExistingInstallProvenance(repo); provenanceErr != nil {
		classification := RepairOwnedDrifted
		if strings.Contains(strings.ToLower(provenanceErr.Error()), "unsafe") || strings.Contains(strings.ToLower(provenanceErr.Error()), "symlink") {
			classification = RepairUnsafe
		}
		if result.InstalledVersion == "" {
			classification = RepairAmbiguous
		}
		result.Items = append(result.Items, InstallationRepairItem{
			Path: ".product-loop/bin/install.lock.json", Classification: classification,
			Reason:        "local helper provenance needs reconstruction: " + provenanceErr.Error(),
			CurrentSHA256: currentFileHash(filepath.Join(repo, ".product-loop", "bin", "install.lock.json")),
		})
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if contains(adapters, host) {
			result.Items = append(result.Items, classifyHookState(repo, host)...)
			result.Items = append(result.Items, classifyExecutionInterceptor(repo, host)...)
		}
	}
	previous := previousFiles(repo)
	if len(previous) == 0 {
		classification := RepairOwnedDrifted
		if result.InstalledVersion == "" {
			classification = RepairAmbiguous
		}
		result.Items = append(result.Items, InstallationRepairItem{Path: ".product-loop/generated.lock.json", Classification: classification, Reason: "generated ownership provenance needs reconstruction", CurrentSHA256: currentFileHash(filepath.Join(repo, ".product-loop", "generated.lock.json"))})
	} else {
		for relative, expected := range previous {
			absolute := filepath.Join(repo, filepath.FromSlash(relative))
			if unsafeRepairPath(repo, absolute) {
				result.Items = append(result.Items, InstallationRepairItem{Path: filepath.ToSlash(relative), Classification: RepairUnsafe, Reason: "generated ownership path uses a symlink"})
				continue
			}
			value, readErr := os.ReadFile(absolute)
			if readErr == nil && SHA256Bytes(value) == expected {
				continue
			}
			classification := RepairOwnedStale
			reason := "owned generated file is missing and can be reconstructed"
			if readErr == nil {
				classification, reason = RepairOwnedDrifted, "installer-owned generated file differs from installed provenance"
			}
			result.Items = append(result.Items, InstallationRepairItem{Path: filepath.ToSlash(relative), Classification: classification, Reason: reason, CurrentSHA256: currentFileHash(filepath.Join(repo, filepath.FromSlash(relative)))})
		}
	}
	config, _, configErr := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if configErr == nil {
		states, stateErr := readInstalledIntegrations(repo, config)
		if stateErr == nil {
			result.PreservedIntegrations = states
		} else if len(config.Integrations) > 0 {
			result.PreservedIntegrations = config.Integrations
		}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].Path == result.Items[j].Path {
			return result.Items[i].Event < result.Items[j].Event
		}
		return result.Items[i].Path < result.Items[j].Path
	})
	needsRepair := false
	for _, item := range result.Items {
		switch item.Classification {
		case RepairOwnedDrifted:
			needsRepair = true
		case RepairUserOwned, RepairAmbiguous, RepairUnsafe:
			result.Blockers = append(result.Blockers, item.Path+": "+item.Reason)
		}
	}
	if len(result.Blockers) > 0 {
		result.VerificationStatus = "BLOCKED"
		result.NextOperation = "resolve_blocker"
	} else if needsRepair {
		result.VerificationStatus = "REPAIR_AVAILABLE"
		result.NextOperation = "update_with_repair"
	}
	fingerprintValue, _ := json.Marshal(struct {
		Installed string                   `json:"installed"`
		Target    string                   `json:"target"`
		Direction string                   `json:"direction"`
		Branch    string                   `json:"branch"`
		Head      string                   `json:"head"`
		Items     []InstallationRepairItem `json:"items"`
	}{result.InstalledVersion, result.TargetVersion, result.Direction, result.HeadBranch, result.StartingHeadCommit, result.Items})
	result.PackageFingerprint = SHA256Bytes(fingerprintValue)
	return result, nil
}

func repairOwnedPaths(result InstallationRepairResult) map[string]bool {
	paths := map[string]bool{}
	for _, item := range result.Items {
		if item.Classification == RepairOwnedStale || item.Classification == RepairOwnedDrifted {
			paths[item.Path] = true
		}
	}
	return paths
}

func writeInstallationRepairBackup(repo string, result InstallationRepairResult) (string, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(common, "boatstack", "repair-backups", result.PackageFingerprint)
	if err := rejectSymlinkComponents(common, directory); err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	for relative := range repairOwnedPaths(result) {
		source := filepath.Join(repo, filepath.FromSlash(relative))
		value, readErr := os.ReadFile(source)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return "", readErr
		}
		target := filepath.Join(directory, filepath.FromSlash(relative))
		if err := atomicWriteMode(target, value, 0o600); err != nil {
			return "", err
		}
	}
	manifest, err := MarshalJSON(map[string]any{
		"schema_version": 1, "created_at": time.Now().UTC().Format(time.RFC3339),
		"package_fingerprint": result.PackageFingerprint, "items": result.Items,
	})
	if err != nil {
		return "", err
	}
	if err := atomicWriteMode(filepath.Join(directory, "repair.json"), manifest, 0o600); err != nil {
		return "", err
	}
	receipt := result
	receipt.BackupPath = filepath.ToSlash(filepath.Join("boatstack", "repair-backups", result.PackageFingerprint))
	receiptValue, err := MarshalJSON(receipt)
	if err != nil {
		return "", err
	}
	version, err := safeCacheSegment(result.TargetVersion, "repair target version")
	if err != nil {
		return "", err
	}
	receiptPath := filepath.Join(common, "boatstack", "updates", version, "repair.json")
	if err := atomicWriteMode(receiptPath, receiptValue, 0o600); err != nil {
		return "", err
	}
	return directory, nil
}

func loadInstallationRepairReceipt(repo, version string) (*InstallationRepairResult, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return nil, err
	}
	segment, err := safeCacheSegment(version, "repair target version")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(common, "boatstack", "updates", segment, "repair.json")
	value, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result InstallationRepairResult
	if err := DecodeJSON("load installation repair receipt", path, value, &result); err != nil {
		return nil, err
	}
	if result.SchemaVersion != 1 || result.TargetVersion != version || result.PackageFingerprint == "" || result.BackupPath == "" {
		return nil, fmt.Errorf("installation repair receipt identity is invalid")
	}
	if result.HeadBranch != strings.TrimSpace(gitOutput(repo, "branch", "--show-current")) || result.StartingHeadCommit != strings.TrimSpace(gitOutput(repo, "rev-parse", "HEAD")) {
		return nil, nil
	}
	return &result, nil
}
