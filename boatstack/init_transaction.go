package boatstack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type repositorySnapshot struct {
	repo   string
	backup string
	active bool
}

func beginRepositorySnapshot(repo string) (*repositorySnapshot, error) {
	backup, err := os.MkdirTemp("", "boatstack-init-rollback-*")
	if err != nil {
		return nil, fmt.Errorf("create initialization rollback snapshot: %w", err)
	}
	snapshot := &repositorySnapshot{repo: repo, backup: backup, active: true}
	if err := copyRepositoryTree(repo, backup); err != nil {
		_ = os.RemoveAll(backup)
		return nil, fmt.Errorf("snapshot repository before initialization: %w", err)
	}
	return snapshot, nil
}

func copyRepositoryTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" || (relative != "." && filepath.Dir(relative) == ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported repository entry in initialization transaction: %s", path)
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, value, info.Mode().Perm())
	})
}

func (snapshot *repositorySnapshot) rollback() error {
	if !snapshot.active {
		return nil
	}
	entries, err := os.ReadDir(snapshot.repo)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(snapshot.repo, entry.Name())); err != nil {
			return fmt.Errorf("remove partial initialization path %s: %w", entry.Name(), err)
		}
	}
	if err := copyRepositoryTree(snapshot.backup, snapshot.repo); err != nil {
		return fmt.Errorf("restore repository after initialization failure: %w", err)
	}
	snapshot.active = false
	return os.RemoveAll(snapshot.backup)
}

func (snapshot *repositorySnapshot) commit() error {
	if !snapshot.active {
		return nil
	}
	if err := os.RemoveAll(snapshot.backup); err != nil {
		return err
	}
	snapshot.active = false
	return nil
}
