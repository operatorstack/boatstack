package boatstack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func withRecoveryGh(t *testing.T, fn func(string, ...string) (string, error)) {
	t.Helper()
	previous := recoveryGh
	recoveryGh = fn
	t.Cleanup(func() { recoveryGh = previous })
}

func recoveryPR(state, branch, sha string) func(string, ...string) (string, error) {
	return func(_ string, _ ...string) (string, error) {
		return fmt.Sprintf(`{"state":%q,"headRefName":%q,"headRefOid":%q,"url":"https://example.invalid/pr/1"}`, state, branch, sha), nil
	}
}

func updateRecoveryDelivery(t *testing.T, repo, feature, branch, prURL, parent string) {
	t.Helper()
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	index := state.ActiveIndex
	if index >= len(state.Slices) {
		index = len(state.Slices) - 1
	}
	state.Slices[index].HeadBranch = branch
	state.Slices[index].PRURL = prURL
	state.ParentDelivery = parent
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRecoveryRoutesActiveCurrentBranch(t *testing.T) {
	repo := nextTestRepo(t)
	branch, _ := gitCommand(repo, "branch", "--show-current")
	writeNextDelivery(t, repo, "active", "BUILD", 0)
	updateRecoveryDelivery(t, repo, "active", branch, "", "")

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "the test failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.Feature != "active" || status.Lifecycle != "ACTIVE" || status.NextOperation != "repair_active" {
		t.Fatalf("unexpected active recovery: %#v", status)
	}
}

func TestResolveRecoveryDraftsPublishedCorrectionByPRState(t *testing.T) {
	for _, test := range []struct {
		state     string
		lifecycle string
	}{
		{state: "OPEN", lifecycle: "PUBLISHED_OPEN"},
		{state: "MERGED", lifecycle: "PUBLISHED_MERGED"},
		{state: "CLOSED", lifecycle: "PUBLISHED_CLOSED"},
	} {
		t.Run(test.state, func(t *testing.T) {
			repo := nextTestRepo(t)
			branch, _ := gitCommand(repo, "branch", "--show-current")
			writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
			updateRecoveryDelivery(t, repo, "published", branch, "https://example.invalid/pr/1", "")
			withRecoveryGh(t, recoveryPR(test.state, branch, "head123"))

			status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "review found a regression", SourceStage: "review", ObservedHeadSHA: "head123"})
			if err != nil {
				t.Fatal(err)
			}
			if status.VerificationStatus != "VERIFIED" || status.Lifecycle != test.lifecycle || status.NextOperation != "draft_corrective_child" || status.SuggestedFeatureID != "published-correction-01" {
				t.Fatalf("unexpected published recovery: %#v", status)
			}
		})
	}
}

func TestResolveRecoveryAllowsDraftWhenGitHubUnavailable(t *testing.T) {
	repo := nextTestRepo(t)
	branch, _ := gitCommand(repo, "branch", "--show-current")
	writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "published", branch, "https://example.invalid/pr/1", "")
	withRecoveryGh(t, func(string, ...string) (string, error) { return "", errors.New("not authenticated") })

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "CI failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle != "PUBLISHED_UNKNOWN" || status.NextOperation != "draft_corrective_child" {
		t.Fatalf("unknown PR state blocked drafting: %#v", status)
	}
}

func TestResolveRecoveryRejectsStaleHeadEvidence(t *testing.T) {
	repo := nextTestRepo(t)
	branch, _ := gitCommand(repo, "branch", "--show-current")
	writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "published", branch, "https://example.invalid/pr/1", "")
	withRecoveryGh(t, recoveryPR("OPEN", branch, "current"))

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "CI failed", SourceStage: "ci", ObservedHeadSHA: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.NextOperation != "none" {
		t.Fatalf("stale evidence was accepted: %#v", status)
	}
}

func TestResolveRecoveryBlocksAmbiguousPublishedHistory(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "first", "PUBLISHED", 1)
	writeNextDelivery(t, repo, "second", "PUBLISHED", 1)
	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "the PR failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.NextOperation != "resolve_ambiguity" || len(status.Blockers) != 2 {
		t.Fatalf("ambiguous history was selected: %#v", status)
	}
}

func TestResolveRecoveryIncrementsOnlyLinkedCorrectionIDs(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "parent", "PUBLISHED", 1)
	writeNextDelivery(t, repo, "parent-correction-01", "BUILD", 0)
	updateRecoveryDelivery(t, repo, "parent-correction-01", "feat/other", "", "parent")
	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Feature: "parent", Message: "another failure", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SuggestedFeatureID != "parent-correction-02" {
		t.Fatalf("unexpected correction id: %#v", status)
	}
}

func TestResolveRecoveryBlocksObservedPRBranchMismatch(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "published", "feat/expected", "https://example.invalid/pr/1", "")
	withRecoveryGh(t, recoveryPR("OPEN", "feat/different", "head123"))
	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Feature: "published", Message: "CI failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || len(status.Blockers) != 2 {
		t.Fatalf("PR branch mismatch was accepted: %#v", status)
	}
}

func TestResolveRecoveryFingerprintsExistingCommittedAndLocalCorrection(t *testing.T) {
	repo := nextTestRepo(t)
	runGit(t, repo, "config", "user.name", "Recovery Test")
	runGit(t, repo, "config", "user.email", "recovery@example.invalid")
	runGit(t, repo, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("published\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	writeNextDelivery(t, repo, "published", "PUBLISHED", 1)
	updateRecoveryDelivery(t, repo, "published", "main", "https://example.invalid/pr/1", "")
	runGit(t, repo, "add", ".product-loop")
	runGit(t, repo, "commit", "-m", "published evidence")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("corrected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new-test.txt"), []byte("regression\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withRecoveryGh(t, recoveryPR("OPEN", "main", "head123"))

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Feature: "published", Message: "CI failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.ExistingDiffSHA256 == "" || len(status.ExistingChangedPaths) != 2 || status.ExistingChangedPaths[0] != "app.txt" || status.ExistingChangedPaths[1] != "new-test.txt" {
		t.Fatalf("existing correction was not fingerprinted: %#v", status)
	}
}
