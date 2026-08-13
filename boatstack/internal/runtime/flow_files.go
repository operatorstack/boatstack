package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func RunFlowFrontend(ctx context.Context, executable, sourceName string, source []byte) ([]byte, error) {
	if executable == "" || !filepath.IsAbs(executable) || !filepath.IsAbs(sourceName) {
		return nil, fmt.Errorf("Flow frontend and source paths must be exact and absolute")
	}
	command := exec.CommandContext(ctx, executable, "--stdin", sourceName)
	command.Stdin = bytes.NewReader(source)
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("Flow frontend failed: %s", string(exit.Stderr))
		}
		return nil, err
	}
	return output, nil
}

type ProjectionWrite struct {
	Path        string
	Content     []byte
	Mode        os.FileMode
	PublishLast bool
}

type ProjectionRemoval struct {
	Path           string
	ExpectedSHA256 string
	AllowMissing   bool
}

type ProjectionExpectation struct {
	Path           string
	Exists         bool
	ExpectedSHA256 string
}

type projectionSnapshot struct {
	path    string
	exists  bool
	content []byte
	mode    os.FileMode
}

type stagedProjection struct {
	target    string
	temporary string
}

type projectionHooks struct {
	afterValidation func()
	afterRemovals   func()
}

// ApplyFlowProjection stages every output before mutating the repository and
// restores the prior bytes if any commit step fails. Generated outputs may not
// traverse repository symlinks.
func ApplyFlowProjection(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation) error {
	return applyFlowProjection(repository, writes, removals, expectations, projectionHooks{})
}

func applyFlowProjection(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation, hooks projectionHooks) error {
	if !filepath.IsAbs(repository) || filepath.Clean(repository) != repository {
		return fmt.Errorf("projection repository must be exact and absolute")
	}
	resolvedRepository, err := filepath.EvalSymlinks(repository)
	if err != nil || resolvedRepository != repository {
		return fmt.Errorf("projection repository must be a resolved directory")
	}
	lock, err := acquireProjectionLock(repository)
	if err != nil {
		return err
	}
	defer lock.release()
	writes = append([]ProjectionWrite(nil), writes...)
	removals = append([]ProjectionRemoval(nil), removals...)
	expectations = append([]ProjectionExpectation(nil), expectations...)
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].PublishLast != writes[j].PublishLast {
			return !writes[i].PublishLast
		}
		return writes[i].Path < writes[j].Path
	})
	sort.Slice(removals, func(i, j int) bool { return removals[i].Path < removals[j].Path })
	sort.Slice(expectations, func(i, j int) bool { return expectations[i].Path < expectations[j].Path })

	seen := map[string]bool{}
	snapshots := map[string]projectionSnapshot{}
	publishLast := 0
	for _, write := range writes {
		if write.Mode.Perm() == 0 || seen[write.Path] {
			return fmt.Errorf("projection contains an invalid or duplicate write path")
		}
		if write.PublishLast {
			publishLast++
		}
		seen[write.Path] = true
		if err := validateProjectionPath(repository, write.Path); err != nil {
			return err
		}
		snapshot, err := snapshotProjectionPath(write.Path)
		if err != nil {
			return err
		}
		snapshots[write.Path] = snapshot
	}
	if publishLast > 1 {
		return fmt.Errorf("projection contains multiple publish-last writes")
	}
	for _, removal := range removals {
		if seen[removal.Path] || removal.ExpectedSHA256 == "" {
			return fmt.Errorf("projection cannot write and remove the same path")
		}
		seen[removal.Path] = true
		if err := validateProjectionPath(repository, removal.Path); err != nil {
			return err
		}
		snapshot, err := snapshotProjectionPath(removal.Path)
		if err != nil {
			return err
		}
		if !snapshot.exists && !removal.AllowMissing {
			return fmt.Errorf("projection removal target is missing")
		}
		if snapshot.exists && projectionDigest(snapshot.content) != removal.ExpectedSHA256 {
			return fmt.Errorf("FLOW_PROJECTION_INPUT_CHANGED: removal target %s changed before commit", removal.Path)
		}
		snapshots[removal.Path] = snapshot
	}
	for _, expectation := range expectations {
		if err := validateProjectionPath(repository, expectation.Path); err != nil {
			return err
		}
		snapshot, err := snapshotProjectionPath(expectation.Path)
		if err != nil {
			return err
		}
		if snapshot.exists != expectation.Exists || (expectation.Exists && projectionDigest(snapshot.content) != expectation.ExpectedSHA256) {
			return fmt.Errorf("FLOW_PROJECTION_INPUT_CHANGED: %s changed before commit", expectation.Path)
		}
	}
	if hooks.afterValidation != nil {
		hooks.afterValidation()
	}

	staged := make([]stagedProjection, 0, len(writes))
	defer func() {
		for _, value := range staged {
			_ = os.Remove(value.temporary)
		}
	}()
	for _, write := range writes {
		if err := os.MkdirAll(filepath.Dir(write.Path), 0o755); err != nil {
			return err
		}
		if err := validateProjectionPath(repository, write.Path); err != nil {
			return err
		}
		temporary, err := stageProjectionFile(write.Path, write.Content, write.Mode)
		if err != nil {
			return err
		}
		staged = append(staged, stagedProjection{target: write.Path, temporary: temporary})
	}

	changed := make([]string, 0, len(writes)+len(removals))
	var commitErr error
	for index, value := range staged {
		if writes[index].PublishLast {
			continue
		}
		if commitErr = os.Rename(value.temporary, value.target); commitErr != nil {
			break
		}
		changed = append(changed, value.target)
	}
	if commitErr == nil {
		for _, removal := range removals {
			current, currentErr := snapshotProjectionPath(removal.Path)
			if currentErr != nil {
				commitErr = currentErr
				break
			}
			if !current.exists && removal.AllowMissing {
				continue
			}
			if !current.exists || projectionDigest(current.content) != removal.ExpectedSHA256 {
				commitErr = fmt.Errorf("FLOW_PROJECTION_INPUT_CHANGED: removal target %s changed before removal", removal.Path)
				break
			}
			if commitErr = os.Remove(removal.Path); commitErr != nil {
				break
			}
			changed = append(changed, removal.Path)
		}
	}
	if commitErr == nil && hooks.afterRemovals != nil {
		hooks.afterRemovals()
	}
	if commitErr == nil && publishLast == 1 {
		for _, expectation := range expectations {
			current, currentErr := snapshotProjectionPath(expectation.Path)
			if currentErr != nil {
				commitErr = currentErr
				break
			}
			if current.exists != expectation.Exists || (expectation.Exists && projectionDigest(current.content) != expectation.ExpectedSHA256) {
				commitErr = fmt.Errorf("FLOW_PROJECTION_INPUT_CHANGED: %s changed before publication", expectation.Path)
				break
			}
		}
	}
	if commitErr == nil && publishLast == 1 {
		for index, value := range staged {
			if !writes[index].PublishLast {
				continue
			}
			if commitErr = os.Rename(value.temporary, value.target); commitErr == nil {
				changed = append(changed, value.target)
			}
			break
		}
	}
	if commitErr == nil {
		return nil
	}
	if rollbackErr := rollbackProjection(changed, snapshots); rollbackErr != nil {
		return fmt.Errorf("commit Flow projection: %v; rollback failed: %w", commitErr, rollbackErr)
	}
	return fmt.Errorf("commit Flow projection: %w", commitErr)
}

