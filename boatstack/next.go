package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const nextStatusSchemaVersion = 2

// NextStatus is the read-only, host-neutral projection of Boatstack's current
// workflow position. Conversation and terminal context are deliberately absent:
// adapters may present them as context, but they are not workflow evidence.
type NextStatus struct {
	SchemaVersion      int              `json:"schema_version"`
	VerificationStatus string           `json:"verification_status"`
	Feature            string           `json:"feature,omitempty"`
	ActiveSlice        string           `json:"active_slice,omitempty"`
	SliceIndex         int              `json:"slice_index,omitempty"`
	TotalSlices        int              `json:"total_slices,omitempty"`
	ObservedStage      string           `json:"observed_stage"`
	NextOperation      string           `json:"next_operation"`
	Operator           DecisionOperator `json:"operator,omitempty"`
	Reason             string           `json:"reason"`
	BlockingAmbiguity  []string         `json:"blocking_ambiguity,omitempty"`
	Lifecycle          string           `json:"lifecycle,omitempty"`
	PRURL              string           `json:"pr_url,omitempty"`
	HeadBranch         string           `json:"head_branch,omitempty"`
	ParentDelivery     string           `json:"parent_delivery,omitempty"`
}

func blockedNextStatus(stage, operation, reason string, ambiguity ...string) NextStatus {
	return NextStatus{
		SchemaVersion: nextStatusSchemaVersion, VerificationStatus: "BLOCKED",
		ObservedStage: stage, NextOperation: operation, Reason: reason,
		BlockingAmbiguity: ambiguity,
	}
}

func featurePlanCandidates(repo string) ([]string, error) {
	root := filepath.Join(repo, ".product-loop", "features")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	features := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) || !fileExists(filepath.Join(root, entry.Name(), "plan.md")) {
			continue
		}
		statePath, stateErr := deliveryStatePath(repo, entry.Name())
		if stateErr != nil {
			return nil, stateErr
		}
		if !fileExists(statePath) {
			features = append(features, entry.Name())
		}
	}
	sort.Strings(features)
	return features, nil
}

func unclaimedSourcePlanCandidates(repo string) ([]string, error) {
	candidates, err := sourcePlanCandidates(repo)
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{}
	root := filepath.Join(repo, ".product-loop", "features")
	entries, readErr := os.ReadDir(root)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		planPath := filepath.Join(root, entry.Name(), "plan.md")
		sourcePath, sourceErr := SourcePlanForStructuredPlan(planPath)
		if sourceErr != nil {
			continue
		}
		absolute, absoluteErr := filepath.Abs(sourcePath)
		if absoluteErr != nil {
			return nil, absoluteErr
		}
		claimed[filepath.Clean(absolute)] = true
	}
	unclaimed := []string{}
	for _, candidate := range candidates {
		absolute := candidate
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(repo, filepath.FromSlash(candidate))
		}
		absolute, err = filepath.Abs(absolute)
		if err != nil {
			return nil, err
		}
		if !claimed[filepath.Clean(absolute)] {
			unclaimed = append(unclaimed, candidate)
		}
	}
	return unclaimed, nil
}

