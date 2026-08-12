package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
)

var flowSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type planInboxConfig struct {
	Path        string `json:"path"`
	Cardinality string `json:"cardinality"`
}

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
	options.runID = runID
	if options.objectiveKind == "" {
		options.objectiveKind = string(objective)
	}
	if options.deliveryID == "" {
		options.deliveryID = deliveryID
	}
	if options.objectiveID == "" {
		options.objectiveID = "objective-" + options.programID + "-" + options.entryID + "-" + deliveryID
	}
	if options.objectiveKind != string(objective) || options.deliveryID != deliveryID {
		return commandOptions{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: objective or delivery changed across the run")
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return commandOptions{}, err
	}
	switch options.transitionID {
	case "objective.bind":
		if _, exists := parameters.Get("objective_kind"); !exists {
			options.parameters = append(options.parameters, "objective_kind="+string(objective))
		}
		if _, exists := parameters.Get("delivery_id"); !exists {
			options.parameters = append(options.parameters, "delivery_id="+deliveryID)
		}
	case "plan.create", "plan.amend":
		if _, exists := parameters.Get("source_path"); !exists {
			options.parameters = append(options.parameters, "source_path="+plan)
		}
		if _, exists := parameters.Get("delivery_id"); !exists {
			options.parameters = append(options.parameters, "delivery_id="+deliveryID)
		}
	}
	return options, nil
}

func resolveBoundPlan(repository string, entry controlprogram.Entry, options commandOptions) (string, string, error) {
	if options.runID != "" && flowSegment.MatchString(options.deliveryID) {
		managed := filepath.Join(repository, ".boatstack", "plans", options.deliveryID+".source")
		if info, err := os.Stat(managed); err == nil && info.Mode().IsRegular() {
			return managed, options.deliveryID, nil
		}
	}
	return resolvePlanInput(repository, entry)
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
	var input *controlprogram.EntryInput
	for index := range entry.Inputs {
		if entry.Inputs[index].Resolver == "software-delivery.plan-inbox" {
			input = &entry.Inputs[index]
			break
		}
	}
	if input == nil {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: entry %q has no trusted plan inbox", entry.ID)
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Config))
	decoder.DisallowUnknownFields()
	var config planInboxConfig
	if err := decoder.Decode(&config); err != nil {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil || config.Cardinality != "exactly-one" {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: plan inbox requires exact cardinality")
	}
	inbox, err := exactRepositoryPath(repository, config.Path)
	if err != nil {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	resolvedInbox, err := filepath.EvalSymlinks(inbox)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: resolve plan inbox: %w", err)
	}
	if err == nil {
		relative, relativeErr := filepath.Rel(repository, resolvedInbox)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", "", fmt.Errorf("FLOW_INPUT_INVALID: plan inbox escapes the repository")
		}
		inbox = resolvedInbox
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
	info, err := os.Lstat(selected)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: selected plan is not a regular repository file")
	}
	resolved, err := filepath.EvalSymlinks(selected)
	if err != nil {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: resolve selected plan: %w", err)
	}
	relative, err := filepath.Rel(inbox, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: selected plan escapes its inbox")
	}
	return resolved, deliveryID, nil
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

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}
