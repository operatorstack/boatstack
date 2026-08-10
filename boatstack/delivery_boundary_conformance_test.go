package boatstack

import (
	"os"
	"path/filepath"
	"testing"
)

// This file holds boundary-conformance tests for two control laws (see
// AGENTS.md "Boundary Conformance Requirement"):
//
//	control-law: stale-delivery-cannot-block-unrelated-feature
//	  A stale/invalid delivery in the shared store must never escalate into a
//	  repo-wide block on resolution of an unrelated delivery. This holds at EVERY
//	  read-only resolution boundary — ResolveNext (new work) and ResolveRecovery
//	  (recovering an existing delivery) alike: each partitions the store instead
//	  of failing closed and applies the ignored-deliveries filter BEFORE
//	  invalidity becomes fatal, blocking only on a still-unignored invalid
//	  delivery. The mutation boundary (ActiveManagedDeliveries) stays fail-closed.
//
//	control-law: discard-preserves-published-authority
//	  A delivery bearing published authority (any slice with a recorded PRState)
//	  is not discardable without an explicit force override; a discard archives
//	  (never hard-deletes) the state and never touches git-tracked artifacts.

// writeInvalidDelivery plants a structurally-malformed state.json in the store so
// LoadDeliveryState fails for that slug — modelling a corrupt or partially
// written delivery left behind by an interrupted run.
func writeInvalidDelivery(t *testing.T, repo, feature string) string {
	t.Helper()
	path, err := deliveryStatePath(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ this is not valid delivery state"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writePublishedDelivery plants a valid delivery whose single slice carries a
// recorded PRState — i.e. it bears published authority.
func writePublishedDelivery(t *testing.T, repo, feature, prState string) {
	t.Helper()
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: "hash",
		ActiveIndex: 1,
		Slices:      []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: "PUBLISHED", PRState: prState}},
	}); err != nil {
		t.Fatal(err)
	}
}

// ---- control-law: stale-delivery-cannot-block-unrelated-feature ----

// Negative + relation conformance: an unignored invalid delivery blocks, but the
// block names exactly the offending delivery and routes to the discard-delivery
// remedy (not the opaque repair-state) — request -> boundary -> decision.
func TestResolveNextInvalidDeliveryBlocksWithDiscardRemedy(t *testing.T) {
	repo := nextTestRepo(t)
	writeInvalidDelivery(t, repo, "stale-one")
	writeSavedFeaturePlan(t, repo, "new-feature")

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.ObservedStage != "INVALID_STATE" {
		t.Fatalf("invalid delivery did not block: %+v", status)
	}
	if status.NextOperation != "discard-delivery" {
		t.Fatalf("block did not route to discard-delivery remedy: %+v", status)
	}
	found := false
	for _, slug := range status.BlockingAmbiguity {
		if slug == "stale-one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("block did not name the offending delivery: %+v", status.BlockingAmbiguity)
	}
}

// Positive + bypass conformance: an IGNORED invalid delivery no longer blocks;
// the unrelated new feature resolves. This is the core fix — the ignore filter
// runs before invalidity can become fatal, so one stale delivery cannot poison
// resolution of distinct work.
func TestResolveNextIgnoredInvalidDeliveryDoesNotBlockNewFeature(t *testing.T) {
	repo := nextTestRepo(t)
	writeInvalidDelivery(t, repo, "stale-one")
	writeSavedFeaturePlan(t, repo, "new-feature")

	if _, err := IgnoreDelivery(repo, "stale-one"); err != nil {
		t.Fatal(err)
	}

	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.Feature != "new-feature" ||
		status.ObservedStage != "DRAFT_PLAN" || status.NextOperation != "plan-gate" {
		t.Fatalf("ignored invalid delivery still blocked the new feature: %+v", status)
	}
}

// Bypass conformance (enforce at the correct boundary): the tolerance is scoped
// to the read-only ResolveNext boundary. The strict mutation-path enumeration
// still fails closed on the same invalid delivery even when it is ignored, so
// corrupt state cannot be laundered into a mutation via the ignore list.
func TestActiveManagedDeliveriesStaysFailClosedOnInvalid(t *testing.T) {
	repo := nextTestRepo(t)
	writeInvalidDelivery(t, repo, "stale-one")
	if _, err := IgnoreDelivery(repo, "stale-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := ActiveManagedDeliveries(repo); err == nil {
		t.Fatal("strict ActiveManagedDeliveries did not fail closed on invalid delivery state")
	}
}

