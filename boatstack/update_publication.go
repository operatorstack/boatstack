package boatstack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const updatePublicationSchemaVersion = 1

type UpdatePublicationPreview struct {
	SchemaVersion      int      `json:"schema_version"`
	Version            string   `json:"version"`
	BaseBranch         string   `json:"base_branch"`
	HeadBranch         string   `json:"head_branch"`
	StartingHeadCommit string   `json:"starting_head_commit"`
	ChangedPaths       []string `json:"changed_paths"`
	PackageFingerprint string   `json:"package_fingerprint"`
	Title              string   `json:"title"`
	Body               string   `json:"body"`
	PreviewPath        string   `json:"preview_path"`
	Fingerprint        string   `json:"fingerprint"`
}

type UpdatePublishOptions struct {
	Repo                string
	PreviewPath         string
	ExpectedFingerprint string
}

func updatePreviewDirectory(repo, version string) (string, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	segment, err := safeCacheSegment(version, "update version")
	if err != nil {
		return "", err
	}
	directory := filepath.Join(common, "boatstack", "updates", segment)
	if err := rejectSymlinkComponents(common, directory); err != nil {
		return "", err
	}
	return directory, nil
}

func updatePreviewPath(repo, version string) (string, error) {
	directory, err := updatePreviewDirectory(repo, version)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "pr-preview.json"), nil
}

func updateChangedPathsAgainst(repo, base string) ([]string, error) {
	value, err := gitCommand(repo, "diff", "--name-only", "--diff-filter=ACDMR", base)
	if err != nil {
		return nil, err
	}
	untracked, err := gitCommand(repo, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(value+"\n"+untracked, "\n") {
		path := filepath.ToSlash(strings.TrimSpace(line))
		if path != "" {
			seen[path] = true
		}
	}
	return sortedKeys(seen), nil
}

func installedGeneratedPaths(repo string) map[string]bool {
	result := map[string]bool{}
	for path := range previousFiles(repo) {
		result[filepath.ToSlash(path)] = true
	}
	return result
}

func updateOwnedPaths(repo string, config ProjectConfig) map[string]bool {
	owned := installedGeneratedPaths(repo)
	owned[".boatstack-project.json"] = true
	for _, path := range HostHookPaths(config.Adapters) {
		owned[filepath.ToSlash(path)] = true
	}
	for _, adapter := range config.Adapters {
		switch adapter {
		case "cursor":
			owned[".cursorrules"] = true
		case "claude":
			owned["CLAUDE.md"] = true
		case "gemini":
			owned["GEMINI.md"] = true
		}
	}
	return owned
}

func validateUpdatePublicationPaths(repo, base string, config ProjectConfig, paths []string) error {
	owned := updateOwnedPaths(repo, config)
	unexpected := []string{}
	for _, path := range paths {
		if owned[path] || strings.HasPrefix(path, ".product-loop/") {
			continue
		}
		// A removed generated file is absent from the incoming generated lock.
		// Accept it only when its base content carries Boatstack's marker.
		baseValue, err := gitCommand(repo, "show", base+":"+path)
		if err == nil && strings.Contains(baseValue, Marker) {
			continue
		}
		unexpected = append(unexpected, path)
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("update publication contains non-Boatstack paths: %s", strings.Join(unexpected, ", "))
	}
	return nil
}

func updatePackageFingerprint(repo, base string, paths []string) (string, error) {
	parts := []string{"base=" + base}
	for _, relative := range paths {
		path, err := resolveRepositoryRelativePath(repo, relative)
		if err != nil {
			return "", err
		}
		if info, statErr := os.Lstat(path); statErr == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("update path is not a safe regular file: %s", relative)
			}
			hash, hashErr := SHA256File(path)
			if hashErr != nil {
				return "", hashErr
			}
			parts = append(parts, relative+"=file:"+hash)
		} else if os.IsNotExist(statErr) {
			parts = append(parts, relative+"=deleted")
		} else {
			return "", statErr
		}
	}
	return SHA256Bytes([]byte(strings.Join(parts, "\n"))), nil
}

