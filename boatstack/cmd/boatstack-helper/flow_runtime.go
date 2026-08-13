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
	planFingerprint := ""
	if plan != "" {
		planRaw, readErr := os.ReadFile(plan)
		if readErr != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: read selected plan: %w", readErr)
		}
		planDigest := sha256.Sum256(planRaw)
		planFingerprint = hex.EncodeToString(planDigest[:])
		repositoryIdentity, identityErr := flowRepositoryIdentity(repository)
		if identityErr != nil {
			return commandOptions{}, identityErr
		}
		runID := flowRunID(repositoryIdentity, compiled.Fingerprint, options.entryID, deliveryID, planFingerprint)
		if options.runID != "" && options.runID != runID {
			return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the selected plan and repository")
		}
		options.runID = runID
	} else if options.runID == "" {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: active abandonment has no committed run identity")
	}
	options.repository = repository
	if options.objectiveKind == "" {
		options.objectiveKind = string(objective)
	}
	if options.deliveryID == "" {
		options.deliveryID = deliveryID
	}
	expectedObjectiveID := "objective-" + options.programID + "-" + options.entryID + "-" + deliveryID
	if options.objectiveID == "" {
		options.objectiveID = expectedObjectiveID
	}
	if options.objectiveKind != string(objective) || options.deliveryID != deliveryID || options.objectiveID != expectedObjectiveID {
		return commandOptions{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: objective or delivery changed across the run")
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return commandOptions{}, err
	}
	for name, expected := range map[string]string{
		"objective_kind":     string(objective),
		"delivery_id":        deliveryID,
		"source_path":        plan,
		"source_fingerprint": planFingerprint,
	} {
		if err := validateResolvedParameter(parameters, name, expected); err != nil {
			return commandOptions{}, err
		}
	}
	switch options.transitionID {
	case "objective.bind":
		if err := bindResolvedParameter(&options, parameters, "objective_kind", string(objective)); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "delivery_id", deliveryID); err != nil {
			return commandOptions{}, err
		}
	case "plan.create", "plan.amend":
		if err := bindResolvedParameter(&options, parameters, "source_path", plan); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "delivery_id", deliveryID); err != nil {
			return commandOptions{}, err
		}
		if err := bindResolvedParameter(&options, parameters, "source_fingerprint", planFingerprint); err != nil {
			return commandOptions{}, err
		}
	}
	return options, nil
}

func bindActiveFlowContext(ctx context.Context, repository string, options commandOptions, entryObjective model.ObjectiveKind) (commandOptions, error) {
	if options.runID != "" && entryObjective != model.ObjectiveAbandoned {
		return options, nil
	}
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
			if _, stateErr := os.Stat(filepath.Join(common, "boatstack", "v2")); os.IsNotExist(stateErr) {
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
	if active.Kind == entryObjective && strings.HasPrefix(active.ID, prefix) {
		options.runID, options.deliveryID = receipt.FlowID, active.DeliveryID
		options.objectiveID, options.objectiveKind = active.ID, string(active.Kind)
		options.activeFlowBound = true
		return options, nil
	}
	if entryObjective == model.ObjectiveAbandoned {
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
		runID:                  request.FlowID, objectiveID: request.Objective.ID, objectiveKind: string(request.Objective.Kind), deliveryID: request.Objective.DeliveryID,
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
	request.Objective.Kind = model.ObjectiveKind(bound.objectiveKind)
	request.Objective.DeliveryID = bound.deliveryID
	request.Parameters = parameters
	return request, nil
}

func resolveBoundPlan(repository string, entry controlprogram.Entry, entryObjective model.ObjectiveKind, options commandOptions) (string, string, error) {
	if options.activeFlowBound && entryObjective == model.ObjectiveAbandoned {
		return "", options.deliveryID, nil
	}
	if options.runID == "" && options.deliveryID == "" {
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
