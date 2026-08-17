package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
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

// VerifyFlowProjectionAtRevision proves that a workspace base contains the
// exact active Flow inputs and generated outputs before authority transfers to
// a new Git worktree.
func VerifyFlowProjectionAtRevision(ctx context.Context, repository, revision string, paths []string) error {
	if !filepath.IsAbs(repository) || filepath.Clean(repository) != repository || revision == "" || len(paths) == 0 {
		return fmt.Errorf("Flow revision projection requires an exact repository, revision, and path set")
	}
	resolvedRepository, err := filepath.EvalSymlinks(repository)
	if err != nil || resolvedRepository != repository {
		return fmt.Errorf("Flow revision projection repository is not resolved")
	}
	seen := map[string]bool{}
	for _, relative := range paths {
		if relative == "" || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative || relative == ".." || strings.HasPrefix(relative, "../") || seen[relative] {
			return fmt.Errorf("Flow revision projection contains an invalid path %q", relative)
		}
		seen[relative] = true
		current, readErr := os.ReadFile(filepath.Join(repository, filepath.FromSlash(relative)))
		if readErr != nil {
			return fmt.Errorf("read active Flow projection %s: %w", relative, readErr)
		}
		command := exec.CommandContext(ctx, "git", "show", revision+":"+relative)
		command.Dir = repository
		committed, showErr := command.Output()
		if showErr != nil || !bytes.Equal(current, committed) {
			return fmt.Errorf("Flow projection %s differs from revision %s", relative, revision)
		}
	}
	return nil
}

type ProjectionWrite struct {
	Path                   string
	Content                []byte
	Mode                   os.FileMode
	ExpectedPreviousSHA256 string
	PublishLast            bool
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
	beforeStage     func(string)
	afterRemovals   func()
}

// ApplyFlowProjection stages every output before mutating the repository and
// restores the prior bytes if any commit step fails. Generated outputs may not
// traverse repository symlinks.
func ApplyFlowProjection(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation) error {
	return applyFlowProjection(repository, writes, removals, expectations, projectionHooks{})
}

func applyFlowProjection(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation, hooks projectionHooks) error {
	return applyFlowProjectionWithOwnership(repository, writes, removals, expectations, hooks, nil)
}

