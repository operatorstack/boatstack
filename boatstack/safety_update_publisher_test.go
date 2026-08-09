package boatstack

import (
	"path/filepath"
	"strings"
	"testing"
)

// updatePreviewArg is the update preview path the version-update publisher must be
// given. It lives under <git-dir>/boatstack/updates/, so every publish-update-pr
// command names a path inside the .git/boatstack/ subtree that the tamper guard
// watches. The exemption exists precisely so that path, passed as a read argument to
// the trusted helper, is not mistaken for a direct edit of runtime authority.
func updatePreviewArg(repo string) string {
	return filepath.Join(repo, ".git", "boatstack", "updates", "v0.7.68", "pr-preview.json")
}

// The sanctioned version-update publisher must run in-session. Before the exemption,
// deliveryStatePathPattern matched the .git/boatstack/ preview argument and the command
// was denied as workflow-state-tamper — the exact reason self-updates could only be
// published from outside the guarded session.
func TestUpdatePublisherIsExemptFromStateTamper(t *testing.T) {
	repo := safetyTestRepo(t)
	preview := updatePreviewArg(repo)
	fingerprint := strings.Repeat("a", 64)
	commands := map[string]string{
		"POSIX launcher":      ".product-loop/boatstack publish-update-pr --repo . --preview " + preview + " --preview-fingerprint " + fingerprint,
		"PowerShell launcher": "& '.product-loop\\boatstack.ps1' publish-update-pr --repo . --preview " + preview + " --preview-fingerprint " + fingerprint,
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			if findings := ClassifyCommand(repo, command); len(findings) != 0 {
				t.Fatalf("the update publisher was denied: %#v", findings)
			}
		})
	}
}

func TestUpdatePublisherRejectsForeignRuntimeEntrypoints(t *testing.T) {
	repo := safetyTestRepo(t)
	preview := updatePreviewArg(repo)
	fingerprint := strings.Repeat("a", 64)
	for name, command := range map[string]string{
		"internal helper": ".product-loop/bin/boatstack-helper publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint,
		"foreign helper":  "/tmp/boatstack-helper publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint,
		"launcher alias":  "/tmp/boatstack publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint,
	} {
		t.Run(name, func(t *testing.T) {
			findings := ClassifyCommand(repo, command)
			if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
				t.Fatalf("foreign runtime entrypoint was admitted: %#v", findings)
			}
		})
	}
}

// The exemption must hold in the state that actually blocked real updates: a repo with
// an active managed delivery. The tamper branch runs before the delivery-aware checks,
// so the publish must pass through regardless of delivery state.
func TestUpdatePublisherIsExemptWithActiveDelivery(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "phased-feature", PlanLockHash: strings.Repeat("a", 64),
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "phase-one", Title: "First", Status: "BUILD"}},
	}); err != nil {
		t.Fatal(err)
	}
	command := ".product-loop/boatstack publish-update-pr --repo . --preview " + updatePreviewArg(repo) + " --preview-fingerprint " + strings.Repeat("a", 64)
	if findings := ClassifyCommand(repo, command); len(findings) != 0 {
		t.Fatalf("the update publisher was denied while a delivery was active: %#v", findings)
	}
}

// The exemption is narrow. A direct write to any .git/boatstack/ path is still denied.
// This proves the exemption did not weaken tamper protection.
func TestDirectWritesUnderBoatstackStillDenied(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "phased-feature", PlanLockHash: strings.Repeat("a", 64),
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "phase-one", Title: "First", Status: "BUILD"}},
	}); err != nil {
		t.Fatal(err)
	}
	statePath, err := deliveryStatePath(repo, "phased-feature")
	if err != nil {
		t.Fatal(err)
	}
	denied := map[string]string{
		"remove update preview": "rm " + updatePreviewArg(repo),
		"remove delivery state": "rm " + statePath,
		"overwrite state":       "printf broken > " + statePath,
	}
	for name, command := range denied {
		t.Run(name, func(t *testing.T) {
			findings := ClassifyCommand(repo, command)
			if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
				t.Fatalf("a direct write under .git/boatstack/ was not denied: %#v", findings)
			}
		})
	}
}

// The exemption is anchored end to end, so no second command can be smuggled after the
// publisher. A chained command still names the preview path and is still denied.
func TestUpdatePublisherExemptionRejectsChaining(t *testing.T) {
	repo := safetyTestRepo(t)
	preview := updatePreviewArg(repo)
	fingerprint := strings.Repeat("a", 64)
	for name, command := range map[string]string{
		"semicolon": ".product-loop/boatstack publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint + "; rm -rf important",
		"and":       ".product-loop/boatstack publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint + " && rm -rf important",
		"pipe":      ".product-loop/boatstack publish-update-pr --preview " + preview + " --preview-fingerprint " + fingerprint + " | tee steal",
	} {
		t.Run(name, func(t *testing.T) {
			findings := ClassifyCommand(repo, command)
			if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
				t.Fatalf("a chained command escaped the tamper guard: %#v", findings)
			}
		})
	}
}

// The pre-existing approved-publisher exemption for publish-pr had no regression test.
// Close that gap: while a delivery is active, the sanctioned publish-pr helper stays
// allowed even though direct pushes and PR creation are denied.
func TestApprovedPublishPRStaysAllowedDuringActiveDelivery(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "phased-feature", PlanLockHash: strings.Repeat("a", 64),
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "phase-one", Title: "First", Status: "REVIEW"}},
	}); err != nil {
		t.Fatal(err)
	}
	if findings := ClassifyCommand(repo, "gh pr create --title phase-one"); len(findings) == 0 || findings[0].Category != "workflow-publication-bypass" {
		t.Fatalf("sanity: a direct PR creation should be denied while a delivery is active: %#v", findings)
	}
	allowed := ".product-loop/boatstack publish-pr --preview .product-loop/features/phased-feature/pr.md --preview-fingerprint " + strings.Repeat("a", 64) + " --action create"
	if findings := ClassifyCommand(repo, allowed); len(findings) != 0 {
		t.Fatalf("the sanctioned publish-pr helper was denied: %#v", findings)
	}
}
