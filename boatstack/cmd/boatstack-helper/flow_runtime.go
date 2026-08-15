package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

var flowSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func bindFlowEntry(ctx context.Context, options commandOptions) (commandOptions, error) {
	if options.programID == "" && options.entryID == "" {
		return options, nil
	}
	if !flowSegment.MatchString(options.programID) || !flowSegment.MatchString(options.entryID) {
		return commandOptions{}, fmt.Errorf("FLOW_ENTRY_INVALID: --flow and --entry require semantic identifiers")
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return commandOptions{}, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return commandOptions{}, err
	}
	artifactPath := filepath.Join(repository, ".boatstack", "flows", options.programID+".flow.ir.json")
	artifactRaw, err := os.ReadFile(artifactPath)
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ARTIFACT_REQUIRED: %w", err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(artifactRaw))
	if err != nil {
		return commandOptions{}, err
	}
	if artifact.Program.Program.ID != options.programID {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_MISMATCH: selected %q but artifact declares %q", options.programID, artifact.Program.Program.ID)
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return commandOptions{}, err
	}
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
	if err != nil {
		return commandOptions{}, err
	}
	if options.flowProgramFingerprint != "" && options.flowProgramFingerprint != compiled.Fingerprint {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_DRIFT: run fingerprint does not match the current artifact")
	}
	options.flowProgramFingerprint = compiled.Fingerprint
	objective, err := softwareflow.ObjectiveForEntry(ctx, compiled, resolver, options.entryID)
	if err != nil {
		return commandOptions{}, err
	}
	entry, ok := findEntry(compiled.Document.Entries, options.entryID)
	if !ok {
		return commandOptions{}, fmt.Errorf("FLOW_ENTRY_UNKNOWN: %s", options.entryID)
	}
	options, err = bindActiveFlowContext(ctx, repository, options, objective)
	if err != nil {
		return commandOptions{}, err
	}
	plan, deliveryID, err := resolveBoundPlan(repository, entry, objective, options)
	if err != nil {
		return commandOptions{}, err
	}
	options.workInputs = map[string]string{}
	for _, input := range entry.Inputs {
		if plan != "" {
			options.workInputs[input.ID] = plan
		}
	}
	planFingerprint := ""
	if plan != "" {
		planRaw, readErr := os.ReadFile(plan)
		if readErr != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: read selected plan: %w", readErr)
		}
		planDigest := sha256.Sum256(planRaw)
		planFingerprint = hex.EncodeToString(planDigest[:])
		options, err = bindSelectedPlanRun(options, repository, compiled.Fingerprint, deliveryID, planFingerprint)
		if err != nil {
			return commandOptions{}, err
		}
	} else if options.runID == "" {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: active abandonment has no committed run identity")
	}
	options.repository = repository
	if options.targetID == "" {
		options.targetID = string(objective.TargetID)
	}
	if options.trustedObjectiveClass == "" {
		options.trustedObjectiveClass = string(objective.TrustedClass)
	}
	if options.deliveryID == "" {
		options.deliveryID = deliveryID
	}
	expectedObjectiveID := "objective-" + options.programID + "-" + options.entryID + "-" + deliveryID
	if options.objectiveID == "" {
		options.objectiveID = expectedObjectiveID
	}
	if options.targetID != string(objective.TargetID) || options.trustedObjectiveClass != string(objective.TrustedClass) || options.deliveryID != deliveryID || options.objectiveID != expectedObjectiveID {
		return commandOptions{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: objective or delivery changed across the run")
	}
	if entry.Delegation != nil {
		contextResolver, resolverErr := plant.NewResolver("")
		if resolverErr != nil {
			return commandOptions{}, resolverErr
		}
		host := options.host
		if host == "" {
			host = "cli"
		}
		invocation, invocationErr := contextResolver.ResolveInvocation(ctx, repository, host, "flow-delegation-request")
		if invocationErr != nil {
			return commandOptions{}, invocationErr
		}
		description := entry.Description
		if description == "" {
			description = fmt.Sprintf("Run %s/%s to %s", options.programID, options.entryID, objective.TargetID)
		}
		delegationRequest := delegation.Request{
			RunID: options.runID, ProgramID: options.programID, ProgramFingerprint: compiled.Fingerprint,
			EntryID: options.entryID, TargetID: string(objective.TargetID), ObjectiveID: options.objectiveID, DeliveryID: deliveryID,
			InputFingerprints: []string{planFingerprint}, RepositoryID: invocation.RepositoryID, GitCommonID: invocation.GitCommonID,
			InitialWorktreeID: invocation.WorktreeID, InitialRef: invocation.Ref,
			BindingFingerprint: entry.Delegation.Fingerprint, RequestedAuthorities: append([]string(nil), entry.Delegation.Authorities...),
			Description: description,
		}
		layout, _, layoutErr := contextResolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			return commandOptions{}, layoutErr
		}
		recordPath, pathErr := delegation.Path(layout.FlowRoot, options.runID)
		if pathErr != nil {
			return commandOptions{}, pathErr
		}
		if record, loadErr := delegation.Load(recordPath); loadErr == nil {
			bound := record.Request
			inputDrift := !options.activeFlowBound && strings.Join(bound.InputFingerprints, "\x00") != strings.Join(delegationRequest.InputFingerprints, "\x00")
			if bound.RunID != delegationRequest.RunID || bound.ProgramID != delegationRequest.ProgramID || bound.ProgramFingerprint != delegationRequest.ProgramFingerprint || bound.EntryID != delegationRequest.EntryID || bound.TargetID != delegationRequest.TargetID || bound.ObjectiveID != delegationRequest.ObjectiveID || bound.DeliveryID != delegationRequest.DeliveryID || inputDrift || bound.RepositoryID != delegationRequest.RepositoryID || bound.GitCommonID != delegationRequest.GitCommonID || bound.BindingFingerprint != delegationRequest.BindingFingerprint || strings.Join(bound.RequestedAuthorities, "\x00") != strings.Join(delegationRequest.RequestedAuthorities, "\x00") || bound.Description != delegationRequest.Description {
				return commandOptions{}, fmt.Errorf("DELEGATION_DRIFT: current Flow context does not match the authorized request")
			}
			delegationRequest = bound
		} else if !os.IsNotExist(loadErr) {
			return commandOptions{}, loadErr
		}
		fingerprint, fingerprintErr := delegationRequest.Fingerprint()
		if fingerprintErr != nil {
			return commandOptions{}, fingerprintErr
		}
		options.delegationBindingFingerprint = entry.Delegation.Fingerprint
		options.delegationRequestFingerprint = fingerprint
		options.delegationAuthorities = append(options.delegationAuthorities[:0], entry.Delegation.Authorities...)
		options.delegationDescription = description
		options.delegationRequest = delegationRequest
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return commandOptions{}, err
	}
	for name, expected := range map[string]string{
		"target_id":          string(objective.TargetID),
		"delivery_id":        deliveryID,
		"source_path":        plan,
		"source_fingerprint": planFingerprint,
	} {
		if err := validateResolvedParameter(parameters, name, expected); err != nil {
			return commandOptions{}, err
		}
	}
	switch options.transitionID {
	case "installation.initialize":
		configPath := filepath.Join(repository, ".boatstack", "project.json")
		if err := bindResolvedParameter(&options, parameters, "config_path", configPath); err != nil {
			return commandOptions{}, err
		}
		if err := populateProjectConfigFingerprint(&options); err != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: bind verified project configuration: %w", err)
		}
		if err := populateRuntimeParameters(&options); err != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: bind exact runtime identity: %w", err)
		}
	case "objective.bind":
		if err := bindResolvedParameter(&options, parameters, "target_id", string(objective.TargetID)); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "delivery_id", deliveryID); err != nil {
			return commandOptions{}, err
		}
	case "plan.create", "plan.amend", softwareflow.PlanningPackageAdmit:
		if err := bindResolvedParameter(&options, parameters, "source_path", plan); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "delivery_id", deliveryID); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "source_fingerprint", planFingerprint); err != nil {
			return commandOptions{}, err
		}
	case softwareflow.PlanningPackageApprove:
		fingerprint, fingerprintErr := softwareflow.PlanningPackageFingerprint(repository, deliveryID)
		if fingerprintErr != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: read planning package fingerprint: %w", fingerprintErr)
		}
		if err := bindResolvedParameter(&options, parameters, "package_fingerprint", fingerprint); err != nil {
			return commandOptions{}, err
		}
	case "publication.preview":
		if err := bindPublicationPreviewParameters(ctx, repository, deliveryID, options.host, &options, parameters); err != nil {
			return commandOptions{}, err
		}
	case "workspace.cut":
		baseRef, exists := parameters.Get("base_ref")
		if !exists {
			return commandOptions{}, fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: workspace.cut requires an exact base_ref")
		}
		if err := verifyWorkspaceControlBundleAtRevision(ctx, repository, artifactPath, artifact, baseRef); err != nil {
			return commandOptions{}, err
		}
	}
	return options, nil
}

