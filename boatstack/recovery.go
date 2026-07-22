package boatstack

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const recoveryStatusSchemaVersion = 1

// RecoveryStatus is the read-only, host-neutral decision for a reported
// delivery problem. It deliberately carries no authority to edit, approve, or
// publish anything.
type RecoveryStatus struct {
	SchemaVersion        int      `json:"schema_version"`
	VerificationStatus   string   `json:"verification_status"`
	Feature              string   `json:"feature,omitempty"`
	Slice                string   `json:"slice,omitempty"`
	ParentDelivery       string   `json:"parent_delivery,omitempty"`
	Lifecycle            string   `json:"lifecycle,omitempty"`
	PRURL                string   `json:"pr_url,omitempty"`
	HeadBranch           string   `json:"head_branch,omitempty"`
	ObservedPRHeadSHA    string   `json:"observed_pr_head_sha,omitempty"`
	NextOperation        string   `json:"next_operation"`
	SuggestedFeatureID   string   `json:"suggested_feature_id,omitempty"`
	ExistingDiffSHA256   string   `json:"existing_diff_sha256,omitempty"`
	ExistingChangedPaths []string `json:"existing_changed_paths,omitempty"`
	Reason               string   `json:"reason"`
	Blockers             []string `json:"blockers,omitempty"`
}

type RecoveryStatusOptions struct {
	Repo            string
	Feature         string
	Message         string
	SourceStage     string
	Evidence        string
	ObservedHeadSHA string
}

type publishedPRObservation struct {
	Lifecycle string
	URL       string
	Branch    string
	HeadSHA   string
}

var recoveryGh = func(repo string, arguments ...string) (string, error) {
	return commandOutput(repo, "gh", arguments...)
}

func blockedRecovery(reason string, blockers ...string) RecoveryStatus {
	return RecoveryStatus{
		SchemaVersion: recoveryStatusSchemaVersion, VerificationStatus: "BLOCKED",
		NextOperation: "resolve_ambiguity", Reason: reason, Blockers: blockers,
	}
}

func allManagedDeliveryStates(repo string) ([]DeliveryState, error) {
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
	states := []DeliveryState{}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		state, loadErr := CurrentDeliveryState(repo, entry.Name())
		if loadErr != nil {
			return nil, fmt.Errorf("invalid managed delivery state for %s: %w", entry.Name(), loadErr)
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Feature < states[j].Feature })
	return states, nil
}

func deliveryBranchAndSlice(state DeliveryState) (string, string, string) {
	if len(state.Slices) == 0 {
		return "", "", ""
	}
	index := state.ActiveIndex
	if index >= len(state.Slices) {
		index = len(state.Slices) - 1
	}
	slice := state.Slices[index]
	return slice.HeadBranch, slice.ID, slice.PRURL
}

func stateMatchesBranch(state DeliveryState, branch string) bool {
	if strings.TrimSpace(branch) == "" {
		return false
	}
	head, _, _ := deliveryBranchAndSlice(state)
	if head != "" {
		return head == branch
	}
	return branchForFeature(state.Feature) == branch
}

func selectRecoveryDelivery(states []DeliveryState, explicitFeature, currentBranch string) (DeliveryState, []string, error) {
	if explicitFeature != "" {
		for _, state := range states {
			if state.Feature == explicitFeature {
				return state, nil, nil
			}
		}
		return DeliveryState{}, nil, fmt.Errorf("managed delivery %s does not exist", explicitFeature)
	}
	selectMatching := func(active bool) []DeliveryState {
		matches := []DeliveryState{}
		for _, state := range states {
			isActive := state.ActiveIndex < len(state.Slices)
			if isActive == active && stateMatchesBranch(state, currentBranch) {
				matches = append(matches, state)
			}
		}
		return matches
	}
	for _, active := range []bool{true, false} {
		matches := selectMatching(active)
		if len(matches) == 1 {
			return matches[0], nil, nil
		}
		if len(matches) > 1 {
			features := make([]string, 0, len(matches))
			for _, state := range matches {
				features = append(features, state.Feature)
			}
			return DeliveryState{}, features, nil
		}
	}
	if len(states) == 1 {
		return states[0], nil, nil
	}
	features := make([]string, 0, len(states))
	for _, state := range states {
		features = append(features, state.Feature)
	}
	return DeliveryState{}, features, nil
}