func updatePreviewFingerprint(preview UpdatePublicationPreview) (string, error) {
	copy := preview
	copy.Fingerprint = ""
	// Publication may deterministically commit the already approved package.
	// The content fingerprint, not the pre-commit HEAD, is the authority.
	copy.StartingHeadCommit = ""
	value, err := MarshalJSON(copy)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(value), nil
}

func PrepareUpdatePublication(repoPath, requestedVersion string) (UpdatePublicationPreview, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	version, err := normalizedVersion(requestedVersion)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	baseBranch := strings.TrimSpace(config.Project.DefaultBranch)
	baseRef := "origin/" + baseBranch
	if _, err := gitCommand(repo, "rev-parse", "--verify", baseRef+"^{commit}"); err != nil {
		return UpdatePublicationPreview{}, fmt.Errorf("update preview requires fetched %s", baseRef)
	}
	headBranch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	if headBranch != "chore/update-boatstack-"+version {
		return UpdatePublicationPreview{}, fmt.Errorf("update preview requires branch chore/update-boatstack-%s; current branch is %s", version, headBranch)
	}
	paths, err := updateChangedPathsAgainst(repo, baseRef)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	if len(paths) == 0 {
		return UpdatePublicationPreview{}, fmt.Errorf("Boatstack update produced no reviewable infrastructure diff")
	}
	if err := validateUpdatePublicationPaths(repo, baseRef, config, paths); err != nil {
		return UpdatePublicationPreview{}, err
	}
	packageFingerprint, err := updatePackageFingerprint(repo, baseRef, paths)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	previewPath, err := updatePreviewPath(repo, version)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	preview := UpdatePublicationPreview{
		SchemaVersion: updatePublicationSchemaVersion, Version: version, BaseBranch: baseBranch, HeadBranch: headBranch,
		StartingHeadCommit: gitOutput(repo, "rev-parse", "HEAD"), ChangedPaths: paths, PackageFingerprint: packageFingerprint,
		Title:       "Update Boatstack to " + version,
		Body:        "## Why this change\n\nUpdate the repository-owned Boatstack infrastructure to " + version + ".\n\n## What changed\n\nOnly the fingerprinted Boatstack-generated files, host hooks, runtime provenance, and preserved integration state in this update package.\n\n## Verification\n\n- Boatstack doctor passed after installation.\n- Generated-file and hook projections are validated by the update transaction.\n\n## Rollback\n\nRevert this infrastructure-only commit and rerun the previously pinned installer.\n",
		PreviewPath: previewPath,
	}
	preview.Fingerprint, err = updatePreviewFingerprint(preview)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	value, err := MarshalJSON(preview)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	if len(value) == 0 {
		return UpdatePublicationPreview{}, fmt.Errorf("refusing to write an empty update preview")
	}
	if err := atomicWriteMode(previewPath, value, 0o600); err != nil {
		return UpdatePublicationPreview{}, err
	}
	return preview, nil
}

func LoadUpdatePublicationPreview(path string) (UpdatePublicationPreview, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return UpdatePublicationPreview{}, err
	}
	if len(strings.TrimSpace(string(value))) == 0 {
		return UpdatePublicationPreview{}, fmt.Errorf("update preview is empty")
	}
	var preview UpdatePublicationPreview
	if err := DecodeJSON("load update publication preview", path, value, &preview); err != nil {
		return UpdatePublicationPreview{}, err
	}
	if preview.SchemaVersion != updatePublicationSchemaVersion || preview.Fingerprint == "" || preview.PackageFingerprint == "" || len(preview.ChangedPaths) == 0 {
		return UpdatePublicationPreview{}, fmt.Errorf("update preview identity is invalid")
	}
	expected, err := updatePreviewFingerprint(preview)
	if err != nil || expected != preview.Fingerprint {
		return UpdatePublicationPreview{}, fmt.Errorf("update preview fingerprint is invalid")
	}
	return preview, nil
}

