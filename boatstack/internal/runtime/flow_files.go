package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func RunFlowFrontend(ctx context.Context, executable, source string) ([]byte, error) {
	if executable == "" || !filepath.IsAbs(executable) || !filepath.IsAbs(source) {
		return nil, fmt.Errorf("Flow frontend and source paths must be exact and absolute")
	}
	command := exec.CommandContext(ctx, executable, source)
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
	Path    string
	Content []byte
	Mode    os.FileMode
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

// ApplyFlowProjection stages every output before mutating the repository and
// restores the prior bytes if any commit step fails. Generated outputs may not
// traverse repository symlinks.
func ApplyFlowProjection(repository string, writes []ProjectionWrite, removals []string) error {
	if !filepath.IsAbs(repository) || filepath.Clean(repository) != repository {
		return fmt.Errorf("projection repository must be exact and absolute")
	}
	resolvedRepository, err := filepath.EvalSymlinks(repository)
	if err != nil || resolvedRepository != repository {
		return fmt.Errorf("projection repository must be a resolved directory")
	}
	writes = append([]ProjectionWrite(nil), writes...)
	removals = append([]string(nil), removals...)
	sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
	sort.Strings(removals)

	seen := map[string]bool{}
	snapshots := map[string]projectionSnapshot{}
	for _, write := range writes {
		if write.Mode.Perm() == 0 || seen[write.Path] {
			return fmt.Errorf("projection contains an invalid or duplicate write path")
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
	for _, path := range removals {
		if seen[path] {
			return fmt.Errorf("projection cannot write and remove the same path")
		}
		seen[path] = true
		if err := validateProjectionPath(repository, path); err != nil {
			return err
		}
		snapshot, err := snapshotProjectionPath(path)
		if err != nil {
			return err
		}
		if !snapshot.exists {
			return fmt.Errorf("projection removal target is missing")
		}
		snapshots[path] = snapshot
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
	for _, value := range staged {
		if commitErr = os.Rename(value.temporary, value.target); commitErr != nil {
			break
		}
		changed = append(changed, value.target)
	}
	if commitErr == nil {
		for _, path := range removals {
			if commitErr = os.Remove(path); commitErr != nil {
				break
			}
			changed = append(changed, path)
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