func bindPublicationPreviewParameters(ctx context.Context, repository, deliveryID, host string, options *commandOptions, parameters protocol.Parameters) error {
	canonicalRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return fmt.Errorf("FLOW_INPUT_REQUIRED: resolve publication repository: %w", err)
	}
	repository = canonicalRepository
	configRaw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "project.json"))
	if err != nil {
		return fmt.Errorf("FLOW_INPUT_REQUIRED: read publication configuration: %w", err)
	}
	config, err := protocol.DecodeProjectConfig(configRaw)
	if err != nil {
		return fmt.Errorf("FLOW_INPUT_REQUIRED: decode publication configuration: %w", err)
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return err
	}
	if host == "" {
		host = "cli"
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, host, "flow-publication-preview")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(invocation.Ref, "refs/heads/") {
		return fmt.Errorf("FLOW_INPUT_REQUIRED: publication requires an attached branch")
	}
	bodyPath, err := resolveRegularRepositoryFile(repository, filepath.Join(repository, ".boatstack", "evidence", deliveryID+"-pr-body.md"), "publication body")
	if err != nil {
		return fmt.Errorf("FLOW_INPUT_REQUIRED: bind publication body: %w", err)
	}
	for name, value := range map[string]string{
		"base_ref":  config.Project.DefaultBranch,
		"head_ref":  strings.TrimPrefix(invocation.Ref, "refs/heads/"),
		"body_path": bodyPath,
	} {
		if err := bindResolvedParameter(options, parameters, name, value); err != nil {
			return err
		}
	}
	return nil
}