func PublishUpdatePublication(options UpdatePublishOptions) (string, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return "", err
	}
	preview, err := LoadUpdatePublicationPreview(options.PreviewPath)
	if err != nil {
		return "", err
	}
	if options.ExpectedFingerprint == "" || options.ExpectedFingerprint != preview.Fingerprint {
		return "", fmt.Errorf("update publication fingerprint does not match the exact preview confirmed by the human")
	}
	current, err := PrepareUpdatePublication(repo, preview.Version)
	if err != nil {
		return "", err
	}
	if current.Fingerprint != preview.Fingerprint || current.PackageFingerprint != preview.PackageFingerprint {
		return "", fmt.Errorf("update package changed after confirmation; regenerate the preview")
	}
	if err := ghAvailable(repo); err != nil {
		return "", err
	}
	existingURL, exists, err := existingPRURL(repo)
	if err != nil {
		return "", err
	}
	target := "github-update-pr:" + preview.HeadBranch
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: "publish-update-pr", Scope: OperationScope{Worktree: filepath.Base(repo), HeadBranch: preview.HeadBranch},
		Target: target, PackageFingerprint: preview.PackageFingerprint, AuthorizationFingerprint: preview.Fingerprint,
		RetryClass: "RECONCILE_FIRST", MaxAttempts: 3,
		ExpectedPostcondition: "origin contains the exact update commit and one pull request contains the fingerprinted update body",
	})
	if err != nil {
		return "", err
	}
	if receipt.State == OperationSucceeded {
		return receipt.Observation.Evidence, nil
	}
	if receipt.State == OperationReconcileRequired {
		result, detail, evidence := "OBSERVED_ABSENT", "no pull request exists for the update branch", preview.HeadBranch
		if exists {
			result, detail, evidence = "OBSERVED_PARTIAL", "the update PR exists; resume exact remaining postconditions", existingURL
		}
		if receipt, err = RecordOperationReconciliation(repo, receipt.OperationID, result, detail, evidence); err != nil {
			return "", err
		}
	}
	attemptKey := SHA256Bytes([]byte("publish-update-pr\x00" + preview.Fingerprint))
	begin, err := BeginOperation(repo, receipt.OperationID, attemptKey, "boatstack-helper publish-update-pr")
	if err != nil {
		if errors.Is(err, ErrOperationInFlight) {
			return "", fmt.Errorf("the identical update publication is already executing; inspect operation-status instead of repeating it")
		}
		return "", err
	}
	unknown := func(cause error, url string) (string, error) {
		_, _ = CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "UNKNOWN", "update publication ended without a verifiable complete postcondition", url)
		return "", cause
	}
	if strings.TrimSpace(gitOutput(repo, "status", "--porcelain")) != "" {
		arguments := append([]string{"add", "--"}, preview.ChangedPaths...)
		if _, err := gitCommand(repo, arguments...); err != nil {
			return unknown(err, existingURL)
		}
		if _, err := gitCommand(repo, "commit", "-m", "chore: update Boatstack to "+preview.Version); err != nil {
			return unknown(err, existingURL)
		}
	}
	if _, err := gitCommand(repo, "push", "--set-upstream", "origin", preview.HeadBranch); err != nil {
		return unknown(err, existingURL)
	}
	temporary, err := os.CreateTemp("", "boatstack-update-pr-*.md")
	if err != nil {
		return unknown(err, existingURL)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(preview.Body); err != nil {
		temporary.Close()
		return unknown(err, existingURL)
	}
	if err := temporary.Close(); err != nil {
		return unknown(err, existingURL)
	}
	url := existingURL
	if !exists {
		url, err = commandOutput(repo, "gh", "pr", "create", "--base", preview.BaseBranch, "--head", preview.HeadBranch, "--title", preview.Title, "--body-file", temporaryPath)
		if err != nil {
			return unknown(err, "")
		}
	} else if _, err := commandOutput(repo, "gh", "pr", "edit", existingURL, "--title", preview.Title, "--body-file", temporaryPath); err != nil {
		return unknown(err, existingURL)
	}
	url = strings.TrimSpace(url)
	if _, err := CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "SUCCEEDED", "update branch and pull request observed", url); err != nil {
		return "", err
	}
	return url, nil
}
