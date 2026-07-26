package boatstack

import (
	"path/filepath"
	"sync"
)

// SupervisionMode selects where Boatstack keeps its controller-owned state.
type SupervisionMode string

const (
	// SupervisionEmbedded keeps controller state inside the target repository
	// (.product-loop/**, host adapter dirs) and its Git directory — the original,
	// repository-owned layout.
	SupervisionEmbedded SupervisionMode = "embedded"
	// SupervisionDetached keeps controller state under an external, developer-local
	// control root, leaving the target repository free of Boatstack-owned files.
	// Detached resolution is wired in a later stage; the type is mode-aware now so
	// callers route every controller path through this one seam.
	SupervisionDetached SupervisionMode = "detached"
)

const (
	// productLoopDirName is the in-repo embedded controller directory.
	productLoopDirName = ".product-loop"
	// sourceConfigName is the editable root configuration file (embedded mode).
	sourceConfigName = ".boatstack-project.json"
	// controlDirName is the Boatstack subtree inside a Git directory (embedded) or
	// external control root (detached) that holds mutable controller state.
	controlDirName = "boatstack"
)

// WorkspaceContext is the single resolver for every Boatstack-owned path. Callers
// obtain controller locations through its methods instead of joining onto the
// repository root directly, so the embedded and detached layouts differ in exactly
// one place. The plant — product files, commits, branches, PRs — is always
// addressed through RepoRoot; everything controller-owned flows through the roots
// below.
//
// Path classes:
//   - embedded controller (generated, model-visible): GeneratedRoot / *ConfigPath
//   - per-worktree mutable controller: DeliveryDir / OperationDir / FlowDir
//   - shared immutable runtime: RuntimeDir
//   - host activation: HostActivationRoot
type WorkspaceContext struct {
	Mode       SupervisionMode
	RepoRoot   string // git worktree top-level (the plant)
	RepoID     string // stable repository identity (detached binding key; empty in embedded)
	WorktreeID string // stable per-worktree identity (detached; empty in embedded)

	// controlRoot is the base for generated/config state: RepoRoot in embedded
	// mode, the external repositories/<RepoID> root in detached mode.
	controlRoot string
	// worktreeControlRoot is the base for per-worktree mutable state. In embedded
	// mode it is left empty and derived lazily from the Git worktree directory; in
	// detached mode it is the external per-worktree directory.
	worktreeControlRoot string
	// sharedControlRoot is the base for shared immutable runtime state. In embedded
	// mode it is left empty and derived lazily from the Git common directory; in
	// detached mode it is the external shared root.
	sharedControlRoot string
}

// WorkspaceFor returns the resolver for a repository. It consults the external
// attachment registry and returns a detached context when the repository is
// attached and its binding verifies; otherwise it returns the embedded layout.
// An attached-but-unverifiable repository resolves to embedded here (best effort,
// so deep path callers stay total) — the fail-closed denial with a bounded
// recovery action is raised at the safety and CLI entry points via
// ResolveWorkspaceContext. Results are cached per input path; attach/detach
// invalidate the cache.
func WorkspaceFor(repo string) WorkspaceContext {
	workspaceCacheMu.Lock()
	if cached, ok := workspaceCache[repo]; ok {
		workspaceCacheMu.Unlock()
		return cached
	}
	workspaceCacheMu.Unlock()

	resolved := embeddedWorkspace(repo)
	if ctx, ok, err := detachedContextFor(repo); ok && err == nil {
		resolved = ctx
	}

	workspaceCacheMu.Lock()
	workspaceCache[repo] = resolved
	workspaceCacheMu.Unlock()
	return resolved
}

// ResolveWorkspaceContext is the strict resolver used at entry points that must
// fail closed: it returns an error for an attached-but-unverifiable repository
// (bad or missing binding, identity mismatch) instead of silently falling back to
// the embedded layout.
func ResolveWorkspaceContext(repo string) (WorkspaceContext, error) {
	ctx, ok, err := detachedContextFor(repo)
	if err != nil {
		return WorkspaceContext{}, err
	}
	if ok {
		return ctx, nil
	}
	return embeddedWorkspace(repo), nil
}

