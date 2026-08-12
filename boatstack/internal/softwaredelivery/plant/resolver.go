package plant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

const stateRootEnvironment = "BOATSTACK_STATE_ROOT"

type CommandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).Output()
}

type Resolver struct {
	externalRoot       string
	runtimePath        string
	runtimeVersion     string
	runtimeFingerprint string
	runner             CommandRunner
}

func NewResolver(externalBase string) (Resolver, error) {
	root, err := externalStateRoot(externalBase)
	if err != nil {
		return Resolver{}, err
	}
	runtimePath, err := os.Executable()
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve executing runtime: %w", err)
	}
	runtimePath, err = filepath.Abs(runtimePath)
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve executing runtime path: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(runtimePath); resolveErr == nil {
		runtimePath = resolved
	}
	runtimeRaw, err := os.ReadFile(runtimePath)
	if err != nil {
		return Resolver{}, fmt.Errorf("read executing runtime: %w", err)
	}
	return Resolver{externalRoot: root, runtimePath: runtimePath, runtimeVersion: buildinfo.Version, runtimeFingerprint: digest(string(runtimeRaw), 0), runner: execRunner{}}, nil
}

func NewResolverWithRunner(externalBase string, runner CommandRunner) (Resolver, error) {
	resolver, err := NewResolver(externalBase)
	if err != nil {
		return Resolver{}, err
	}
	if runner == nil {
		return Resolver{}, fmt.Errorf("plant resolver requires a command runner")
	}
	resolver.runner = runner
	return resolver, nil
}

func externalStateRoot(explicit string) (string, error) {
	if explicit == "" {
		explicit = strings.TrimSpace(os.Getenv(stateRootEnvironment))
	}
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		return filepath.Join(absolute, "boatstack", "v2"), nil
	}
	if runtime.GOOS == "windows" {
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "boatstack", "v2"), nil
		}
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "boatstack", "v2"), nil
	}
	if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
		return filepath.Join(base, "boatstack", "v2"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "boatstack", "v2"), nil
}

func canonicalExisting(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func digest(value string, width int) string {
	sum := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(sum[:])
	if width > 0 && width < len(encoded) {
		return encoded[:width]
	}
	return encoded
}

func normalizeOrigin(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, ".git"))
	for _, prefix := range []string{"https://", "http://", "ssh://", "git+ssh://"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.TrimPrefix(value, "git@")
	value = strings.ReplaceAll(value, ":", "/")
	return strings.ToLower(strings.Trim(value, "/"))
}

