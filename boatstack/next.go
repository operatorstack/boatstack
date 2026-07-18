package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const nextStatusSchemaVersion = 1

// NextStatus is the read-only, host-neutral projection of Boatstack's current
// workflow position. Conversation and terminal context are deliberately absent:
// adapters may present them as context, but they are not workflow evidence.
type NextStatus struct {
	SchemaVersion      int      `json:"schema_version"`
	VerificationStatus string   `json:"verification_status"`
	Feature            string   `json:"feature,omitempty"`
	ActiveSlice        string   `json:"active_slice,omitempty"`
	ObservedStage      string   `json:"observed_stage"`
	NextOperation      string   `json:"next_operation"`
	Reason             string   `json:"reason"`
	BlockingAmbiguity  []string `json:"blocking_ambiguity,omitempty"`
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
		if entry.IsDir() && featureSlugPattern.MatchString(entry.Name()) && fileExists(filepath.Join(root, entry.Name(), "plan.md")) {
			features = append(features, entry.Name())
		}
	}
	sort.Strings(features)
	return features, nil
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

// ResolveNext performs bounded, local, read-only state inspection. It never
// contacts GitHub and never treats process or conversation history as evidence.
func ResolveNext(repoPath string) (NextStatus, error) {
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

	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return NextStatus{}, err
	}
	if len(active) > 1 {
		base.VerificationStatus = "BLOCKED"
		base.ObservedStage = "AMBIGUOUS"
		base.NextOperation = "resolve-ambiguity"
		base.Reason = "More than one managed delivery is active; Boatstack will not choose by recency."
		base.BlockingAmbiguity = active
		return base, nil
	}
	if len(active) == 1 {
		return nextForDelivery(repo, active[0])
	}
	completed, err := completedManagedStates(repo)
	if err != nil {
		return NextStatus{}, err
	}
	if len(completed) > 0 {
		base.VerificationStatus = "VERIFIED"
		base.ObservedStage = "FEATURE_COMPLETE"
		base.NextOperation = "none"
		if len(completed) == 1 {
			base.Feature = completed[0].Feature
			if len(completed[0].Slices) > 0 {
				base.ActiveSlice = completed[0].Slices[len(completed[0].Slices)-1].ID
			}
			base.Reason = fmt.Sprintf("All managed slices for feature %q are already published.", completed[0].Feature)
		} else {
			base.Reason = "All managed delivery states are already published."
		}
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
		if fileExists(filepath.Join(directory, "approval.md")) {
			base.ObservedStage = "APPROVED"
			base.NextOperation = "build"
			base.Reason = "The saved feature has an approval receipt but no active delivery state."
		} else {
			base.ObservedStage = "DRAFT_PLAN"
			base.NextOperation = "plan-gate"
			base.Reason = "The saved feature plan has not been approved."
		}
		return base, nil
	}

	base.VerificationStatus = "VERIFIED"
	base.ObservedStage = "FEATURE_COMPLETE"
	base.NextOperation = "none"
	base.Reason = "No managed Boatstack plan or delivery remains active."
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
		parts = append(parts, "Active slice: "+status.ActiveSlice)
	}
	parts = append(parts, "Reason: "+status.Reason, "Next: "+status.NextOperation)
	if len(status.BlockingAmbiguity) > 0 {
		parts = append(parts, "Candidates: "+strings.Join(status.BlockingAmbiguity, ", "))
	}
	return strings.Join(parts, "\n") + "\n"
}
