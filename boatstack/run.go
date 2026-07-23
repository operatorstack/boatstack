package boatstack

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const runPreflightSchemaVersion = 1

var runGitCommand = gitCommand

// RunPreflight is the deterministic Git freshness boundary used before the
// host-driven run operation is allowed to mutate workflow or product state.
type RunPreflight struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"`
	BaseBranch         string `json:"base_branch,omitempty"`
	HeadBranch         string `json:"head_branch,omitempty"`
	Upstream           string `json:"upstream,omitempty"`
	Relation           string `json:"relation,omitempty"`
	Reason             string `json:"reason"`
}

func blockedRunPreflight(base, head, upstream, relation, reason string) RunPreflight {
	return RunPreflight{
		SchemaVersion: runPreflightSchemaVersion, VerificationStatus: "BLOCKED",
		BaseBranch: base, HeadBranch: head, Upstream: upstream, Relation: relation, Reason: reason,
	}
}

func runBranches(repo, explicitFeature string) (string, string, error) {
	base := defaultPRBase(repo)
	head, err := runGitCommand(repo, "branch", "--show-current")
	if err != nil || strings.TrimSpace(head) == "" {
		return "", "", fmt.Errorf("boatstack run requires a named current branch")
	}

	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return "", "", err
	}
	// Scope the ambiguity check to un-ignored deliveries so the foreground
	// coordinator matches ResolveNext. A config that fails to load leaves active
	// unfiltered, preserving the prior >1-active behavior.
	if config, _, configErr := LoadConfig(filepath.Join(repo, ".product-loop", "project.json")); configErr == nil {
		active = withoutIgnoredDeliveries(active, config.Workflow.IgnoredDeliveries)
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
		} else {
			return base, head, fmt.Errorf("feature %s is not currently an active managed delivery", explicitFeature)
		}
	}

	if len(active) > 1 {
		return base, head, fmt.Errorf("more than one managed delivery is active; Boatstack run will not choose by recency")
	}
	if len(active) == 1 {
		state, stateErr := CurrentDeliveryState(repo, active[0])
		if stateErr != nil {
			return "", "", stateErr
		}
		slice, sliceErr := activeDeliverySlice(state)
		if sliceErr != nil {
			return "", "", sliceErr
		}
		if slice.BaseBranch != "" {
			base = slice.BaseBranch
		}
		if slice.HeadBranch != "" && slice.HeadBranch != head {
			return base, head, fmt.Errorf("active delivery slice %s requires head branch %s; current branch is %s", slice.ID, slice.HeadBranch, head)
		}
	}
	if head == base {
		return base, head, fmt.Errorf("Boatstack run requires a feature branch; current branch %s is the configured base branch", head)
	}
	return base, head, nil
}

// CheckRunPreflight fetches origin and proves that the current branch contains
// the fetched base and is not behind or diverged from its configured upstream.
// It never merges, rebases, switches branches, discards changes, or pushes.
func CheckRunPreflight(repoPath, explicitFeature string) RunPreflight {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return blockedRunPreflight("", "", "", "INVALID_REPOSITORY", err.Error())
	}
	if !fileExists(filepath.Join(repo, ".product-loop", "project.json")) {
		return blockedRunPreflight("", "", "", "NOT_INITIALIZED", "This repository has no Boatstack project installation to run.")
	}
	if _, err := runGitCommand(repo, "remote", "get-url", "origin"); err != nil {
		return blockedRunPreflight("", "", "", "MISSING_ORIGIN", "Boatstack run requires a usable origin remote.")
	}
	if _, err := runGitCommand(repo, "fetch", "origin"); err != nil {
		return blockedRunPreflight("", "", "", "FETCH_FAILED", "Boatstack could not fetch origin: "+err.Error())
	}

	base, head, err := runBranches(repo, explicitFeature)
	if err != nil {
		return blockedRunPreflight(base, head, "", "BRANCH_MISMATCH", err.Error())
	}
	remoteBase := "refs/remotes/origin/" + base
	if _, err := runGitCommand(repo, "rev-parse", "--verify", remoteBase+"^{commit}"); err != nil {
		return blockedRunPreflight(base, head, "", "MISSING_REMOTE_BASE", fmt.Sprintf("Fetched origin does not contain base branch %s.", base))
	}
	if _, err := runGitCommand(repo, "merge-base", "--is-ancestor", remoteBase, "HEAD"); err != nil {
		return blockedRunPreflight(base, head, "", "STALE_BASE", fmt.Sprintf("Current branch %s does not contain fetched origin/%s; synchronize it outside Boatstack run.", head, base))
	}

	upstream, upstreamErr := runGitCommand(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	relation := "UNPUBLISHED"
	if upstreamErr == nil && upstream != "" {
		counts, countErr := runGitCommand(repo, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
		if countErr != nil {
			return blockedRunPreflight(base, head, upstream, "UPSTREAM_UNKNOWN", "Boatstack could not compare the current branch with its upstream: "+countErr.Error())
		}
		fields := strings.Fields(counts)
		if len(fields) != 2 {
			return blockedRunPreflight(base, head, upstream, "UPSTREAM_UNKNOWN", "Boatstack received an invalid Git upstream comparison.")
		}
		ahead, aheadErr := strconv.Atoi(fields[0])
		behind, behindErr := strconv.Atoi(fields[1])
		if aheadErr != nil || behindErr != nil {
			return blockedRunPreflight(base, head, upstream, "UPSTREAM_UNKNOWN", "Boatstack received an invalid Git upstream comparison.")
		}
		switch {
		case ahead > 0 && behind > 0:
			return blockedRunPreflight(base, head, upstream, "DIVERGED", fmt.Sprintf("Current branch %s has diverged from %s; synchronize it outside Boatstack run.", head, upstream))
		case behind > 0:
			return blockedRunPreflight(base, head, upstream, "BEHIND", fmt.Sprintf("Current branch %s is behind %s; synchronize it outside Boatstack run.", head, upstream))
		case ahead > 0:
			relation = "AHEAD"
		default:
			relation = "CURRENT"
		}
	}

	return RunPreflight{
		SchemaVersion: runPreflightSchemaVersion, VerificationStatus: "VERIFIED",
		BaseBranch: base, HeadBranch: head, Upstream: upstream, Relation: relation,
		Reason: "Origin was fetched and the current branch is fresh enough to run Boatstack.",
	}
}
