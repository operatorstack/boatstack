package boatstack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type runtimeManifest struct {
	SchemaVersion          int                         `json:"schema_version"`
	BoatstackVersion       string                      `json:"boatstack_version"`
	SourceCommit           string                      `json:"source_commit"`
	Platform               string                      `json:"platform"`
	BinarySHA256           string                      `json:"binary_sha256"`
	ReleaseChecksumsSHA256 string                      `json:"release_checksums_sha256"`
	Integrations           map[string]IntegrationState `json:"integrations,omitempty"`
}

type generatedRuntimeLock struct {
	BoatstackVersion string `json:"boatstack_version"`
	Runtime          struct {
		SourceCommit string `json:"source_commit"`
	} `json:"runtime"`
}

func helperName() string {
	name := "boatstack-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func platformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }

func safeCacheSegment(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value ||
		strings.ContainsAny(value, `/\\`) {
		return "", fmt.Errorf("invalid %s for shared runtime cache", label)
	}
	return value, nil
}

func gitCommonDir(repo string) (string, error) {
	value := gitOutput(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if value == "" {
		value = gitOutput(repo, "rev-parse", "--git-common-dir")
	}
	if value == "" {
		return "", fmt.Errorf("cannot resolve the Git common directory")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(repo, value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func sharedRuntimeDirectory(repo, version, sourceCommit string) (string, error) {
	version, err := safeCacheSegment(version, "Boatstack version")
	if err != nil {
		return "", err
	}
	sourceCommit, err = safeCacheSegment(sourceCommit, "source commit")
	if err != nil {
		return "", err
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "boatstack", "runtimes", version, sourceCommit, platformKey()), nil
}

func sharedRuntimePaths(repo, version, sourceCommit string) (string, string, error) {
	directory, err := sharedRuntimeDirectory(repo, version, sourceCommit)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(directory, helperName()), filepath.Join(directory, "runtime.lock.json"), nil
}

func atomicWriteMode(path string, content []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked runtime path: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".boatstack-runtime-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}

func installSharedRuntime(source, repo string, integrations map[string]IntegrationState) (runtimeManifest, error) {
	value, err := os.ReadFile(source)
	if err != nil {
		return runtimeManifest{}, err
	}
	manifest := runtimeManifest{
		SchemaVersion: 1, BoatstackVersion: Version, SourceCommit: SourceCommit,
		Platform: platformKey(), BinarySHA256: SHA256Bytes(value),
		ReleaseChecksumsSHA256: ChecksumsSHA256, Integrations: integrations,
	}
	binaryPath, manifestPath, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		return runtimeManifest{}, err
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return runtimeManifest{}, err
	}
	for _, path := range []string{binaryPath, manifestPath} {
		if err := rejectSymlinkComponents(common, path); err != nil {
			return runtimeManifest{}, err
		}
	}
	// This exact provenance path is Boatstack-owned. A verified installer is the
	// repair surface for an interrupted or corrupted cache population, so it may
	// atomically replace the cached bytes after the symlink checks above.
	if err := atomicWriteMode(binaryPath, value, 0o755); err != nil {
		return runtimeManifest{}, err
	}
	encoded, err := MarshalJSON(manifest)
	if err != nil {
		return runtimeManifest{}, err
	}
	if err := atomicWriteMode(manifestPath, encoded, 0o644); err != nil {
		return runtimeManifest{}, err
	}
	return manifest, nil
}

func loadSharedRuntime(repo string) (runtimeManifest, string, error) {
	binaryPath, manifestPath, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		return runtimeManifest{}, "", err
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return runtimeManifest{}, "", err
	}
	for _, path := range []string{binaryPath, manifestPath} {
		if err := rejectSymlinkComponents(common, path); err != nil {
			return runtimeManifest{}, "", err
		}
	}
	value, err := os.ReadFile(manifestPath)
	if err != nil {
		return runtimeManifest{}, "", fmt.Errorf("shared Boatstack runtime is missing; run the verified installer once from any checkout in this Git clone: %w", err)
	}
	var manifest runtimeManifest
	if err := DecodeJSON("load shared Boatstack runtime manifest", manifestPath, value, &manifest); err != nil {
		return runtimeManifest{}, "", err
	}
	if manifest.SchemaVersion != 1 || manifest.BoatstackVersion != Version ||
		manifest.SourceCommit != SourceCommit || manifest.Platform != platformKey() {
		return runtimeManifest{}, "", fmt.Errorf("shared Boatstack runtime provenance does not match this worktree")
	}
	if info, err := os.Lstat(binaryPath); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return runtimeManifest{}, "", fmt.Errorf("shared Boatstack runtime is missing or unsafe: %s", binaryPath)
	}
	hash, err := SHA256File(binaryPath)
	if err != nil {
		return runtimeManifest{}, "", err
	}
	if hash != manifest.BinarySHA256 {
		return runtimeManifest{}, "", fmt.Errorf("shared Boatstack runtime checksum does not match its manifest")
	}
	return manifest, binaryPath, nil
}

func verifyGeneratedRuntime(repo string) error {
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "generated.lock.json"))
	if err != nil {
		return fmt.Errorf("missing generated Boatstack runtime provenance: %w", err)
	}
	var lock generatedRuntimeLock
	lockPath := filepath.Join(repo, ".product-loop", "generated.lock.json")
	if err := DecodeJSON("verify generated Boatstack runtime provenance", lockPath, value, &lock); err != nil {
		return err
	}
	if lock.BoatstackVersion != Version || lock.Runtime.SourceCommit != SourceCommit {
		return fmt.Errorf("this worktree expects Boatstack %s (%s), but the runtime is %s (%s); update or rebase its Boatstack infrastructure",
			lock.BoatstackVersion, lock.Runtime.SourceCommit, Version, SourceCommit)
	}
	return nil
}

func acquireHydrationLock(repo string) (func(), error) {
	lockPath := filepath.Join(repo, ".product-loop", "bin", ".hydrate.lock")
	if err := rejectSymlinkComponents(repo, lockPath); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 100; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if _, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid()); writeErr != nil {
				file.Close()
				os.Remove(lockPath)
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				os.Remove(lockPath)
				return nil, closeErr
			}
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Another process may have completed hydration while this process
		// waited. Avoid acquiring and rewriting state that is already valid.
		if verifyErr := verifyLocalRuntime(repo); verifyErr == nil {
			return func() {}, nil
		}
		if info, statErr := os.Lstat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for another Boatstack worktree activation; retry the original command")
}

