package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Detached Supervision keeps Boatstack's controller-owned state outside the target
// repository. This file resolves the external control root, computes a stable
// repository identity, and manages the attachment registry and per-repository
// binding that let WorkspaceFor return a detached layout for an attached repo.

const (
	// stateRootEnv overrides the external control-state root. Tests inject a temp
	// directory through it so they never read or write a real home directory.
	stateRootEnv = "BOATSTACK_STATE_ROOT"
	// detachedSchemaVersion versions the public detached status and binding
	// records. Version 2 binds the exact detached project configuration bytes.
	detachedSchemaVersion = 2
	// The registry remains a path-to-repository index. Configuration provenance
	// belongs to the authoritative per-repository binding, not this index.
	detachedRegistrySchemaVersion = 1
	// repoIDLength is the hex width of a repository identity key.
	repoIDLength = 16
	// worktreeIDLength is the hex width of a per-worktree identity key.
	worktreeIDLength = 12
)

// detachedStateRoot returns the base directory for all detached controller state,
// always ending in a "boatstack" segment. Resolution order: the test/override env
// var, then the OS-appropriate user state directory, then a home-dir fallback.
func detachedStateRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv(stateRootEnv)); override != "" {
		return filepath.Join(override, "boatstack"), nil
	}
	switch runtime.GOOS {
	case "windows":
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "boatstack"), nil
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "boatstack"), nil
		}
	default: // linux and other unix
		if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
			return filepath.Join(base, "boatstack"), nil
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "state", "boatstack"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve a user state directory for detached Boatstack: %w", err)
	}
	return filepath.Join(home, ".boatstack"), nil
}

// RepoIdentity is the independently verifiable identity of a repository, used to
// bind an external control root to exactly one repo and to fail closed when the
// bound repo's identity no longer matches.
type RepoIdentity struct {
	RepoID            string
	CanonicalRepoPath string
	GitCommonIdentity string
	InitialCommit     string
	NormalizedOrigin  string
	WorktreeID        string
}

