package effects

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
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

type githubAuthorityObservation struct {
	NameWithOwner    string `json:"nameWithOwner"`
	URL              string `json:"url"`
	ViewerPermission string `json:"viewerPermission"`
}

// ResolveGitHubProviderAuthority derives short-lived provider capability from
// the trusted GitHub CLI boundary. It is capability evidence, not human
// approval; run delegation remains the independent approval source.
func (b NativeBoundary) ResolveGitHubProviderAuthority(ctx context.Context, repository, authorityBinding string, now time.Time) (protocol.AuthorityReceipt, error) {
	if authorityBinding == "" || len(authorityBinding) > 256 || strings.TrimSpace(authorityBinding) != authorityBinding || strings.IndexFunc(authorityBinding, unicode.IsControl) >= 0 {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_INVALID: authority binding must be non-empty, bounded, and free of control characters")
	}
	remoteOutput, err := b.runner.CombinedOutput(ctx, repository, "git", "remote", "get-url", "--push", "origin")
	if err != nil {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_UNAVAILABLE: origin push repository identity is unavailable")
	}
	remoteRepository, err := githubRepositoryFromRemote(strings.TrimSpace(string(remoteOutput)))
	if err != nil {
		return protocol.AuthorityReceipt{}, err
	}
	output, err := b.runner.CombinedOutput(ctx, repository, "gh", "repo", "view", remoteRepository, "--json", "nameWithOwner,viewerPermission,url")
	if err != nil {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_UNAVAILABLE: GitHub repository identity or authenticated access is unavailable")
	}
	var observed githubAuthorityObservation
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_INVALID: GitHub authority response is invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_INVALID: GitHub authority response contains trailing JSON")
	}
	switch observed.ViewerPermission {
	case "ADMIN", "MAINTAIN", "WRITE":
	default:
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_DENIED: GitHub identity lacks write permission for the repository")
	}
	if observed.NameWithOwner == "" || observed.URL == "" {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_INVALID: GitHub repository identity is incomplete")
	}
	if !strings.EqualFold(observed.NameWithOwner, remoteRepository) {
		return protocol.AuthorityReceipt{}, fmt.Errorf("PROVIDER_AUTHORITY_INVALID: GitHub authority does not match the origin push repository")
	}
	issued := now.UTC()
	subject := "github:" + observed.NameWithOwner
	digest := sha256.Sum256([]byte(subject + "\x00" + authorityBinding))
	return protocol.AuthorityReceipt{
		ID: "provider-" + fmt.Sprintf("%x", digest[:8]), Class: catalog.AuthorityProvider,
		Subject: subject, Fingerprint: authorityBinding, IssuedAt: issued, ExpiresAt: issued.Add(2 * time.Minute),
	}, nil
}

func githubRepositoryFromRemote(remote string) (string, error) {
	path := ""
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		path = strings.TrimPrefix(remote, "git@github.com:")
	default:
		parsed, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") || (parsed.Scheme != "https" && parsed.Scheme != "ssh" && parsed.Scheme != "git") {
			return "", fmt.Errorf("PROVIDER_AUTHORITY_INVALID: origin push remote is not an exact GitHub repository")
		}
		path = strings.TrimPrefix(parsed.Path, "/")
	}
	path = strings.TrimSuffix(path, ".git")
	segments := strings.Split(path, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" || strings.ContainsAny(path, "\x00\r\n\t ") {
		return "", fmt.Errorf("PROVIDER_AUTHORITY_INVALID: origin push remote is not an exact GitHub repository")
	}
	return path, nil
}