func observePublishedPR(repo string, state DeliveryState) publishedPRObservation {
	branch, _, prURL := deliveryBranchAndSlice(state)
	observation := publishedPRObservation{Lifecycle: "PUBLISHED_UNKNOWN", URL: prURL, Branch: branch}
	target := prURL
	if target == "" {
		target = branch
	}
	if target == "" {
		return observation
	}
	value, err := recoveryGh(repo, "pr", "view", target, "--json", "state,headRefName,headRefOid,url")
	if err != nil {
		return observation
	}
	var payload struct {
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
		HeadRefOID  string `json:"headRefOid"`
		URL         string `json:"url"`
	}
	if DecodeJSON("inspect published PR", target, []byte(value), &payload) != nil {
		return observation
	}
	if payload.URL != "" {
		observation.URL = payload.URL
	}
	if payload.HeadRefName != "" {
		observation.Branch = payload.HeadRefName
	}
	observation.HeadSHA = payload.HeadRefOID
	switch strings.ToUpper(strings.TrimSpace(payload.State)) {
	case "OPEN":
		observation.Lifecycle = "PUBLISHED_OPEN"
	case "MERGED":
		observation.Lifecycle = "PUBLISHED_MERGED"
	case "CLOSED":
		observation.Lifecycle = "PUBLISHED_CLOSED"
	}
	return observation
}

func suggestedCorrectionFeature(states []DeliveryState, parent string) string {
	used := map[int]bool{}
	prefix := parent + "-correction-"
	for _, state := range states {
		if state.ParentDelivery != parent || !strings.HasPrefix(state.Feature, prefix) {
			continue
		}
		value, err := strconv.Atoi(strings.TrimPrefix(state.Feature, prefix))
		if err == nil && value > 0 {
			used[value] = true
		}
	}
	for value := 1; ; value++ {
		if !used[value] {
			return fmt.Sprintf("%s%02d", prefix, value)
		}
	}
}

func existingRecoveryDiff(repo string, state DeliveryState) (string, []string) {
	baseCommit := ""
	if len(state.Slices) > 0 {
		last := state.Slices[len(state.Slices)-1]
		if receipt, err := readDeliveryReceipt(repo, state.Feature, last.ID, "review"); err == nil {
			baseCommit = strings.TrimSpace(receipt.HeadCommit)
		}
	}
	if baseCommit == "" {
		base := defaultPRBase(repo)
		if len(state.Slices) > 0 && strings.TrimSpace(state.Slices[len(state.Slices)-1].BaseBranch) != "" {
			base = state.Slices[len(state.Slices)-1].BaseBranch
		}
		resolved, err := resolveBaseCommit(repo, base)
		if err != nil {
			return "", nil
		}
		baseCommit = resolved
	}
	diff, err := exec.Command("git", "-C", repo, "diff", "--binary", "--no-ext-diff", baseCommit, "--", ".").Output()
	if err != nil {
		return "", nil
	}
	// Read stdout only: Git may emit platform-specific line-ending warnings on
	// stderr, and those messages are not changed paths. NUL separation also
	// preserves unusual but valid filenames.
	names, err := exec.Command("git", "-C", repo, "diff", "--name-only", "-z", baseCommit, "--", ".").Output()
	if err != nil {
		return "", nil
	}
	paths := []string{}
	for _, raw := range bytes.Split(names, []byte{0}) {
		if len(raw) != 0 {
			paths = append(paths, filepath.ToSlash(string(raw)))
		}
	}
	untracked, err := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return "", nil
	}
	canonical := bytes.NewBuffer(diff)
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := filepath.ToSlash(string(raw))
		value, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
		if readErr != nil {
			return "", nil
		}
		paths = append(paths, path)
		canonical.WriteString("\nuntracked ")
		canonical.WriteString(path)
		canonical.WriteString(" ")
		canonical.WriteString(SHA256Bytes(value))
	}
	if canonical.Len() == 0 {
		return "", nil
	}
	sort.Strings(paths)
	return SHA256Bytes(canonical.Bytes()), paths
}

