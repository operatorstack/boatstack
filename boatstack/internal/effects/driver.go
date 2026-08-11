package effects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

type CommandBoundary interface {
	PrepareObservation(context.Context, protocol.Admission, catalog.Transition, ports.ControllerLayout, *durable.State) error
	Execute(context.Context, protocol.Admission, catalog.Transition, ports.ControllerLayout, durable.State) (ports.EffectResult, error)
}

type Driver struct {
	resolver          ports.InvocationResolver
	clock             ports.Clock
	boundary          CommandBoundary
	resourceOwnership map[string]string
}

func NewDriver(resolver ports.InvocationResolver, clock ports.Clock, boundary CommandBoundary) (Driver, error) {
	if resolver == nil || clock == nil || boundary == nil {
		return Driver{}, fmt.Errorf("effect driver requires resolver, clock, and command boundary")
	}
	return Driver{resolver: resolver, clock: clock, boundary: boundary}, nil
}

func NewProgramDriver(resolver ports.InvocationResolver, clock ports.Clock, boundary CommandBoundary, ownership map[string]string) (Driver, error) {
	driver, err := NewDriver(resolver, clock, boundary)
	if err != nil {
		return Driver{}, err
	}
	if len(ownership) == 0 {
		return Driver{}, fmt.Errorf("effect driver requires compiled resource ownership")
	}
	driver.resourceOwnership = make(map[string]string, len(ownership))
	for resource, owner := range ownership {
		driver.resourceOwnership[resource] = owner
	}
	return driver, nil
}