func embeddedWorkspace(repo string) WorkspaceContext {
	return WorkspaceContext{Mode: SupervisionEmbedded, RepoRoot: repo, controlRoot: repo}
}

var (
	workspaceCacheMu sync.Mutex
	workspaceCache   = map[string]WorkspaceContext{}
)

// invalidateWorkspaceCache clears the WorkspaceFor cache. Call it after any change
// to the attachment registry or bindings so subsequent resolution reflects it.
func invalidateWorkspaceCache() {
	workspaceCacheMu.Lock()
	workspaceCache = map[string]WorkspaceContext{}
	workspaceCacheMu.Unlock()
}

// configBase is the root under which the generated controller bundle (.product-loop
// and host adapter dirs) lives: the repository in embedded mode, the external
// control root in detached mode. Detached mirrors the embedded bundle layout so
// generation and guard resolution are reused unchanged, only relocated.
func (w WorkspaceContext) configBase() string {
	if w.Mode == SupervisionDetached {
		return w.controlRoot
	}
	return w.RepoRoot
}

// GeneratedRoot is the root of generated, model-visible controller files
// (references, templates, project.json). Embedded: <repo>/.product-loop.
func (w WorkspaceContext) GeneratedRoot() string {
	return filepath.Join(w.configBase(), productLoopDirName)
}

// ProjectConfigPath is the generated runtime configuration copy that runtime
// operations read. Embedded: <repo>/.product-loop/project.json.
func (w WorkspaceContext) ProjectConfigPath() string {
	return filepath.Join(w.configBase(), productLoopDirName, "project.json")
}

// SourceConfigPath is the editable, authoritative configuration source. Embedded:
// <repo>/.boatstack-project.json. Detached: the external control root's copy.
func (w WorkspaceContext) SourceConfigPath() string {
	return filepath.Join(w.configBase(), sourceConfigName)
}

// HostActivationRoot is the base under which a host adapter's activation files are
// written. Embedded: the repository root (adapters live in <repo>/.claude, ...).
// Detached: the external control root (host dirs live outside the repository).
func (w WorkspaceContext) HostActivationRoot() string {
	return w.configBase()
}

// worktreeControlDir is the base subtree for this worktree's mutable controller
// state. Embedded: <worktreeGitDir>/boatstack; detached: the external per-worktree
// control directory.
func (w WorkspaceContext) worktreeControlDir() (string, error) {
	if w.Mode == SupervisionDetached {
		return w.worktreeControlRoot, nil
	}
	gitDir, err := worktreeGitDir(w.RepoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, controlDirName), nil
}

// sharedControlDir is the base subtree for shared immutable runtime state.
// Embedded: <gitCommonDir>/boatstack; detached: the external shared root.
func (w WorkspaceContext) sharedControlDir() (string, error) {
	if w.Mode == SupervisionDetached {
		return w.sharedControlRoot, nil
	}
	common, err := gitCommonDir(w.RepoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, controlDirName), nil
}

// DeliveryDir holds per-worktree delivery state.
func (w WorkspaceContext) DeliveryDir() (string, error) {
	base, err := w.worktreeControlDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "deliveries"), nil
}

// OperationDir holds the per-worktree operation ledger. The "v2" segment orphans
// the pre-isolation clone-shared "v1" ledger (see operation.go).
func (w WorkspaceContext) OperationDir() (string, error) {
	base, err := w.worktreeControlDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "operations", "v2"), nil
}

// FlowDir holds the per-worktree append-only flow trajectory log.
func (w WorkspaceContext) FlowDir() (string, error) {
	base, err := w.worktreeControlDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "flow"), nil
}

// RuntimeDir holds the shared, version-namespaced runtime binary for the current
// platform. version and sourceCommit are validated as single safe path segments.
func (w WorkspaceContext) RuntimeDir(version, sourceCommit string) (string, error) {
	version, err := safeCacheSegment(version, "Boatstack version")
	if err != nil {
		return "", err
	}
	sourceCommit, err = safeCacheSegment(sourceCommit, "source commit")
	if err != nil {
		return "", err
	}
	base, err := w.sharedControlDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "runtimes", version, sourceCommit, platformKey()), nil
}