func verifyWorkspaceControlBundleAtRevision(ctx context.Context, repository, artifactPath string, artifact controlprogram.Artifact, revision string) error {
	if err := protocol.ValidateGitReference(revision); err != nil {
		return fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: invalid workspace base_ref: %w", err)
	}
	artifactRelative, err := filepath.Rel(repository, artifactPath)
	if err != nil {
		return fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: resolve compiled artifact path: %w", err)
	}
	paths := map[string]struct{}{
		filepath.ToSlash(artifactRelative): {},
		artifact.SourcePath:                {},
		artifact.DependencyLockPath:        {},
	}
	for path := range artifact.Assets {
		paths[path] = struct{}{}
	}
	for path := range artifact.GeneratedSkills {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	if err := boatstackruntime.VerifyFlowProjectionAtRevision(ctx, repository, revision, ordered); err != nil {
		return fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: workspace base %q does not contain the active control bundle; commit or regenerate the Flow bundle before workspace.cut: %w", revision, err)
	}
	return nil
}

func populateProjectConfigFingerprint(options *commandOptions) error {
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	if _, exists := parameters.Get("config_sha256"); exists {
		return nil
	}
	path, exists := parameters.Get("config_path")
	if !exists {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return err
	}
	options.parameters = append(options.parameters, "config_sha256="+fingerprint)
	return nil
}

func bindSelectedPlanRun(options commandOptions, repository, programFingerprint, deliveryID, planFingerprint string) (commandOptions, error) {
	if options.activeFlowBound {
		return options, nil
	}
	repositoryIdentity, err := flowRepositoryIdentity(repository)
	if err != nil {
		return commandOptions{}, err
	}
	runID := flowRunID(repositoryIdentity, programFingerprint, options.entryID, deliveryID, planFingerprint)
	if options.runID != "" && options.runID != runID {
		return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the selected plan and repository")
	}
	options.runID = runID
	return options, nil
}

func bindActiveFlowContext(ctx context.Context, repository string, options commandOptions, entryObjective softwareflow.EntryObjective) (commandOptions, error) {
	resolver, err := plant.NewResolver("")
	if err != nil {
		return commandOptions{}, err
	}
	host := options.host
	if host == "" {
		host = "cli"
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, host, "flow-entry-resume")
	if err != nil {
		common, commonErr := flowRepositoryIdentity(repository)
		if commonErr == nil {
			if _, stateErr := os.Stat(filepath.Join(common, "boatstack")); os.IsNotExist(stateErr) {
				return options, nil
			}
		}
		if _, stateErr := os.Stat(filepath.Join(repository, ".git", "boatstack")); stateErr != nil {
			return options, nil
		}
		return commandOptions{}, err
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return commandOptions{}, err
	}
	raw, err := os.ReadFile(layout.StatePath)
	if os.IsNotExist(err) {
		return options, nil
	}
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: read durable state: %w", err)
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: decode durable state: %w", err)
	}
	active, ok := state.ActiveObjective()
	if !ok {
		return options, nil
	}
	prefix := "objective-" + options.programID + "-" + options.entryID + "-"
	receipt, found, findErr := effects.FindLatestCommittedFlowForObjective(layout, invocation, active, state.Revision)
	if findErr != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: inspect committed flow receipts: %w", findErr)
	}
	if !found || !strings.HasPrefix(receipt.FlowID, "run-") {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: active objective has no committed run identity")
	}
	if active.TargetID == entryObjective.TargetID && strings.HasPrefix(active.ID, prefix) {
		bound, bindErr := bindCommittedActiveRun(options, active, receipt)
		if bindErr != nil {
			return commandOptions{}, bindErr
		}
		return bindStateOwnedTransitionParameters(bound, state)
	}
	if entryObjective.TrustedClass == model.ObjectiveAbandoned {
		repositoryIdentity, identityErr := flowRepositoryIdentity(repository)
		if identityErr != nil {
			return commandOptions{}, identityErr
		}
		expectedRunID := flowRunID(repositoryIdentity, options.flowProgramFingerprint, options.entryID, active.DeliveryID, "active-run:"+receipt.FlowID)
		if options.runID != "" && options.runID != expectedRunID {
			return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the active delivery")
		}
		options.runID = expectedRunID
		options.deliveryID, options.activeFlowBound = active.DeliveryID, true
		return options, nil
	}
	return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_CONFLICT: delivery %q is active under objective %q; abandon it before selecting another inbox plan", active.DeliveryID, active.ID)
}