// Negative conformance for
// control-law: ambient-plans-never-activate-workflow-control. Invalid delivery
// observations remain actionable through ResolveNext, but they do not acquire
// ambient authority over unrelated product tools.
func TestInvalidUnselectedDeliveryDoesNotBlockOrdinaryMutation(t *testing.T) {
	repo := nextTestRepo(t)
	writeInvalidDelivery(t, repo, "stale-one")
	if _, err := IgnoreDelivery(repo, "stale-one"); err != nil {
		t.Fatal(err)
	}
	if finding, blocked := preActivationFinding(repo, "product.go"); blocked {
		t.Fatalf("invalid unselected delivery controlled an ordinary product path: %+v", finding)
	}
	// The bounded recovery remains available when Boatstack is explicitly used.
	if _, err := DiscardDelivery(repo, "stale-one", true); err != nil {
		t.Fatalf("discard-delivery refused the invalid delivery it was prescribed for: %v", err)
	}
	if _, stillBlocked := preActivationFinding(repo, "product.go"); stillBlocked {
		t.Fatal("mutation still blocked after discarding the invalid delivery")
	}
}

// Failure-state conformance: ResolveNext is read-only. A blocking decision must
// leave the offending state file byte-for-byte unchanged (no partial repair).
func TestResolveNextLeavesInvalidStateUntouched(t *testing.T) {
	repo := nextTestRepo(t)
	statePath := writeInvalidDelivery(t, repo, "stale-one")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveNext(repo, ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("read-only resolution mutated the invalid state file\nbefore=%s\nafter=%s", before, after)
	}
}

// The same law holds at the OTHER read-only resolution boundary: ResolveRecovery.
// ResolveNext resolves new work; ResolveRecovery resolves an existing delivery
// that hit a problem. Both scan the shared store, so both must tolerate an
// unrelated stale delivery. These tests are the recovery-boundary twins of the
// ResolveNext cases above — the defect that motivated generalizing the law was
// that recovery had none of them and fell through to a repo-wide block.

// Positive + bypass conformance: an IGNORED invalid delivery no longer poisons
// recovery of an unrelated healthy delivery on the current branch. The ignore
// filter runs before invalidity can become fatal, so recovery selects and routes
// the real target instead of blocking on abandoned state.
func TestResolveRecoveryIgnoredInvalidDeliveryDoesNotBlockHealthyBranch(t *testing.T) {
	repo := nextTestRepo(t)
	branch, _ := gitCommand(repo, "branch", "--show-current")

	writeNextDelivery(t, repo, "healthy-feature", "BUILD", 0)
	updateRecoveryDelivery(t, repo, "healthy-feature", branch, "", "")

	writeInvalidDelivery(t, repo, "stale-one")
	if _, err := IgnoreDelivery(repo, "stale-one"); err != nil {
		t.Fatal(err)
	}

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "the test failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "VERIFIED" || status.Feature != "healthy-feature" ||
		status.Lifecycle != "ACTIVE" || status.NextOperation != "repair_active" {
		t.Fatalf("ignored invalid delivery poisoned recovery of an unrelated healthy branch: %#v", status)
	}
}

// Negative + relation conformance: a still-unignored invalid delivery does block
// recovery, but the block names exactly the offending delivery and routes to the
// discard-delivery remedy — request -> boundary -> decision.
func TestResolveRecoveryUnignoredInvalidDeliveryBlocksWithDiscardRemedy(t *testing.T) {
	repo := nextTestRepo(t)
	branch, _ := gitCommand(repo, "branch", "--show-current")
	writeNextDelivery(t, repo, "healthy-feature", "BUILD", 0)
	updateRecoveryDelivery(t, repo, "healthy-feature", branch, "", "")
	writeInvalidDelivery(t, repo, "stale-one")

	status, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "the test failed", SourceStage: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.NextOperation != "discard-delivery" {
		t.Fatalf("unignored invalid delivery did not block with discard remedy: %#v", status)
	}
	found := false
	for _, slug := range status.Blockers {
		if slug == "stale-one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("block did not name the offending delivery: %#v", status.Blockers)
	}
}