// ResolveRecovery identifies the delivery that owns an exact correction and
// returns the safe transition. It does not record the request or draft files.
func ResolveRecovery(options RecoveryStatusOptions) (RecoveryStatus, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return RecoveryStatus{}, err
	}
	if strings.TrimSpace(options.Message) == "" || strings.TrimSpace(options.SourceStage) == "" {
		return RecoveryStatus{}, fmt.Errorf("recovery status requires the exact message and source stage")
	}
	switch strings.ToLower(strings.TrimSpace(options.SourceStage)) {
	case "ci", "review", "publication", "user":
	default:
		return RecoveryStatus{}, fmt.Errorf("recovery source stage must be ci, review, publication, or user")
	}
	if !fileExists(filepath.Join(repo, ".product-loop", "project.json")) {
		return RecoveryStatus{
			SchemaVersion: recoveryStatusSchemaVersion, VerificationStatus: "UNVERIFIED",
			NextOperation: "none", Reason: "This repository has no managed delivery installation to inspect.",
		}, nil
	}
	states, err := allManagedDeliveryStates(repo)
	if err != nil {
		return blockedRecovery("Managed delivery state cannot be verified: " + err.Error()), nil
	}
	branch, _ := gitCommand(repo, "branch", "--show-current")
	selected, ambiguity, selectErr := selectRecoveryDelivery(states, strings.TrimSpace(options.Feature), strings.TrimSpace(branch))
	if selectErr != nil {
		return blockedRecovery(selectErr.Error()), nil
	}
	if len(ambiguity) > 0 {
		return blockedRecovery("More than one managed delivery could own this correction; choose the feature explicitly.", ambiguity...), nil
	}
	if selected.Feature == "" {
		return RecoveryStatus{
			SchemaVersion: recoveryStatusSchemaVersion, VerificationStatus: "UNVERIFIED",
			NextOperation: "none", Reason: "No managed delivery matches the current branch or correction.",
		}, nil
	}
	head, sliceID, prURL := deliveryBranchAndSlice(selected)
	status := RecoveryStatus{
		SchemaVersion: recoveryStatusSchemaVersion, VerificationStatus: "VERIFIED",
		Feature: selected.Feature, Slice: sliceID, ParentDelivery: selected.ParentDelivery,
		HeadBranch: head, PRURL: prURL,
	}
	if selected.ActiveIndex < len(selected.Slices) {
		status.Lifecycle = "ACTIVE"
		status.NextOperation = "repair_active"
		status.Reason = fmt.Sprintf("Managed delivery %q is active; route the exact correction through its current repair boundary.", selected.Feature)
		return status, nil
	}
	pr := observePublishedPR(repo, selected)
	status.Lifecycle = pr.Lifecycle
	status.PRURL = pr.URL
	status.ObservedPRHeadSHA = pr.HeadSHA
	if pr.Branch != "" {
		status.HeadBranch = pr.Branch
	}
	if head != "" && pr.Branch != "" && head != pr.Branch {
		status.VerificationStatus = "BLOCKED"
		status.NextOperation = "resolve_ambiguity"
		status.Reason = "The recorded delivery branch does not match the observed PR head branch."
		status.Blockers = []string{head, pr.Branch}
		return status, nil
	}
	if expected := strings.TrimSpace(options.ObservedHeadSHA); expected != "" && pr.HeadSHA != "" && expected != pr.HeadSHA {
		status.VerificationStatus = "BLOCKED"
		status.NextOperation = "none"
		status.Reason = "The reported failure belongs to a stale PR head; refresh the failure evidence before drafting a correction."
		status.Blockers = []string{"reported_head=" + expected, "current_head=" + pr.HeadSHA}
		return status, nil
	}
	status.NextOperation = "draft_corrective_child"
	status.SuggestedFeatureID = suggestedCorrectionFeature(states, selected.Feature)
	status.ExistingDiffSHA256, status.ExistingChangedPaths = existingRecoveryDiff(repo, selected)
	switch pr.Lifecycle {
	case "PUBLISHED_OPEN":
		status.Reason = "The published PR is open; draft an independently approved corrective child that will update the same PR."
	case "PUBLISHED_MERGED", "PUBLISHED_CLOSED":
		status.Reason = "The prior PR is no longer open; draft an independently approved corrective child on a fresh branch and PR."
	default:
		status.Reason = "The PR state cannot be verified; draft the corrective child now and defer its publication destination until verification succeeds."
	}
	return status, nil
}
