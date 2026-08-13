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
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
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
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver)
	if err != nil {
		return commandOptions{}, err
	}
	if options.flowProgramFingerprint != "" && options.flowProgramFingerprint != compiled.Fingerprint {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_DRIFT: run fingerprint does not match the current artifact")
	}
	objective, err := softwareflow.ObjectiveForEntry(ctx, compiled, resolver, options.entryID)
	if err != nil {
		return commandOptions{}, err
	}
	entry, ok := findEntry(compiled.Document.Entries, options.entryID)
	if !ok {
		return commandOptions{}, fmt.Errorf("FLOW_ENTRY_UNKNOWN: %s", options.entryID)
	}
	plan, deliveryID, err := resolveBoundPlan(repository, entry, options)
	if err != nil {
		return commandOptions{}, err
	}
	runID := flowRunID(repository, compiled.Fingerprint, options.entryID, deliveryID)
	if options.runID != "" && options.runID != runID {
		return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the selected plan and worktree")
	}
	options.repository = repository
	options.flowProgramFingerprint = compiled.Fingerprint
	options.runID = runID
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
		"objective_kind": string(objective),
		"delivery_id":    deliveryID,
		"source_path":    plan,
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
	}
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

func resolveBoundPlan(repository string, entry controlprogram.Entry, options commandOptions) (string, string, error) {
	if options.runID == "" {
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
	selected := filepath.Join(inbox, options.deliveryID+".md")
	resolved, err := resolveRegularRepositoryFile(repository, selected, "active run inbox plan")
	return resolved, options.deliveryID, err
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
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver)
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

func flowRunID(repository, fingerprint, entry, delivery string) string {
	value := strings.Join([]string{repository, fingerprint, entry, delivery}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "run-" + hex.EncodeToString(digest[:16])
}