func bindStateOwnedTransitionParameters(options commandOptions, state durable.State) (commandOptions, error) {
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return commandOptions{}, err
	}
	var name, value string
	switch options.transitionID {
	case "workspace.activate", "workspace.sync", "workspace.publish":
		name, value = "branch", state.WorkspaceBranch
	case "publication.execute":
		name, value = "preview_fingerprint", state.PreviewFingerprint
	case "publication.observe":
		name, value = "publication_id", state.PublicationID
	}
	if name == "" || value == "" {
		return options, nil
	}
	if err := bindResolvedParameter(&options, parameters, name, value); err != nil {
		return commandOptions{}, err
	}
	return options, nil
}

func bindCommittedActiveRun(options commandOptions, active model.Objective, receipt protocol.TransitionReceipt) (commandOptions, error) {
	if options.runID != "" && options.runID != receipt.FlowID {
		return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the committed active delivery")
	}
	options.runID, options.deliveryID = receipt.FlowID, active.DeliveryID
	options.objectiveID, options.targetID, options.trustedObjectiveClass = active.ID, string(active.TargetID), string(active.TrustedObjectiveClass())
	options.activeFlowBound = true
	return options, nil
}

func validateResolvedParameter(parameters protocol.Parameters, name, expected string) error {
	if actual, exists := parameters.Get(name); exists && actual != expected {
		return fmt.Errorf("FLOW_INPUT_MISMATCH: parameter %s conflicts with the entry-resolved value", name)
	}
	return nil
}

func bindResolvedParameter(options *commandOptions, parameters protocol.Parameters, name, expected string) error {
	if actual, exists := parameters.Get(name); exists {
		if actual != expected {
			return fmt.Errorf("FLOW_INPUT_MISMATCH: parameter %s conflicts with the entry-resolved value", name)
		}
		return nil
	}
	options.parameters = append(options.parameters, name+"="+expected)
	return nil
}