func applyFlowProjectionWithOwnership(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation, hooks projectionHooks, ownership *flowProjectionOwnershipChange) error {
	if !filepath.IsAbs(repository) || filepath.Clean(repository) != repository {
		return fmt.Errorf("projection repository must be exact and absolute")
	}
	resolvedRepository, err := filepath.EvalSymlinks(repository)
	if err != nil || resolvedRepository != repository {
		return fmt.Errorf("projection repository must be a resolved directory")
	}
	root, err := os.OpenRoot(repository)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := acquireProjectionLock(repository)
	if err != nil {
		return err
	}
	defer lock.release()
	if ownership != nil {
		if ownership.prior.path == "" || ownership.next.SourcePath != ownership.prior.sourcePath || (ownership.prior.exists && ownership.prior.Record.SourcePath != ownership.next.SourcePath) {
			return fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: provenance update does not identify one source")
		}
		if err := validateOwnershipSnapshot(ownership.prior); err != nil {
			return err
		}
	}
	writes = append([]ProjectionWrite(nil), writes...)
	removals = append([]ProjectionRemoval(nil), removals...)
	expectations = append([]ProjectionExpectation(nil), expectations...)
	if ownership != nil {
		filtered := removals[:0]
		for _, removal := range removals {
			relative, relativeErr := filepath.Rel(repository, removal.Path)
			if relativeErr != nil {
				return relativeErr
			}
			relative = filepath.ToSlash(relative)
			if hostprojection.IsSharedCheckoutPath(relative) {
				referenced, referenceErr := sharedProjectionReferenced(repository, relative, removal.ExpectedSHA256, ownership.next.SourcePath, true)
				if referenceErr != nil {
					return referenceErr
				}
				if referenced {
					continue
				}
			}
			filtered = append(filtered, removal)
		}
		removals = filtered
	}
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
		snapshot, err := snapshotProjectionPath(root, repository, write.Path)
		if err != nil {
			return err
		}
		if !projectionWriteAuthorized(snapshot, write) {
			return fmt.Errorf("FLOW_PROJECTION_WRITE_UNAUTHORIZED: %s is not absent, exact crash residue, or authorized by kernel provenance", write.Path)
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
		snapshot, err := snapshotProjectionPath(root, repository, removal.Path)
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
		snapshot, err := snapshotProjectionPath(root, repository, expectation.Path)
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
	ownershipTemporary := ""
	defer func() {
		for _, value := range staged {
			_ = root.Remove(value.temporary)
		}
		if ownershipTemporary != "" {
			_ = os.Remove(ownershipTemporary)
		}
	}()
	for _, write := range writes {
		relative, relativeErr := projectionRelativePath(repository, write.Path)
		if relativeErr != nil {
			return relativeErr
		}
		if err := root.MkdirAll(filepath.Dir(relative), 0o755); err != nil {
			return err
		}
		if err := validateProjectionPath(repository, write.Path); err != nil {
			return err
		}
		if hooks.beforeStage != nil {
			hooks.beforeStage(write.Path)
		}
		temporary, err := stageProjectionFile(root, repository, write.Path, write.Content, write.Mode)
		if err != nil {
			return err
		}
		staged = append(staged, stagedProjection{target: write.Path, temporary: temporary})
	}
	if ownership != nil {
		ownershipTemporary, err = stageOwnership(ownership.prior, ownership.next)
		if err != nil {
			return err
		}
	}

	changed := make([]string, 0, len(writes)+len(removals))
	committed := map[string]projectionSnapshot{}
	var commitErr error
	for index, value := range staged {
		if writes[index].PublishLast {
			continue
		}
		current, currentErr := snapshotProjectionPath(root, repository, value.target)
		if currentErr != nil {
			commitErr = currentErr
			break
		}
		if !projectionWriteAuthorized(current, writes[index]) {
			commitErr = fmt.Errorf("FLOW_PROJECTION_WRITE_UNAUTHORIZED: %s changed before replacement", value.target)
			break
		}
		desired := projectionSnapshot{path: value.target, exists: true, content: writes[index].Content, mode: writes[index].Mode.Perm()}
		if sameProjectionState(current, desired) {
			committed[value.target] = current
			continue
		}
		target, targetErr := projectionRelativePath(repository, value.target)
		if targetErr != nil {
			commitErr = targetErr
			break
		}
		if commitErr = root.Rename(value.temporary, target); commitErr != nil {
			break
		}
		changed = append(changed, value.target)
		committed[value.target] = projectionSnapshot{path: value.target, exists: true, content: writes[index].Content, mode: writes[index].Mode.Perm()}
	}
	if commitErr == nil {
		for _, removal := range removals {
			current, currentErr := snapshotProjectionPath(root, repository, removal.Path)
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
			relative, relativeErr := projectionRelativePath(repository, removal.Path)
			if relativeErr != nil {
				commitErr = relativeErr
				break
			}
			if commitErr = root.Remove(relative); commitErr != nil {
				break
			}
			changed = append(changed, removal.Path)
			committed[removal.Path] = projectionSnapshot{path: removal.Path}
		}
	}
	if commitErr == nil && hooks.afterRemovals != nil {
		hooks.afterRemovals()
	}
	if commitErr == nil && publishLast == 1 {
		for index, value := range staged {
			if writes[index].PublishLast {
				continue
			}
			current, currentErr := snapshotProjectionPath(root, repository, value.target)
			if currentErr != nil {
				commitErr = currentErr
				break
			}
			if !sameProjectionState(current, committed[value.target]) {
				commitErr = fmt.Errorf("FLOW_PROJECTION_OUTPUT_CHANGED: %s changed before artifact publication", value.target)
				break
			}
		}
	}
	if commitErr == nil && publishLast == 1 {
		for _, removal := range removals {
			current, currentErr := snapshotProjectionPath(root, repository, removal.Path)
			if currentErr != nil {
				commitErr = currentErr
				break
			}
			if current.exists {
				commitErr = fmt.Errorf("FLOW_PROJECTION_OUTPUT_CHANGED: retired output %s reappeared before artifact publication", removal.Path)
				break
			}
		}
	}
	if commitErr == nil && publishLast == 1 {
		for _, expectation := range expectations {
			current, currentErr := snapshotProjectionPath(root, repository, expectation.Path)
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
			current, currentErr := snapshotProjectionPath(root, repository, value.target)
			if currentErr != nil {
				commitErr = currentErr
				break
			}
			if !projectionWriteAuthorized(current, writes[index]) {
				commitErr = fmt.Errorf("FLOW_PROJECTION_WRITE_UNAUTHORIZED: %s changed before replacement", value.target)
				break
			}
			desired := projectionSnapshot{path: value.target, exists: true, content: writes[index].Content, mode: writes[index].Mode.Perm()}
			if sameProjectionState(current, desired) {
				committed[value.target] = current
				break
			}
			target, targetErr := projectionRelativePath(repository, value.target)
			if targetErr != nil {
				commitErr = targetErr
				break
			}
			if commitErr = root.Rename(value.temporary, target); commitErr == nil {
				changed = append(changed, value.target)
				committed[value.target] = projectionSnapshot{path: value.target, exists: true, content: writes[index].Content, mode: writes[index].Mode.Perm()}
			}
			break
		}
	}
	if commitErr == nil && ownership != nil {
		if commitErr = validateOwnershipSnapshot(ownership.prior); commitErr == nil {
			commitErr = os.Rename(ownershipTemporary, ownership.prior.path)
			if commitErr == nil {
				ownershipTemporary = ""
			}
		}
	}
	if commitErr == nil {
		return nil
	}
	if rollbackErr := rollbackProjection(root, repository, changed, snapshots, committed); rollbackErr != nil {
		return fmt.Errorf("commit Flow projection: %v; rollback failed: %w", commitErr, rollbackErr)
	}
	return fmt.Errorf("commit Flow projection: %w", commitErr)
}

func projectionDigest(value []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

func projectionWriteAuthorized(current projectionSnapshot, write ProjectionWrite) bool {
	if !current.exists {
		return write.ExpectedPreviousSHA256 == ""
	}
	desired := projectionSnapshot{exists: true, content: write.Content, mode: write.Mode.Perm()}
	if sameProjectionState(current, desired) {
		return true
	}
	return write.ExpectedPreviousSHA256 != "" && projectionDigest(current.content) == write.ExpectedPreviousSHA256
}

type projectionLock struct {
	file *os.File
}

func acquireProjectionLock(repository string) (*projectionLock, error) {
	gitDirectory, err := projectionGitDirectory(repository)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(gitDirectory, "boatstack-flow-projection.lock")
	if info, statErr := os.Lstat(path); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("FLOW_PROJECTION_LOCK_INVALID: repository lock is not a regular file")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, err
	}
	if info, statErr := file.Stat(); statErr != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("FLOW_PROJECTION_LOCK_INVALID: repository lock is not a regular file")
	}
	if err := file.Chmod(0o666); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := lockProjectionFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("FLOW_PROJECTION_BUSY: %w", err)
	}
	return &projectionLock{file: file}, nil
}