func orphanedFeatureArtifacts(repo string) ([]string, error) {
	root := filepath.Join(repo, ".product-loop", "features")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	orphans := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		if fileExists(filepath.Join(directory, "pr.md")) && !fileExists(filepath.Join(directory, "plan.lock.json")) {
			orphans = append(orphans, entry.Name())
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

func nextForDelivery(repo, feature string) (NextStatus, error) {
	state, err := CurrentDeliveryState(repo, feature)
	if err != nil {
		return NextStatus{}, err
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return NextStatus{}, err
	}
	status := NextStatus{
		SchemaVersion: nextStatusSchemaVersion, VerificationStatus: "VERIFIED",
		Feature: feature, ActiveSlice: slice.ID, ObservedStage: slice.Status,
		SliceIndex: state.ActiveIndex + 1, TotalSlices: len(state.Slices),
	}
	switch slice.Status {
	case "BUILD":
		status.NextOperation = "build"
		status.Reason = "The approved delivery slice is active and has no current test-gate receipt."
	case "TEST_PASSED":
		status.NextOperation = "review-gate"
		status.Reason = "The active delivery slice has current test evidence and still requires review."
	case "REVIEW_PASSED":
		previewPath := filepath.Join(repo, ".product-loop", "features", feature, "pr.md")
		if preview, previewErr := ParsePRPreview(previewPath); previewErr == nil && preview.Feature == feature && preview.SliceID == slice.ID {
			status.ObservedStage = "PR_PREVIEW"
			status.Reason = "A reviewer-ready PR preview exists for the reviewed active slice and must be reconfirmed through the ship gate."
		} else {
			status.Reason = "The active delivery slice has current test and review evidence and is ready for PR preparation."
		}
		status.NextOperation = "ship-gate"
	default:
		return NextStatus{}, fmt.Errorf("managed delivery slice %s has unsupported status %q", slice.ID, slice.Status)
	}
	return status, nil
}

func nextForPublished(repo string, state DeliveryState) NextStatus {
	pr := observePublishedPR(repo, state)
	_, sliceID, _ := deliveryBranchAndSlice(state)
	status := NextStatus{
		SchemaVersion: nextStatusSchemaVersion, VerificationStatus: "VERIFIED",
		Feature: state.Feature, ActiveSlice: sliceID, SliceIndex: len(state.Slices),
		TotalSlices: len(state.Slices), ObservedStage: "PUBLISHED", NextOperation: "none",
		Lifecycle: pr.Lifecycle, PRURL: pr.URL, HeadBranch: pr.Branch,
		ParentDelivery: state.ParentDelivery,
	}
	switch pr.Lifecycle {
	case "PUBLISHED_MERGED":
		status.ObservedStage = "FEATURE_COMPLETE"
		status.Reason = fmt.Sprintf("The published PR for feature %q is merged.", state.Feature)
	case "PUBLISHED_OPEN":
		status.Reason = fmt.Sprintf("Feature %q is published in an open PR; review and required checks may still produce a corrective delivery.", state.Feature)
	case "PUBLISHED_CLOSED":
		status.Reason = fmt.Sprintf("The PR for feature %q is closed without a verified merge; a future correction requires a fresh PR.", state.Feature)
	default:
		status.Reason = fmt.Sprintf("Feature %q is published, but its PR state could not be verified.", state.Feature)
	}
	return status
}

func completedManagedStates(repo string) ([]DeliveryState, error) {
	directory, err := deliveryStateDirectory(repo)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	completed := []DeliveryState{}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		state, err := CurrentDeliveryState(repo, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("invalid managed delivery state for %s: %w", entry.Name(), err)
		}
		if state.ActiveIndex >= len(state.Slices) {
			completed = append(completed, state)
		}
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].Feature < completed[j].Feature })
	return completed, nil
}