func bindRPCFlowEntry(ctx context.Context, request surfaces.Request) (surfaces.Request, error) {
	if request.ProgramID == "" && request.EntryID == "" {
		return request, nil
	}
	parameterFlags := make([]string, 0, len(request.Parameters))
	for _, parameter := range request.Parameters {
		parameterFlags = append(parameterFlags, parameter.Name+"="+parameter.Value)
	}
	bound, err := bindFlowEntry(ctx, commandOptions{
		repository: request.Repository, host: request.Host, programID: request.ProgramID, entryID: request.EntryID,
		flowProgramFingerprint: request.ProgramFingerprint,
		runID:                  request.FlowID, objectiveID: request.Objective.ID, targetID: string(request.Objective.TargetID), trustedObjectiveClass: string(request.Objective.TrustedObjectiveClass()), deliveryID: request.Objective.DeliveryID,
		transitionID: string(request.TransitionID), parameters: parameterFlags,
	})
	if err != nil {
		return surfaces.Request{}, err
	}
	parameters, err := parseParameters(bound.parameters)
	if err != nil {
		return surfaces.Request{}, err
	}
	request.Repository = bound.repository
	request.ProgramFingerprint = bound.flowProgramFingerprint
	request.FlowID = bound.runID
	request.Objective.ID = bound.objectiveID
	request.Objective.TargetID = model.TargetID(bound.targetID)
	request.Objective.TrustedClass = model.TargetID(bound.trustedObjectiveClass)
	request.Objective.DeliveryID = bound.deliveryID
	request.Parameters = parameters
	request.DelegationBindingFingerprint = bound.delegationBindingFingerprint
	request.DelegationRequestFingerprint = bound.delegationRequestFingerprint
	request.DelegatedAuthorities = delegationClasses(bound.delegationAuthorities)
	request.WorkInputs = bound.workInputs
	return request, nil
}

func resolveBoundPlan(repository string, entry controlprogram.Entry, entryObjective softwareflow.EntryObjective, options commandOptions) (string, string, error) {
	if options.activeFlowBound && entryObjective.TrustedClass == model.ObjectiveAbandoned {
		return "", options.deliveryID, nil
	}
	if options.deliveryID == "" {
		return resolvePlanInput(repository, entry)
	}
	if !flowSegment.MatchString(options.deliveryID) {
		return "", "", fmt.Errorf("FLOW_CONTEXT_MISMATCH: active run requires its delivery identity")
	}
	managed := filepath.Join(repository, ".boatstack", "plans", options.deliveryID+".source")
	if _, err := os.Lstat(managed); err == nil {
		resolved, resolveErr := resolveRegularRepositoryFile(repository, managed, "active run plan")
		return resolved, options.deliveryID, resolveErr
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: inspect active run plan: %w", err)
	}
	inbox, _, err := resolvePlanInbox(repository, entry)
	if err != nil {
		return "", "", err
	}
	resolved, err := resolveActiveInboxPlan(repository, inbox, options.deliveryID)
	return resolved, options.deliveryID, err
}

func resolveActiveInboxPlan(repository, inbox, deliveryID string) (string, error) {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: active run inbox is unavailable: %w", err)
	}
	var candidates []string
	for _, candidate := range entries {
		extension := filepath.Ext(candidate.Name())
		if candidate.Type()&os.ModeSymlink != 0 || candidate.IsDir() || !strings.EqualFold(extension, ".md") || strings.TrimSuffix(candidate.Name(), extension) != deliveryID {
			continue
		}
		info, infoErr := candidate.Info()
		if infoErr == nil && info.Mode().IsRegular() {
			candidates = append(candidates, candidate.Name())
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: active run inbox plan %q is unavailable", deliveryID)
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: active run inbox plan %q is ambiguous", deliveryID)
	}
	return resolveRegularRepositoryFile(repository, filepath.Join(inbox, candidates[0]), "active run inbox plan")
}

func resolveRegularRepositoryFile(repository, path, label string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: %s is unavailable: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: %s must be a regular non-symlink file", label)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: resolve %s: %w", label, err)
	}
	relative, err := filepath.Rel(repository, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: %s escapes the repository", label)
	}
	return resolved, nil
}

