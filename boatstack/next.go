package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
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
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		if !fileExists(filepath.Join(directory, "plan.md")) {
			continue
		}
		// A feature that has been locked (built) or shipped is past planning and
		// must never re-register as an open plan candidate, even when its
		// ephemeral per-worktree delivery state.json was destroyed by worktree
		// cleanup on ship. plan.lock.json / pr.md are the durable committed
		// signals, mirroring orphanedFeatureArtifacts.
		if fileExists(filepath.Join(directory, "plan.lock.json")) || fileExists(filepath.Join(directory, "pr.md")) {
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
	case StatusBuild:
		status.NextOperation = "build"
		status.Reason = "The approved delivery slice is active and has no current test-gate receipt."
	case StatusTestPassed:
		status.NextOperation = "review-gate"
		status.Reason = "The active delivery slice has current test evidence and still requires review."
	case StatusReviewPassed:
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
	persistObservedTerminalPRState(repo, state, pr)
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
			// A completed delivery that cannot be verified on THIS branch — e.g. a
			// divergent or absent committed plan lock for work shipped on another
			// branch that shares the delivery store — must not poison resolution of
			// an unrelated new feature. Structurally corrupt state is already
			// surfaced upstream by scanManagedDeliveries; skipping here only
			// tolerates cross-branch lock divergence. A delivery the caller actually
			// acts on is still verified at its own boundary (explicit-feature lookup
			// / nextForDelivery). control-law: stale-delivery-cannot-block-unrelated-feature
			continue
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
	if !fileExists(WorkspaceFor(repo).ProjectConfigPath()) {
		base.VerificationStatus = "UNVERIFIED"
		base.ObservedStage = "NOT_INITIALIZED"
		base.NextOperation = "init"
		base.Reason = "This repository has no Boatstack project installation to inspect."
		return base, nil
	}
	config, _, configErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if configErr != nil {
		// Channel fault: an invalid config cannot be cleared by any mutation verb —
		// the operator repairs the file. Route to the read-only doctor to diagnose,
		// not repair-state (which quarantines a draft and would not help). Coreachability.
		return blockedNextStatus("INVALID_STATE", "doctor", "Boatstack project configuration is invalid; fix the config file, then re-run (doctor diagnoses): "+configErr.Error()), nil
	}

	// Read-only boundary: apply the ignored-deliveries filter BEFORE a single
	// invalid delivery can escalate into a repo-wide INVALID_STATE. The strict
	// ActiveManagedDeliveries (used by mutation paths) aborts on any invalid
	// delivery; here we partition instead, filter both lists by the operator's
	// ignore policy, and only then block — and only on a still-unignored invalid
	// delivery, naming it and pointing at the discard-delivery remedy. This is
	// what keeps one stale delivery in the shared store from blocking an
	// unrelated new feature. control-law: stale-delivery-cannot-block-unrelated-feature
	active, invalidDeliveries, scanErr := scanManagedDeliveries(repo)
	if scanErr != nil {
		// Channel fault reading the store: observation loss, diagnosed by doctor —
		// not repaired by quarantining a draft. Coreachability.
		return blockedNextStatus("INVALID_STATE", "doctor", "Boatstack could not read the managed delivery store; diagnose the channel with doctor: "+scanErr.Error()), nil
	}
	active = withoutIgnoredDeliveries(active, config.Workflow.IgnoredDeliveries)
	invalidDeliveries = withoutIgnoredDeliveries(invalidDeliveries, config.Workflow.IgnoredDeliveries)
	if len(invalidDeliveries) > 0 {
		return blockedNextStatus("INVALID_STATE", "discard-delivery", "Boatstack found managed delivery state it cannot verify. Restore its evidence, add it to workflow.ignored_deliveries, or run discard-delivery to clear it before continuing.", invalidDeliveries...), nil
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
			// Unverifiable named delivery: discard-delivery accepts and archives it
			// (repair-state refuses registered/tracked dirs). Coreachability.
			return blockedNextStatus("INVALID_STATE", "discard-delivery", fmt.Sprintf("Feature %s is not a verifiable active or published managed delivery; clear it with discard-delivery.", explicitFeature), explicitFeature), nil
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
			// Unverifiable active delivery state: discard-delivery archives it
			// (reversibly); repair-state would refuse the registered dir. Coreachability.
			return blockedNextStatus("INVALID_STATE", "discard-delivery", "Boatstack could not verify the active managed delivery; restore its evidence, or archive it with discard-delivery to continue: "+deliveryErr.Error(), active[0]), nil
		}
		return status, nil
	}

	orphans, err := orphanedFeatureArtifacts(repo)
	if err != nil {
		return NextStatus{}, err
	}
	if len(orphans) > 0 {
		// An orphan (pr.md, no plan.lock) is a published-then-unlinked delivery.
		// repair-state refuses it (pr.md is a durable-authority blocker); discard-delivery
		// accepts and archives the orphaned artifacts. Coreachability.
		return blockedNextStatus("INVALID_STATE", "discard-delivery", "Boatstack found a PR preview without the plan lock required to verify it; restore the feature evidence, or archive the orphan with discard-delivery.", orphans...), nil
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
		// Invalid completed delivery state: discard-delivery archives it reversibly;
		// repair-state refuses a delivery-bearing dir. Coreachability.
		return blockedNextStatus("INVALID_STATE", "discard-delivery", "Boatstack found invalid completed delivery state; restore its evidence, or archive it with discard-delivery to continue: "+err.Error()), nil
	}
	completed = withoutIgnoredDeliveryStates(completed, config.Workflow.IgnoredDeliveries)
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
				// At the merge checkpoint, prefer the backlog sweep: if reaping is
				// enabled and there are terminal Boatstack workspaces to reclaim,
				// surface workspace-reap so one prompt clears the accumulated backlog
				// rather than only the just-merged feature.
				if reapEnabled(repo) {
					if count := CountReclaimableWorkspaces(repo); count > 0 {
						base.NextOperation = "workspace-reap"
						base.Reason = fmt.Sprintf("Feature %q is merged; %d merged or abandoned Boatstack workspace(s) are reclaimable.", completed[0].Feature, count)
					}
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
	base.Reason = "No Boatstack feature has started; run auto-plan with the plan produced in the host conversation (--plan <path>)."
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

// Banner glyphs (Kit C "Flightpath"). Deliberately no glyph for Boatstack itself.
const (
	bannerGlyphDone     = "✓" // a journey node that is finished
	bannerGlyphNow      = "▸" // the node in progress right now
	bannerGlyphTodo     = "·" // a node not yet reached
	bannerGlyphBlocked  = "▲" // the current node needs a human
	bannerGlyphComplete = "✱" // the whole feature is done
	bannerWordmark      = "Boatstack"
	bannerRuleWidth     = 44
)

// RenderNextStatusBanner produces the branded, human-facing header shown at the
// top of every Boatstack message. It is pure presentation of the read-only
// NextStatus: a wordmark rule, a plain-language subtitle, and an unlabeled
// four-node progress rail with one friendly active phrase.
//
// Control law "banner-hides-internal-machinery": the banner MUST NEVER surface
// internal stage names or machine codes (BUILD, TEST_PASSED, REVIEW_PASSED,
// POLICY_READY, PUBLISHED, DRAFT_PLAN, APPROVED, NOT_INITIALIZED, INVALID_STATE,
// AMBIGUOUS, discard-delivery, repair-state, ship-gate, …) and MUST NOT carry a
// logo/badge for Boatstack. Every ObservedStage/NextOperation/VerificationStatus/
// Lifecycle value maps to friendly words or degrades to a safe generic phrase.
// The renderer emits plain Unicode (no ANSI): the banner lands inside a Markdown
// response, so colour is a host concern applied later.
func RenderNextStatusBanner(status NextStatus) string {
	var b strings.Builder
	b.WriteString(bannerRule(bannerWordmark, bannerRuleWidth) + "\n")

	if subtitle := bannerSubtitle(status); subtitle != "" {
		b.WriteString(" " + subtitle + "\n")
	}

	phrase := friendlyPhrase(status)
	if status.VerificationStatus == "UNVERIFIED" {
		// No feature is being tracked yet: show the wordmark and a plain phrase,
		// no rail (an all-todo rail would imply work is queued when it is not).
		b.WriteString(" " + phrase + "\n")
	} else {
		rail := strings.Join(journeyNodes(status), "──")
		b.WriteString(" " + rail + "   " + phrase + "\n")
	}

	b.WriteString(strings.Repeat("━", bannerRuleWidth) + "\n")
	return b.String()
}

// bannerRule renders the top rule "━━ <title> ━━━…" padded to width runes.
func bannerRule(title string, width int) string {
	prefix := "━━ " + title + " "
	fill := width - utf8.RuneCountInString(prefix)
	if fill < 1 {
		fill = 1
	}
	return prefix + strings.Repeat("━", fill)
}

// bannerSubtitle is the feature line, using the non-coder word "part" for slices.
func bannerSubtitle(status NextStatus) string {
	if status.Feature == "" {
		return ""
	}
	if status.TotalSlices > 1 {
		return fmt.Sprintf("%s · part %d of %d", status.Feature, status.SliceIndex, status.TotalSlices)
	}
	return status.Feature
}

// journeyNodes returns the four rail glyphs. The four nodes are a deliberate
// user-facing abstraction of the internal machine; they are never labelled.
func journeyNodes(status NextStatus) []string {
	if status.ObservedStage == "FEATURE_COMPLETE" || status.Lifecycle == "PUBLISHED_MERGED" {
		return []string{bannerGlyphDone, bannerGlyphDone, bannerGlyphDone, bannerGlyphComplete}
	}
	if status.ObservedStage == "PUBLISHED" {
		return []string{bannerGlyphDone, bannerGlyphDone, bannerGlyphDone, bannerGlyphDone}
	}

	pos := stagePosition(status.ObservedStage)
	blocked := bannerBlocked(status)
	nodes := make([]string, 4)
	for i := range nodes {
		switch {
		case i < pos:
			nodes[i] = bannerGlyphDone
		case i == pos:
			if blocked {
				nodes[i] = bannerGlyphBlocked
			} else {
				nodes[i] = bannerGlyphNow
			}
		default:
			nodes[i] = bannerGlyphTodo
		}
	}
	return nodes
}

// stagePosition collapses the internal stages into a 0..3 position on the rail.
func stagePosition(stage string) int {
	switch stage {
	case "NOT_STARTED", "NOT_INITIALIZED", "DRAFT_PLAN", "AMBIGUOUS":
		return 0
	case "POLICY_READY", "APPROVED", "BUILD", "INVALID_STATE":
		return 1
	case "TEST_PASSED":
		return 2
	case "PR_PREVIEW", "REVIEW_PASSED":
		return 3
	default:
		return 0
	}
}

func bannerBlocked(status NextStatus) bool {
	return status.VerificationStatus == "BLOCKED" ||
		status.ObservedStage == "AMBIGUOUS" ||
		status.ObservedStage == "INVALID_STATE"
}

// friendlyPhrase maps the internal status to one plain-language sentence. It must
// never echo a raw stage name or machine code.
func friendlyPhrase(status NextStatus) string {
	if status.VerificationStatus == "UNVERIFIED" {
		return "not tracking a feature here yet"
	}
	if bannerBlocked(status) {
		return "needs you: " + friendlyBlockReason(status)
	}
	switch status.ObservedStage {
	case "NOT_STARTED", "NOT_INITIALIZED", "DRAFT_PLAN":
		return "getting your plan ready"
	case "POLICY_READY", "APPROVED":
		return "ready to build"
	case "BUILD":
		return "building your changes"
	case "TEST_PASSED":
		return "checking your changes"
	case "PR_PREVIEW", "REVIEW_PASSED":
		return "ready to ship"
	case "PUBLISHED":
		if status.Lifecycle == "PUBLISHED_MERGED" {
			return "complete"
		}
		return "shipped — in review"
	case "FEATURE_COMPLETE":
		return "complete"
	default:
		return "working on your changes"
	}
}

// friendlyBlockReason translates NextOperation into a plain "what you need to do"
// sentence. The raw operation name is never printed.
func friendlyBlockReason(status NextStatus) string {
	switch status.NextOperation {
	case "resolve-ambiguity":
		return "pick which feature to continue"
	case "discard-delivery":
		return "an old draft needs clearing before we continue"
	case "repair-state":
		return "the workspace needs a quick reset"
	case "init":
		return "Boatstack isn't set up here yet"
	case "plan-gate":
		return "your plan needs approval"
	case "review-gate":
		return "a review check needs attention"
	case "ship-gate":
		return "a ship check needs attention"
	default:
		return "a check needs your attention"
	}
}
