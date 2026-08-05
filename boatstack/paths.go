package boatstack

import (
	"fmt"
	"path/filepath"
	"strings"
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

// controllerPath carries a Boatstack-owned path together with the boundary that
// owns it. Effectful callers validate this value instead of independently
// choosing a repository, Git, or detached-state root.
type controllerPath struct {
	path string
	root string
}

func newControllerPath(root, target string) (controllerPath, error) {
	if root == "" || target == "" {
		return controllerPath{}, fmt.Errorf("controller path ownership is incomplete")
	}
	owned := controllerPath{path: filepath.Clean(target), root: filepath.Clean(root)}
	if err := owned.Validate(); err != nil {
		return controllerPath{}, err
	}
	return owned, nil
}

func (p controllerPath) Validate() error {
	return rejectSymlinkComponents(p.root, p.path)
}

// Sibling derives another target without losing the owning boundary.
func (p controllerPath) Sibling(name string) (controllerPath, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return controllerPath{}, fmt.Errorf("invalid controller path name: %s", name)
	}
	return newControllerPath(p.root, filepath.Join(filepath.Dir(p.path), name))
}

func (w WorkspaceContext) worktreeOwnedPath(target string) (controllerPath, error) {
	if w.Mode == SupervisionDetached {
		return newControllerPath(w.sharedControlRoot, target)
	}
	root, err := worktreeGitDir(w.RepoRoot)
	if err != nil {
		return controllerPath{}, err
	}
	return newControllerPath(root, target)
}

func (w WorkspaceContext) sharedOwnedPath(target string) (controllerPath, error) {
	if w.Mode == SupervisionDetached {
		return newControllerPath(w.sharedControlRoot, target)
	}
	root, err := gitCommonDir(w.RepoRoot)
	if err != nil {
		return controllerPath{}, err
	}
	return newControllerPath(root, target)
}

// WorkspaceFor returns the resolver for a repository. It consults the external
// attachment registry and returns a detached context whenever the repository is
// attached. Verification failures do not redirect paths into the repository:
// strict operational entry points deny through ResolveWorkspaceContext, while
// best-effort path projection remains external. Results are cached per input
// path; attach/detach invalidate the cache.
func WorkspaceFor(repo string) WorkspaceContext {
	workspaceCacheMu.Lock()
	if cached, ok := workspaceCache[repo]; ok {
		workspaceCacheMu.Unlock()
		return cached
	}
	workspaceCacheMu.Unlock()

	resolved := embeddedWorkspace(repo)
	if ctx, ok, _ := detachedContextFor(repo); ok {
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

func pathWithin(root, target string) bool {
	root = canonicalizeExistingAncestor(root)
	target = canonicalizeExistingAncestor(target)
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ResolveControllerRepository maps either a product path or a detached
// controller path back to the repository whose identity owns it. This is the
// inverse boundary required by plan validation after FeatureDir moves outside
// the Git worktree.
func ResolveControllerRepository(path string) (string, error) {
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return "", err
	}
	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return "", err
	}
	for repo := range registry.Repositories {
		ctx, ok, _ := detachedContextFor(repo)
		if !ok {
			continue
		}
		if pathWithin(ctx.ExportRoot(), path) {
			return repo, nil
		}
	}
	if repo, err := ResolveRepository(path); err == nil {
		return repo, nil
	}
	return "", fmt.Errorf("path is not owned by a repository or verified detached controller: %s", path)
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

// ExportRoot is the base beneath which generated bundle paths are materialized.
// Bundle keys include .product-loop and host-adapter directories, so callers
// must pass this root — never RepoRoot — to export write/check operations.
func (w WorkspaceContext) ExportRoot() string {
	return w.configBase()
}

// FeatureRoot owns generated planning and delivery artifacts. Source plans are
// product inputs and remain at their declared repository paths; everything
// compiled from them lives below this controller-owned root.
func (w WorkspaceContext) FeatureRoot() string {
	return filepath.Join(w.GeneratedRoot(), "features")
}

// FeatureDir returns one validated feature package directory. Invalid slugs
// return an empty path so no caller can accidentally escape the ownership root.
func (w WorkspaceContext) FeatureDir(feature string) string {
	if !featureSlugPattern.MatchString(feature) {
		return ""
	}
	return filepath.Join(w.FeatureRoot(), feature)
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

// InsightDir is the tracked repository inbox for independent insight captures.
// Unlike controller state, insight content is a plant artifact: every capture
// and event must be visible as a reviewable Git diff and must never be routed to
// the Git directory or detached control root.
func (w WorkspaceContext) InsightDir() (string, error) {
	return filepath.Join(w.RepoRoot, "docs", "insights"), nil
}

// GuardDir holds the per-worktree guard bookkeeping (the denial ledger). It is
// worktree-partitioned like the delivery state: one worktree's denial history
// must never escalate a sibling's denials.
// control-law: repeated-denials-escalate-to-solutions
func (w WorkspaceContext) GuardDir() (string, error) {
	base, err := w.worktreeControlDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "guard"), nil
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
