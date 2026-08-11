package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
)

type NativeCommandRunner interface {
	CombinedOutput(context.Context, string, string, ...string) ([]byte, error)
}

type nativeExecRunner struct{}

func (nativeExecRunner) CombinedOutput(ctx context.Context, directory, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	return command.CombinedOutput()
}

type NativeBoundary struct{ runner NativeCommandRunner }

func NewNativeBoundary() NativeBoundary { return NativeBoundary{runner: nativeExecRunner{}} }

func NewNativeBoundaryWithRunner(runner NativeCommandRunner) (NativeBoundary, error) {
	if runner == nil {
		return NativeBoundary{}, fmt.Errorf("native command boundary requires a runner")
	}
	return NativeBoundary{runner: runner}, nil
}

type pullRequestObservation struct {
	State             string `json:"state"`
	URL               string `json:"url"`
	Number            int    `json:"number"`
	MergedAt          string `json:"mergedAt"`
	BaseRefName       string `json:"baseRefName"`
	HeadRefName       string `json:"headRefName"`
	HeadRefOID        string `json:"headRefOid"`
	IsCrossRepository bool   `json:"isCrossRepository"`
}

func (b NativeBoundary) PrepareObservation(ctx context.Context, admission protocol.Admission, transition catalog.Transition, layout ports.ControllerLayout, state *durable.State) error {
	switch transition.ID {
	case "publication.observe", "publication.reconcile":
		publicationID, _ := admission.Parameters.Get("publication_id")
		if state.PublicationID != "" && state.PublicationID != publicationID {
			state.Publication = model.PublicationConflicting
			return nil
		}
		output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "gh", "pr", "view", "--json", "state,url,number,mergedAt,baseRefName,headRefName,headRefOid,isCrossRepository", "--", publicationID)
		if err != nil {
			state.Publication, state.PublicationID, state.PublicationURL = model.PublicationUnavailable, publicationID, ""
			return nil
		}
		var observation pullRequestObservation
		if err := json.Unmarshal(output, &observation); err != nil {
			state.Publication, state.PublicationID, state.PublicationURL = model.PublicationConflicting, publicationID, ""
			return nil
		}
		state.PublicationID, state.PublicationURL = fmt.Sprintf("%d", observation.Number), observation.URL
		configRaw, readErr := os.ReadFile(layout.ConfigPath)
		if readErr != nil {
			return readErr
		}
		config, decodeErr := protocol.DecodeProjectConfig(configRaw)
		if decodeErr != nil {
			return decodeErr
		}
		head := strings.TrimPrefix(admission.Invocation.Ref, "refs/heads/")
		identityMatches := strings.HasPrefix(admission.Invocation.Ref, "refs/heads/") && observation.Number > 0 && observation.URL != "" &&
			!observation.IsCrossRepository && observation.BaseRefName == config.Project.DefaultBranch && observation.HeadRefName == head &&
			admission.SourceRevision != "" && strings.EqualFold(observation.HeadRefOID, admission.SourceRevision)
		if !identityMatches {
			state.Publication = model.PublicationConflicting
			return nil
		}
		switch strings.ToUpper(observation.State) {
		case "OPEN":
			state.Publication = model.PublicationOpen
		case "MERGED":
			state.Publication = model.PublicationMerged
		case "CLOSED":
			if observation.MergedAt != "" {
				state.Publication = model.PublicationMerged
			} else {
				state.Publication = model.PublicationClosedUnmerged
			}
		default:
			state.Publication = model.PublicationConflicting
		}
	case "configuration.reconcile":
		raw, err := os.ReadFile(layout.ConfigPath)
		if err != nil {
			return err
		}
		config, err := protocol.DecodeProjectConfig(raw)
		if err != nil {
			return err
		}
		policy := config.ControlPolicy()
		state.ConfigFingerprint = sha256Bytes(raw)
		state.PlanApprovalPolicy = policy.PlanApproval
		state.VisualEvidencePolicy = policy.VisualEvidence
		state.ExternalEffectPolicy = policy.ExternalEffectAuthority
		state.IndependentReview = policy.IndependentReviewForHighRisk
		state.EnabledHosts = append([]string(nil), policy.Hosts...)
	}
	return nil
}