func (r Resolver) git(ctx context.Context, path string, arguments ...string) (string, error) {
	output, err := r.runner.Output(ctx, "git", append([]string{"-C", path}, arguments...)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r Resolver) ResolveInvocation(ctx context.Context, path, host, correlation string) (model.InvocationContext, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(host) == "" || strings.TrimSpace(correlation) == "" {
		return model.InvocationContext{}, fmt.Errorf("repository path, host, and correlation are required")
	}
	invokingPath, err := canonicalExisting(path)
	if err != nil {
		return model.InvocationContext{}, fmt.Errorf("resolve invoking path: %w", err)
	}
	rootValue, err := r.git(ctx, invokingPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return model.InvocationContext{}, fmt.Errorf("resolve repository: %w", err)
	}
	repositoryRoot, err := canonicalExisting(rootValue)
	if err != nil {
		return model.InvocationContext{}, err
	}
	commonValue, err := r.git(ctx, repositoryRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return model.InvocationContext{}, fmt.Errorf("resolve git common identity: %w", err)
	}
	gitCommon, err := canonicalExisting(commonValue)
	if err != nil {
		return model.InvocationContext{}, err
	}
	gitDirValue, err := r.git(ctx, repositoryRoot, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return model.InvocationContext{}, fmt.Errorf("resolve worktree identity: %w", err)
	}
	if _, err := canonicalExisting(gitDirValue); err != nil {
		return model.InvocationContext{}, err
	}
	origin, _ := r.git(ctx, repositoryRoot, "remote", "get-url", "origin")
	initial, _ := r.git(ctx, repositoryRoot, "rev-list", "--max-parents=0", "HEAD")
	seed := normalizeOrigin(origin)
	if initial != "" {
		seed += "@" + strings.Split(initial, "\n")[0]
	}
	if seed == "" {
		seed = "git-common:" + gitCommon
	}
	repositoryID := digest(seed, 16)
	gitCommonID := digest(gitCommon, 16)
	worktreeID, err := model.DeriveWorktreeID(gitCommonID, repositoryRoot)
	if err != nil {
		return model.InvocationContext{}, err
	}
	ref, err := r.git(ctx, repositoryRoot, "symbolic-ref", "-q", "HEAD")
	if err != nil || ref == "" {
		head, headErr := r.git(ctx, repositoryRoot, "rev-parse", "HEAD")
		if headErr != nil {
			return model.InvocationContext{}, fmt.Errorf("resolve git ref: %w", headErr)
		}
		ref = "detached:" + head
	}

	topology := model.TopologyEmbedded
	controllerID := digest("embedded:"+repositoryID+":"+gitCommonID, 20)
	bindingPath := filepath.Join(r.externalRoot, "repositories", repositoryID, gitCommonID, "binding.json")
	if raw, readErr := os.ReadFile(bindingPath); readErr == nil {
		binding, decodeErr := durable.DecodeBinding(raw)
		if decodeErr != nil {
			return model.InvocationContext{}, fmt.Errorf("decode external binding: %w", decodeErr)
		}
		if binding.RepositoryID != repositoryID || binding.GitCommonID != gitCommonID {
			return model.InvocationContext{}, fmt.Errorf("external binding identity conflicts with invoking repository")
		}
		topology, controllerID = binding.Topology, binding.ControllerID
	} else if !os.IsNotExist(readErr) {
		return model.InvocationContext{}, fmt.Errorf("read external binding: %w", readErr)
	}
	invocation := model.InvocationContext{
		RepositoryID: repositoryID, GitCommonID: gitCommonID, WorktreeID: worktreeID, Ref: ref,
		ControllerID: controllerID, InvokingPath: invokingPath, RuntimeVersion: r.runtimeVersion, RuntimePath: r.runtimePath, RuntimeFingerprint: r.runtimeFingerprint,
		Topology: topology, Host: host, Correlation: correlation,
	}
	if err := invocation.Validate(true); err != nil {
		return model.InvocationContext{}, err
	}
	return invocation, nil
}

func (r Resolver) ResolveLayout(ctx context.Context, invocation model.InvocationContext) (ports.ControllerLayout, model.InvocationContext, error) {
	current, err := r.ResolveInvocation(ctx, invocation.InvokingPath, invocation.Host, invocation.Correlation)
	if err != nil {
		return ports.ControllerLayout{}, model.InvocationContext{}, err
	}
	if current.RepositoryID != invocation.RepositoryID || current.GitCommonID != invocation.GitCommonID || current.WorktreeID != invocation.WorktreeID {
		return ports.ControllerLayout{}, current, fmt.Errorf("invocation identity no longer resolves to the same repository and worktree")
	}
	repositoryRootValue, err := r.git(ctx, current.InvokingPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return ports.ControllerLayout{}, current, err
	}
	repositoryRoot, err := canonicalExisting(repositoryRootValue)
	if err != nil {
		return ports.ControllerLayout{}, current, err
	}
	commonValue, err := r.git(ctx, repositoryRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ports.ControllerLayout{}, current, err
	}
	commonRoot, err := canonicalExisting(commonValue)
	if err != nil {
		return ports.ControllerLayout{}, current, err
	}
	gitDirValue, err := r.git(ctx, repositoryRoot, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return ports.ControllerLayout{}, current, err
	}
	if _, err := canonicalExisting(gitDirValue); err != nil {
		return ports.ControllerLayout{}, current, err
	}
	bindingPath := filepath.Join(r.externalRoot, "repositories", current.RepositoryID, current.GitCommonID, "binding.json")
	embeddedSharedRoot := filepath.Join(commonRoot, "boatstack", "v2")
	embeddedStateRoot := filepath.Join(embeddedSharedRoot, "worktrees", current.WorktreeID)
	externalSharedRoot := filepath.Join(r.externalRoot, "repositories", current.RepositoryID, current.GitCommonID)
	externalStateRoot := filepath.Join(externalSharedRoot, "worktrees", current.WorktreeID)
	stateRoot := embeddedStateRoot
	sharedRoot := embeddedSharedRoot
	configAuthority := "repository"
	if current.Topology == model.TopologyDetached || current.Topology == model.TopologyHybrid {
		sharedRoot = externalSharedRoot
		stateRoot = externalStateRoot
		raw, readErr := os.ReadFile(bindingPath)
		if readErr != nil {
			return ports.ControllerLayout{}, current, readErr
		}
		binding, decodeErr := durable.DecodeBinding(raw)
		if decodeErr != nil {
			return ports.ControllerLayout{}, current, decodeErr
		}
		configAuthority = binding.ConfigAuthority
	}
	configPath := filepath.Join(repositoryRoot, ".boatstack", "project.json")
	if configAuthority == "external" {
		configPath = filepath.Join(externalSharedRoot, "project.json")
	}
	layout := ports.ControllerLayout{
		RepositoryRoot: repositoryRoot, GitCommonRoot: commonRoot, StateRoot: stateRoot, SharedRoot: sharedRoot, FlowRoot: externalSharedRoot,
		EmbeddedStateRoot: embeddedStateRoot, ExternalStateRoot: externalStateRoot,
		StatePath: filepath.Join(stateRoot, "state.json"), BindingPath: bindingPath,
		JournalRoot: filepath.Join(externalSharedRoot, "journals"), ReceiptPath: filepath.Join(externalSharedRoot, "receipts.jsonl"),
		EventPath: filepath.Join(externalSharedRoot, "events.jsonl"), LockRoot: filepath.Join(externalSharedRoot, "locks"),
		ConfigPath: configPath, ConfigAuthority: configAuthority, EvidenceRoot: filepath.Join(repositoryRoot, ".boatstack", "evidence"),
	}
	return layout, current, nil
}