// normalizeOrigin reduces an origin URL to a host/owner/repo key that is stable
// across https/ssh/git protocols and a trailing .git, so the same remote yields
// the same repo identity regardless of clone URL form.
func normalizeOrigin(url string) string {
	value := strings.TrimSpace(url)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, ".git")
	for _, prefix := range []string{"https://", "http://", "ssh://", "git+ssh://"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.TrimPrefix(value, "git@")
	// user@host:owner/repo → host/owner/repo
	if at := strings.Index(value, "@"); at >= 0 && at < strings.IndexAny(value+"/", "/") {
		value = value[at+1:]
	}
	value = strings.ReplaceAll(value, ":", "/")
	return strings.ToLower(strings.Trim(value, "/"))
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexAny(value, "\r\n"); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

// repoIdentity derives a stable identity for the repository containing repo. The
// repo_id prefers remote-and-history identity (normalized origin + initial commit)
// so a moved or renamed checkout keeps the same binding; it falls back to the Git
// common directory only when neither is available (a local-only, history-less
// repo). The worktree_id isolates per-worktree mutable state.
func repoIdentity(repo string) (RepoIdentity, error) {
	root, err := ResolveRepository(repo)
	if err != nil {
		return RepoIdentity{}, err
	}
	common, err := gitCommonDir(root)
	if err != nil {
		return RepoIdentity{}, err
	}
	worktreeDir, err := worktreeGitDir(root)
	if err != nil {
		return RepoIdentity{}, err
	}
	initial := firstLine(gitOutput(root, "rev-list", "--max-parents=0", "HEAD"))
	origin := normalizeOrigin(gitOutput(root, "remote", "get-url", "origin"))

	seed := origin
	if initial != "" {
		seed += "@" + initial
	}
	if seed == "" {
		// Local-only, history-less repo: fall back to the clone-wide common dir so
		// the identity is at least stable for this clone.
		seed = "gitdir:" + common
	}
	identity := RepoIdentity{
		RepoID:            SHA256Bytes([]byte(seed))[:repoIDLength],
		CanonicalRepoPath: root,
		GitCommonIdentity: common,
		InitialCommit:     initial,
		NormalizedOrigin:  origin,
		WorktreeID:        SHA256Bytes([]byte(worktreeDir))[:worktreeIDLength],
	}
	return identity, nil
}

// DetachedBinding is the per-repository record stored under the external control
// root. It carries enough independently verifiable identity that a control root
// can never be applied to the wrong repository.
type DetachedBinding struct {
	SchemaVersion     int    `json:"schema_version"`
	Mode              string `json:"mode"`
	RepoID            string `json:"repo_id"`
	CanonicalRepoPath string `json:"canonical_repo_path"`
	GitCommonIdentity string `json:"git_common_identity"`
	InitialCommit     string `json:"initial_commit"`
	NormalizedOrigin  string `json:"normalized_origin"`
	ConfigSHA256      string `json:"config_sha256"`
	CreatedByVersion  string `json:"created_by_version"`
	CreatedAt         string `json:"created_at"`
}

// detachedRegistry is the fast index from a canonical repository path to its
// repo_id. The per-repository binding.json is authoritative; the registry only
// speeds the common "is this path attached?" lookup.
type detachedRegistry struct {
	SchemaVersion int               `json:"schema_version"`
	Repositories  map[string]string `json:"repositories"`
}

func registryPath(stateRoot string) string { return filepath.Join(stateRoot, "registry.json") }

func repositoryControlRoot(stateRoot, repoID string) string {
	return filepath.Join(stateRoot, "repositories", repoID)
}

func bindingPath(stateRoot, repoID string) string {
	return filepath.Join(repositoryControlRoot(stateRoot, repoID), "binding.json")
}

func loadRegistry(stateRoot string) (detachedRegistry, error) {
	registry := detachedRegistry{SchemaVersion: detachedRegistrySchemaVersion, Repositories: map[string]string{}}
	raw, err := os.ReadFile(registryPath(stateRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return registry, err
	}
	if err := DecodeJSON("load detached registry", registryPath(stateRoot), raw, &registry); err != nil {
		return detachedRegistry{}, err
	}
	if registry.Repositories == nil {
		registry.Repositories = map[string]string{}
	}
	return registry, nil
}

func saveRegistry(stateRoot string, registry detachedRegistry) error {
	registry.SchemaVersion = detachedRegistrySchemaVersion
	raw, err := MarshalJSON(registry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return err
	}
	return os.WriteFile(registryPath(stateRoot), raw, 0o644)
}

func loadBinding(stateRoot, repoID string) (DetachedBinding, error) {
	var binding DetachedBinding
	raw, err := os.ReadFile(bindingPath(stateRoot, repoID))
	if err != nil {
		return DetachedBinding{}, err
	}
	if err := DecodeJSON("load detached binding", bindingPath(stateRoot, repoID), raw, &binding); err != nil {
		return DetachedBinding{}, err
	}
	return binding, nil
}

// bindingMatchesIdentity reports whether a stored binding still describes the
// repository now on disk. It compares the strong identity components; the
// canonical path may legitimately change (a moved checkout) so it is not required
// to match as long as origin+history do.
func bindingMatchesIdentity(binding DetachedBinding, identity RepoIdentity) bool {
	if binding.RepoID != identity.RepoID {
		return false
	}
	if binding.NormalizedOrigin != identity.NormalizedOrigin {
		return false
	}
	if binding.InitialCommit != identity.InitialCommit {
		return false
	}
	return true
}

type detachedGeneratedLock struct {
	ConfigSHA256 string            `json:"config_sha256"`
	Files        map[string]string `json:"files"`
}

// verifyDetachedConfiguration proves that the authoritative source copy, its
// generated snapshot, and the generated runtime configuration still describe
// the exact bytes accepted at attachment.
// control-law: detached-config-digest-gates-resume
func verifyDetachedConfiguration(ctx WorkspaceContext, binding DetachedBinding) error {
	if binding.SchemaVersion != detachedSchemaVersion {
		return fmt.Errorf("detached binding schema_version %d is unsupported; reattach with `boatstack-helper attach --repo %s --mode detached --force --config <path>`", binding.SchemaVersion, ctx.RepoRoot)
	}
	if strings.TrimSpace(binding.ConfigSHA256) == "" {
		return fmt.Errorf("detached binding is missing config_sha256; reattach with `boatstack-helper attach --repo %s --mode detached --force --config <path>`", ctx.RepoRoot)
	}
	sourceSHA, err := SHA256File(ctx.SourceConfigPath())
	if err != nil {
		return fmt.Errorf("detached project configuration is missing or unreadable: %w", err)
	}
	if sourceSHA != binding.ConfigSHA256 {
		return fmt.Errorf("detached project configuration drifted from bound SHA-256 %s; restore the exact attached bytes or reattach with `boatstack-helper attach --repo %s --mode detached --force --config <path>`", binding.ConfigSHA256, ctx.RepoRoot)
	}
	lockPath := filepath.Join(ctx.GeneratedRoot(), "generated.lock.json")
	lockRaw, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("detached generated configuration snapshot is missing or unreadable: %w", err)
	}
	var lock detachedGeneratedLock
	if err := DecodeJSON("verify detached generated configuration snapshot", lockPath, lockRaw, &lock); err != nil {
		return err
	}
	if lock.ConfigSHA256 != binding.ConfigSHA256 {
		return fmt.Errorf("detached generated configuration snapshot does not match bound SHA-256 %s", binding.ConfigSHA256)
	}
	expectedProjectSHA := lock.Files[productLoopDirName+"/project.json"]
	if expectedProjectSHA == "" {
		return fmt.Errorf("detached generated configuration snapshot does not bind %s/project.json", productLoopDirName)
	}
	projectSHA, err := SHA256File(ctx.ProjectConfigPath())
	if err != nil {
		return fmt.Errorf("detached generated project configuration is missing or unreadable: %w", err)
	}
	if projectSHA != expectedProjectSHA {
		return fmt.Errorf("detached generated project configuration drifted from its snapshot")
	}
	return nil
}

// detachedContextFor returns the detached WorkspaceContext for repo when the
// repository is attached and its binding verifies. ok is false for an unattached
// repository (the caller should use the embedded layout). err is non-nil only for
// an attached-but-unverifiable repository — the fail-closed case.
func detachedContextFor(repo string) (ctx WorkspaceContext, ok bool, err error) {
	stateRoot, rootErr := detachedStateRoot()
	if rootErr != nil {
		return WorkspaceContext{}, false, nil // no external root resolvable → treat as embedded
	}
	registry, regErr := loadRegistry(stateRoot)
	if regErr != nil {
		return WorkspaceContext{}, false, regErr
	}
	if len(registry.Repositories) == 0 {
		return WorkspaceContext{}, false, nil // nothing attached anywhere → embedded, no git calls
	}
	repoID, listed := registry.Repositories[repo]
	if !listed {
		// The caller may have passed a non-canonical path; resolve the root once
		// (only reached when a detached registry actually has entries).
		root, resolveErr := ResolveRepository(repo)
		if resolveErr != nil {
			return WorkspaceContext{}, false, nil
		}
		if repoID, listed = registry.Repositories[root]; !listed {
			return WorkspaceContext{}, false, nil
		}
		repo = root
	}
	identity, idErr := repoIdentity(repo)
	if idErr != nil {
		return WorkspaceContext{}, true, fmt.Errorf("detached binding cannot be verified: %w", idErr)
	}
	binding, bindErr := loadBinding(stateRoot, repoID)
	ctx = detachedContextFromIdentity(stateRoot, identity)
	if bindErr != nil {
		return ctx, true, fmt.Errorf("detached binding for %s is missing or unreadable: %w", repo, bindErr)
	}
	if !bindingMatchesIdentity(binding, identity) {
		return ctx, true, fmt.Errorf("detached binding does not match this repository's identity; reattach with `boatstack-helper attach` or migrate the binding")
	}
	if configErr := verifyDetachedConfiguration(ctx, binding); configErr != nil {
		return ctx, true, configErr
	}
	return ctx, true, nil
}

// detachedContextFromIdentity builds the detached WorkspaceContext for a resolved
// identity under a state root. Runtimes are shared across all detached repos
// (stateRoot), per-repository generated/config state lives under the repo control
// root, and mutable per-worktree state lives under a worktree subdirectory.
func detachedContextFromIdentity(stateRoot string, identity RepoIdentity) WorkspaceContext {
	control := repositoryControlRoot(stateRoot, identity.RepoID)
	return WorkspaceContext{
		Mode:                SupervisionDetached,
		RepoRoot:            identity.CanonicalRepoPath,
		RepoID:              identity.RepoID,
		WorktreeID:          identity.WorktreeID,
		controlRoot:         control,
		sharedControlRoot:   stateRoot,
		worktreeControlRoot: filepath.Join(control, "worktrees", identity.WorktreeID),
	}
}

func nowRFC3339() string { return operationNow().UTC().Truncate(time.Second).Format(time.RFC3339) }

// RepositoryIsManaged reports whether Boatstack supervises a repository — either
// through a detached attachment (verified or not: an attached-but-broken repo is
// still managed and must fail closed) or an embedded in-repo install. It is the
// gate a developer-level guard uses to leave unmanaged repositories uncontrolled.
func RepositoryIsManaged(repo string) bool {
	if _, ok, _ := detachedContextFor(repo); ok {
		return true
	}
	return fileExists(filepath.Join(repo, productLoopDirName, "project.json"))
}