func (b NativeBoundary) PrepareObservation(ctx context.Context, admission protocol.Admission, transition catalog.Transition, layout ports.ControllerLayout, state *durable.State) error {
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return err
	}
	switch transition.ID {
	case "publication.preview":
		output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
		if err != nil {
			return fmt.Errorf("WORKSPACE_COMMIT_REQUIRED: inspect worktree before publication preview")
		}
		if publicationProductStatus(string(output)) != "" {
			return fmt.Errorf("WORKSPACE_COMMIT_REQUIRED: commit the intended delivery changes before publication preview")
		}
		head, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil || strings.TrimSpace(string(head)) != admission.SourceRevision {
			return fmt.Errorf("WORKSPACE_HEAD_CHANGED: publication preview is not bound to the exact committed HEAD")
		}
	case "publication.observe", "publication.reconcile":
		publicationID, _ := admission.Parameters.Get("publication_id")
		if transition.ID == "publication.reconcile" && publicationID == "" {
			publicationID = state.PublicationID
		}
		if state.PublicationID != "" && publicationID != "" && state.PublicationID != publicationID {
			state.Publication = model.PublicationConflicting
			return nil
		}
		arguments := []string{"pr", "view", "--json", "state,url,number,mergedAt,baseRefName,headRefName,headRefOid,isCrossRepository"}
		if publicationID != "" {
			arguments = append(arguments, "--", publicationID)
		}
		output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "gh", arguments...)
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
		config, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
		if err != nil {
			return err
		}
		policy := config.ControlPolicy()
		state.ConfigFingerprint = fingerprint
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
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return settled, err
	}
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
		gate, _ := standardGateName(transition.ID)
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
		if admission.ControlBundle == nil || admission.ControlBundle.Target == nil || admission.ControlBundle.TargetRevision == "" {
			return settled, fmt.Errorf("CONTROL_BUNDLE_REQUIRED: workspace.cut has no exact target revision")
		}
		baseRevision := admission.ControlBundle.TargetRevision
		absolute, err := canonicalWorkspaceDestination(admission)
		if err != nil || absolute == layout.RepositoryRoot {
			return settled, fmt.Errorf("workspace destination must be an explicit non-primary path")
		}
		if _, err := os.Stat(absolute); err == nil {
			return settled, fmt.Errorf("workspace destination already exists: %s", absolute)
		} else if !os.IsNotExist(err) {
			return settled, err
		}
		baseConfig, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "show", baseRevision+":.boatstack/project.json")
		if err != nil {
			return settled, fmt.Errorf("workspace base does not contain the verified Boatstack configuration: %w", err)
		}
		_, baseFingerprint, fingerprintErr := protocol.ProjectConfigFingerprint(baseConfig)
		if fingerprintErr != nil {
			return settled, fmt.Errorf("workspace base configuration is invalid: %w", fingerprintErr)
		}
		if state.ConfigFingerprint == "" || baseFingerprint != state.ConfigFingerprint {
			return settled, fmt.Errorf("workspace base configuration does not match current repository authority")
		}
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "check-ref-format", "--branch", branch); err != nil {
			return settled, fmt.Errorf("invalid workspace branch: %s: %w", strings.TrimSpace(string(output)), err)
		}
		arguments := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf", "worktree", "add", "-b", branch, absolute, baseRevision}
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
		removeArguments := append(gitPrefix, "worktree", "remove")
		removeArguments = append(removeArguments, state.WorkspacePath)
		if output, err := b.runner.CombinedOutput(ctx, neutralDirectory, "git", removeArguments...); err != nil {
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
		deliveryID, err := safeSegment(admission.Objective.DeliveryID, "delivery identity")
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
		refspec := admission.SourceRevision + ":refs/heads/" + preview.HeadRef
		if output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "git", "push", "origin", refspec); err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
		output, err := b.runner.CombinedOutput(ctx, layout.RepositoryRoot, "gh", "pr", "create", "--base", preview.BaseRef, "--head", preview.HeadRef, "--fill-first", "--body-file", preview.BodyPath)
		if err != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: strings.TrimSpace(string(output))}, nil
		}
		publicationID, parseErr := publicationIDFromCreateOutput(output)
		if parseErr != nil {
			return ports.EffectResult{Settlement: ports.EffectUnknown, Detail: parseErr.Error()}, nil
		}
		return ports.EffectResult{Settlement: ports.EffectSettled, Outputs: protocol.Parameters{{Name: "publication_id", Value: publicationID}}}, nil
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

func publicationIDFromCreateOutput(output []byte) (string, error) {
	lines := strings.Fields(string(output))
	for index := len(lines) - 1; index >= 0; index-- {
		parsed, err := url.Parse(lines[index])
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) < 2 || segments[len(segments)-2] != "pull" {
			continue
		}
		if number, err := strconv.ParseUint(segments[len(segments)-1], 10, 64); err == nil && number > 0 {
			return strconv.FormatUint(number, 10), nil
		}
	}
	return "", fmt.Errorf("publication provider did not return an exact pull-request identity")
}

func publicationProductStatus(status string) string {
	records := strings.Split(status, "\x00")
	kept := make([]string, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		code, name := record[:2], filepath.ToSlash(record[3:])
		prior := ""
		if (code[0] == 'R' || code[0] == 'C' || code[1] == 'R' || code[1] == 'C') && index+1 < len(records) {
			index++
			prior = filepath.ToSlash(records[index])
		}
		if publicationGeneratedPath(name) && (prior == "" || publicationGeneratedPath(prior)) {
			continue
		}
		kept = append(kept, record)
		if prior != "" {
			kept = append(kept, prior)
		}
	}
	return strings.Join(kept, "\x00")
}

func publicationGeneratedPath(name string) bool {
	for _, prefix := range []string{
		".boatstack/approvals/", ".boatstack/evidence/", ".boatstack/planning-packages/",
		".boatstack/plans/", ".boatstack/publication/",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