func (d Driver) Prepare(ctx context.Context, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	if len(d.resourceOwnership) != 0 {
		for _, resource := range transition.OwnedResources {
			if owner := d.resourceOwnership[resource]; owner == "" || owner != transition.Owner {
				return nil, fmt.Errorf("transition %q cannot write resource %q owned by %q", transition.ID, resource, owner)
			}
		}
	}
	layout, currentInvocation, err := d.resolver.ResolveLayout(ctx, admission.Invocation)
	if err != nil {
		return nil, err
	}
	if currentInvocation.RepositoryID != admission.Invocation.RepositoryID || currentInvocation.GitCommonID != admission.Invocation.GitCommonID || currentInvocation.WorktreeID != admission.Invocation.WorktreeID {
		return nil, fmt.Errorf("effect invocation identity changed before preparation")
	}
	if transition.ID == "recovery.resume" || transition.ID == "recovery.rollback" || transition.ID == "workspace.reconcile" {
		return d.prepareRecoveryReplay(ctx, layout, admission, transition)
	}
	state, err := loadDurableState(layout.StatePath, admission.Invocation, d.clock.Now())
	if err != nil {
		return nil, err
	}
	if state.RepositoryID != admission.Invocation.RepositoryID || state.GitCommonID != admission.Invocation.GitCommonID || state.WorktreeID != admission.Invocation.WorktreeID {
		return nil, fmt.Errorf("durable state belongs to a different invocation")
	}
	if state.ProgramFingerprint != "" && state.ProgramFingerprint != admission.ProgramFingerprint && !transition.Policy.ReconcilesProgram {
		return nil, fmt.Errorf("compiled control program drifted; explicit program reconciliation is required")
	}
	if transition.ID == "catalog.reconcile" && (state.RuntimePath != admission.Invocation.RuntimePath || state.RuntimeFingerprint != admission.Invocation.RuntimeFingerprint) {
		return nil, fmt.Errorf("catalog reconciliation cannot activate a different runtime; use installation.reconcile-update")
	}
	if err := verifyWorkspaceBranchParameter(state, admission, transition.ID); err != nil {
		return nil, err
	}
	if err := verifyRuntimeParameters(admission, transition); err != nil {
		return nil, err
	}
	next := state
	if next.ProgramFingerprint == "" {
		next.ProgramFingerprint = admission.ProgramFingerprint
	}
	if err := d.boundary.PrepareObservation(ctx, admission, transition, layout, &next); err != nil {
		return nil, err
	}
	if err := applyStateTransition(&next, admission, transition); err != nil {
		return nil, err
	}
	var launcherMutation *ports.ResourceMutation
	if transition.ID == "installation.initialize" || transition.ID == "installation.update" || transition.ID == "installation.reconcile-update" {
		mutation, launcherPath, launcherFingerprint, launcherErr := prepareLauncherMutation(admission)
		if launcherErr != nil {
			return nil, launcherErr
		}
		next.LauncherPath, next.LauncherFingerprint = launcherPath, launcherFingerprint
		launcherMutation = &mutation
	}
	next.Revision++
	next.UpdatedAt = d.clock.Now().UTC()
	var verificationInvocation *model.InvocationContext
	if transition.ID == "workspace.cut" {
		destination, destinationErr := canonicalWorkspaceDestination(admission)
		if destinationErr != nil {
			return nil, destinationErr
		}
		destinationID, destinationErr := model.DeriveWorktreeID(admission.Invocation.GitCommonID, destination)
		if destinationErr != nil {
			return nil, destinationErr
		}
		branch, _ := admission.Parameters.Get("branch")
		targetInvocation := admission.Invocation
		targetInvocation.WorktreeID = destinationID
		targetInvocation.InvokingPath = destination
		targetInvocation.Ref = "refs/heads/" + branch
		verificationInvocation = &targetInvocation
		next.WorktreeID = destinationID
		next.WorkspacePath = destination
		next.SourceRevision = ""
		next.WorktreeFingerprint = ""
		next.Verification = model.VerificationUnverified
	} else if transition.ID == "workspace.cleanup" || transition.ID == "workspace.reap" {
		targetInvocation, targetErr := d.resolver.ResolveInvocation(ctx, state.WorkspaceSourcePath, admission.Invocation.Host, admission.Invocation.Correlation)
		if targetErr != nil {
			return nil, fmt.Errorf("resolve preserved source checkout before workspace removal: %w", targetErr)
		}
		if targetInvocation.RepositoryID != admission.Invocation.RepositoryID || targetInvocation.GitCommonID != admission.Invocation.GitCommonID ||
			targetInvocation.WorktreeID != state.WorkspaceSourceID || targetInvocation.Ref != state.WorkspaceSourceRef ||
			targetInvocation.ControllerID != admission.Invocation.ControllerID || targetInvocation.Topology != admission.Invocation.Topology {
			return nil, fmt.Errorf("preserved source checkout identity changed; refusing workspace removal")
		}
		verificationInvocation = &targetInvocation
		next.WorktreeID = targetInvocation.WorktreeID
		next.WorkspaceBranch = ""
		next.WorkspacePath = ""
		next.WorkspaceBaseRef = ""
		next.WorkspaceSourcePath = ""
		next.WorkspaceSourceID = ""
		next.WorkspaceSourceRef = ""
	}

	mutations, err := prepareArtifacts(layout, admission, transition, &next)
	if err != nil {
		return nil, err
	}
	if launcherMutation != nil {
		mutations = append(mutations, *launcherMutation)
	}
	statePath := layout.StatePath
	stateInstallLast := true
	if transition.ID == "repository.attach" {
		statePath = filepath.Join(layout.ExternalStateRoot, "state.json")
		stateInstallLast = false
		configMutations, configErr := prepareConfigurationAuthorityTransfer(layout, admission, state, true)
		if configErr != nil {
			return nil, configErr
		}
		mutations = append(mutations, configMutations...)
		bindingMutation, mutationErr := prepareAttachBinding(layout, admission, d.clock.Now())
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, bindingMutation)
	} else if transition.ID == "repository.detach" {
		statePath = filepath.Join(layout.EmbeddedStateRoot, "state.json")
		stateInstallLast = false
		configMutations, configErr := prepareConfigurationAuthorityTransfer(layout, admission, state, false)
		if configErr != nil {
			return nil, configErr
		}
		mutations = append(mutations, configMutations...)
		bindingMutation, mutationErr := mutationFor(layout.BindingPath, nil, 0o600, true, true)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, bindingMutation)
	}
	if transition.ID == "workspace.cut" {
		parked := parkedSourceState(state, transition.ID, d.clock.Now())
		parkedRaw, encodeErr := durable.EncodeState(parked)
		if encodeErr != nil {
			return nil, encodeErr
		}
		parkedMutation, mutationErr := mutationFor(statePath, parkedRaw, 0o600, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, parkedMutation)
		destinationRaw, encodeErr := durable.EncodeState(next)
		if encodeErr != nil {
			return nil, encodeErr
		}
		destinationStatePath := filepath.Join(layout.SharedRoot, "worktrees", next.WorktreeID, "state.json")
		destinationMutation, mutationErr := mutationFor(destinationStatePath, destinationRaw, 0o600, true, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, destinationMutation)
	} else if transition.ID == "workspace.cleanup" || transition.ID == "workspace.reap" {
		currentStateRemoval, mutationErr := mutationFor(statePath, nil, 0o600, false, true)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, currentStateRemoval)
		targetRaw, encodeErr := durable.EncodeState(next)
		if encodeErr != nil {
			return nil, encodeErr
		}
		targetStatePath := filepath.Join(layout.SharedRoot, "worktrees", next.WorktreeID, "state.json")
		targetMutation, mutationErr := mutationFor(targetStatePath, targetRaw, 0o600, true, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, targetMutation)
	} else {
		stateRaw, encodeErr := durable.EncodeState(next)
		if encodeErr != nil {
			return nil, encodeErr
		}
		stateMutation, mutationErr := mutationFor(statePath, stateRaw, 0o600, stateInstallLast, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, stateMutation)
	}
	if closesInterruptedJournal(transition.ID) {
		transactionID, _ := admission.Parameters.Get("transaction_id")
		recoveryMutations, recoveryErr := prepareJournalClosure(layout, transactionID, string(transition.ID), d.clock.Now(), admission.ID)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		mutations = append(mutations, recoveryMutations...)
	}
	for index := range mutations {
		if len(transition.OwnedResources) == 0 {
			return nil, fmt.Errorf("transition %q produced an undeclared resource write", transition.ID)
		}
		mutations[index].Resource = transition.OwnedResources[0]
		mutations[index].Owner = transition.Owner
	}
	prepared := &preparedEffect{mutations: mutations, verifyInvocation: verificationInvocation}
	if requiresCommandBoundary(transition.ID) {
		prepared.boundary = func(boundaryContext context.Context) (ports.EffectResult, error) {
			return d.boundary.Execute(boundaryContext, admission, transition, layout, state)
		}
	}
	return prepared, nil
}