func loadFlowDefinition(ctx context.Context, repository, programID string) (softwareflow.Definition, error) {
	if !flowSegment.MatchString(programID) {
		return softwareflow.Definition{}, fmt.Errorf("FLOW_PROGRAM_INVALID: program identity is not a semantic segment")
	}
	artifactPath := filepath.Join(repository, ".boatstack", "flows", programID+".flow.ir.json")
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return softwareflow.Definition{}, err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	return softwareflow.NewDefinition(compiled, resolver)
}

func resolvePlanInput(repository string, entry controlprogram.Entry) (string, string, error) {
	inbox, config, err := resolvePlanInbox(repository, entry)
	if err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("PLAN_REQUIRED: no plan exists in %s", config.Path)
		}
		return "", "", err
	}
	var candidates []string
	for _, candidate := range entries {
		if candidate.Type()&os.ModeSymlink != 0 || candidate.IsDir() || !strings.EqualFold(filepath.Ext(candidate.Name()), ".md") {
			continue
		}
		info, infoErr := candidate.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		name := strings.TrimSuffix(candidate.Name(), filepath.Ext(candidate.Name()))
		if flowSegment.MatchString(name) {
			candidates = append(candidates, candidate.Name())
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("PLAN_REQUIRED: no eligible Markdown plan exists in %s", config.Path)
	}
	if len(candidates) != 1 {
		return "", "", fmt.Errorf("PLAN_SELECTION_REQUIRED: found %d eligible plans in %s", len(candidates), config.Path)
	}
	deliveryID := strings.TrimSuffix(candidates[0], filepath.Ext(candidates[0]))
	selected := filepath.Join(inbox, candidates[0])
	resolved, err := resolveRegularRepositoryFile(repository, selected, "selected plan")
	if err != nil {
		return "", "", err
	}
	return resolved, deliveryID, nil
}

func resolvePlanInbox(repository string, entry controlprogram.Entry) (string, softwareflow.PlanInbox, error) {
	config, err := softwareflow.PlanInboxForEntry(entry)
	if err != nil {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	inbox, err := exactRepositoryPath(repository, filepath.FromSlash(config.Path))
	if err != nil {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	resolvedInbox, err := filepath.EvalSymlinks(inbox)
	if err != nil && !os.IsNotExist(err) {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: resolve plan inbox: %w", err)
	}
	if err == nil {
		relative, relativeErr := filepath.Rel(repository, resolvedInbox)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: plan inbox escapes the repository")
		}
		inbox = resolvedInbox
	}
	return inbox, config, nil
}

func findEntry(entries []controlprogram.Entry, id string) (controlprogram.Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return controlprogram.Entry{}, false
}

func generateSoftwareFlowSkills(compiled controlprogram.Compiled) (map[string][]byte, error) {
	return softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
}

func flowRepositoryIdentity(repository string) (string, error) {
	marker := filepath.Join(repository, ".git")
	info, err := os.Lstat(marker)
	if err != nil {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_REQUIRED: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: .git is a symlink")
	}
	common := marker
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: .git is not a directory or worktree marker")
		}
		raw, readErr := os.ReadFile(marker)
		if readErr != nil {
			return "", readErr
		}
		line := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(line, "gitdir: ") || strings.Contains(strings.TrimPrefix(line, "gitdir: "), "\n") {
			return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: invalid worktree Git marker")
		}
		gitDirectory := strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDirectory) {
			gitDirectory = filepath.Join(repository, gitDirectory)
		}
		common = gitDirectory
		if rawCommon, commonErr := os.ReadFile(filepath.Join(gitDirectory, "commondir")); commonErr == nil {
			common = strings.TrimSpace(string(rawCommon))
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitDirectory, common)
			}
		} else if !os.IsNotExist(commonErr) {
			return "", commonErr
		}
	}
	common, err = filepath.EvalSymlinks(filepath.Clean(common))
	if err != nil {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: %w", err)
	}
	if info, err := os.Stat(common); err != nil || !info.IsDir() {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: Git common directory is unavailable")
	}
	return common, nil
}

func flowRunID(repositoryIdentity, fingerprint, entry, delivery, planFingerprint string) string {
	value := strings.Join([]string{repositoryIdentity, fingerprint, entry, delivery, planFingerprint}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "run-" + hex.EncodeToString(digest[:16])
}