// Failure-state conformance: ResolveRecovery is read-only. A blocking decision on
// an invalid delivery must leave the offending state file byte-for-byte unchanged.
func TestResolveRecoveryLeavesInvalidStateUntouched(t *testing.T) {
	repo := nextTestRepo(t)
	statePath := writeInvalidDelivery(t, repo, "stale-one")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRecovery(RecoveryStatusOptions{Repo: repo, Message: "boom", SourceStage: "ci"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("read-only recovery mutated the invalid state file\nbefore=%s\nafter=%s", before, after)
	}
}

// ---- control-law: discard-preserves-published-authority ----

// Positive + relation conformance: an unpublished delivery is discardable; the
// live state directory is gone, an archive exists, the store no longer surfaces
// it, and an unrelated new feature then resolves cleanly.
// request -> boundary -> effect -> resulting state.
func TestDiscardDeliveryUnpublishedIsRemovedAndUnblocks(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "stale-active", "BUILD", 0)
	statePath, err := deliveryStatePath(repo, "stale-active")
	if err != nil {
		t.Fatal(err)
	}
	featureDir := filepath.Dir(statePath)

	result, err := DiscardDelivery(repo, "stale-active", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "discarded" {
		t.Fatalf("unpublished delivery was not discarded: %+v", result)
	}
	if _, statErr := os.Stat(featureDir); !os.IsNotExist(statErr) {
		t.Fatalf("live delivery directory still present after discard: %v", statErr)
	}
	// Effect: the archive exists and the store no longer surfaces the delivery.
	archive := filepath.Join(filepath.Dir(featureDir), ".discarded", "stale-active")
	if _, statErr := os.Stat(archive); statErr != nil {
		t.Fatalf("discard did not archive the state: %v", statErr)
	}
	active, err := ActiveManagedDeliveries(repo)
	if err != nil || len(active) != 0 {
		t.Fatalf("discarded delivery still active: %#v %v", active, err)
	}

	// Resulting state: a fresh feature now resolves without interference.
	writeSavedFeaturePlan(t, repo, "new-feature")
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Feature != "new-feature" || status.NextOperation != "plan-gate" {
		t.Fatalf("new feature did not resolve after discard: %+v", status)
	}
}

// Bypass conformance: the archive lives under a dotted sibling the slug pattern
// skips, so a discarded delivery can never re-enter through the live scan.
func TestDiscardedDeliveryIsNotRescanned(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "stale-active", "BUILD", 0)
	if _, err := DiscardDelivery(repo, "stale-active", false); err != nil {
		t.Fatal(err)
	}
	active, invalid, err := scanManagedDeliveries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 || len(invalid) != 0 {
		t.Fatalf("archived delivery leaked back into the scan: active=%#v invalid=%#v", active, invalid)
	}
}

// Negative + failure-state conformance: a published delivery is refused without
// force, and the refusal leaves the live state directory unchanged (the effect
// is not partially applied).
func TestDiscardDeliveryPublishedRefusedWithoutForce(t *testing.T) {
	repo := nextTestRepo(t)
	writePublishedDelivery(t, repo, "shipped-feature", "OPEN")
	statePath, err := deliveryStatePath(repo, "shipped-feature")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := DiscardDelivery(repo, "shipped-feature", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "refused" {
		t.Fatalf("published delivery was not refused: %+v", result)
	}
	if len(result.Published) == 0 {
		t.Fatalf("refusal did not report the published slices: %+v", result)
	}
	// Failure-state: state untouched.
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("refused discard removed the live state: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("refused discard mutated the live state\nbefore=%s\nafter=%s", before, after)
	}
}

// Positive (override) conformance: the authorized actor may discard a published
// delivery with an explicit force override.
func TestDiscardDeliveryPublishedForced(t *testing.T) {
	repo := nextTestRepo(t)
	writePublishedDelivery(t, repo, "shipped-feature", "OPEN")

	result, err := DiscardDelivery(repo, "shipped-feature", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "discarded" {
		t.Fatalf("forced discard of published delivery did not succeed: %+v", result)
	}
}

// Negative conformance: a malformed request (invalid slug) is rejected, and a
// request for a delivery that does not exist is a no-op — never a spurious effect.
func TestDiscardDeliveryRejectsBadRequests(t *testing.T) {
	repo := nextTestRepo(t)
	if _, err := DiscardDelivery(repo, "Not A Slug", false); err == nil {
		t.Fatal("discard accepted an invalid feature slug")
	}
	result, err := DiscardDelivery(repo, "never-existed", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "none" {
		t.Fatalf("discard of a nonexistent delivery was not a no-op: %+v", result)
	}
}

// Idempotency + reversal conformance: discarding twice does not duplicate the
// effect (second call is a no-op), and re-planting then discarding the same slug
// archives to a deterministic, collision-free location — the discard is
// reversible and replayable.
func TestDiscardDeliveryIdempotentAndCollisionFree(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "stale-active", "BUILD", 0)

	first, err := DiscardDelivery(repo, "stale-active", false)
	if err != nil || first.Action != "discarded" {
		t.Fatalf("first discard failed: %+v %v", first, err)
	}
	second, err := DiscardDelivery(repo, "stale-active", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != "none" {
		t.Fatalf("repeating discard duplicated the effect: %+v", second)
	}

	// Re-plant the same slug and discard again — the archive must not collide.
	writeNextDelivery(t, repo, "stale-active", "BUILD", 0)
	third, err := DiscardDelivery(repo, "stale-active", false)
	if err != nil || third.Action != "discarded" {
		t.Fatalf("third discard failed: %+v %v", third, err)
	}
	if first.ArchivePath == third.ArchivePath {
		t.Fatalf("second archive collided with the first: %s", third.ArchivePath)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "boatstack", "deliveries", ".discarded", "stale-active-2")); statErr != nil {
		t.Fatalf("collision-free archive not created deterministically: %v", statErr)
	}
}