func prepareConfigurationAuthorityTransfer(layout ports.ControllerLayout, admission protocol.Admission, state durable.State, attaching bool) ([]ports.ResourceMutation, error) {
	target := ""
	if attaching {
		authority, _ := admission.Parameters.Get("config_authority")
		if authority != "external" {
			return nil, nil
		}
		target = filepath.Join(layout.FlowRoot, "project.json")
	} else {
		if layout.ConfigAuthority != "external" {
			return nil, nil
		}
		target = filepath.Join(layout.RepositoryRoot, ".boatstack", "project.json")
	}
	raw, err := os.ReadFile(layout.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return nil, err
	}
	if state.Configuration != model.ConfigurationVerified || state.ConfigFingerprint == "" || fingerprint != state.ConfigFingerprint {
		return nil, nil
	}
	mutation, err := mutationFor(target, raw, 0o600, false, false)
	if err != nil {
		return nil, err
	}
	return []ports.ResourceMutation{mutation}, nil
}

func verifyWorkspaceBranchParameter(state durable.State, admission protocol.Admission, id catalog.TransitionID) error {
	switch id {
	case "workspace.sync", "workspace.activate", "workspace.publish", "workspace.cleanup", "workspace.reap", "workspace.abandon":
	default:
		return nil
	}
	branch, _ := admission.Parameters.Get("branch")
	if state.WorkspaceBranch == "" || branch != state.WorkspaceBranch || admission.Invocation.Ref != "refs/heads/"+branch {
		return fmt.Errorf("workspace transition %q branch does not match durable workspace and invocation identity", id)
	}
	return nil
}