func projectionDigest(value []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

type projectionLock struct {
	file *os.File
}

func acquireProjectionLock(repository string) (*projectionLock, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(cache, "boatstack", "flow-projection-locks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	identity := sha256.Sum256([]byte(repository))
	path := filepath.Join(root, fmt.Sprintf("%x.lock", identity))
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockProjectionFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("FLOW_PROJECTION_BUSY: %w", err)
	}
	return &projectionLock{file: file}, nil
}

func (l *projectionLock) release() {
	_ = unlockProjectionFile(l.file)
	_ = l.file.Close()
}

func validateProjectionPath(repository, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("managed output path must be exact and absolute")
	}
	relative, err := filepath.Rel(repository, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed output path escapes the repository")
	}
	current := repository
	parts := strings.Split(filepath.Dir(relative), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed output parent is a repository symlink")
		}
		if !info.IsDir() {
			return fmt.Errorf("managed output parent is not a directory")
		}
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("managed output is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func snapshotProjectionPath(path string) (projectionSnapshot, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return projectionSnapshot{path: path}, nil
	}
	if err != nil {
		return projectionSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return projectionSnapshot{}, fmt.Errorf("managed output is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return projectionSnapshot{}, err
	}
	return projectionSnapshot{path: path, exists: true, content: content, mode: info.Mode().Perm()}, nil
}

func stageProjectionFile(path string, content []byte, mode os.FileMode) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".boatstack-flow-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	if _, err = temporary.Write(content); err == nil {
		err = temporary.Chmod(mode)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporaryPath)
		return "", err
	}
	return temporaryPath, nil
}

func rollbackProjection(changed []string, snapshots map[string]projectionSnapshot) error {
	for index := len(changed) - 1; index >= 0; index-- {
		snapshot := snapshots[changed[index]]
		if !snapshot.exists {
			if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		temporary, err := stageProjectionFile(snapshot.path, snapshot.content, snapshot.mode)
		if err != nil {
			return err
		}
		if err := os.Rename(temporary, snapshot.path); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	}
	return nil
}