func (b NativeBoundary) Execute(ctx context.Context, admission protocol.Admission, transition catalog.Transition, layout ports.ControllerLayout, state durable.State) (ports.EffectResult, error) {
	settled := ports.EffectResult{Settlement: ports.EffectSettled}
	switch transition.ID {
	case "gate.build.record", "gate.test.record":
		raw, err := os.ReadFile(layout.ConfigPath)
		if err != nil {
			return settled, err
		}
		config, err := protocol.DecodeProjectConfig(raw)
		if err != nil {
			return settled, err
		}
		gate, _ := catalog.GateName(transition.ID)
		command := strings.TrimSpace(config.Project.Commands[gate])
		if command == "" {
			return settled, fmt.Errorf("repository configuration has no %s command", gate)
		}
		intent := supervisor.ClassifyCommandIntent(command)
		if intent.Class != supervisor.IntentOrdinary {
			return settled, fmt.Errorf("configured %s command crosses protected effect boundary %s", gate, intent.Operation)
		}
		executable, arguments := "/bin/sh", []string{"-c", command}
		if runtime.GOOS == "windows" {
			executable, arguments = "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", command}
		}
		if _, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, executable, arguments...); err != nil {
			return settled, fmt.Errorf("configured %s command did not pass: %w", gate, err)
		}
	case "workspace.cut":
		branch, _ := admission.Parameters.Get("branch")
		baseRef, _ := admission.Parameters.Get("base_ref")
		if err := protocol.ValidateGitBranch(branch); err != nil {
			return settled, err
		}
		if err := protocol.ValidateGitReference(baseRef); err != nil {
			return settled, err
		}
		absolute, err := canonicalWorkspaceDestination(admission)
		if err != nil || absolute == layout.RepositoryRoot {
			return settled, fmt.Errorf("workspace destination must be an explicit non-primary path")
		}
		if _, err := os.Stat(absolute); err == nil {
			return settled, fmt.Errorf("workspace destination already exists: %s", absolute)
		} else if !os.IsNotExist(err) {
			return settled, err
		}
		baseConfig, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "show", baseRef+":.boatstack/project.json")
		if err != nil {
			return settled, fmt.Errorf("workspace base does not contain the verified V2 configuration: %w", err)
		}
		if state.ConfigFingerprint == "" || sha256Bytes(baseConfig) != state.ConfigFingerprint {
			return settled, fmt.Errorf("workspace base configuration does not match current repository authority")
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "check-ref-format", "--branch", branch); err != nil {
			return settled, fmt.Errorf("invalid workspace branch: %s: %w", strings.TrimSpace(string(output)), err)
		}
		arguments := []string{"worktree", "add", "-b", branch, absolute, baseRef}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", arguments...); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
	case "workspace.sync":
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "fetch", "origin"); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
	case "workspace.cleanup", "workspace.reap":
		if state.Workspace != model.WorkspaceLanded && state.Workspace != model.WorkspaceAbandoned {
			return settled, fmt.Errorf("workspace is neither landed nor explicitly abandoned")
		}
		if state.WorkspacePath == "" || state.WorkspaceBranch == "" {
			return settled, fmt.Errorf("workspace cleanup identity is incomplete")
		}
		neutralDirectory := filepath.Dir(state.WorkspacePath)
		gitPrefix := []string{"--git-dir", layout.GitCommonRoot}
		if output, err := b.runner.CombinedOutput(ctx, neutralDirectory, "git", append(gitPrefix, "worktree", "remove", state.WorkspacePath)...); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
		branchMode := "-d"
		if state.Workspace == model.WorkspaceAbandoned {
			branchMode = "-D"
		}
		if output, err := b.runner.CombinedOutput(ctx, neutralDirectory, "git", append(gitPrefix, "branch", branchMode, state.WorkspaceBranch)...); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
	case "publication.execute":
		deliveryID, err := safeSegment(admission.Goal.DeliveryID, "delivery identity")
		if err != nil {
			return settled, err
		}
		preview, err := loadPublicationPreview(filepath.Join(layout.RepositoryRoot, ".boatstack", "publication", deliveryID+".preview.json"))
		if err != nil {
			return settled, err
		}
		if err := validatePublicationPreviewForAdmission(layout, admission, preview); err != nil {
			return settled, err
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "push", "--set-upstream", "origin", preview.HeadRef); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "gh", "pr", "create", "--base", preview.BaseRef, "--head", preview.HeadRef, "--body-file", preview.BodyPath); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
	case "publication.correct":
		publicationID, _ := admission.Parameters.Get("publication_id")
		bodyPath, _ := admission.Parameters.Get("body_path")
		if err := validateCorrectionBody(admission); err != nil {
			return settled, err
		}
		if state.PublicationID == "" || publicationID != state.PublicationID {
			return settled, fmt.Errorf("publication correction identity does not match durable provider evidence")
		}
		if !strings.HasPrefix(admission.Invocation.Ref, "refs/heads/") {
			return settled, fmt.Errorf("publication correction requires an exact branch invocation")
		}
		branch := strings.TrimPrefix(admission.Invocation.Ref, "refs/heads/")
		if err := protocol.ValidateGitBranch(branch); err != nil {
			return settled, err
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "push", "--set-upstream", "origin", branch); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "gh", "pr", "edit", "--body-file", bodyPath, "--", publicationID); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
	}
	return settled, nil
}