func canonicalWorkspaceDestination(admission protocol.Admission) (string, error) {
	value, _ := admission.Parameters.Get("destination")
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve workspace destination parent: %w", err)
	}
	canonical := filepath.Join(parent, filepath.Base(absolute))
	if canonical == admission.Invocation.InvokingPath {
		return "", fmt.Errorf("workspace destination must differ from the invoking worktree")
	}
	return canonical, nil
}

func parkedSourceState(state durable.State, transition catalog.TransitionID, now time.Time) durable.State {
	state.Revision++
	state.Phase = model.PhaseDormant
	state.Engagement = model.EngagementDormant
	state.Delivery = model.DeliveryUninitialized
	state.Workspace = model.WorkspaceAbsent
	state.Plan = model.PlanAbsent
	state.Publication = model.PublicationNone
	state.Verification = model.VerificationUnverified
	state.Recovery = model.RecoveryNone
	state.Transaction = model.TransactionNone
	state.Terminal = model.TerminalNonterminal
	state.Goal = model.Goal{}
	state.SourceRevision = ""
	state.WorktreeFingerprint = ""
	state.PlanFingerprint = ""
	state.WorkspaceBranch = ""
	state.WorkspacePath = ""
	state.WorkspaceBaseRef = ""
	state.WorkspaceSourcePath = ""
	state.WorkspaceSourceID = ""
	state.WorkspaceSourceRef = ""
	state.PublicationID = ""
	state.PublicationURL = ""
	state.PreviewFingerprint = ""
	state.Gates = nil
	clearRecoveryContext(&state)
	state.LastTransition = transition
	state.UpdatedAt = now.UTC()
	return state
}

func closesInterruptedJournal(id catalog.TransitionID) bool {
	switch id {
	case "runtime.reconcile", "configuration.reconcile", "publication.reconcile", "recovery.escalate":
		return true
	default:
		return false
	}
}

func requiresCommandBoundary(id catalog.TransitionID) bool {
	switch id {
	case "gate.build.record", "gate.test.record", "workspace.cut", "workspace.sync", "workspace.cleanup", "workspace.reap", "publication.execute", "publication.correct":
		return true
	default:
		return false
	}
}

func loadDurableState(path string, invocation model.InvocationContext, now time.Time) (durable.State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return durable.Default(invocation, now), nil
		}
		return durable.State{}, err
	}
	return durable.DecodeState(raw)
}