func projectionGitDirectory(repository string) (string, error) {
	marker := filepath.Join(repository, ".git")
	info, err := os.Lstat(marker)
	if err != nil {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: .git is a symlink")
	}
	if info.IsDir() {
		return marker, nil
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: .git is not a directory or worktree marker")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir: ") || strings.Contains(strings.TrimPrefix(line, "gitdir: "), "\n") {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: invalid worktree Git marker")
	}
	gitDirectory := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(repository, gitDirectory)
	}
	gitDirectory, err = filepath.EvalSymlinks(filepath.Clean(gitDirectory))
	if err != nil {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: %w", err)
	}
	if info, err := os.Stat(gitDirectory); err != nil || !info.IsDir() {
		return "", fmt.Errorf("FLOW_PROJECTION_LOCK_UNAVAILABLE: worktree Git directory is unavailable")
	}
	return gitDirectory, nil
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

func projectionRelativePath(repository, path string) (string, error) {
	relative, err := filepath.Rel(repository, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("managed output path escapes the repository")
	}
	return relative, nil
}

func snapshotProjectionPath(root *os.Root, repository, path string) (projectionSnapshot, error) {
	relative, err := projectionRelativePath(repository, path)
	if err != nil {
		return projectionSnapshot{}, err
	}
	info, err := root.Lstat(relative)
	if os.IsNotExist(err) {
		return projectionSnapshot{path: path}, nil
	}
	if err != nil {
		return projectionSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return projectionSnapshot{}, fmt.Errorf("managed output is not a regular file")
	}
	content, err := root.ReadFile(relative)
	if err != nil {
		return projectionSnapshot{}, err
	}
	return projectionSnapshot{path: path, exists: true, content: content, mode: info.Mode().Perm()}, nil
}

func stageProjectionFile(root *os.Root, repository, path string, content []byte, mode os.FileMode) (string, error) {
	relative, err := projectionRelativePath(repository, path)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 100; attempt++ {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return "", err
		}
		temporaryPath := filepath.Join(filepath.Dir(relative), fmt.Sprintf(".boatstack-flow-%x", nonce))
		temporary, openErr := root.OpenFile(temporaryPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if os.IsExist(openErr) {
			continue
		}
		if openErr != nil {
			return "", openErr
		}
		if _, err = temporary.Write(content); err == nil {
			err = temporary.Chmod(mode)
		}
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = root.Remove(temporaryPath)
			return "", err
		}
		return temporaryPath, nil
	}
	return "", fmt.Errorf("cannot allocate a staged projection file")
}

func rollbackProjection(root *os.Root, repository string, changed []string, snapshots, committed map[string]projectionSnapshot) error {
	for index := len(changed) - 1; index >= 0; index-- {
		snapshot := snapshots[changed[index]]
		current, err := snapshotProjectionPath(root, repository, snapshot.path)
		if err != nil {
			return err
		}
		if !sameProjectionState(current, committed[changed[index]]) {
			continue
		}
		relative, err := projectionRelativePath(repository, snapshot.path)
		if err != nil {
			return err
		}
		if !snapshot.exists {
			if err := root.Remove(relative); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		temporary, err := stageProjectionFile(root, repository, snapshot.path, snapshot.content, snapshot.mode)
		if err != nil {
			return err
		}
		if err := root.Rename(temporary, relative); err != nil {
			_ = root.Remove(temporary)
			return err
		}
	}
	return nil
}

func sameProjectionState(left, right projectionSnapshot) bool {
	return left.exists == right.exists && (!left.exists || (sameProjectionMode(left.mode, right.mode) && bytes.Equal(left.content, right.content)))
}

func sameProjectionMode(left, right os.FileMode) bool {
	if runtime.GOOS == "windows" {
		// Windows chmod only controls the writable bit. The other Unix
		// permission bits are not an observable projection-state identity.
		return left.Perm()&0o200 == right.Perm()&0o200
	}
	return left.Perm() == right.Perm()
}
