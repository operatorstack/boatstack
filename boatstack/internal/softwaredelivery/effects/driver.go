package effects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
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
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return nil, err
	}
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
	if admission.ControlBundle != nil {
		if err := boatstackruntime.VerifyControlBundleRoot(layout.RepositoryRoot, admission.ControlBundle.Source); err != nil {
			return nil, err
		}
	}
	if transition.ID == "recovery.resume" || transition.ID == "recovery.rollback" || transition.ID == "workspace.reconcile" {
		prepared, prepareErr := d.prepareRecoveryReplay(ctx, layout, admission, transition)
		if prepareErr != nil {
			return nil, prepareErr
		}
		effect, ok := prepared.(*preparedEffect)
		if !ok {
			return nil, fmt.Errorf("recovery did not return a Boatstack prepared effect")
		}
		if err := bindPreparedCapabilities(effect, admission, transition); err != nil {
			return nil, err
		}
		return effect, nil
	}
	state, err := loadDurableState(layout.StatePath, admission.Invocation, d.clock.Now())
	if err != nil {
		return nil, err
	}
	if state.RepositoryID != admission.Invocation.RepositoryID || state.GitCommonID != admission.Invocation.GitCommonID || state.WorktreeID != admission.Invocation.WorktreeID {
		return nil, fmt.Errorf("durable state belongs to a different invocation")
	}
	if state.Revision != admission.ExpectedStateRevision {
		return nil, fmt.Errorf("durable state revision changed after admission")
	}
	resultingRevision, err := durable.NextRevision(state.Revision)
	if err != nil {
		return nil, err
	}
	if state.ProgramFingerprint != "" && state.ProgramFingerprint != admission.ExpectedProgramFingerprint && !transition.Policy.ReconcilesProgram {
		return nil, fmt.Errorf("compiled control program drifted; explicit program reconciliation is required")
	}
	if transition.ID == "catalog.reconcile" && (state.RuntimeVersion != admission.Invocation.RuntimeVersion || state.RuntimeFingerprint != admission.Invocation.RuntimeFingerprint) {
		return nil, fmt.Errorf("catalog reconciliation cannot activate a different runtime; use installation.reconcile-update")
	}
	if err := verifyWorkspaceBranchParameter(state, admission, transition.ID); err != nil {
		return nil, err
	}
	if err := d.verifyClearedWorkspaceDestination(ctx, state, admission, transition); err != nil {
		return nil, err
	}
	if admission.ControlBundle != nil && admission.ControlBundle.Target != nil && (transition.ID == "workspace.cleanup" || transition.ID == "workspace.reap") {
		if err := boatstackruntime.VerifyControlBundleRoot(state.WorkspaceSourcePath, *admission.ControlBundle.Target); err != nil {
			return nil, fmt.Errorf("CONTROL_BUNDLE_TARGET_STALE: preserved source checkout: %w", err)
		}
	}
	if err := verifyRuntimeParameters(admission, transition); err != nil {
		return nil, err
	}
	next := state
	if admission.ControlBundle != nil {
		next.ControlBundleFingerprint = admission.ControlBundle.Source.Fingerprint
		if admission.ControlBundle.Target != nil {
			next.ControlBundleFingerprint = admission.ControlBundle.Target.Fingerprint
		}
	}
	if next.ProgramFingerprint == "" {
		next.ProgramFingerprint = admission.ExpectedProgramFingerprint
	}
	if err := d.boundary.PrepareObservation(ctx, admission, transition, layout, &next); err != nil {
		return nil, err
	}
	if err := applyStateTransition(&next, admission, transition); err != nil {
		return nil, err
	}
	next.Revision = resultingRevision
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
	if transition.ID == "workspace.cut" {
		transferMutations, transferErr := prepareWorkspacePlanTransfer(layout.RepositoryRoot, next.WorkspacePath, admission.Objective.DeliveryID, next.PlanFingerprint, next.ApprovalFingerprint)
		if transferErr != nil {
			return nil, transferErr
		}
		mutations = append(mutations, transferMutations...)
	}
	if transitionSetsRuntimePin(transition.ID) || transition.ID == "catalog.reconcile" {
		pinMutation, pinErr := prepareRuntimePinMutation(layout.RepositoryRoot, next)
		if pinErr != nil {
			return nil, pinErr
		}
		mutations = append(mutations, pinMutation)
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
	var facetGroups [][2]durable.State
	facetGroups = append(facetGroups, [2]durable.State{state, next})
	if transition.ID == "workspace.cut" {
		parked := parkedSourceState(state, next.Revision, transition.ID, d.clock.Now())
		facetGroups = append(facetGroups, [2]durable.State{state, parked})
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
	changedFacets, facetErr := changedStateFacets(facetGroups...)
	if facetErr != nil {
		return nil, facetErr
	}
	changedFacets, facetErr = validateTransitionStateFacets(transition, changedFacets)
	if facetErr != nil {
		return nil, facetErr
	}
	mutations = annotateStateFacetMutations(mutations, changedFacets)
	prepared := &preparedEffect{mutations: mutations, verifyInvocation: verificationInvocation, changedStateFacets: changedFacets}
	if admission.ControlBundle != nil && admission.ControlBundle.Target != nil {
		targetRoot := layout.RepositoryRoot
		switch transition.ID {
		case "workspace.cut":
			targetRoot = next.WorkspacePath
		case "workspace.cleanup", "workspace.reap":
			targetRoot = state.WorkspaceSourcePath
		}
		target := *admission.ControlBundle.Target
		targetRevision := admission.ControlBundle.TargetRevision
		prepared.postVerify = func(ctx context.Context) error {
			if transition.ID == "workspace.cut" {
				return boatstackruntime.VerifyControlBundleHead(ctx, targetRoot, targetRevision, target)
			}
			return boatstackruntime.VerifyControlBundleRoot(targetRoot, target)
		}
	}
	if err := bindPreparedCapabilities(prepared, admission, transition); err != nil {
		return nil, err
	}
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

func (d Driver) verifyClearedWorkspaceDestination(ctx context.Context, state durable.State, admission protocol.Admission, transition catalog.Transition) error {
	clearsWorkspace := false
	for _, condition := range transition.TargetConditions {
		if condition.Facet != model.FacetWorkspace {
			continue
		}
		for _, value := range condition.Values {
			if value == string(model.WorkspaceAbsent) {
				clearsWorkspace = true
				break
			}
		}
	}
	if !clearsWorkspace {
		return nil
	}
	destination, err := d.resolver.ResolveInvocation(ctx, state.WorkspacePath, admission.Invocation.Host, admission.Invocation.Correlation)
	if err != nil {
		return fmt.Errorf("resolve workspace destination before clearing it: %w", err)
	}
	if destination.RepositoryID != admission.Invocation.RepositoryID || destination.GitCommonID != admission.Invocation.GitCommonID ||
		destination.WorktreeID != admission.Invocation.WorktreeID || destination.Ref != admission.Invocation.Ref ||
		destination.ControllerID != admission.Invocation.ControllerID || destination.Topology != admission.Invocation.Topology {
		return fmt.Errorf("workspace destination identity changed; refusing workspace removal")
	}
	return nil
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

func parkedSourceState(state durable.State, resultingRevision uint64, transition catalog.TransitionID, now time.Time) durable.State {
	state.Revision = resultingRevision
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
	state.Objective = model.Objective{}
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
	version, _ := admission.Parameters.Get("runtime_version")
	expected, _ := admission.Parameters.Get("runtime_sha256")
	revision, _ := admission.Parameters.Get("source_revision")
	identity := boatstackruntime.Identity{Version: version, SHA256: expected, SourceRevision: revision}
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("declared runtime identity: %w", err)
	}
	home, err := boatstackruntime.Home("")
	if err != nil {
		return err
	}
	expectedPath, err := boatstackruntime.ExecutablePath(home, identity)
	if err != nil {
		return err
	}
	if err := boatstackruntime.VerifyExecutable(expectedPath, identity); err != nil {
		return err
	}
	if version != admission.Invocation.RuntimeVersion || expected != admission.Invocation.RuntimeFingerprint {
		return fmt.Errorf("runtime transition must be owned by the exact immutable candidate process")
	}
	return nil
}

func transitionSetsRuntimePin(id catalog.TransitionID) bool {
	switch id {
	case "runtime.hydrate", "runtime.replace", "runtime.reconcile", "installation.initialize", "installation.update", "installation.reconcile-update":
		return true
	default:
		return false
	}
}

func prepareRuntimePinMutation(repository string, state durable.State) (ports.ResourceMutation, error) {
	identity := boatstackruntime.Identity{Version: state.RuntimeVersion, SHA256: state.RuntimeFingerprint, SourceRevision: state.RuntimeSource}
	pinRaw, err := boatstackruntime.EncodePin(boatstackruntime.NewPin(identity, state.ProgramFingerprint, durable.StateSchemaVersion))
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	return mutationFor(boatstackruntime.PinPath(repository), pinRaw, 0o644, false, false)
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