func verifyRuntimeParameters(admission protocol.Admission, transition catalog.Transition) error {
	if transition.ID != "runtime.hydrate" && transition.ID != "runtime.replace" && transition.ID != "runtime.reconcile" && transition.ID != "installation.initialize" && transition.ID != "installation.update" && transition.ID != "installation.reconcile-update" {
		return nil
	}
	runtimePath, _ := admission.Parameters.Get("runtime_path")
	if !filepath.IsAbs(runtimePath) {
		return fmt.Errorf("declared runtime path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(runtimePath)
	if err != nil {
		return fmt.Errorf("resolve declared runtime: %w", err)
	}
	if filepath.Clean(runtimePath) != resolved {
		return fmt.Errorf("declared runtime path must be canonical: got %s, want %s", runtimePath, resolved)
	}
	expected, _ := admission.Parameters.Get("runtime_sha256")
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		return fmt.Errorf("read declared runtime: %w", err)
	}
	if actual := sha256Bytes(raw); actual != expected {
		return fmt.Errorf("declared runtime fingerprint mismatch: got %s", actual)
	}
	if strings.HasPrefix(string(transition.ID), "installation.") && (runtimePath != admission.Invocation.RuntimePath || expected != admission.Invocation.RuntimeFingerprint) {
		return fmt.Errorf("installation runtime must be the exact candidate process that owns admission")
	}
	return nil
}

func prepareLauncherMutation(admission protocol.Admission) (ports.ResourceMutation, string, string, error) {
	runtimePath, _ := admission.Parameters.Get("runtime_path")
	launcherPath := filepath.Join(filepath.Dir(runtimePath), "boatstack")
	if runtime.GOOS == "windows" {
		launcherPath += ".cmd"
		body := []byte("@echo off\r\n\"" + runtimePath + "\" %*\r\n")
		mutation, err := mutationFor(launcherPath, body, 0o700, false, false)
		return mutation, launcherPath, sha256Bytes(body), err
	}
	target := filepath.Base(runtimePath)
	mutation, err := mutationForSymlink(launcherPath, target, false)
	return mutation, launcherPath, sha256Bytes([]byte("symlink\x00" + target)), err
}

func mutationForSymlink(path, target string, installLast bool) (ports.ResourceMutation, error) {
	if !filepath.IsAbs(path) || target == "" {
		return ports.ResourceMutation{}, fmt.Errorf("managed symlink requires an absolute path and target")
	}
	mutation := ports.ResourceMutation{Path: path, TargetLink: target, Mode: 0o700, InstallLast: installLast}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return mutation, nil
	}
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	mutation.PriorExists = true
	if info.Mode()&os.ModeSymlink != 0 {
		mutation.PriorLink, err = os.Readlink(path)
		return mutation, err
	}
	if !info.Mode().IsRegular() {
		return ports.ResourceMutation{}, fmt.Errorf("managed launcher is neither a regular file nor symlink: %s", path)
	}
	mutation.Prior, err = os.ReadFile(path)
	mutation.Mode = uint32(info.Mode().Perm())
	return mutation, err
}

func mutationForExactResource(path string, target []byte, targetLink string, mode os.FileMode, installLast, deleteResource bool) (ports.ResourceMutation, error) {
	if !filepath.IsAbs(path) || (targetLink != "" && deleteResource) {
		return ports.ResourceMutation{}, fmt.Errorf("managed resource target is invalid: %s", path)
	}
	mutation := ports.ResourceMutation{Path: path, Target: target, TargetLink: targetLink, Mode: uint32(mode.Perm()), InstallLast: installLast, Delete: deleteResource}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return mutation, nil
	}
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	mutation.PriorExists = true
	if info.Mode()&os.ModeSymlink != 0 {
		mutation.PriorLink, err = os.Readlink(path)
		return mutation, err
	}
	if !info.Mode().IsRegular() {
		return ports.ResourceMutation{}, fmt.Errorf("managed resource is neither a regular file nor symlink: %s", path)
	}
	mutation.Prior, err = os.ReadFile(path)
	mutation.Mode = uint32(info.Mode().Perm())
	return mutation, err
}

func mutationFor(path string, target []byte, mode os.FileMode, installLast, deleteResource bool) (ports.ResourceMutation, error) {
	if !filepath.IsAbs(path) {
		return ports.ResourceMutation{}, fmt.Errorf("managed resource path is not absolute: %s", path)
	}
	prior, exists, priorMode, err := readAllIfExists(path)
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	if exists {
		mode = priorMode
	}
	return ports.ResourceMutation{Path: path, Prior: prior, Target: target, PriorExists: exists, Mode: uint32(mode.Perm()), InstallLast: installLast, Delete: deleteResource}, nil
}

var safeArtifactSegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func safeSegment(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if !safeArtifactSegment.MatchString(value) || value == "." || value == ".." {
		return "", fmt.Errorf("invalid %s %q", name, value)
	}
	return value, nil
}