// ResolveNext performs bounded, read-only state inspection. Published states
// use the recorded PR identity when GitHub is available; conversation and
// process history are never treated as evidence.
func ResolveNext(repoPath, explicitFeature string) (NextStatus, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return NextStatus{}, err
	}
	base := NextStatus{SchemaVersion: nextStatusSchemaVersion}
	if !fileExists(filepath.Join(repo, ".product-loop", "project.json")) {
		base.VerificationStatus = "UNVERIFIED"
		base.ObservedStage = "NOT_INITIALIZED"
		base.NextOperation = "init"
		base.Reason = "This repository has no Boatstack project installation to inspect."
		return base, nil
	}
	config, _, configErr := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if configErr != nil {
		return blockedNextStatus("INVALID_STATE", "repair-state", "Boatstack project configuration is invalid: "+configErr.Error()), nil
	}

	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return blockedNextStatus("INVALID_STATE", "repair-state", "Boatstack found invalid managed delivery state. Preserve the artifacts and restore the missing or stale evidence before continuing: "+err.Error()), nil
	}

	if explicitFeature != "" {
		found := false
		for _, f := range active {
			if f == explicitFeature {
				found = true
				break
			}
		}
		if found {
			active = []string{explicitFeature}
		} else if completedState, completedErr := CurrentDeliveryState(repo, explicitFeature); completedErr == nil && completedState.ActiveIndex >= len(completedState.Slices) {
			return nextForPublished(repo, completedState), nil
		} else {
			return blockedNextStatus("INVALID_STATE", "repair-state", fmt.Sprintf("Feature %s is not a verifiable active or published managed delivery.", explicitFeature)), nil
		}
	}

	if len(active) > 1 {
		base.VerificationStatus = "BLOCKED"
		base.ObservedStage = "AMBIGUOUS"
		base.NextOperation = "resolve-ambiguity"
		base.Operator = OperatorQuery
		base.Reason = "More than one managed delivery is active; Boatstack will not choose by recency."
		base.BlockingAmbiguity = active
		return base, nil
	}
	if len(active) == 1 {
		status, deliveryErr := nextForDelivery(repo, active[0])
		if deliveryErr != nil {
			return blockedNextStatus("INVALID_STATE", "repair-state", "Boatstack could not verify the active managed delivery. Preserve the artifacts and restore its evidence before continuing: "+deliveryErr.Error()), nil
		}
		return status, nil
	}

	orphans, err := orphanedFeatureArtifacts(repo)
	if err != nil {
		return NextStatus{}, err
	}
	if len(orphans) > 0 {
		return blockedNextStatus("INVALID_STATE", "repair-state", "Boatstack found a PR preview without the plan lock required to verify it. Preserve the artifacts and restore the feature evidence before continuing.", orphans...), nil
	}

	sourcePlans, sourceErr := unclaimedSourcePlanCandidates(repo)
	if sourceErr != nil {
		return NextStatus{}, sourceErr
	}
	if len(sourcePlans) == 1 {
		base.VerificationStatus = "VERIFIED"
		base.ObservedStage = "SOURCE_PLAN_READY"
		base.NextOperation = "auto-plan"
		base.Reason = fmt.Sprintf("Saved Plan-mode file %q is ready to become a Boatstack feature.", sourcePlans[0])
		return base, nil
	}
	if len(sourcePlans) > 1 {
		base.VerificationStatus = "BLOCKED"
		base.ObservedStage = "AMBIGUOUS"
		base.NextOperation = "resolve-ambiguity"
		base.Reason = "Multiple unclaimed Plan-mode files are available; Boatstack will not choose by recency."
		base.BlockingAmbiguity = sourcePlans
		return base, nil
	}

	candidates, err := featurePlanCandidates(repo)
	if err != nil {
		return NextStatus{}, err
	}
	if len(candidates) > 1 {
		base.VerificationStatus = "BLOCKED"
		base.ObservedStage = "AMBIGUOUS"
		base.NextOperation = "resolve-ambiguity"
		base.Reason = "More than one saved feature plan is available; Boatstack will not choose by recency."
		base.BlockingAmbiguity = candidates
		return base, nil
	}
	if len(candidates) == 1 {
		feature := candidates[0]
		directory := filepath.Join(repo, ".product-loop", "features", feature)
		base.VerificationStatus = "VERIFIED"
		base.Feature = feature
		if !config.Workflow.HumanPlanApproval {
			base.ObservedStage = "POLICY_READY"
			base.NextOperation = "build"
			base.Reason = "The saved feature is ready for fingerprinted policy activation without a human approval receipt."
			if workspaceEnabled(repo) && needsFreshCut(repo, feature) {
				base.NextOperation = "workspace-cut"
				base.Reason = fmt.Sprintf("Feature %q is policy-authorized; cut a fresh workspace from the default branch before building.", feature)
			}
		} else if fileExists(filepath.Join(directory, "approval.md")) {
			base.ObservedStage = "APPROVED"
			base.NextOperation = "build"
			base.Reason = "The saved feature has an approval receipt but no active delivery state."
			// Cut a fresh workspace before building so work never starts on a
			// stale base branch. Local-only check; the cut itself fetches origin.
			if workspaceEnabled(repo) && needsFreshCut(repo, feature) {
				base.NextOperation = "workspace-cut"
				base.Reason = fmt.Sprintf("Feature %q is approved; cut a fresh workspace from the default branch before building.", feature)
			}
		} else {
			base.ObservedStage = "DRAFT_PLAN"
			base.NextOperation = "plan-gate"
			base.Reason = "The saved feature plan has not been approved."
		}
		return base, nil
	}

	completed, err := completedManagedStates(repo)
	if err != nil {
		return blockedNextStatus("INVALID_STATE", "repair-state", "Boatstack found invalid completed delivery state. Preserve the artifacts and restore its evidence before continuing: "+err.Error()), nil
	}
	if len(completed) > 0 {
		if len(completed) == 1 {
			base = nextForPublished(repo, completed[0])
			// When workspace management is on and the shipped feature still has a
			// linked worktree locally, surface cleanup only after a verified merge.
			if base.Lifecycle == "PUBLISHED_MERGED" && base.HeadBranch != "" && workspaceEnabled(repo) {
				if path := worktreePathForBranch(repo, base.HeadBranch); path != "" {
					base.NextOperation = "workspace-cleanup"
					base.Reason = fmt.Sprintf("Feature %q is merged; its workspace on %q can be cleaned up.", completed[0].Feature, base.HeadBranch)
				}
			}
		} else {
			branch, _ := gitCommand(repo, "branch", "--show-current")
			matches := []DeliveryState{}
			for _, state := range completed {
				if stateMatchesBranch(state, strings.TrimSpace(branch)) {
					matches = append(matches, state)
				}
			}
			if len(matches) == 1 {
				base = nextForPublished(repo, matches[0])
			} else {
				base.VerificationStatus = "BLOCKED"
				base.ObservedStage = "AMBIGUOUS"
				base.NextOperation = "resolve-ambiguity"
				base.Reason = "Multiple published deliveries exist and none is uniquely associated with the current branch."
				for _, state := range completed {
					base.BlockingAmbiguity = append(base.BlockingAmbiguity, state.Feature)
				}
			}
		}
		return base, nil
	}

	base.VerificationStatus = "VERIFIED"
	base.ObservedStage = "NOT_STARTED"
	base.NextOperation = "auto-plan"
	base.Reason = "No Boatstack feature has started and no saved Plan-mode file is available."
	return base, nil
}

func FormatNextStatus(status NextStatus) string {
	parts := []string{
		"Boatstack stage: " + status.ObservedStage,
		"Verification: " + status.VerificationStatus,
	}
	if status.Feature != "" {
		parts = append(parts, "Feature: "+status.Feature)
	}
	if status.ActiveSlice != "" {
		if status.TotalSlices > 1 {
			parts = append(parts, fmt.Sprintf("Active slice: %s (PR %d of %d)", status.ActiveSlice, status.SliceIndex, status.TotalSlices))
		} else {
			parts = append(parts, "Active slice: "+status.ActiveSlice)
		}
	}
	if status.Lifecycle != "" {
		parts = append(parts, "Lifecycle: "+status.Lifecycle)
	}
	if status.PRURL != "" {
		parts = append(parts, "PR: "+status.PRURL)
	}
	parts = append(parts, "Reason: "+status.Reason, "Next: "+status.NextOperation)
	if len(status.BlockingAmbiguity) > 0 {
		parts = append(parts, "Candidates: "+strings.Join(status.BlockingAmbiguity, ", "))
	}
	return strings.Join(parts, "\n") + "\n"
}