func HydrateWorktree(repoPath string) error {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return err
	}
	if err := verifyGeneratedRuntime(repo); err != nil {
		return err
	}
	manifest, sharedBinary, err := loadSharedRuntime(repo)
	if err != nil {
		return err
	}
	// Guards call this operation for every agent event. Once the local runtime is
	// current and intact, verification is enough; avoid rewriting ignored state
	// on every safe command. A missing, stale, or tampered local runtime falls
	// through to verified shared-cache hydration.
	if err := verifyLocalRuntime(repo); err == nil {
		return nil
	}
	release, err := acquireHydrationLock(repo)
	if err != nil {
		return err
	}
	defer release()
	if err := verifyLocalRuntime(repo); err == nil {
		return nil
	}
	value, err := os.ReadFile(sharedBinary)
	if err != nil {
		return err
	}
	localBinary := filepath.Join(repo, ".product-loop", "bin", helperName())
	if err := rejectSymlinkComponents(repo, localBinary); err != nil {
		return err
	}
	if err := atomicWriteMode(localBinary, value, 0o755); err != nil {
		return err
	}
	if err := writeInstallLock(repo, localBinary, manifest.BinarySHA256, manifest.Integrations); err != nil {
		return err
	}
	return verifyLocalRuntime(repo)
}

func verifyLocalRuntime(repo string) error {
	lockPath := filepath.Join(repo, ".product-loop", "bin", "install.lock.json")
	if err := rejectSymlinkComponents(repo, lockPath); err != nil {
		return err
	}
	value, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("missing local install lock: %w", err)
	}
	var lock installLock
	if err := DecodeJSON("verify local Boatstack runtime", lockPath, value, &lock); err != nil {
		return err
	}
	if lock.BoatstackVersion != Version || lock.SourceCommit != SourceCommit {
		return fmt.Errorf("helper version drift: installed %s (%s), expected %s (%s)", lock.BoatstackVersion, lock.SourceCommit, Version, SourceCommit)
	}
	if lock.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fmt.Errorf("helper platform drift: installed %s, expected %s/%s", lock.Platform, runtime.GOOS, runtime.GOARCH)
	}
	binaryPath, err := resolveRepositoryRelativePath(repo, lock.BinaryPath)
	if err != nil {
		return fmt.Errorf("invalid Boatstack helper path in install lock: %w", err)
	}
	if err := rejectSymlinkComponents(repo, binaryPath); err != nil {
		return err
	}
	if err := checkNonEmptyFile(binaryPath, "Boatstack helper"); err != nil {
		return err
	}
	hash, err := SHA256File(binaryPath)
	if err != nil {
		return err
	}
	if hash != lock.BinarySHA256 {
		return fmt.Errorf("Boatstack helper checksum does not match the install lock")
	}
	return nil
}
