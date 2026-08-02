package boatstack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DetachedFeatureMigration reports one embedded feature package considered by
// explicit attachment repair. Status is IMPORTED, UNCHANGED, CONFLICTING, or
// REJECTED; the vocabulary is stable for host adapters.
type DetachedFeatureMigration struct {
	Feature string `json:"feature"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
}

type detachedFeatureImport struct {
	feature string
	source  string
	target  string
}

var detachedImportBeforeRename func(source, temporary, target string) error

func directoryFingerprint(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("feature package root is not a real directory: %s", root)
	}
	parts := []string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("feature package contains a symlink: %s", relative)
		}
		if entry.IsDir() {
			parts = append(parts, filepath.ToSlash(relative)+"/")
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("feature package contains a non-regular file: %s", relative)
		}
		hash, err := SHA256File(path)
		if err != nil {
			return err
		}
		parts = append(parts, filepath.ToSlash(relative)+"\x00"+hash)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(parts)
	return SHA256Bytes([]byte(joinNUL(parts))), nil
}

func joinNUL(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += "\x00"
		}
		result += value
	}
	return result
}

func validateEmbeddedFeaturePackage(repo, directory, feature string) error {
	check, err := CheckPlan(filepath.Join(directory, "plan.md"))
	if err != nil {
		return err
	}
	if stringValue(check.Plan["feature_id"]) != feature {
		return fmt.Errorf("plan feature_id does not match directory")
	}
	if path := filepath.Join(directory, "approval.md"); fileExists(path) {
		receipt, loadErr := LoadApprovalReceipt(path)
		if loadErr != nil || receipt.Fingerprint != check.Fingerprint {
			return fmt.Errorf("approval receipt fingerprint is invalid or stale")
		}
	}
	if path := filepath.Join(directory, "autonomy.md"); fileExists(path) {
		value, loadErr := loadJSONObject(path, "autonomy receipt", autonomyMarkerStart, autonomyMarkerEnd, true)
		if loadErr != nil {
			return loadErr
		}
		data, marshalErr := MarshalJSON(value)
		if marshalErr != nil {
			return marshalErr
		}
		var receipt AutonomyReceipt
		if decodeErr := DecodeJSON("autonomy receipt", path, data, &receipt); decodeErr != nil {
			return decodeErr
		}
		fingerprint, fingerprintErr := autonomyFingerprint(receipt)
		if fingerprintErr != nil || fingerprint != receipt.Fingerprint || receipt.Feature != feature || receipt.PlanFingerprint != check.Fingerprint {
			return fmt.Errorf("autonomy receipt fingerprint is invalid or stale")
		}
	}
	return nil
}

func planDetachedFeatureImports(repo string, ctx WorkspaceContext) ([]detachedFeatureImport, []DetachedFeatureMigration, error) {
	sourceRoot := filepath.Join(repo, productLoopDirName, "features")
	entries, err := os.ReadDir(sourceRoot)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	candidates := []string{}
	for _, entry := range entries {
		if entry.IsDir() && featureSlugPattern.MatchString(entry.Name()) && fileExists(filepath.Join(sourceRoot, entry.Name(), "plan.md")) {
			candidates = append(candidates, entry.Name())
		}
	}
	selected := detachedOpenFeatureCandidates(repo, candidates)
	imports := []detachedFeatureImport{}
	results := []DetachedFeatureMigration{}
	blocked := false
	for _, entry := range entries {
		feature := entry.Name()
		if !selected[feature] {
			continue
		}
		source := filepath.Join(sourceRoot, feature)
		target := ctx.FeatureDir(feature)
		if err := validateEmbeddedFeaturePackage(repo, source, feature); err != nil {
			results = append(results, DetachedFeatureMigration{Feature: feature, Status: "REJECTED", Reason: err.Error()})
			blocked = true
			continue
		}
		sourceHash, err := directoryFingerprint(source)
		if err != nil {
			return nil, results, err
		}
		if pathExists(target) {
			targetHash, targetErr := directoryFingerprint(target)
			if targetErr != nil {
				return nil, results, targetErr
			}
			if sourceHash == targetHash {
				results = append(results, DetachedFeatureMigration{Feature: feature, Status: "UNCHANGED", Reason: "Embedded and detached packages are byte-identical."})
				continue
			}
			results = append(results, DetachedFeatureMigration{Feature: feature, Status: "CONFLICTING", Reason: "Embedded and detached packages differ; Boatstack will not choose by recency."})
			blocked = true
			continue
		}
		imports = append(imports, detachedFeatureImport{feature: feature, source: source, target: target})
	}
	if blocked {
		return nil, results, fmt.Errorf("embedded feature migration requires conflict or receipt repair")
	}
	return imports, results, nil
}

// detachedOpenFeatureCandidates excludes historical packages. The current
// feature branch is authoritative when it names an embedded package; otherwise
// one active delivery or the sole package can be recovered without ambiguity.
func detachedOpenFeatureCandidates(repo string, candidates []string) map[string]bool {
	selected := map[string]bool{}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	for _, prefix := range []string{"feat/", "fix/", "chore/", "ci/"} {
		feature := strings.TrimPrefix(branch, prefix)
		if feature == branch {
			continue
		}
		for _, candidate := range candidates {
			if candidate == feature {
				selected[candidate] = true
				return selected
			}
		}
	}
	active, _, err := scanManagedDeliveries(repo)
	if err == nil && len(active) == 1 {
		for _, candidate := range candidates {
			if candidate == active[0] {
				selected[candidate] = true
				return selected
			}
		}
	}
	if len(candidates) == 1 {
		selected[candidates[0]] = true
	}
	return selected
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func copyDirectoryAtomic(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(target), ".boatstack-feature-import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		destination := filepath.Join(temporary, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return atomicWriteMode(destination, value, 0o644)
	})
	if err != nil {
		return err
	}
	before, err := directoryFingerprint(source)
	if err != nil {
		return err
	}
	after, err := directoryFingerprint(temporary)
	if err != nil || before != after {
		return fmt.Errorf("copied feature package failed fingerprint verification")
	}
	if detachedImportBeforeRename != nil {
		if err := detachedImportBeforeRename(source, temporary, target); err != nil {
			return err
		}
	}
	return os.Rename(temporary, target)
}

func applyDetachedFeatureImports(imports []detachedFeatureImport, results []DetachedFeatureMigration) ([]DetachedFeatureMigration, error) {
	for _, planned := range imports {
		if err := copyDirectoryAtomic(planned.source, planned.target); err != nil {
			return results, err
		}
		results = append(results, DetachedFeatureMigration{Feature: planned.feature, Status: "IMPORTED", Reason: "Validated embedded package was atomically imported into detached controller state."})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Feature < results[j].Feature })
	return results, nil
}
